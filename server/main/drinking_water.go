package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"strings"
	"time"
)

type drinkingWaterHandler struct{ db *sql.DB }

func NewDrinkingWaterHandler(db *sql.DB) *drinkingWaterHandler { return &drinkingWaterHandler{db: db} }

func (h *drinkingWaterHandler) Search(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if len(q) < 2 {
		drinkingJSON(w, http.StatusOK, []any{})
		return
	}
	ctx, cancel := contextWithTimeout(r)
	defer cancel()
	rows, err := h.db.QueryContext(ctx, `SELECT pws_id,name,COALESCE(county,''),population FROM drinking_water_systems WHERE name ILIKE $1 OR pws_id ILIKE $1 OR county ILIKE $1 ORDER BY CASE WHEN lower(name)=lower($2) THEN 0 ELSE 1 END,name,pws_id LIMIT 40`, "%"+escapeLike(q)+"%", q)
	if err != nil {
		drinkingError(w, err)
		return
	}
	defer rows.Close()
	type system struct {
		ID         string `json:"id"`
		Name       string `json:"name"`
		County     string `json:"county"`
		Population *int   `json:"population"`
	}
	output := []system{}
	for rows.Next() {
		var x system
		if err := rows.Scan(&x.ID, &x.Name, &x.County, &x.Population); err != nil {
			drinkingError(w, err)
			return
		}
		output = append(output, x)
	}
	if err := rows.Err(); err != nil {
		drinkingError(w, err)
		return
	}
	drinkingJSON(w, http.StatusOK, output)
}
func (h *drinkingWaterHandler) System(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(r.PathValue("id"))
	if id == "" {
		http.Error(w, "missing system ID", http.StatusBadRequest)
		return
	}
	ctx, cancel := contextWithTimeout(r)
	defer cancel()
	type system struct {
		ID         string `json:"id"`
		Name       string `json:"name"`
		County     string `json:"county"`
		Population *int   `json:"population"`
		FIPS       string `json:"fips"`
	}
	var x system
	err := h.db.QueryRowContext(ctx, `SELECT pws_id,name,COALESCE(county,''),population,COALESCE(fips,'') FROM drinking_water_systems WHERE pws_id=$1`, id).Scan(&x.ID, &x.Name, &x.County, &x.Population, &x.FIPS)
	if err == sql.ErrNoRows {
		http.Error(w, "water system not found", http.StatusNotFound)
		return
	}
	if err != nil {
		drinkingError(w, err)
		return
	}
	drinkingJSON(w, http.StatusOK, x)
}
func (h *drinkingWaterHandler) Contaminants(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(r.PathValue("id"))
	if id == "" {
		http.Error(w, "missing system ID", http.StatusBadRequest)
		return
	}
	ctx, cancel := contextWithTimeout(r)
	defer cancel()
	rows, err := h.db.QueryContext(ctx, `SELECT contaminant_name,COALESCE(contaminant_code,''),summary_period,
 COALESCE(mean_raw,''),COALESCE(max_raw,''),COALESCE(range_of_mean,''),COALESCE(range_of_max,''),num_samples,num_nondetects,COALESCE(unit,'')
 FROM drinking_water_contaminants WHERE pws_id=$1 ORDER BY LEFT(summary_period,4) DESC,summary_period DESC,contaminant_name LIMIT 3000`, id)
	if err != nil {
		drinkingError(w, err)
		return
	}
	defer rows.Close()
	type reading struct {
		Contaminant   string `json:"contaminant"`
		Code          string `json:"code"`
		Period        string `json:"period"`
		Mean          string `json:"mean"`
		Max           string `json:"max"`
		RangeOfMean   string `json:"rangeOfMean"`
		RangeOfMax    string `json:"rangeOfMax"`
		NumSamples    *int   `json:"numSamples"`
		NumNonDetects *int   `json:"numNonDetects"`
		Unit          string `json:"unit"`
	}
	output := []reading{}
	for rows.Next() {
		var x reading
		err = rows.Scan(&x.Contaminant, &x.Code, &x.Period, &x.Mean, &x.Max, &x.RangeOfMean, &x.RangeOfMax, &x.NumSamples, &x.NumNonDetects, &x.Unit)
		if err != nil {
			drinkingError(w, err)
			return
		}
		output = append(output, x)
	}
	if err = rows.Err(); err != nil {
		drinkingError(w, err)
		return
	}
	drinkingJSON(w, http.StatusOK, struct {
		SystemID string    `json:"systemId"`
		Source   string    `json:"source"`
		Note     string    `json:"note"`
		Records  []reading `json:"records"`
	}{id, "NYSDOH Drinking Water Contaminants Data", "Historical system-level summaries, not current tap-water measurements. Annual and quarterly records may overlap. Some values may reflect untreated or out-of-service sources.", output})
}
func escapeLike(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}
func contextWithTimeout(r *http.Request) (context.Context, context.CancelFunc) {
	return context.WithTimeout(r.Context(), 15*time.Second)
}
func drinkingJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
func drinkingError(w http.ResponseWriter, err error) {
	http.Error(w, "drinking water database query failed", http.StatusInternalServerError)
}
