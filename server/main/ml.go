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

const maxResponseBytes = 10 << 20 // 10 MiB

type ForecastHandlers struct {
baseURL string
client  *http.Client
logger  *log.Logger
}

func NewForecastHandlers(baseURL string) ForecastHandlers {
if baseURL == "" {
baseURL = os.Getenv("WQ_API_URL")
}
if baseURL == "" {
baseURL = "http://localhost:8000"
}

return ForecastHandlers{
baseURL: strings.TrimRight(baseURL, "/"),
client: &http.Client{
Timeout: 30 * time.Second,
},
logger: log.Default(),
}
}

// ---------- Public API response types ----------

type ForecastResponse struct {
StationID   string                `json:"stationId"`
AsOf        string                `json:"asOf"`
DataAgeDays *int                  `json:"dataAgeDays,omitempty"`
Stale       bool                  `json:"stale"`
Forecasts   []Prediction          `json:"forecasts"`
}

type Prediction struct {
Parameter        string             `json:"parameter"`
Unit             *string            `json:"unit,omitempty"`
HorizonDays      int                `json:"horizonDays"`
Date             string             `json:"date"`
Value            *float64           `json:"value"`
Interval         *PredictionRange   `json:"interval,omitempty"`
Exceedance       *float64           `json:"exceedanceProbability,omitempty"`
Threshold        *float64           `json:"screeningThreshold,omitempty"`
ExceedsThreshold *bool              `json:"exceedsThreshold,omitempty"`
Status           string             `json:"status"`
DataAgeDays      *float64           `json:"dataAgeDays,omitempty"`
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

// ---------- Python API response types ----------

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
Threshold        map[string]any    `json:"threshold"`
Reliability      pythonReliability `json:"reliability"`
}

type pythonReliability struct {
Confidence          string   `json:"confidence"`
Caveats             []string `json:"caveats"`
MAEKnownStations    *float64 `json:"mae_known_stations"`
MAENewStations      *float64 `json:"mae_new_stations"`
SkillVsPersistence  *float64 `json:"skill_vs_persistence"`
SkillVsClimatology  *float64 `json:"skill_vs_climatology"`
IntervalCoverage    *float64 `json:"interval_coverage"`
AUC                 *float64 `json:"auc"`
}

// ---------- Python request types ----------

// Matches Python NewStationIn.
type NewStationRequest struct {
Latitude          float64           `json:"latitude"`
Longitude         float64           `json:"longitude"`
SiteType          string            `json:"site_type,omitempty"`
HUC               *string           `json:"huc,omitempty"`
ElevationM        *float64          `json:"elevation_m,omitempty"`
DrainageAreaKM2   *float64          `json:"drainage_area_km2,omitempty"`
Observations      []Observation     `json:"observations"`
}

type Observation struct {
Date      string  `json:"date"`
Parameter string  `json:"parameter"`
Value     float64 `json:"value"`
Unit      string  `json:"unit"`
}

type pythonNewStationResponse struct {
DataBasis string               `json:"data_basis"`
DataDate  string               `json:"data_date"`
Forecasts []pythonForecastItem `json:"forecasts"`
}

// Matches Python BatchIn / BatchItemIn.
type BatchRequest struct {
Stations []BatchStation `json:"stations"`
}

type BatchStation struct {
ID         string              `json:"id"`
NewStation *NewStationRequest  `json:"new_station,omitempty"`
}

type pythonBatchResponse struct {
NOK     int                 `json:"n_ok"`
NError  int                 `json:"n_error"`
Results []pythonBatchResult `json:"results"`
}

type pythonBatchResult struct {
ID        string               `json:"id"`
Status    string               `json:"status"`
Source    *string              `json:"source"`
Error     *string              `json:"error"`
DataDate  *string              `json:"data_date"`
DataAge   *int                 `json:"data_age_days"`
Stale     *bool                `json:"stale"`
DataBasis *string              `json:"data_basis"`
Forecasts []pythonForecastItem `json:"forecasts"`
}

// ---------- Common HTTP utilities ----------

func writeJSON(w http.ResponseWriter, status int, value any) {
w.Header().Set("Content-Type", "application/json")
w.WriteHeader(status)

if err := json.NewEncoder(w).Encode(value); err != nil {
log.Printf("ERROR encoding public JSON response: %v", err)
}
}

func writeJSONError(w http.ResponseWriter, status int, err error) {
writeJSON(w, status, map[string]string{
"error": err.Error(),
})
}

func (h ForecastHandlers) upstreamURL(path string, query url.Values) (string, error) {
endpoint, err := url.JoinPath(h.baseURL, path)
if err != nil {
return "", fmt.Errorf("build upstream URL: %w", err)
}

if len(query) > 0 {
endpoint += "?" + query.Encode()
}

return endpoint, nil
}

func (h ForecastHandlers) doJSON(
ctx context.Context,
method string,
path string,
query url.Values,
payload any,
dst any,
) (int, error) {
start := time.Now()

endpoint, err := h.upstreamURL(path, query)
if err != nil {
return 0, err
}

var body io.Reader
if payload != nil {
data, err := json.Marshal(payload)
if err != nil {
return 0, fmt.Errorf("encode request: %w", err)
}

// Log the request size and endpoint, not the observation data.
h.logger.Printf(
"DEBUG upstream request payload_bytes=%d method=%s path=%s",
len(data), method, path,
)
body = bytes.NewReader(data)
}

req, err := http.NewRequestWithContext(ctx, method, endpoint, body)
if err != nil {
return 0, fmt.Errorf("create upstream request: %w", err)
}

req.Header.Set("Accept", "application/json")
if payload != nil {
req.Header.Set("Content-Type", "application/json")
}

h.logger.Printf(
"INFO upstream request start method=%s path=%s query=%q",
method, path, query.Encode(),
)

resp, err := h.client.Do(req)
if err != nil {
h.logger.Printf(
"ERROR upstream transport failure method=%s path=%s elapsed_ms=%d error=%q",
method, path, time.Since(start).Milliseconds(), err,
)
return 0, fmt.Errorf("forecast service request failed: %w", err)
}
defer resp.Body.Close()

h.logger.Printf(
"INFO upstream response method=%s path=%s status=%d content_type=%q elapsed_ms=%d",
method, path, resp.StatusCode,
resp.Header.Get("Content-Type"),
time.Since(start).Milliseconds(),
)

if resp.StatusCode < 200 || resp.StatusCode >= 300 {
// Read a bounded amount for diagnostic logging.
snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
h.logger.Printf(
"WARN upstream HTTP error method=%s path=%s status=%d body_preview=%q",
method, path, resp.StatusCode, string(snippet),
)

return resp.StatusCode, fmt.Errorf(
"upstream returned HTTP %d", resp.StatusCode,
)
}

limited := io.LimitReader(resp.Body, maxResponseBytes+1)
data, err := io.ReadAll(limited)
if err != nil {
return resp.StatusCode, fmt.Errorf("read upstream response: %w", err)
}

if len(data) > maxResponseBytes {
return resp.StatusCode, errors.New("upstream response exceeds size limit")
}

if err := json.Unmarshal(data, dst); err != nil {
h.logger.Printf(
"ERROR upstream JSON decode failure method=%s path=%s bytes=%d error=%q body_preview=%q",
method, path, len(data), err, string(data[:min(len(data), 512)]),
)
return resp.StatusCode, fmt.Errorf("decode upstream response: %w", err)
}

h.logger.Printf(
"INFO upstream request complete method=%s path=%s response_bytes=%d elapsed_ms=%d",
method, path, len(data), time.Since(start).Milliseconds(),
)

return resp.StatusCode, nil
}

// ---------- Validation and mapping ----------

func parseHorizons(raw string) (string, error) {
if strings.TrimSpace(raw) == "" {
return "", nil
}

seen := make(map[int]bool)
values := make([]string, 0)

for _, part := range strings.Split(raw, ",") {
part = strings.TrimSpace(part)
n, err := strconv.Atoi(part)
if err != nil || n < 1 || n > 365 {
return "", errors.New(
"horizons must be comma-separated integers between 1 and 365",
)
}

if !seen[n] {
seen[n] = true
values = append(values, strconv.Itoa(n))
}
}

if len(values) == 0 {
return "", errors.New("at least one valid horizon is required")
}

return strings.Join(values, ","), nil
}

func parseParameters(raw string) (string, error) {
if strings.TrimSpace(raw) == "" {
return "", nil
}

values := make([]string, 0)
seen := make(map[string]bool)

for _, part := range strings.Split(raw, ",") {
p := strings.TrimSpace(part)
if p == "" {
return "", errors.New(
"parameters must be a comma-separated list without empty values",
)
}
if !seen[p] {
seen[p] = true
values = append(values, p)
}
}

return strings.Join(values, ","), nil
}

func forecastQuery(r *http.Request) (url.Values, error) {
q := url.Values{}

horizons, err := parseHorizons(r.URL.Query().Get("horizons"))
if err != nil {
return nil, err
}
if horizons != "" {
q.Set("horizons", horizons)
}

parameters, err := parseParameters(r.URL.Query().Get("parameters"))
if err != nil {
return nil, err
}
if parameters != "" {
q.Set("parameters", parameters)
}

return q, nil
}

func floatFromThreshold(threshold map[string]any) *float64 {
if threshold == nil {
return nil
}

v, ok := threshold["value"]
if !ok {
return nil
}

switch n := v.(type) {
case float64:
return &n
case json.Number:
f, err := n.Float64()
if err == nil {
return &f
}
}
return nil
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

func mapPredictions(items []pythonForecastItem) []Prediction {
out := make([]Prediction, 0, len(items))

for _, f := range items {
p := Prediction{
Parameter:        f.Parameter,
Unit:             f.Unit,
HorizonDays:      f.HorizonDays,
Date:              f.ForecastDate,
Value:             f.Prediction,
Exceedance:        f.Probability,
Threshold:         floatFromThreshold(f.Threshold),
ExceedsThreshold:  f.ExceedsThreshold,
DataAgeDays:       f.DataAgeDays,
Status:            publicForecastStatus(f.Basis),
Reliability: ForecastReliability{
Confidence: f.Reliability.Confidence,
Caveats:    f.Reliability.Caveats,
},
}

if f.Lower != nil || f.Upper != nil {
p.Interval = &PredictionRange{
Lower: f.Lower,
Upper: f.Upper,
}
}

out = append(out, p)
}

return out
}

func mapForecastResponse(id string, r pythonForecastResponse) ForecastResponse {
return ForecastResponse{
StationID:   id,
AsOf:        r.DataDate,
DataAgeDays: r.DataAgeDays,
Stale:       r.Stale,
Forecasts:   mapPredictions(r.Forecasts),
}
}

func upstreamError(w http.ResponseWriter, status int, err error) {
switch status {
case http.StatusNotFound:
writeJSONError(w, http.StatusNotFound, errors.New("station not found"))
case http.StatusBadRequest, http.StatusUnprocessableEntity:
writeJSONError(w, http.StatusBadRequest, errors.New("invalid forecast request"))
case http.StatusTooManyRequests:
writeJSONError(w, http.StatusServiceUnavailable, errors.New("forecast service is busy"))
default:
writeJSONError(w, http.StatusBadGateway, errors.New(
"forecast service unavailable or returned an invalid response",
))
}
}

// ---------- GET: existing station forecast ----------

// GET /api/stations/{id}/forecast
func (h ForecastHandlers) ForecastByStation(w http.ResponseWriter, r *http.Request) {
start := time.Now()
id := strings.TrimSpace(r.PathValue("id"))

h.logger.Printf("INFO incoming request method=%s path=%s station_id=%q",
r.Method, r.URL.Path, id)

if r.Method != http.MethodGet {
w.Header().Set("Allow", http.MethodGet)
writeJSONError(w, http.StatusMethodNotAllowed, errors.New("method not allowed"))
return
}
if id == "" {
writeJSONError(w, http.StatusBadRequest, errors.New("station ID is required"))
return
}

query, err := forecastQuery(r)
if err != nil {
writeJSONError(w, http.StatusBadRequest, err)
return
}

path := "/sites/" + url.PathEscape(id) + "/forecast"

var result pythonForecastResponse
status, err := h.doJSON(
r.Context(), http.MethodGet, path, query, nil, &result,
)
if err != nil {
h.logger.Printf("ERROR station forecast failed station_id=%q status=%d error=%q",
id, status, err)
upstreamError(w, status, err)
return
}

response := mapForecastResponse(id, result)
h.logger.Printf(
"INFO station forecast success station_id=%q forecasts=%d stale=%t elapsed_ms=%d",
id, len(response.Forecasts), response.Stale,
time.Since(start).Milliseconds(),
)
writeJSON(w, http.StatusOK, response)
}

// ---------- POST: new station forecast ----------

// POST /api/stations/new/forecast?horizons=1,7&parameters=ph,lead
func (h ForecastHandlers) ForecastNewStation(w http.ResponseWriter, r *http.Request) {
start := time.Now()

h.logger.Printf("INFO incoming request method=%s path=%s remote=%q",
r.Method, r.URL.Path, r.RemoteAddr)

if r.Method != http.MethodPost {
w.Header().Set("Allow", http.MethodPost)
writeJSONError(w, http.StatusMethodNotAllowed, errors.New("method not allowed"))
return
}

query, err := forecastQuery(r)
if err != nil {
writeJSONError(w, http.StatusBadRequest, err)
return
}

var body NewStationRequest
decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 2<<20))
decoder.DisallowUnknownFields()

if err := decoder.Decode(&body); err != nil {
writeJSONError(w, http.StatusBadRequest, errors.New("invalid JSON request body"))
return
}

if body.Latitude < -90 || body.Latitude > 90 ||
body.Longitude < -180 || body.Longitude > 180 {
writeJSONError(w, http.StatusBadRequest, errors.New("invalid latitude or longitude"))
return
}

if body.SiteType == "" {
body.SiteType = "stream"
}
if body.Observations == nil {
body.Observations = []Observation{}
}

var result pythonNewStationResponse
status, err := h.doJSON(
r.Context(),
http.MethodPost,
"/forecast/new-station",
query,
body,
&result,
)
if err != nil {
h.logger.Printf("ERROR new station forecast failed status=%d error=%q", status, err)
upstreamError(w, status, err)
return
}

response := map[string]any{
"dataBasis": result.DataBasis,
"asOf":      result.DataDate,
"forecasts": mapPredictions(result.Forecasts),
}

h.logger.Printf(
"INFO new station forecast success forecasts=%d elapsed_ms=%d",
len(result.Forecasts), time.Since(start).Milliseconds(),
)
writeJSON(w, http.StatusOK, response)
}

// ---------- POST: batch forecasts ----------

// POST /api/stations/batch/forecast?horizons=1,7&parameters=ph,lead
func (h ForecastHandlers) ForecastBatch(w http.ResponseWriter, r *http.Request) {
start := time.Now()

h.logger.Printf("INFO incoming request method=%s path=%s remote=%q",
r.Method, r.URL.Path, r.RemoteAddr)

if r.Method != http.MethodPost {
w.Header().Set("Allow", http.MethodPost)
writeJSONError(w, http.StatusMethodNotAllowed, errors.New("method not allowed"))
return
}

query, err := forecastQuery(r)
if err != nil {
writeJSONError(w, http.StatusBadRequest, err)
return
}

var body BatchRequest
decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 5<<20))
decoder.DisallowUnknownFields()

if err := decoder.Decode(&body); err != nil {
writeJSONError(w, http.StatusBadRequest, errors.New("invalid batch JSON body"))
return
}

if len(body.Stations) < 1 || len(body.Stations) > 200 {
writeJSONError(w, http.StatusBadRequest, errors.New(
"stations must contain between 1 and 200 items",
))
return
}

for i, station := range body.Stations {
if strings.TrimSpace(station.ID) == "" {
writeJSONError(w, http.StatusBadRequest, fmt.Errorf(
"stations[%d].id is required", i,
))
return
}
}

var result pythonBatchResponse
status, err := h.doJSON(
r.Context(), http.MethodPost, "/forecast/batch", query, body, &result,
)
if err != nil {
h.logger.Printf("ERROR batch forecast failed status=%d error=%q", status, err)
upstreamError(w, status, err)
return
}

// Keep Python's per-station errors; one failure should not fail the entire batch.
publicResults := make([]map[string]any, 0, len(result.Results))
for _, item := range result.Results {
row := map[string]any{
"id":        item.ID,
"status":    item.Status,
"forecasts": mapPredictions(item.Forecasts),
}
if item.Source != nil {
row["source"] = *item.Source
}
if item.Error != nil {
row["error"] = *item.Error
}
if item.DataDate != nil {
row["asOf"] = *item.DataDate
}
if item.DataAge != nil {
row["dataAgeDays"] = *item.DataAge
}
if item.Stale != nil {
row["stale"] = *item.Stale
}
if item.DataBasis != nil {
row["dataBasis"] = *item.DataBasis
}
publicResults = append(publicResults, row)
}

h.logger.Printf(
"INFO batch forecast success n_ok=%d n_error=%d elapsed_ms=%d",
result.NOK, result.NError, time.Since(start).Milliseconds(),
)
writeJSON(w, http.StatusOK, map[string]any{
"nOk":      result.NOK,
"nError":   result.NError,
"results":  publicResults,
})
}
