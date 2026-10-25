package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
)

// measurement represents one usgs observation
type Measurement struct {
	ParameterCode  string          `json:"parameterCode"`
	Value          json.RawMessage `json:"value"`
	Unit           string          `json:"unit"`
	Time           string          `json:"time"`
	ApprovalStatus string          `json:"approvalStatus"`
}

// Defines and makes sure go knows the structure of the usgs geojson response
type USGSMeasurementResponse struct {
	Features []struct {
		Properties struct {
			ParameterCode  string          `json:"parameter_code"`
			Value          json.RawMessage `json:"value"`
			Unit           string          `json:"unit_of_measure"`
			Time           string          `json:"time"`
			ApprovalStatus string          `json:"approval_status"`
		} `json:"properties"`
	} `json:"features"`
}

func (h *StationHandlers) StationMeasurements(
	w http.ResponseWriter,
	r *http.Request,
) {
	log.Println("Serving station measurements API.")
	id := strings.TrimSpace(r.PathValue("id"))

	if id == "" {
		writeJSONError(
			w,
			http.StatusBadRequest,
			fmt.Errorf("station ID is required"),
		)
		return
	}

	endpoint := UsgsBaseURL +
		"/collections/latest-continuous/items"

	params := url.Values{}
	params.Set("monitoring_location_id", id)
	params.Set("f", "json")
	params.Set("limit", "1000")
	params.Set("skipGeometry", "true")

	requestURL := endpoint + "?" + params.Encode()

	req, err := http.NewRequestWithContext(
		r.Context(),
		http.MethodGet,
		requestURL,
		nil,
	)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err)
		return
	}

	req.Header.Set("X-Api-Key", cfg.APIKey)
	req.Header.Set("Accept", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		writeJSONError(
			w,
			http.StatusBadGateway,
			fmt.Errorf("USGS request failed: %w", err),
		)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))

		writeJSONError(
			w,
			http.StatusBadGateway,
			fmt.Errorf(
				"USGS returned status %d: %s",
				resp.StatusCode,
				string(body),
			),
		)
		return
	}
	var usgsData USGSMeasurementResponse

	if err := json.NewDecoder(
		io.LimitReader(resp.Body, 10<<20),
	).Decode(&usgsData); err != nil {
		writeJSONError(
			w,
			http.StatusBadGateway,
			fmt.Errorf("decode USGS response: %w", err),
		)
		return
	}

	measurements := make([]Measurement, 0, len(usgsData.Features))

	for _, feature := range usgsData.Features {
		p := feature.Properties

		measurements = append(measurements, Measurement{
			ParameterCode:  p.ParameterCode,
			Value:          p.Value,
			Unit:           p.Unit,
			Time:           p.Time,
			ApprovalStatus: p.ApprovalStatus,
		})
	}

	writeJSON(
		w,
		http.StatusOK,
		map[string]any{
			"stationId":    id,
			"measurements": measurements,
			"count":        len(measurements),
		},
	)
}
