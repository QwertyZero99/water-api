package main

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/paulmach/orb"
)

// StationHandlers provides HTTP handlers backed directly by PostgreSQL.
//
// PostgreSQL is the source of truth. No station data is cached in memory
// by these handlers.
type StationHandlers struct {
	store *StationStore
}

// NewStationHandlers creates station HTTP handlers.
func NewStationHandlers(store *StationStore) *StationHandlers {
	return &StationHandlers{
		store: store,
	}
}

// StationLocationResponse is the minimal representation returned by the
// bounding-box endpoint.
type StationLocationResponse struct {
	ID       string    `json:"id"`
	Location orb.Point `json:"location"`
}

// StationsInBoundingBox handles:
//
//	GET /api/stations?bbox=minLong,minLat,maxLong,maxLat
//
// Example:
//
//	GET /api/stations?bbox=-79.8,40.4,-71.8,45.1
//
// The response contains only station IDs and locations.
//
// PostgreSQL performs the geographic filtering, so the complete station
// dataset is never loaded into Go.
func (h *StationHandlers) StationsInBoundingBox(
	w http.ResponseWriter,
	r *http.Request,
) {
	bbox, err := parseBoundingBox(
		r.URL.Query().Get("bbox"),
	)
	if err != nil {
		writeJSONError(
			w,
			http.StatusBadRequest,
			err,
		)
		return
	}

	if h.store == nil || h.store.db == nil {
		writeJSONError(
			w,
			http.StatusInternalServerError,
			errors.New("station database is not initialized"),
		)
		return
	}

	rows, err := h.store.db.Query(`
SELECT
station->>'ID' AS station_id,
(station->'Geometry'->>0)::double precision AS longitude,
(station->'Geometry'->>1)::double precision AS latitude
FROM stations
WHERE
station->'Geometry' IS NOT NULL
AND jsonb_typeof(station->'Geometry') = 'array'
AND jsonb_array_length(station->'Geometry') >= 2

AND (station->'Geometry'->>0)::double precision
BETWEEN $1 AND $2

AND (station->'Geometry'->>1)::double precision
BETWEEN $3 AND $4

ORDER BY station->>'ID'
`,
		bbox.MinLong,
		bbox.MaxLong,
		bbox.MinLat,
		bbox.MaxLat,
	)
	if err != nil {
		writeJSONError(
			w,
			http.StatusInternalServerError,
			fmt.Errorf(
				"query stations in bounding box: %w",
				err,
			),
		)
		return
	}
	defer rows.Close()

	stations := make(
		[]StationLocationResponse,
		0,
	)

	for rows.Next() {
		var (
			id        string
			longitude float64
			latitude  float64
		)

		if err := rows.Scan(
			&id,
			&longitude,
			&latitude,
		); err != nil {
			writeJSONError(
				w,
				http.StatusInternalServerError,
				fmt.Errorf(
					"scan station location: %w",
					err,
				),
			)
			return
		}

		stations = append(
			stations,
			StationLocationResponse{
				ID: id,
				Location: orb.Point{
					longitude,
					latitude,
				},
			},
		)
	}

	if err := rows.Err(); err != nil {
		writeJSONError(
			w,
			http.StatusInternalServerError,
			fmt.Errorf(
				"iterate station locations: %w",
				err,
			),
		)
		return
	}

	writeJSON(
		w,
		http.StatusOK,
		stations,
	)
}

// StationByID handles:
//
//	GET /api/stations/{id}
//
// The complete station JSON is read directly from PostgreSQL.
//
// The stored JSON is deliberately not unmarshaled into Station. Station
// contains orb.Geometry and geojson.Feature, whose geometry fields use
// interface types that encoding/json cannot automatically reconstruct.
//
// Instead, the JSONB document is validated and returned directly.
func (h *StationHandlers) StationByID(
	w http.ResponseWriter,
	r *http.Request,
) {
	const prefix = "/api/stations/"

	if !strings.HasPrefix(r.URL.Path, prefix) {
		writeJSONError(
			w,
			http.StatusBadRequest,
			errors.New("invalid station URL"),
		)
		return
	}

	id := strings.TrimPrefix(
		r.URL.Path,
		prefix,
	)

	id = strings.TrimSpace(id)

	if id == "" {
		writeJSONError(
			w,
			http.StatusBadRequest,
			errors.New("station ID is required"),
		)
		return
	}

	if h.store == nil || h.store.db == nil {
		writeJSONError(
			w,
			http.StatusInternalServerError,
			errors.New("station database is not initialized"),
		)
		return
	}

	var rawJSON []byte

	err := h.store.db.QueryRow(`
SELECT station
FROM stations
WHERE station_id = $1
`, id).Scan(&rawJSON)

	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeJSONError(
				w,
				http.StatusNotFound,
				fmt.Errorf(
					"station %q not found",
					id,
				),
			)
			return
		}

		writeJSONError(
			w,
			http.StatusInternalServerError,
			fmt.Errorf(
				"query station %q: %w",
				id,
				err,
			),
		)
		return
	}

	// Validate the stored JSON without attempting to decode it into
	// Station, orb.Geometry, or geojson.Feature.
	var document json.RawMessage

	if err := json.Unmarshal(
		rawJSON,
		&document,
	); err != nil {
		writeJSONError(
			w,
			http.StatusInternalServerError,
			fmt.Errorf(
				"invalid stored JSON for station %q: %w",
				id,
				err,
			),
		)
		return
	}

	// json.RawMessage marshals the original JSON document as JSON.
	data, err := json.Marshal(document)
	if err != nil {
		writeJSONError(
			w,
			http.StatusInternalServerError,
			fmt.Errorf(
				"encode station %q: %w",
				id,
				err,
			),
		)
		return
	}

	w.Header().Set(
		"Content-Type",
		"application/json",
	)

	w.WriteHeader(http.StatusOK)

	if _, err := w.Write(data); err != nil {
		return
	}
}

// parseBoundingBox parses:
//
//	minLong,minLat,maxLong,maxLat
//
// Example:
//
//	-79.8,40.4,-71.8,45.1
func parseBoundingBox(
	value string,
) (BoundingBox, error) {
	value = strings.TrimSpace(value)

	if value == "" {
		return BoundingBox{}, errors.New(
			"bbox query parameter is required",
		)
	}

	parts := strings.Split(value, ",")

	if len(parts) != 4 {
		return BoundingBox{}, errors.New(
			"bbox must contain minLong,minLat,maxLong,maxLat",
		)
	}

	values := make([]float32, 4)

	for i, part := range parts {
		part = strings.TrimSpace(part)

		value, err := strconv.ParseFloat(
			part,
			32,
		)
		if err != nil {
			return BoundingBox{}, fmt.Errorf(
				"invalid bbox coordinate %q: %w",
				part,
				err,
			)
		}

		values[i] = float32(value)
	}

	bbox := BoundingBox{
		MinLong: values[0],
		MinLat:  values[1],
		MaxLong: values[2],
		MaxLat:  values[3],
	}

	if bbox.MinLong > bbox.MaxLong {
		return BoundingBox{}, errors.New(
			"bbox minLong must be less than or equal to maxLong",
		)
	}

	if bbox.MinLat > bbox.MaxLat {
		return BoundingBox{}, errors.New(
			"bbox minLat must be less than or equal to maxLat",
		)
	}

	if bbox.MinLong < -180 ||
		bbox.MaxLong > 180 {
		return BoundingBox{}, errors.New(
			"longitude must be between -180 and 180",
		)
	}

	if bbox.MinLat < -90 ||
		bbox.MaxLat > 90 {
		return BoundingBox{}, errors.New(
			"latitude must be between -90 and 90",
		)
	}

	return bbox, nil
}

// writeJSON writes a JSON HTTP response.
func writeJSON(
	w http.ResponseWriter,
	status int,
	value any,
) {
	data, err := json.Marshal(value)
	if err != nil {
		http.Error(
			w,
			fmt.Sprintf(
				"encode JSON response: %v",
				err,
			),
			http.StatusInternalServerError,
		)
		return
	}

	w.Header().Set(
		"Content-Type",
		"application/json",
	)

	w.WriteHeader(status)

	_, _ = w.Write(data)
}

// writeJSONError writes an error response as JSON.
func writeJSONError(
	w http.ResponseWriter,
	status int,
	err error,
) {
	writeJSON(
		w,
		status,
		map[string]string{
			"error": err.Error(),
		},
	)
}
