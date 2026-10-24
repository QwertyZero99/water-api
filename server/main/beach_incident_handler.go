package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"time"
)

func incidentType(status, reason string) string {
	s := strings.ToLower(strings.TrimSpace(status))
	r := strings.ToLower(strings.TrimSpace(reason))
	if s != "closed" && s != "open with advisory" {
		return ""
	}
	switch {
	case strings.Contains(r, "harmful algal bloom"), strings.Contains(r, "blue-green algae"):
		return "Harmful algal bloom"
	case strings.Contains(r, "sewage overflow"):
		return "Sewage overflow precaution"
	case strings.Contains(r, "exceedance"):
		if strings.Contains(r, "predicted") {
			return "Predicted bacterial exceedance"
		}
		return "Reported exceedance"
	case strings.Contains(r, "rainfall"):
		return "Rainfall-related precaution"
	case strings.Contains(r, "turbidity"):
		return "Turbidity-related precaution"
	default:
		return "" // Exclude seasonal/staffing/wave closures and ambiguous 'Other'.
	}
}

type pastIncident struct {
	Date              string `json:"date"`
	Beach             string `json:"beach"`
	Waterbody         string `json:"waterbody"`
	Type              string `json:"type"`
	Status            string `json:"status"`
	Reason            string `json:"reason"`
	SupportingRecords int    `json:"supportingRecords"`
}
type incidentResponse struct {
	WaterbodyID   string         `json:"waterbodyId"`
	WaterbodyName string         `json:"waterbodyName"`
	CoverageEnd   string         `json:"coverageEnd,omitempty"`
	Incidents     []pastIncident `json:"incidents"`
	Note          string         `json:"note"`
}

// Register: http.HandleFunc("GET /api/waterbodies/{id}/incidents", WaterbodyPastIncidents)
func WaterbodyPastIncidents(w http.ResponseWriter, r *http.Request) {
	log.Println("Serving beach incident API.")
	if stationStore.db == nil {
		http.Error(w, "database unavailable", http.StatusServiceUnavailable)
		return
	}
	id := strings.TrimSpace(r.PathValue("id"))
	if id == "" || len(id) > 120 {
		http.Error(w, "invalid waterbody ID", 400)
		return
	}
	var waterbody string
	err := stationStore.db.QueryRowContext(r.Context(), `SELECT name FROM waterbodies_v2 WHERE id = $1`, id).Scan(&waterbody)
	if err == sql.ErrNoRows {
		http.Error(w, "waterbody not found", 404)
		return
	}
	if err != nil {
		http.Error(w, "database query failed", 500)
		return
	}
	result, err := getIncidents(r.Context(), stationStore.db, id, waterbody)
	if err != nil {
		http.Error(w, "incident query failed", 500)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(result)
}
func getIncidents(ctx context.Context, db *sql.DB, id, waterbody string) (incidentResponse, error) {
	output := incidentResponse{WaterbodyID: id, WaterbodyName: waterbody, Incidents: []pastIncident{}, Note: "Historical State Parks beach records only. Names are matched exactly, not spatially verified. Results are beach-specific, may omit other beaches or incidents, and do not establish current swimming safety."}
	var latest sql.NullTime
	if err := db.QueryRowContext(ctx, `SELECT MAX(sample_date) FROM beach_test_records`).Scan(&latest); err != nil {
		return output, err
	}
	if latest.Valid {
		output.CoverageEnd = latest.Time.Format("2006-01-02")
	}
	rows, err := db.QueryContext(ctx, `
 SELECT sample_date,beach_name,waterbody_name,beach_status,status_reason,COUNT(*)
 FROM beach_test_records
 WHERE lower(trim(waterbody_name))=lower(trim($1))
   AND lower(trim(beach_status)) IN ('closed','open with advisory')
 GROUP BY sample_date,beach_name,waterbody_name,beach_status,status_reason
 ORDER BY sample_date DESC,beach_name,beach_status LIMIT 3000`, waterbody)
	if err != nil {
		return output, err
	}
	defer rows.Close()
	for rows.Next() {
		var dt time.Time
		var entry pastIncident
		if err = rows.Scan(&dt, &entry.Beach, &entry.Waterbody, &entry.Status, &entry.Reason, &entry.SupportingRecords); err != nil {
			return output, err
		}
		entry.Type = incidentType(entry.Status, entry.Reason)
		if entry.Type == "" {
			continue
		}
		entry.Date = dt.Format("2006-01-02")
		output.Incidents = append(output.Incidents, entry)
		if len(output.Incidents) == 100 {
			break
		}
	}
	return output, rows.Err()
}
