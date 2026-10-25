package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

// ForecastHandlers exposes public forecast endpoints while treating
// the Python forecasting service as an internal implementation detail.
type ForecastHandlers struct {
	baseURL string
	client  *http.Client
	logger  *log.Logger
}

// NewForecastHandlers creates forecast handlers.
func NewForecastHandlers(baseURL string) *ForecastHandlers {
	if baseURL == "" {
		baseURL = os.Getenv("WQ_API_URL")
	}
	if baseURL == "" {
		baseURL = "http://localhost:8000"
	}

	return &ForecastHandlers{
		baseURL: strings.TrimRight(baseURL, "/"),
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
		logger: log.Default(),
	}
}

// Public response types. These types belong to the Go API and are
// independent of the internal Python response models.

type ForecastResponse struct {
	StationID   string       `json:"stationId"`
	AsOf        string       `json:"asOf"`
	DataAgeDays *int         `json:"dataAgeDays,omitempty"`
	Stale       bool         `json:"stale"`
	Forecasts   []Prediction `json:"forecasts"`
}

type Prediction struct {
	Parameter        string              `json:"parameter"`
	Unit             string              `json:"unit,omitempty"`
	HorizonDays      int                 `json:"horizonDays"`
	Date             string              `json:"date"`
	Value            *float64            `json:"value"`
	Interval         *PredictionRange    `json:"interval,omitempty"`
	Exceedance       *float64            `json:"exceedanceProbability,omitempty"`
	Threshold        *float64            `json:"screeningThreshold,omitempty"`
	ExceedsThreshold *bool               `json:"exceedsThreshold,omitempty"`
	Status           string              `json:"status"`
	DataAgeDays      *float64            `json:"dataAgeDays,omitempty"`
	Reliability      ForecastReliability `json:"reliability"`
}

type PredictionRange struct {
	Lower *float64 `json:"lower"`
	Upper *float64 `json:"upper"`
}

type ForecastReliability struct {
	Confidence string   `json:"confidence"`
	Caveats    []string `json:"caveats"`
}

// Internal-only Python response types.

type pythonForecastResponse struct {
	SiteNo      string               `json:"site_no"`
	DataDate    string               `json:"data_date"`
	DataAgeDays *int                 `json:"data_age_days"`
	Stale       bool                 `json:"stale"`
	Forecasts   []pythonForecastItem `json:"forecasts"`
}

type pythonForecastItem struct {
	Parameter        string           `json:"parameter"`
	Unit             *string          `json:"unit"`
	HorizonDays      int              `json:"horizon_days"`
	ForecastDate     string           `json:"forecast_date"`
	Prediction       *float64         `json:"prediction"`
	Lower            *float64         `json:"lower"`
	Upper            *float64         `json:"upper"`
	Probability      *float64         `json:"probability_exceedance"`
	Basis            string           `json:"basis"`
	DataAgeDays      *float64         `json:"data_age_days"`
	ExceedsThreshold *bool            `json:"exceeds_screening_threshold"`
	Threshold        *pythonThreshold `json:"threshold"`
	Model            string           `json:"model"`
	Reliability      pythonReliability `json:"reliability"`
}

type pythonThreshold struct {
	Value *float64 `json:"value"`
}

type pythonReliability struct {
	Confidence string   `json:"confidence"`
	Caveats    []string `json:"caveats"`
}

// GET /api/stations/{id}/forecast
//
// Optional query parameters:
//
//   horizons=1,7,30
//   parameters=ph,lead
//
// The public response uses Go's own JSON schema rather than exposing
// the internal Python response directly.
func (h *ForecastHandlers) ForecastByStation(
	w http.ResponseWriter,
	r *http.Request,
) {
	start := time.Now()
	id := strings.TrimSpace(r.PathValue("id"))

	log.Printf(
		"INFO forecast request received method=%s station_id=%q",
		r.Method,
		id,
	)

	if id == "" {
		log.Printf("WARN forecast request rejected reason=missing_station_id")

		writeJSONError(
			w,
			http.StatusBadRequest,
			errors.New("station ID is required"),
		)
		return
	}

	query := url.Values{}

	// Validate and forward the public query options.
	if raw := r.URL.Query().Get("horizons"); raw != "" {
		horizons, err := parseHorizons(raw)
		if err != nil {
			log.Printf(
				"WARN forecast request rejected station_id=%q reason=invalid_horizons error=%q",
				id,
				err,
			)

			writeJSONError(w, http.StatusBadRequest, err)
			return
		}

		query.Set("horizons", horizons)
	}

	if raw := strings.TrimSpace(r.URL.Query().Get("parameters")); raw != "" {
		parameters := strings.Split(raw, ",")
		for i := range parameters {
			parameters[i] = strings.TrimSpace(parameters[i])
			if parameters[i] == "" {
				err := errors.New(
					"parameters must be a comma-separated list",
				)

				log.Printf(
					"WARN forecast request rejected station_id=%q reason=invalid_parameters",
					id,
				)

				writeJSONError(w, http.StatusBadRequest, err)
				return
			}
		}

		query.Set("parameters", strings.Join(parameters, ","))
	}

	path := "/sites/" + url.PathEscape(id) + "/forecast"
	if encoded := query.Encode(); encoded != "" {
		path += "?" + encoded
	}

	var result pythonForecastResponse
	status, err := h.getJSON(r.Context(), path, &result)
	if err != nil {
		log.Printf(
			"ERROR forecast retrieval failed station_id=%q upstream_status=%d duration_ms=%d error=%q",
			id,
			status,
			time.Since(start).Milliseconds(),
			err,
		)

		if status == http.StatusNotFound {
			writeJSONError(
				w,
				http.StatusNotFound,
				fmt.Errorf("station %q not found", id),
			)
			return
		}

		if status == http.StatusUnprocessableEntity ||
			status == http.StatusBadRequest {
			writeJSONError(
				w,
				http.StatusBadRequest,
				errors.New("invalid forecast request"),
			)
			return
		}

		if status != 0 && status >= 400 && status < 500 {
			writeJSONError(
				w,
				status,
				errors.New("forecast request rejected"),
			)
			return
		}

		writeJSONError(
			w,
			http.StatusBadGateway,
			errors.New("forecast service unavailable or returned invalid data"),
		)
		return
	}

	response := ForecastResponse{
		StationID:   id,
		AsOf:        result.DataDate,
		DataAgeDays: result.DataAgeDays,
		Stale:       result.Stale,
		Forecasts:   make([]Prediction, 0, len(result.Forecasts)),
	}

	for _, f := range result.Forecasts {
		prediction := Prediction{
			Parameter:        f.Parameter,
			HorizonDays:      f.HorizonDays,
			Date:             f.ForecastDate,
			Value:            f.Prediction,
			Exceedance:       f.Probability,
			ExceedsThreshold: f.ExceedsThreshold,
			DataAgeDays:      f.DataAgeDays,
			Reliability: ForecastReliability{
				Confidence: f.Reliability.Confidence,
				Caveats:    f.Reliability.Caveats,
			},
			Status: publicForecastStatus(f.Basis),
		}

		if f.Unit != nil {
			prediction.Unit = *f.Unit
		}

		if f.Lower != nil || f.Upper != nil {
			prediction.Interval = &PredictionRange{
				Lower: f.Lower,
				Upper: f.Upper,
			}
		}

		if f.Threshold != nil {
			prediction.Threshold = f.Threshold.Value
		}

		response.Forecasts = append(response.Forecasts, prediction)
	}

	log.Printf(
		"INFO forecast request completed station_id=%q forecast_count=%d stale=%t duration_ms=%d",
		id,
		len(response.Forecasts),
		response.Stale,
		time.Since(start).Milliseconds(),
	)

	writeJSON(w, http.StatusOK, response)
}

// GET /api/stations/{id}/forecast
// A helper endpoint can expose the same forecast for a single horizon
// by calling ForecastByStation with ?horizons=7, for example.

func (h *ForecastHandlers) getJSON(
	ctx context.Context,
	path string,
	dst any,
) (int, error) {
	start := time.Now()

	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodGet,
		h.baseURL+path,
		nil,
	)
	if err != nil {
		log.Printf(
			"ERROR failed to create forecast service request error=%q",
			err,
		)
		return 0, fmt.Errorf("create forecast request: %w", err)
	}

	req.Header.Set("Accept", "application/json")

	resp, err := h.client.Do(req)
	if err != nil {
		log.Printf(
			"ERROR forecast service request failed duration_ms=%d error=%q",
			time.Since(start).Milliseconds(),
			err,
		)
		return 0, fmt.Errorf("call forecast service: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		// Do not expose internal Python error details to the website.
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))

		log.Printf(
			"WARN forecast service returned non-success status=%d duration_ms=%d",
			resp.StatusCode,
			time.Since(start).Milliseconds(),
		)

		return resp.StatusCode, fmt.Errorf(
			"forecast service returned status %d",
			resp.StatusCode,
		)
	}

	decoder := json.NewDecoder(io.LimitReader(resp.Body, 10<<20))
	if err := decoder.Decode(dst); err != nil {
		log.Printf(
			"ERROR failed to decode forecast service response status=%d duration_ms=%d error=%q",
			resp.StatusCode,
			time.Since(start).Milliseconds(),
			err,
		)

		return resp.StatusCode, fmt.Errorf(
			"decode forecast response: %w",
			err,
		)
	}

	log.Printf(
		"INFO forecast service request completed status=%d duration_ms=%d",
		resp.StatusCode,
		time.Since(start).Milliseconds(),
	)

	return resp.StatusCode, nil
}

func parseHorizons(raw string) (string, error) {
	parts := strings.Split(raw, ",")
	values := make([]string, 0, len(parts))
	seen := make(map[int]bool)

	for _, part := range parts {
		part = strings.TrimSpace(part)
		n, err := strconv.Atoi(part)
		if err != nil || n < 1 || n > 365 {
			return "", errors.New(
				"horizons must contain integers between 1 and 365",
			)
		}

		if seen[n] {
			continue
		}

		seen[n] = true
		values = append(values, strconv.Itoa(n))
	}

	if len(values) == 0 {
		return "", errors.New("at least one forecast horizon is required")
	}

	return strings.Join(values, ","), nil
}

// Translate internal implementation details into stable public statuses.
func publicForecastStatus(basis string) string {
	switch basis {
	case "recent_data":
		return "forecast"
	case "stale_data_fallback":
		return "stale_estimate"
	case "never_measured_fallback":
		return "estimated_without_history"
	default:
		return "unavailable"
	}
}
