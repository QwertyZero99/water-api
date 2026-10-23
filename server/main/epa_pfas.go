package main

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// EPA Water Quality Portal: sampled PFAS results, not continuous/current readings.
// Cache the statewide dataset so each waterbody click does NOT trigger six EPA downloads.
const epaPFASCacheTTL = 12 * time.Hour
const epaPFASMaxBytes = 32 << 20 // 32 MB per CSV response
const epaPFASMatchMeters = 120.0 // for lines; polygons require point containment

var epaPFASNames = map[string]string{
	"Perfluorooctanoic acid":        "PFOA",
	"Perfluorooctanesulfonate":      "PFOS",
	"Perfluorooctane sulfonic acid": "PFOS",
}

type epaStation struct {
	ID   string
	Name string
	Kind string
	Lat  float64
	Lon  float64
}

type epaRecord struct {
	StationID          string   `json:"stationId"`
	StationName        string   `json:"stationName"`
	Chemical           string   `json:"chemical"`
	Characteristic     string   `json:"characteristic"`
	SampleDate         string   `json:"sampleDate"`
	Value              *float64 `json:"value"`
	Unit               string   `json:"unit"`
	Nondetect          bool     `json:"nondetect"`
	DetectionCondition string   `json:"detectionCondition,omitempty"`
	ReportingLimit     string   `json:"reportingLimit,omitempty"`
	ActivityID         string   `json:"activityId,omitempty"`
	ResultID           string   `json:"resultId,omitempty"`
	Longitude          float64  `json:"longitude"`
	Latitude           float64  `json:"latitude"`
	Source             string   `json:"source"`
}

type epaDataset struct {
	Stations map[string]epaStation
	Records  []epaRecord
}

var epaPFASCache = struct {
	sync.Mutex
	Fetched time.Time
	Data    epaDataset
}{}

func epaCSV(ctx context.Context, profile, chemical string) ([][]string, error) {
	q := url.Values{}
	q.Set("statecode", "US:36")
	q.Set("characteristicName", chemical)
	q.Set("sampleMedia", "Water")
	q.Set("mimeType", "csv")
	q.Set("zip", "no")
	endpoint := "https://www.waterqualitydata.us/data/" + profile + "/search?" + q.Encode()
	ctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, "GET", endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "text/csv")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 400))
		return nil, fmt.Errorf("EPA %s %s: HTTP %d: %s", profile, chemical, resp.StatusCode, string(b))
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, epaPFASMaxBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > epaPFASMaxBytes {
		return nil, errors.New("EPA CSV exceeds size limit")
	}
	reader := csv.NewReader(strings.NewReader(strings.TrimPrefix(string(data), "\ufeff")))
	reader.FieldsPerRecord = -1
	return reader.ReadAll()
}

func epaRows(records [][]string) []map[string]string {
	if len(records) < 2 {
		return nil
	}
	headers := records[0]
	out := make([]map[string]string, 0, len(records)-1)
	for _, record := range records[1:] {
		row := make(map[string]string, len(headers))
		for i, h := range headers {
			if i < len(record) {
				row[strings.TrimSpace(h)] = strings.TrimSpace(record[i])
			}
		}
		out = append(out, row)
	}
	return out
}

func epaSurfaceType(kind string) bool {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "river/stream", "stream", "lake", "great lake", "lake, reservoir, impoundment", "estuary", "ocean":
		return true
	default:
		return false
	}
}
func epaFloat(s string) (float64, bool) {
	v, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	return v, err == nil && !math.IsNaN(v) && !math.IsInf(v, 0)
}
func epaField(row map[string]string, names ...string) string {
	for _, name := range names {
		if v := row[name]; v != "" {
			return v
		}
	}
	return ""
}

func fetchEPAPFAS(ctx context.Context) (epaDataset, error) {
	dataset := epaDataset{Stations: make(map[string]epaStation), Records: []epaRecord{}}
	names := []string{"Perfluorooctanoic acid", "Perfluorooctanesulfonate", "Perfluorooctane sulfonic acid"}
	for _, chemical := range names {
		stationsCSV, err := epaCSV(ctx, "Station", chemical)
		if err != nil {
			return epaDataset{}, err
		}
		allowed := make(map[string]bool)
		for _, row := range epaRows(stationsCSV) {
			id := row["MonitoringLocationIdentifier"]
			kind := row["MonitoringLocationTypeName"]
			lat, okLat := epaFloat(row["LatitudeMeasure"])
			lon, okLon := epaFloat(row["LongitudeMeasure"])
			if id == "" || !epaSurfaceType(kind) || !okLat || !okLon || lat < 40 || lat > 46 || lon < -80 || lon > -71 {
				continue
			}
			allowed[id] = true
			dataset.Stations[id] = epaStation{ID: id, Name: row["MonitoringLocationName"], Kind: kind, Lat: lat, Lon: lon}
		}
		resultsCSV, err := epaCSV(ctx, "Result", chemical)
		if err != nil {
			return epaDataset{}, err
		}
		for _, row := range epaRows(resultsCSV) {
			id := row["MonitoringLocationIdentifier"]
			if !allowed[id] || row["CharacteristicName"] != chemical {
				continue
			}
			station := dataset.Stations[id]
			// Don't treat all 'Water' media as ambient surface-water: monitoring location must also be surface water.
			if medium := row["ActivityMediaName"]; medium != "" && !strings.EqualFold(medium, "Water") {
				continue
			}
			rawValue := row["ResultMeasureValue"]
			var val *float64
			if v, ok := epaFloat(rawValue); ok {
				val = &v
			}
			condition := row["ResultDetectionConditionText"]
			nondetect := condition != "" && (strings.Contains(strings.ToLower(condition), "not detected") || strings.Contains(strings.ToLower(condition), "below detection") || strings.Contains(strings.ToLower(condition), "not quantified"))
			if val == nil && condition == "" {
				continue
			}
			dataset.Records = append(dataset.Records, epaRecord{
				StationID: id, StationName: station.Name, Chemical: epaPFASNames[chemical], Characteristic: chemical,
				SampleDate: row["ActivityStartDate"], Value: val, Unit: epaField(row, "ResultMeasure/MeasureUnitCode"),
				Nondetect: nondetect, DetectionCondition: condition,
				ReportingLimit: epaField(row, "DetectionQuantitationLimitMeasure/MeasureValue", "ResultDetectionQuantitationLimitMeasure/MeasureValue"),
				ActivityID:     row["ActivityIdentifier"], ResultID: row["ResultIdentifier"],
				Longitude: station.Lon, Latitude: station.Lat, Source: "EPA Water Quality Portal",
			})
		}
	}
	return dataset, nil
}

func cachedEPAPFAS(ctx context.Context) (epaDataset, error) {
	epaPFASCache.Lock()
	defer epaPFASCache.Unlock()
	if !epaPFASCache.Fetched.IsZero() && time.Since(epaPFASCache.Fetched) < epaPFASCacheTTL {
		return epaPFASCache.Data, nil
	}
	// Independent from the requesting browser's lifetime; upstream may take over a minute.
	data, err := fetchEPAPFAS(ctx)
	if err != nil {
		if !epaPFASCache.Fetched.IsZero() {
			return epaPFASCache.Data, nil
		} // stale data on upstream outage
		return epaDataset{}, err
	}
	epaPFASCache.Data, epaPFASCache.Fetched = data, time.Now()
	return data, nil
}

// Reuse the existing WaterbodyByID database handler, including special grouped IDs.
// This avoids a second, potentially inconsistent query of waterbody tables.
func epaWaterbodyGeometries(r *http.Request, id string) ([]json.RawMessage, error) {
	sub := httptest.NewRequest("GET", "/api/waterbodies/"+url.PathEscape(id), nil).WithContext(r.Context())
	sub.SetPathValue("id", id)
	writer := httptest.NewRecorder()
	WaterbodyByID(writer, sub)
	if writer.Code != http.StatusOK {
		return nil, fmt.Errorf("waterbody lookup HTTP %d", writer.Code)
	}
	var result struct {
		Segments []struct {
			Geometry json.RawMessage `json:"geometry"`
		} `json:"segments"`
		Geometry json.RawMessage `json:"geometry"`
	}
	if err := json.Unmarshal(writer.Body.Bytes(), &result); err != nil {
		return nil, err
	}
	geometries := []json.RawMessage{}
	for _, s := range result.Segments {
		if len(s.Geometry) > 0 {
			geometries = append(geometries, s.Geometry)
		}
	}
	if len(geometries) == 0 && len(result.Geometry) > 0 {
		geometries = append(geometries, result.Geometry)
	}
	return geometries, nil
}

type epaPoint struct{ Lon, Lat float64 }

func epaLineDistanceMeters(p, a, b epaPoint) float64 {
	// Equirectangular local projection (sufficient at ~100m matching scales).
	scale := math.Cos(p.Lat * math.Pi / 180)
	ax, ay := (a.Lon-p.Lon)*111320*scale, (a.Lat-p.Lat)*111320
	bx, by := (b.Lon-p.Lon)*111320*scale, (b.Lat-p.Lat)*111320
	dx, dy := bx-ax, by-ay
	t := 0.0
	if dx*dx+dy*dy > 0 {
		t = -(ax*dx + ay*dy) / (dx*dx + dy*dy)
	}
	t = math.Max(0, math.Min(1, t))
	return math.Hypot(ax+t*dx, ay+t*dy)
}
func epaRingContains(ring []epaPoint, p epaPoint) bool {
	inside := false
	for i, j := 0, len(ring)-1; i < len(ring); j, i = i, i+1 {
		a, b := ring[i], ring[j]
		if (a.Lat > p.Lat) != (b.Lat > p.Lat) && p.Lon < (b.Lon-a.Lon)*(p.Lat-a.Lat)/(b.Lat-a.Lat)+a.Lon {
			inside = !inside
		}
	}
	return inside
}
func epaPoints(raw json.RawMessage) []epaPoint {
	var coords [][]float64
	if json.Unmarshal(raw, &coords) != nil {
		return nil
	}
	out := make([]epaPoint, 0, len(coords))
	for _, v := range coords {
		if len(v) >= 2 {
			out = append(out, epaPoint{v[0], v[1]})
		}
	}
	return out
}
func epaMatchGeometry(raw json.RawMessage, p epaPoint) bool {
	var shape struct {
		Type        string            `json:"type"`
		Coordinates json.RawMessage   `json:"coordinates"`
		Geometries  []json.RawMessage `json:"geometries"`
	}
	if json.Unmarshal(raw, &shape) != nil {
		return false
	}
	switch shape.Type {
	case "LineString":
		points := epaPoints(shape.Coordinates)
		for i := 1; i < len(points); i++ {
			if epaLineDistanceMeters(p, points[i-1], points[i]) <= epaPFASMatchMeters {
				return true
			}
		}
	case "MultiLineString":
		var lines []json.RawMessage
		_ = json.Unmarshal(shape.Coordinates, &lines)
		for _, line := range lines {
			b, _ := json.Marshal(map[string]any{"type": "LineString", "coordinates": json.RawMessage(line)})
			if epaMatchGeometry(b, p) {
				return true
			}
		}
	case "Polygon":
		var rings []json.RawMessage
		_ = json.Unmarshal(shape.Coordinates, &rings)
		if len(rings) == 0 || !epaRingContains(epaPoints(rings[0]), p) {
			return false
		}
		for _, hole := range rings[1:] {
			if epaRingContains(epaPoints(hole), p) {
				return false
			}
		}
		return true
	case "MultiPolygon":
		var polygons []json.RawMessage
		_ = json.Unmarshal(shape.Coordinates, &polygons)
		for _, polygon := range polygons {
			b, _ := json.Marshal(map[string]any{"type": "Polygon", "coordinates": json.RawMessage(polygon)})
			if epaMatchGeometry(b, p) {
				return true
			}
		}
	case "GeometryCollection":
		for _, g := range shape.Geometries {
			if epaMatchGeometry(g, p) {
				return true
			}
		}
	}
	return false
}

func WaterbodyEPAPFAS(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(r.PathValue("id"))
	if id == "" || len(id) > 120 {
		writeJSONError(w, 400, fmt.Errorf("invalid waterbody ID"))
		return
	}
	geometries, err := epaWaterbodyGeometries(r, id)
	if err != nil {
		writeJSONError(w, 500, fmt.Errorf("EPA PFAS waterbody geometry: %w", err))
		return
	}
	if len(geometries) == 0 {
		writeJSON(w, 200, map[string]any{"waterbodyId": id, "samples": []epaRecord{}, "count": 0})
		return
	}
	data, err := cachedEPAPFAS(r.Context())
	if err != nil {
		writeJSONError(w, 502, fmt.Errorf("EPA PFAS upstream: %w", err))
		return
	}
	matchStation := make(map[string]bool)
	for sid, site := range data.Stations {
		p := epaPoint{site.Lon, site.Lat}
		for _, geom := range geometries {
			if epaMatchGeometry(geom, p) {
				matchStation[sid] = true
				break
			}
		}
	}
	samples := make([]epaRecord, 0)
	seen := make(map[string]bool)
	for _, sample := range data.Records {
		if !matchStation[sample.StationID] {
			continue
		}
		// Deduplicate repeated records across queries, but preserve distinct activity and result IDs.
		key := sample.ResultID
		if key == "" {
			key = sample.StationID + "|" + sample.ActivityID + "|" + sample.Characteristic + "|" + sample.SampleDate + "|" + sample.DetectionCondition
		}
		if sample.Value != nil {
			key += "|" + strconv.FormatFloat(*sample.Value, 'g', -1, 64)
		}
		if seen[key] {
			continue
		}
		seen[key] = true
		samples = append(samples, sample)
	}
	sort.Slice(samples, func(i, j int) bool { return samples[i].SampleDate > samples[j].SampleDate })
	if len(samples) > 200 {
		samples = samples[:200]
	}
	writeJSON(w, 200, map[string]any{
		"waterbodyId": id, "count": len(samples), "samples": samples,
		"source": "EPA Water Quality Portal", "historical": true,
		"note": "Historical surface-water laboratory samples at geographically associated monitoring sites. Not current conditions or a safety assessment. Match distances for stream centerlines are at most 120 m.",
	})
}
