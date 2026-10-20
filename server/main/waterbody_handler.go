package main

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
)

type WaterbodySearchItem struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Type         string `json:"type"`
	Grouped      bool   `json:"grouped"`
	SegmentCount int    `json:"segmentCount"`
}

type WaterbodyDetail struct {
	ID           string          `json:"id"`
	Name         string          `json:"name"`
	Type         string          `json:"type"`
	Grouped      bool            `json:"grouped"`
	SegmentCount int             `json:"segmentCount"`
	Segments     json.RawMessage `json:"segments"`
}

// Search ordinary waterbodies and manually defined display groups.
func WaterbodySearch(w http.ResponseWriter, r *http.Request) {
	if stationStore.db == nil {
		writeJSONError(w, http.StatusServiceUnavailable,
			errors.New("database not ready"))
		return
	}

	q := strings.TrimSpace(r.URL.Query().Get("q"))

	if len(q) < 2 {
		writeJSON(w, http.StatusOK, []WaterbodySearchItem{})
		return
	}

	if len(q) > 120 {
		writeJSONError(w, http.StatusBadRequest,
			errors.New("query too long"))
		return
	}

	rows, err := stationStore.db.QueryContext(r.Context(), `
		WITH matches AS (
			-- Existing waterbodies, excluding members of
			-- display groups matching the current search.
			SELECT
				w.id,
				w.name,
				w.waterbody_type,
				w.grouped,
				w.segment_count
			FROM waterbodies_v2 w
			WHERE w.name ILIKE '%' || $1 || '%'
			  AND NOT EXISTS (
				SELECT 1
				FROM waterbody_display_members m
				JOIN waterbody_display_groups g
				  ON g.id = m.group_id
				WHERE m.waterbody_id = w.id
				  AND g.name ILIKE '%' || $1 || '%'
			  )

			UNION ALL

			-- Consolidated display groups.
			SELECT
				g.id,
				g.name,
				'river'::text AS waterbody_type,
				true AS grouped,
				SUM(w.segment_count)::integer AS segment_count
			FROM waterbody_display_groups g
			JOIN waterbody_display_members m
			  ON m.group_id = g.id
			JOIN waterbodies_v2 w
			  ON w.id = m.waterbody_id
			WHERE g.name ILIKE '%' || $1 || '%'
			GROUP BY g.id, g.name
		)
		SELECT
			id, name, waterbody_type, grouped, segment_count
		FROM matches
		ORDER BY
			CASE
				WHEN lower(name) = lower($1) THEN 0
				WHEN lower(name) LIKE lower($1) || '%' THEN 1
				ELSE 2
			END,
			name, id
		LIMIT 40
	`, q)

	if err != nil {
		writeJSONError(w, http.StatusInternalServerError,
			fmt.Errorf("waterbody search: %w", err))
		return
	}
	defer rows.Close()

	results := make([]WaterbodySearchItem, 0)

	for rows.Next() {
		var item WaterbodySearchItem

		if err := rows.Scan(
			&item.ID,
			&item.Name,
			&item.Type,
			&item.Grouped,
			&item.SegmentCount,
		); err != nil {
			writeJSONError(w, http.StatusInternalServerError, err)
			return
		}

		results = append(results, item)
	}

	if err := rows.Err(); err != nil {
		writeJSONError(w, http.StatusInternalServerError, err)
		return
	}

	writeJSON(w, http.StatusOK, results)
}

// Return either a display group or an ordinary waterbody.
func WaterbodyByID(w http.ResponseWriter, r *http.Request) {
	if stationStore.db == nil {
		writeJSONError(w, http.StatusServiceUnavailable,
			errors.New("database not ready"))
		return
	}

	id := strings.TrimSpace(r.PathValue("id"))

	if id == "" || len(id) > 120 {
		writeJSONError(w, http.StatusBadRequest,
			errors.New("invalid waterbody ID"))
		return
	}

	var detail WaterbodyDetail

	// First check whether this ID belongs to a display group.
	err := stationStore.db.QueryRowContext(r.Context(), `
		SELECT id, name
		FROM waterbody_display_groups
		WHERE id = $1
	`, id).Scan(&detail.ID, &detail.Name)

	if err == nil {
		detail.Type = "river"
		detail.Grouped = true

		// Combine all original DEC segment JSON arrays.
		// Individual assessments, classifications, and
		// geometries remain unchanged.
		err = stationStore.db.QueryRowContext(r.Context(), `
			SELECT
				COALESCE(SUM(w.segment_count), 0)::integer,
				COALESCE(
					(
						SELECT jsonb_agg(
							s.segment
							ORDER BY wb.name, wb.id, s.ordinality
						)
						FROM waterbody_display_members gm
						JOIN waterbodies_v2 wb
						  ON wb.id = gm.waterbody_id
						CROSS JOIN LATERAL
							jsonb_array_elements(wb.segments::jsonb)
							WITH ORDINALITY AS s(segment, ordinality)
						WHERE gm.group_id = $1
					),
					'[]'::jsonb
				)
			FROM waterbody_display_members m
			JOIN waterbodies_v2 w
			  ON w.id = m.waterbody_id
			WHERE m.group_id = $1
		`, id).Scan(
			&detail.SegmentCount,
			&detail.Segments,
		)

		if err != nil {
			writeJSONError(w, http.StatusInternalServerError,
				fmt.Errorf("waterbody group detail: %w", err))
			return
		}

		writeJSON(w, http.StatusOK, detail)
		return
	}

	if !errors.Is(err, sql.ErrNoRows) {
		writeJSONError(w, http.StatusInternalServerError,
			fmt.Errorf("waterbody group lookup: %w", err))
		return
	}

	// Otherwise, use the original waterbody lookup.
	err = stationStore.db.QueryRowContext(r.Context(), `
		SELECT
			id, name, waterbody_type, grouped,
			segment_count, segments
		FROM waterbodies_v2
		WHERE id = $1
	`, id).Scan(
		&detail.ID,
		&detail.Name,
		&detail.Type,
		&detail.Grouped,
		&detail.SegmentCount,
		&detail.Segments,
	)

	if errors.Is(err, sql.ErrNoRows) {
		writeJSONError(w, http.StatusNotFound,
			errors.New("waterbody not found"))
		return
	}

	if err != nil {
		writeJSONError(w, http.StatusInternalServerError,
			fmt.Errorf("waterbody detail: %w", err))
		return
	}

	writeJSON(w, http.StatusOK, detail)
}
