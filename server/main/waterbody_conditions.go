package main

import (
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Initial implementation: Hudson River only, based on the reviewed geographic
// preview. No database changes. Extend this map when additional groups have
// validated station matches. CSV paths are relative to the server directory.
var conditionMatchFiles = map[string]string{
	"hudson-river": "tools/station_match_preview.csv",
}

var conditionParameters = map[string]string{
	"00010": "Water temperature",
	"00300": "Dissolved oxygen",
	"63680": "Turbidity",
	"00076": "Turbidity",
}

type conditionStation struct {
	ID        string  `json:"id"`
	Name      string  `json:"name"`
	Longitude float64 `json:"longitude"`
	Latitude  float64 `json:"latitude"`
	SegmentID string  `json:"segmentId,omitempty"`
}

type conditionReading struct {
	StationID      string    `json:"stationId"`
	StationName    string    `json:"stationName"`
	ParameterCode  string    `json:"parameterCode"`
	Parameter      string    `json:"parameter"`
	Value          float64   `json:"value"`
	Unit           string    `json:"unit"`
	Time           time.Time `json:"time"`
	ApprovalStatus string    `json:"approvalStatus,omitempty"`
	Longitude      float64   `json:"longitude"`
	Latitude       float64   `json:"latitude"`
}

type conditionsResult struct {
	WaterbodyID  string             `json:"waterbodyId"`
	AsOf         time.Time          `json:"asOf"`
	WindowHours  int                `json:"windowHours"`
	StationCount int                `json:"stationCount"`
	ReadingCount int                `json:"readingCount"`
	Stations     []conditionStation `json:"stations"`
	Readings     []conditionReading `json:"readings"`
	Note         string             `json:"note"`
}

type conditionCacheEntry struct {
	At     time.Time
	Result conditionsResult
}

var conditionsCache = struct {
	sync.Mutex
	Entries map[string]conditionCacheEntry
}{Entries: make(map[string]conditionCacheEntry)}

func WaterbodyConditions(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(r.PathValue("id"))
	csvPath, supported := conditionMatchFiles[id]
	if !supported {
		writeJSONError(w, http.StatusNotFound, fmt.Errorf("conditions not yet configured for waterbody %q", id))
		return
	}

	conditionsCache.Lock()
	defer conditionsCache.Unlock()
	if entry, ok := conditionsCache.Entries[id]; ok && time.Since(entry.At) < 20*time.Minute {
		writeJSON(w, http.StatusOK, entry.Result)
		return
	}

	stations, err := loadConditionStations(csvPath)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, fmt.Errorf("station matching data: %w", err))
		return
	}
	result, err := collectWaterbodyConditions(r, id, stations)
	if err != nil {
		// Preserve a successful previous result only if still within the
		// 24-hour measurement window; otherwise return an explicit error.
		writeJSONError(w, http.StatusBadGateway, fmt.Errorf("USGS conditions: %w", err))
		return
	}
	conditionsCache.Entries[id] = conditionCacheEntry{At: time.Now(), Result: result}
	writeJSON(w, http.StatusOK, result)
}

func loadConditionStations(path string) (map[string]conditionStation, error) {
	f, err := os.Open(filepath.Clean(path))
	if err != nil {
		return nil, err
	}
	defer f.Close()
	reader := csv.NewReader(f)
	header, err := reader.Read()
	if err != nil {
		return nil, err
	}
	fields := make(map[string]int)
	for i, name := range header {
		fields[strings.TrimPrefix(strings.TrimSpace(name), "\ufeff")] = i
	}
	for _, name := range []string{"status", "station_id", "station_name", "longitude", "latitude", "dec_segment_id"} {
		if _, ok := fields[name]; !ok {
			return nil, fmt.Errorf("CSV missing column %q", name)
		}
	}
	stations := make(map[string]conditionStation)
	for {
		record, err := reader.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		get := func(key string) string { return strings.TrimSpace(record[fields[key]]) }
		if get("status") != "STRONG_CANDIDATE" {
			continue
		}
		longitude, err1 := strconv.ParseFloat(get("longitude"), 64)
		latitude, err2 := strconv.ParseFloat(get("latitude"), 64)
		if err1 != nil || err2 != nil {
			continue
		}
		station := conditionStation{
			ID: get("station_id"), Name: get("station_name"),
			Longitude: longitude, Latitude: latitude,
			SegmentID: get("dec_segment_id"),
		}
		if station.ID != "" {
			stations[station.ID] = station
		}
	}
	return stations, nil
}

// Load the same statewide latest-continuous feed used by the existing
// availability endpoint, once per cache interval, not once per station.
// Keep only matching IDs and the three selected water-quality parameters.
func collectWaterbodyConditions(r *http.Request, waterbodyID string, candidates map[string]conditionStation) (conditionsResult, error) {
	now := time.Now().UTC()
	result := conditionsResult{
		WaterbodyID: waterbodyID, AsOf: now, WindowHours: 24,
		Stations: []conditionStation{}, Readings: []conditionReading{},
		Note: "Latest available USGS observations at individual locations; not a river-wide average or safety assessment.",
	}
	if len(candidates) == 0 {
		return result, nil
	}

	params := url.Values{}
	params.Set("bbox", nyBoundingBox.String())
	params.Set("f", "json")
	params.Set("limit", "10000")
	params.Set("skipGeometry", "true")
	next := UsgsBaseURL + "/collections/latest-continuous/items?" + params.Encode()
	cutoff := now.Add(-24 * time.Hour)
	best := make(map[string]conditionReading)

	for page := 0; next != ""; page++ {
		if page >= 500 {
			return result, fmt.Errorf("USGS pagination exceeded 500 pages")
		}
		parsed, err := url.Parse(next)
		if err != nil || parsed.Scheme != "https" || parsed.Host != "api.waterdata.usgs.gov" {
			return result, fmt.Errorf("unexpected USGS pagination URL")
		}
		req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, next, nil)
		if err != nil {
			return result, err
		}
		req.Header.Set("X-Api-Key", cfg.APIKey)
		req.Header.Set("Accept", "application/json")
		resp, err := httpClient.Do(req)
		if err != nil {
			return result, err
		}
		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
			resp.Body.Close()
			return result, fmt.Errorf("USGS %s: %s", resp.Status, string(body))
		}
		var payload struct {
			Features []struct {
				Properties struct {
					MonitoringLocationID string          `json:"monitoring_location_id"`
					ParameterCode        string          `json:"parameter_code"`
					Value                json.RawMessage `json:"value"`
					Unit                 string          `json:"unit_of_measure"`
					Time                 string          `json:"time"`
					ApprovalStatus       string          `json:"approval_status"`
				} `json:"properties"`
			} `json:"features"`
			Links []struct {
				Rel  string `json:"rel"`
				Href string `json:"href"`
			} `json:"links"`
		}
		err = json.NewDecoder(io.LimitReader(resp.Body, 32<<20)).Decode(&payload)
		resp.Body.Close()
		if err != nil {
			return result, fmt.Errorf("decode USGS page %d: %w", page, err)
		}
		for _, feature := range payload.Features {
			p := feature.Properties
			station, ok := candidates[p.MonitoringLocationID]
			if !ok {
				continue
			}
			paramName, ok := conditionParameters[p.ParameterCode]
			if !ok {
				continue
			}
			at := parseUSGSTime(p.Time) // Reuse existing Go helper.
			if at.IsZero() || at.Before(cutoff) || at.After(now.Add(5*time.Minute)) {
				continue
			}
			value, err := parseConditionNumber(p.Value)
			if err != nil {
				continue
			}
			reading := conditionReading{
				StationID: station.ID, StationName: station.Name,
				ParameterCode: p.ParameterCode, Parameter: paramName,
				Value: value, Unit: p.Unit, Time: at,
				ApprovalStatus: p.ApprovalStatus,
				Longitude:      station.Longitude, Latitude: station.Latitude,
			}
			key := station.ID + "|" + p.ParameterCode
			if previous, ok := best[key]; !ok || reading.Time.After(previous.Time) {
				best[key] = reading
			}
		}
		next = ""
		for _, link := range payload.Links {
			if link.Rel == "next" {
				next = link.Href
				break
			}
		}
	}
	active := make(map[string]conditionStation)
	for _, reading := range best {
		result.Readings = append(result.Readings, reading)
		active[reading.StationID] = candidates[reading.StationID]
	}
	for _, station := range active {
		result.Stations = append(result.Stations, station)
	}
	// Stable ordering makes frontend display and manual testing deterministic.
	sortConditionResults(&result)
	result.StationCount = len(result.Stations)
	result.ReadingCount = len(result.Readings)
	return result, nil
}

func parseConditionNumber(raw json.RawMessage) (float64, error) {
	value := strings.TrimSpace(string(raw))
	if value == "" || value == "null" || value == `""` {
		return 0, fmt.Errorf("empty reading")
	}
	value = strings.Trim(value, `"`)
	number, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return 0, err
	}
	if number != number || number > 1e308 || number < -1e308 {
		return 0, fmt.Errorf("nonfinite reading")
	}
	return number, nil
}

func sortConditionResults(result *conditionsResult) {
	sort.Slice(result.Stations, func(i, j int) bool { return result.Stations[i].ID < result.Stations[j].ID })
	sort.Slice(result.Readings, func(i, j int) bool {
		a, b := result.Readings[i], result.Readings[j]
		if a.StationID != b.StationID {
			return a.StationID < b.StationID
		}
		return a.ParameterCode < b.ParameterCode
	})
}
