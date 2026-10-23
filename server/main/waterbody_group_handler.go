package main

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strings"
)

type waterbodyGroupResult struct {
	ID           int64  `json:"id"`
	Name         string `json:"name"`
	Type         string `json:"type"`
	SegmentCount int    `json:"segmentCount"`
}

type waterbodyGroupSegment struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	Assessment     string `json:"assessment"`
	Classification string `json:"classification"`
}

type waterbodyGroupDetail struct {
	ID       int64                   `json:"id"`
	Name     string                  `json:"name"`
	Type     string                  `json:"type"`
	Segments []waterbodyGroupSegment `json:"segments"`
}

func WaterbodyGroupSearch(w http.ResponseWriter, r *http.Request) {
	if stationStore.db == nil {
		http.Error(w, "database unavailable", http.StatusServiceUnavailable)
		return
	}

	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if len(q) < 2 {
		http.Error(w, "query must contain at least two characters", http.StatusBadRequest)
		return
	}

	rows, err := stationStore.db.QueryContext(r.Context(), `
		SELECT g.id, g.name, g.waterbody_type, COUNT(m.feature_id)
		FROM waterbody_groups g
		JOIN waterbody_group_members m ON m.group_id = g.id
		WHERE g.name ILIKE '%' || $1 || '%'
		GROUP BY g.id, g.name, g.waterbody_type
		ORDER BY g.name
		LIMIT 40
	`, q)

	if err != nil {
		http.Error(w, "database query failed", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	results := []waterbodyGroupResult{}

	for rows.Next() {
		var item waterbodyGroupResult

		if err := rows.Scan(
			&item.ID,
			&item.Name,
			&item.Type,
			&item.SegmentCount,
		); err != nil {
			http.Error(w, "database scan failed", http.StatusInternalServerError)
			return
		}

		results = append(results, item)
	}

	if err := rows.Err(); err != nil {
		http.Error(w, "database query failed", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(results)
}

func WaterbodyGroupByID(w http.ResponseWriter, r *http.Request) {
	if stationStore.db == nil {
		http.Error(w, "database unavailable", http.StatusServiceUnavailable)
		return
	}

	id := r.PathValue("id")

	var group waterbodyGroupDetail
	group.Segments = []waterbodyGroupSegment{}

	err := stationStore.db.QueryRowContext(r.Context(), `
		SELECT id, name, waterbody_type
		FROM waterbody_groups
		WHERE id::text = $1
	`, id).Scan(&group.ID, &group.Name, &group.Type)

	if err == sql.ErrNoRows {
		http.Error(w, "waterbody group not found", http.StatusNotFound)
		return
	}
	if err != nil {
		http.Error(w, "database query failed", http.StatusInternalServerError)
		return
	}

	rows, err := stationStore.db.QueryContext(r.Context(), `
		SELECT
			w.feature_id,
			w.name,
			COALESCE(w.assessment, ''),
			COALESCE(w.classification, '')
		FROM waterbody_group_members m
		JOIN waterbodies w ON w.feature_id = m.feature_id
		WHERE m.group_id = $1
		ORDER BY w.name
	`, group.ID)

	if err != nil {
		http.Error(w, "database query failed", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	for rows.Next() {
		var segment waterbodyGroupSegment

		if err := rows.Scan(
			&segment.ID,
			&segment.Name,
			&segment.Assessment,
			&segment.Classification,
		); err != nil {
			http.Error(w, "database scan failed", http.StatusInternalServerError)
			return
		}

		group.Segments = append(group.Segments, segment)
	}

	if err := rows.Err(); err != nil {
		http.Error(w, "database query failed", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(group)
}
func WaterbodyGroupIncidents(w http.ResponseWriter, r *http.Request) {
	if stationStore.db == nil {
		http.Error(w, "database unavailable", http.StatusServiceUnavailable)
		return
	}

	id := r.PathValue("id")

	var name string
	err := stationStore.db.QueryRowContext(
		r.Context(),
		`SELECT name FROM waterbody_groups WHERE id::text = $1`,
		id,
	).Scan(&name)

	if err == sql.ErrNoRows {
		http.Error(w, "waterbody group not found", http.StatusNotFound)
		return
	}
	if err != nil {
		http.Error(w, "database query failed", http.StatusInternalServerError)
		return
	}

	// Reuse the existing historical incident query,
	// but match against the parent waterbody's name.
	result, err := getIncidents(
		r.Context(),
		stationStore.db,
		id,
		name,
	)
	if err != nil {
		http.Error(w, "incident query failed", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(result)
}
