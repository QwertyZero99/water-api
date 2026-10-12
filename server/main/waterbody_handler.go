package main

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
)

// WaterbodySearch finds waterbody segments. Metadata only: geometry is deliberately
// excluded to keep autocomplete responses fast.
func WaterbodySearch(w http.ResponseWriter, r *http.Request) {
	if stationStore.db == nil {
		writeJSONError(w, 500, errors.New("database not ready"))
		return
	}
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if len(q) < 2 {
		writeJSON(w, 200, []any{})
		return
	}
	if len(q) > 120 {
		writeJSONError(w, 400, errors.New("query too long"))
		return
	}
	rows, err := stationStore.db.QueryContext(r.Context(), `
  SELECT feature_id, segment_id, name, waterbody_type, description, basin,
         classification, assessment, assessed_year
  FROM waterbodies
  WHERE name ILIKE '%' || $1 || '%'
  ORDER BY CASE WHEN lower(name)=lower($1) THEN 0
                WHEN lower(name) LIKE lower($1) || '%' THEN 1 ELSE 2 END,
           name, segment_id, feature_id
  LIMIT 40`, q)
	if err != nil {
		writeJSONError(w, 500, fmt.Errorf("waterbody search: %w", err))
		return
	}
	defer rows.Close()
	type Item struct {
		ID          string `json:"id"`
		SegmentID   string `json:"segmentId"`
		Name        string `json:"name"`
		Type        string `json:"type"`
		Description string `json:"description"`
		Basin       string `json:"basin"`
		Class       string `json:"class"`
		Assessment  string `json:"assessment"`
		Year        any    `json:"year"`
	}
	result := make([]Item, 0)
	for rows.Next() {
		var v Item
		var year sql.NullInt64
		if err := rows.Scan(&v.ID, &v.SegmentID, &v.Name, &v.Type, &v.Description, &v.Basin, &v.Class, &v.Assessment, &year); err != nil {
			writeJSONError(w, 500, err)
			return
		}
		if year.Valid {
			v.Year = year.Int64
		}
		result = append(result, v)
	}
	if err := rows.Err(); err != nil {
		writeJSONError(w, 500, err)
		return
	}
	writeJSON(w, 200, result)
}

// WaterbodyByID returns exactly one geometry with normalized metadata.
// feature_id is distinct from segment_id because a segment ID can occur in
// multiple source features.
func WaterbodyByID(w http.ResponseWriter, r *http.Request) {
	if stationStore.db == nil {
		writeJSONError(w, 500, errors.New("database not ready"))
		return
	}
	id := strings.TrimSpace(r.PathValue("id"))
	if id == "" || len(id) > 120 {
		writeJSONError(w, 400, errors.New("invalid waterbody ID"))
		return
	}
	var geometry json.RawMessage
	var fid, segmentID, name, kind, description, basin, classification, assessment, factsheet string
	var year sql.NullInt64
	err := stationStore.db.QueryRowContext(r.Context(), `
  SELECT feature_id,segment_id,name,waterbody_type,description,basin,
         classification,assessment,assessed_year,factsheet,geometry
  FROM waterbodies WHERE feature_id=$1`, id).Scan(
		&fid, &segmentID, &name, &kind, &description, &basin, &classification, &assessment, &year, &factsheet, &geometry)
	if errors.Is(err, sql.ErrNoRows) {
		writeJSONError(w, 404, errors.New("waterbody not found"))
		return
	}
	if err != nil {
		writeJSONError(w, 500, fmt.Errorf("waterbody detail: %w", err))
		return
	}
	var yearValue any
	if year.Valid {
		yearValue = year.Int64
	}
	feature := map[string]any{
		"type": "Feature", "geometry": json.RawMessage(geometry),
		"properties": map[string]any{
			"id": fid, "segmentId": segmentID, "name": name, "type": kind,
			"description": description, "basin": basin, "class": classification,
			"assessment": assessment, "year": yearValue, "factsheet": factsheet,
		},
	}
	writeJSON(w, 200, feature)
}
