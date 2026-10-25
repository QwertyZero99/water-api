package main

import (
	"database/sql"
	"encoding/json"
	"log"
	"net/http"
	"strings"
)

type PFASMeasurement struct {
	ObjectID                int64    `json:"objectId"`
	EventID                 string   `json:"eventId"`
	SampledAt               string   `json:"sampledAt"`
	Analyte                 string   `json:"analyte"`
	CASRN                   string   `json:"casrn,omitempty"`
	Concentration           *float64 `json:"concentration"`
	Nondetect               bool     `json:"nondetect"`
	Unit                    string   `json:"unit"`
	Qualifier               string   `json:"qualifier,omitempty"`
	MethodDetectionLimit    *float64 `json:"methodDetectionLimit"`
	QuantitationLimit       *float64 `json:"quantitationLimit"`
	ReportingDetectionLimit *float64 `json:"reportingDetectionLimit"`
	AnalyticalMethod        string   `json:"analyticalMethod,omitempty"`
	SampleDesign            string   `json:"sampleDesign,omitempty"`
	SourceOrganization      string   `json:"sourceOrganization,omitempty"`
}

type PFASSite struct {
	ID            int64             `json:"id"`
	SiteCode      string            `json:"siteCode"`
	WaterbodyName string            `json:"waterbodyName"`
	WaterbodyType string            `json:"waterbodyType"`
	Basin         string            `json:"basin"`
	Latitude      float64           `json:"latitude"`
	Longitude     float64           `json:"longitude"`
	MatchMethod   string            `json:"matchMethod"`
	Measurements  []PFASMeasurement `json:"measurements"`
}

type PFASResponse struct {
	WaterbodyID      string     `json:"waterbodyId"`
	SiteCount        int        `json:"siteCount"`
	MeasurementCount int        `json:"measurementCount"`
	Sites            []PFASSite `json:"sites"`
	Note             string     `json:"note"`
}

type PFASHandler struct {
	db *sql.DB
}

func NewPFASHandler(db *sql.DB) *PFASHandler {
	return &PFASHandler{db: db}
}

func nullableString(value sql.NullString) string {
	if value.Valid {
		return value.String
	}
	return ""
}

func nullableFloat(value sql.NullFloat64) *float64 {
	if value.Valid {
		result := value.Float64
		return &result
	}
	return nil
}

func (h *PFASHandler) WaterbodyPFAS(
	w http.ResponseWriter,
	r *http.Request,
) {
	log.Println("Serve waterbody pfas API.")
	waterbodyID := strings.TrimSpace(r.PathValue("id"))

	if h.db == nil {
		http.Error(w, "Database not ready", http.StatusServiceUnavailable)
		return
	}

	if waterbodyID == "" || len(waterbodyID) > 120 {
		http.Error(w, "Invalid waterbody ID", http.StatusBadRequest)
		return
	}
	const query = `
		SELECT
			s.id,
			s.site_code,
			s.waterbody_name,
			s.waterbody_type,
			s.basin,
			s.latitude,
			s.longitude,

			CASE
				WHEN gp.site_id IS NOT NULL
					THEN 'display_group_association'
				ELSE s.match_method
			END AS match_method,

			m.objectid,
			m.event_id,
			m.sampled_at,
			m.analyte,
			m.casrn,
			m.concentration,
			m.nondetect,
			m.unit,
			m.qualifier,
			m.method_detection_limit,
			m.quantitation_limit,
			m.reporting_detection_limit,
			m.analytical_method,
			m.sample_design,
			m.source_organization

		FROM pfas_sites s

		LEFT JOIN waterbody_display_pfas_sites gp
			ON gp.site_id = s.id
			AND gp.group_id = $1

		LEFT JOIN pfas_results m
			ON m.site_id = s.id

		WHERE
			gp.site_id IS NOT NULL
			OR (
				s.waterbody_id = $1
				AND s.match_method = 'verified_name_and_geography'
			)

		ORDER BY
			s.site_code,
			s.id,
			m.sampled_at DESC,
			m.analyte
	`

	rows, err := h.db.QueryContext(
		r.Context(),
		query,
		waterbodyID,
	)
	if err != nil {
		log.Printf("PFAS query failed for %s: %v", waterbodyID, err)
		http.Error(
			w,
			"Unable to retrieve monitoring data",
			http.StatusInternalServerError,
		)
		return
	}
	defer rows.Close()

	response := PFASResponse{
		WaterbodyID: waterbodyID,
		Sites:       make([]PFASSite, 0),
		Note: "Historical NYSDEC ambient monitoring results. " +
			"Ordinary waterbody sites use automated name and geographic matching. " +
			"Display-group sites use explicit group associations. " +
			"These associations are not individually field-verified. " +
			"Measurements represent specific sampling locations and dates, " +
			"not entire waterbodies. Nondetect does not mean zero. " +
			"Results may include non-PFAS chemicals such as 1,4-dioxane. " +
			"Do not use these records alone to determine drinking or swimming safety.",
	}

	siteIndex := make(map[int64]int)

	for rows.Next() {
		var site PFASSite
		var matchMethod sql.NullString

		var objectID sql.NullInt64
		var eventID sql.NullString
		var sampledAt sql.NullTime
		var analyte sql.NullString
		var casrn sql.NullString
		var concentration sql.NullFloat64
		var nondetect sql.NullBool
		var unit sql.NullString
		var qualifier sql.NullString
		var methodDetectionLimit sql.NullFloat64
		var quantitationLimit sql.NullFloat64
		var reportingDetectionLimit sql.NullFloat64
		var analyticalMethod sql.NullString
		var sampleDesign sql.NullString
		var sourceOrganization sql.NullString

		err := rows.Scan(
			&site.ID,
			&site.SiteCode,
			&site.WaterbodyName,
			&site.WaterbodyType,
			&site.Basin,
			&site.Latitude,
			&site.Longitude,
			&matchMethod,

			&objectID,
			&eventID,
			&sampledAt,
			&analyte,
			&casrn,
			&concentration,
			&nondetect,
			&unit,
			&qualifier,
			&methodDetectionLimit,
			&quantitationLimit,
			&reportingDetectionLimit,
			&analyticalMethod,
			&sampleDesign,
			&sourceOrganization,
		)
		if err != nil {
			log.Printf("PFAS row scan failed: %v", err)
			http.Error(
				w,
				"Unable to read monitoring data",
				http.StatusInternalServerError,
			)
			return
		}

		index, exists := siteIndex[site.ID]

		if !exists {
			site.MatchMethod = nullableString(matchMethod)
			site.Measurements = make([]PFASMeasurement, 0)

			index = len(response.Sites)
			siteIndex[site.ID] = index
			response.Sites = append(response.Sites, site)
		}

		if !objectID.Valid {
			continue
		}

		measurement := PFASMeasurement{
			ObjectID:                objectID.Int64,
			EventID:                 nullableString(eventID),
			Analyte:                 nullableString(analyte),
			CASRN:                   nullableString(casrn),
			Concentration:           nullableFloat(concentration),
			Nondetect:               nondetect.Valid && nondetect.Bool,
			Unit:                    nullableString(unit),
			Qualifier:               nullableString(qualifier),
			MethodDetectionLimit:    nullableFloat(methodDetectionLimit),
			QuantitationLimit:       nullableFloat(quantitationLimit),
			ReportingDetectionLimit: nullableFloat(reportingDetectionLimit),
			AnalyticalMethod:        nullableString(analyticalMethod),
			SampleDesign:            nullableString(sampleDesign),
			SourceOrganization:      nullableString(sourceOrganization),
		}

		if sampledAt.Valid {
			measurement.SampledAt = sampledAt.Time.Format(
				"2006-01-02T15:04:05",
			)
		}

		response.Sites[index].Measurements = append(
			response.Sites[index].Measurements,
			measurement,
		)

		response.MeasurementCount++
	}

	if err := rows.Err(); err != nil {
		log.Printf("PFAS rows error: %v", err)
		http.Error(
			w,
			"Unable to retrieve monitoring data",
			http.StatusInternalServerError,
		)
		return
	}

	response.SiteCount = len(response.Sites)

	w.Header().Set("Content-Type", "application/json")

	if err := json.NewEncoder(w).Encode(response); err != nil {
		log.Printf("PFAS JSON encoding failed: %v", err)
	}
}
