
package main

import (
	"bytes"
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

type ForecastHandlers struct {
	baseURL string
	client  *http.Client
	logger  *log.Logger
}

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

// Public Go API response types.

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

// Internal Python API response types.

type pythonForecastResponse struct {
	SiteNo      string               `json:"site_no"`
	DataDate    string               `json:"data_date"`
	DataAgeDays *int                 `json:"data_age_days"`
	Stale       bool                 `json:"stale"`
	Forecasts   []pythonForecastItem `json:"forecasts"`
}

type pythonForecastItem struct {
	Parameter        string            `json:"parameter"`
	Unit             *string           `json:"unit"`
	HorizonDays      int               `json:"horizon_days"`
	ForecastDate     string            `json:"forecast_date"`
	Prediction       *float64          `json:"prediction"`
	Lower            *float64          `json:"lower"`
	Upper            *float64          `json:"upper"`
	Probability      *float64          `json:"probability_exceedance"`
	Basis            string            `json:"basis"`
	DataAgeDays      *float64          `json:"data_age_days"`
	ExceedsThreshold *bool             `json:"exceeds_screening_threshold"`
	Threshold        *pythonThreshold  `json:"threshold"`
	Reliability      pythonReliability `json:"reliability"`
}

type pythonThreshold struct {
	Value *float64 `json:"value"`
}

type pythonReliability struct {
	Confidence string   `json:"confidence"`
	Caveats    []string `json:"caveats"`
}

// Request body for the Python new-station endpoint.
// Confirm these field names against the NewStationIn schema.
type pythonForecastRequest struct {
	SiteNo     string   `json:"site_no"`
	Horizons   []int    `json:"horizons,omitempty"`
	Parameters []string `json:"parameters,omitempty"`
}

// GET /api/stations/{id}/forecast
func (h *ForecastHandlers) ForecastByStation(
	w http.ResponseWriter,
	r *http.Request,
) {
	start := time.Now()
	id := strings.TrimSpace(r.PathValue("id"))

	if id == "" {
		writeJSONError(
			w,
			http.StatusBadRequest,
			errors.New("station ID is required"),
		)
		return
	}

	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		writeJSONError(
			w,
			http.StatusMethodNotAllowed,
			errors.New("method not allowed"),
		)
		return
	}

	request := pythonForecastRequest{
		SiteNo: id,
	}

	query := r.URL.Query()

	if raw := query.Get("horizons"); raw != "" {
		horizons, err := parseHorizons(raw)
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, err)
			return
		}

		for _, value := range strings.Split(horizons, ",") {
			n, _ := strconv.Atoi(value)
			request.Horizons = append(request.Horizons, n)
		}
	}

	if raw := strings.TrimSpace(query.Get("parameters")); raw != "" {
		for _, parameter := range strings.Split(raw, ",") {
			parameter = strings.TrimSpace(parameter)
			if parameter == "" {
				writeJSONError(
					w,
					http.StatusBadRequest,
					errors.New(
						"parameters must be a comma-separated list",
					),
				)
				return
			}

			request.Parameters = append(
				request.Parameters,
				parameter,
			)
		}
	}

	// The Python API call is POST, not GET.
	//
	// IMPORTANT: this example uses /forecast/new-station because
	// that is a POST endpoint shown in the supplied Swagger docs.
	// Use the endpoint whose request schema matches this payload.
	var result pythonForecastResponse

	status, err := h.postJSON(
		r.Context(),
		"/forecast/new-station",
		request,
		&result,
	)
	if err != nil {
		h.logger.Printf(
			"ERROR forecast failed station_id=%q upstream_status=%d duration_ms=%d error=%q",
			id,
			status,
			time.Since(start).Milliseconds(),
			err,
		)

		switch status {
		case http.StatusNotFound:
			writeJSONError(
				w,
				http.StatusNotFound,
				fmt.Errorf("station %q not found", id),
			)
		case http.StatusBadRequest,
			http.StatusUnprocessableEntity:
			writeJSONError(
				w,
				http.StatusBadRequest,
				errors.New("invalid forecast request"),
			)
		default:
			writeJSONError(
				w,
				http.StatusBadGateway,
				errors.New(
					"forecast service unavailable or returned invalid data",
				),
			)
		}
		return
	}

	response := mapForecastResponse(id, result)

	h.logger.Printf(
		"INFO forecast completed station_id=%q forecast_count=%d stale=%t duration_ms=%d",
		id,
		len(response.Forecasts),
		response.Stale,
		time.Since(start).Milliseconds(),
	)

	writeJSON(w, http.StatusOK, response)
}

func (h *ForecastHandlers) postJSON(
	ctx context.Context,
	path string,
	payload any,
	dst any,
) (int, error) {
	start := time.Now()

	body, err := json.Marshal(payload)
	if err != nil {
		return 0, fmt.Errorf("encode forecast request: %w", err)
	}

	endpoint, err := url.JoinPath(h.baseURL, path)
	if err != nil {
		return 0, fmt.Errorf("build forecast URL: %w", err)
	}

	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		endpoint,
		bytes.NewReader(body),
	)
	if err != nil {
		return 0, fmt.Errorf("create forecast request: %w", err)
	}

	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")

	resp, err := h.client.Do(req)
	if err != nil {
		h.logger.Printf(
			"ERROR forecast service request failed duration_ms=%d error=%q",
			time.Since(start).Milliseconds(),
			err,
		)
		return 0, fmt.Errorf("call forecast service: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		// Consume only a bounded amount. Never return internal
		// Python error details to the public client.
		_, _ = io.Copy(
			io.Discard,
			io.LimitReader(resp.Body, 4096),
		)

		h.logger.Printf(
			"WARN forecast service returned status=%d duration_ms=%d",
			resp.StatusCode,
			time.Since(start).Milliseconds(),
		)

		return resp.StatusCode, fmt.Errorf(
			"forecast service returned status %d",
			resp.StatusCode,
		)
	}

	const maxResponseBytes = 10 << 20
	limited := io.LimitReader(resp.Body, maxResponseBytes+1)

	data, err := io.ReadAll(limited)
	if err != nil {
		return resp.StatusCode, fmt.Errorf(
			"read forecast response: %w",
			err,
		)
	}
	if len(data) > maxResponseBytes {
		return resp.StatusCode, errors.New(
			"forecast response exceeds size limit",
		)
	}

	if err := json.Unmarshal(data, dst); err != nil {
		return resp.StatusCode, fmt.Errorf(
			"decode forecast response: %w",
			err,
		)
	}

	h.logger.Printf(
		"INFO forecast service completed status=%d duration_ms=%d",
		resp.StatusCode,
		time.Since(start).Milliseconds(),
	)

	return resp.StatusCode, nil
}

func mapForecastResponse(
	id string,
	result pythonForecastResponse,
) ForecastResponse {
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
			Value:             f.Prediction,
			Exceedance:        f.Probability,
			ExceedsThreshold:  f.ExceedsThreshold,
			DataAgeDays:       f.DataAgeDays,
			Status:            publicForecastStatus(f.Basis),
			Reliability: ForecastReliability{
				Confidence: f.Reliability.Confidence,
				Caveats:    f.Reliability.Caveats,
			},
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

		response.Forecasts = append(
			response.Forecasts,
			prediction,
		)
	}

	return response
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
		return "", errors.New(
			"at least one forecast horizon is required",
		)
	}

	return strings.Join(values, ","), nil
}

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

