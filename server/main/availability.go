package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

type availabilityRecord struct {
	ID       string
	Observed time.Time
}

type availabilitySnapshot struct {
	At      time.Time
	Records []availabilityRecord
}

var availabilityCache = struct {
	sync.Mutex
	data map[string]availabilitySnapshot
}{
	data: make(map[string]availabilitySnapshot),
}

func availabilityCollection(coverage string) string {
	if coverage == "any" {
		return "combined-metadata"
	}
	return "latest-continuous"
}

func parseUSGSTime(value string) time.Time {
	if value == "" {
		return time.Time{}
	}

	for _, layout := range []string{
		time.RFC3339Nano,
		"2006-01-02",
	} {
		t, err := time.Parse(layout, value)
		if err == nil {
			return t
		}
	}

	return time.Time{}
}

// GET /api/stations/availability
func MeasurementAvailability(
	w http.ResponseWriter,
	r *http.Request,
) {
	log.Println("Serving measurement availability API.")
	coverage := r.URL.Query().Get("coverage")
	recency := r.URL.Query().Get("recency")

	if coverage == "" {
		coverage = "latest"
	}
	if recency == "" {
		recency = "all"
	}

	if coverage != "latest" && coverage != "any" {
		writeJSONError(
			w,
			http.StatusBadRequest,
			fmt.Errorf("invalid coverage"),
		)
		return
	}

	if recency != "all" &&
		recency != "24h" &&
		recency != "7d" {
		writeJSONError(
			w,
			http.StatusBadRequest,
			fmt.Errorf("invalid recency"),
		)
		return
	}

	records, err := cachedAvailability(r, coverage)
	if err != nil {
		writeJSONError(
			w,
			http.StatusBadGateway,
			fmt.Errorf("USGS availability: %w", err),
		)
		return
	}

	cutoff := time.Time{}

	switch recency {
	case "24h":
		cutoff = time.Now().UTC().Add(-24 * time.Hour)
	case "7d":
		cutoff = time.Now().UTC().Add(-7 * 24 * time.Hour)
	}

	ids := make([]string, 0)
	seen := make(map[string]bool)

	for _, record := range records {
		if record.ID == "" {
			continue
		}

		if !cutoff.IsZero() {
			if record.Observed.IsZero() ||
				record.Observed.Before(cutoff) {
				continue
			}
		}

		if !seen[record.ID] {
			seen[record.ID] = true
			ids = append(ids, record.ID)
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"coverage":   coverage,
		"recency":    recency,
		"stationIds": ids,
		"count":      len(ids),
	})
}

func cachedAvailability(
	r *http.Request,
	coverage string,
) ([]availabilityRecord, error) {
	availabilityCache.Lock()
	defer availabilityCache.Unlock()

	cached, exists := availabilityCache.data[coverage]

	if exists && time.Since(cached.At) < time.Hour {
		return cached.Records, nil
	}

	records, err := loadAvailabilityRecords(
		r,
		availabilityCollection(coverage),
		coverage,
	)

	if err != nil {
		if exists {
			return cached.Records, nil
		}
		return nil, err
	}

	availabilityCache.data[coverage] = availabilitySnapshot{
		At:      time.Now(),
		Records: records,
	}

	return records, nil
}

func loadAvailabilityRecords(
	r *http.Request,
	collection string,
	coverage string,
) ([]availabilityRecord, error) {
	params := url.Values{}
	params.Set("bbox", nyBoundingBox.String())
	params.Set("f", "json")
	params.Set("limit", "10000")
	params.Set("skipGeometry", "true")

	next := UsgsBaseURL +
		"/collections/" + collection +
		"/items?" + params.Encode()

	records := make([]availabilityRecord, 0)

	for page := 0; next != ""; page++ {
		if page >= 500 {
			return nil, fmt.Errorf(
				"USGS pagination exceeded 500 pages",
			)
		}

		parsed, err := url.Parse(next)

		if err != nil ||
			parsed.Scheme != "https" ||
			parsed.Host != "api.waterdata.usgs.gov" {
			return nil, fmt.Errorf(
				"unexpected USGS pagination URL",
			)
		}

		req, err := http.NewRequestWithContext(
			r.Context(),
			http.MethodGet,
			next,
			nil,
		)
		if err != nil {
			return nil, err
		}

		req.Header.Set("X-Api-Key", cfg.APIKey)
		req.Header.Set(
			"Accept",
			"application/geo+json, application/json",
		)

		resp, err := httpClient.Do(req)
		if err != nil {
			return nil, err
		}

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(
				io.LimitReader(resp.Body, 1024),
			)
			resp.Body.Close()

			return nil, fmt.Errorf(
				"USGS %s: %s",
				resp.Status,
				string(body),
			)
		}

		var payload struct {
			Features []struct {
				Properties struct {
					MonitoringLocationID string          `json:"monitoring_location_id"`
					Time                 string          `json:"time"`
					End                  string          `json:"end"`
					Value                json.RawMessage `json:"value"`
				} `json:"properties"`
			} `json:"features"`

			Links []struct {
				Rel  string `json:"rel"`
				Href string `json:"href"`
			} `json:"links"`
		}

		err = json.NewDecoder(
			io.LimitReader(resp.Body, 32<<20),
		).Decode(&payload)

		resp.Body.Close()

		if err != nil {
			return nil, fmt.Errorf(
				"decode USGS %s: %w",
				collection,
				err,
			)
		}

		for _, feature := range payload.Features {
			p := feature.Properties

			if p.MonitoringLocationID == "" {
				continue
			}

			if coverage == "latest" {
				v := strings.TrimSpace(string(p.Value))

				if v == "" || v == "null" ||
					v == `""` {
					continue
				}
			}

			observationTime := p.Time

			if coverage == "any" {
				observationTime = p.End
			}

			records = append(
				records,
				availabilityRecord{
					ID:       p.MonitoringLocationID,
					Observed: parseUSGSTime(observationTime),
				},
			)
		}

		next = ""

		for _, link := range payload.Links {
			if link.Rel == "next" {
				next = link.Href
				break
			}
		}
	}

	return records, nil
}
