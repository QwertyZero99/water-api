package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// ZIP lookup uses an approximate ZIP-center point, not an address or full ZIP polygon.
// Service areas are from EPA's national public-water-system boundary layer.
// A returned candidate is NOT proof that it supplies a particular home.
const epaServiceAreaQueryURL = "https://services.arcgis.com/cJ9YHowT8TU7DUyn/ArcGIS/rest/services/Water_System_Boundaries/FeatureServer/0/query"

var fiveDigitZIP = regexp.MustCompile(`^\d{5}$`)
var drinkingZIPClient = &http.Client{Timeout: 12 * time.Second}

type drinkingZIPSystem struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	County         string `json:"county"`
	Population     *int   `json:"population"`
	BoundarySource string `json:"boundarySource,omitempty"`
}

func fetchLimitedJSON(ctx context.Context, address string, target any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	response, err := drinkingZIPClient.Do(req)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("lookup service status %d", response.StatusCode)
	}
	return json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(target)
}

func (h *drinkingWaterHandler) SearchByZIP(w http.ResponseWriter, r *http.Request) {
	zipCode := strings.TrimSpace(r.URL.Query().Get("zip"))
	if !fiveDigitZIP.MatchString(zipCode) {
		http.Error(w, "enter a five-digit ZIP code", http.StatusBadRequest)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()

	var location struct {
		Places []struct {
			Latitude  string `json:"latitude"`
			Longitude string `json:"longitude"`
			State     string `json:"state abbreviation"`
		} `json:"places"`
	}
	err := fetchLimitedJSON(ctx, "https://api.zippopotam.us/us/"+url.PathEscape(zipCode), &location)
	if err != nil {
		http.Error(w, "ZIP lookup temporarily unavailable", http.StatusBadGateway)
		return
	}
	var latitude, longitude float64
	foundNY := false
	for _, place := range location.Places {
		if place.State != "NY" {
			continue
		}
		latitude, err = strconv.ParseFloat(place.Latitude, 64)
		if err != nil {
			continue
		}
		longitude, err = strconv.ParseFloat(place.Longitude, 64)
		if err == nil {
			foundNY = true
			break
		}
	}
	if !foundNY {
		http.Error(w, "ZIP code is not a valid New York ZIP", http.StatusBadRequest)
		return
	}

	params := url.Values{}
	params.Set("f", "json")
	params.Set("where", "1=1")
	params.Set("geometry", fmt.Sprintf("%.7f,%.7f", longitude, latitude))
	params.Set("geometryType", "esriGeometryPoint")
	params.Set("inSR", "4326")
	params.Set("spatialRel", "esriSpatialRelIntersects")
	params.Set("outFields", "PWSID,PWS_Name,Data_Provider_Type,Model_Method")
	params.Set("returnGeometry", "false")
	params.Set("resultRecordCount", "100")
	var epa struct {
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
		Features []struct {
			Attributes struct {
				PWSID    string `json:"PWSID"`
				Name     string `json:"PWS_Name"`
				Provider string `json:"Data_Provider_Type"`
				Method   string `json:"Model_Method"`
			} `json:"attributes"`
		} `json:"features"`
	}
	err = fetchLimitedJSON(ctx, epaServiceAreaQueryURL+"?"+params.Encode(), &epa)
	if err != nil || epa.Error != nil {
		http.Error(w, "EPA service-area lookup temporarily unavailable", http.StatusBadGateway)
		return
	}

	// Only return verified PWSID matches in our existing NYSDOH dataset.
	systems := make([]drinkingZIPSystem, 0)
	seen := make(map[string]bool)
	for _, feature := range epa.Features {
		id := strings.ToUpper(strings.TrimSpace(feature.Attributes.PWSID))
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		var s drinkingZIPSystem
		err := h.db.QueryRowContext(ctx, `SELECT pws_id,name,COALESCE(county,''),population FROM drinking_water_systems WHERE UPPER(pws_id)=$1`, id).Scan(&s.ID, &s.Name, &s.County, &s.Population)
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			http.Error(w, "lookup timed out", http.StatusGatewayTimeout)
			return
		}
		if err != nil {
			continue
		} // EPA can include systems not present in AquaNY.
		s.BoundarySource = strings.TrimSpace(feature.Attributes.Provider)
		if s.BoundarySource == "" {
			s.BoundarySource = strings.TrimSpace(feature.Attributes.Method)
		}
		systems = append(systems, s)
	}
	sort.Slice(systems, func(i, j int) bool { return systems[i].Name < systems[j].Name })
	drinkingJSON(w, http.StatusOK, map[string]any{
		"zip":     zipCode,
		"method":  "EPA service areas intersecting approximate ZIP center",
		"note":    "Note: ZIP-based results are approximate and may not identify your actual water provider. For more accurate results, search using your public water system ID (PWSID) below.",
		"systems": systems,
	})
}
