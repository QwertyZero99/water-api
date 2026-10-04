package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"
)

const (
	latestURL     = "https://api.waterdata.usgs.gov/ogcapi/v0/collections/latest-continuous/items"
	parameterURL  = "https://api.waterdata.usgs.gov/ogcapi/v0/collections/parameter-codes/items"
	cacheDuration = 1 * time.Hour
)

var httpClient = &http.Client{
	Timeout: 30 * time.Second,
}

type LatestResponse struct {
	Features []LatestFeature `json:"features"`
}

type LatestFeature struct {
	Geometry struct {
		Coordinates []float64 `json:"coordinates"`
	} `json:"geometry"`

	Properties struct {
		MonitoringLocationID   string  `json:"monitoring_location_id"`
		MonitoringLocationName string  `json:"monitoring_location_name"`
		ParameterCode          string  `json:"parameter_code"`
		Value                  *string `json:"value"`
		UnitOfMeasure          string  `json:"unit_of_measure"`
		Time                   string  `json:"time"`
		StateCode              string  `json:"state_code"`
	} `json:"properties"`
}

type ParameterResponse struct {
	Features []ParameterFeature `json:"features"`
}

type ParameterFeature struct {
	Properties struct {
		ID                   string `json:"id"`
		ParameterName        string `json:"parameter_name"`
		ParameterDescription string `json:"parameter_description"`
		UnitOfMeasure        string `json:"unit_of_measure"`
	} `json:"properties"`
}

type ParameterInfo struct {
	Name        string
	Description string
}

type CollectedParameter struct {
	Code        string `json:"code"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Value       string `json:"value"`
	Unit        string `json:"unit"`
	Time        string `json:"time"`
}

type Measurement struct {
	Value float64 `json:"value"`
}

type StationMeasurements struct {
	PH              Measurement `json:"ph"`
	DissolvedOxygen Measurement `json:"dissolved_oxygen"`
	Turbidity       Measurement `json:"turbidity"`
	Nitrate         Measurement `json:"nitrate"`
}

type Station struct {
	ID           string               `json:"id"`
	Lat          float64              `json:"lat"`
	Lng          float64              `json:"lng"`
	Name         string               `json:"name"`
	Value        string               `json:"value"`
	Time         string               `json:"time"`
	Parameters   []CollectedParameter `json:"parameters"`
	Measurements StationMeasurements  `json:"measurements"`
}

type StationResponse struct {
	Stations []Station `json:"stations"`
}

var (
	cachedStations []Station
	lastCacheTime  time.Time
)

func main() {
	fs := http.FileServer(http.Dir("./static"))
	http.Handle("/static/", http.StripPrefix("/static/", fs))

	jsFS := http.FileServer(http.Dir("./js"))
	http.Handle("/js/", http.StripPrefix("/js/", jsFS))

	http.HandleFunc("/data/", dataHandler)
	http.HandleFunc("/map", mapHandler)
	http.HandleFunc("/", homeHandler)

	fmt.Println("Server running at http://localhost:8080")
	log.Fatal(http.ListenAndServe(":8080", nil))
}

func homeHandler(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}

	http.ServeFile(w, r, "html/index.html")
}

func mapHandler(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/map" {
		http.NotFound(w, r)
		return
	}

	http.ServeFile(w, r, "html/map.html")
}

func dataHandler(w http.ResponseWriter, r *http.Request) {
	if time.Since(lastCacheTime) < cacheDuration && len(cachedStations) > 0 {
		writeStationResponse(w, cachedStations)
		return
	}

	parameterNames, err := fetchParameterNames()
	if err != nil {
		log.Printf("Failed to fetch parameter names: %v", err)
	}

	usgsURL := latestURL + "?f=json&state_code=36&limit=10000"

	resp, err := httpClient.Get(usgsURL)
	if err != nil {
		log.Printf("Failed to fetch USGS data: %v", err)
		http.Error(w, "Failed to fetch USGS data", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		log.Printf("USGS returned status: %s", resp.Status)
		http.Error(w, "USGS API returned an error", http.StatusBadGateway)
		return
	}

	var data LatestResponse

	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		log.Printf("Failed to parse USGS response: %v", err)
		http.Error(w, "Failed to parse USGS response", http.StatusInternalServerError)
		return
	}

	stationMap := make(map[string]*Station)
	hasStreamflow := make(map[string]bool)

	for _, feature := range data.Features {
		props := feature.Properties
		measurementTime, err := time.Parse(time.RFC3339, props.Time)
		if err != nil {
			continue
		}

		if time.Since(measurementTime) > 7*24*time.Hour {
			continue
		}

		if props.MonitoringLocationID == "" {
			continue
		}

		if len(feature.Geometry.Coordinates) < 2 {
			continue
		}

		if props.Value == nil {
			continue
		}

		lng := feature.Geometry.Coordinates[0]
		lat := feature.Geometry.Coordinates[1]

		station, exists := stationMap[props.MonitoringLocationID]

		if !exists {
			station = &Station{
				ID:         props.MonitoringLocationID,
				Lat:        lat,
				Lng:        lng,
				Name:       props.MonitoringLocationName,
				Parameters: []CollectedParameter{},
				Measurements: StationMeasurements{
					PH:              Measurement{Value: 7.2},
					DissolvedOxygen: Measurement{Value: 10.0},
					Turbidity:       Measurement{Value: 2.5},
					Nitrate:         Measurement{Value: 1.0},
				},
			}

			stationMap[props.MonitoringLocationID] = station
		}

		info := parameterNames[props.ParameterCode]

		name := info.Name
		if name == "" {
			name = "Parameter " + props.ParameterCode
		}

		unit := props.UnitOfMeasure

		station.Parameters = append(
			station.Parameters,
			CollectedParameter{
				Code:        props.ParameterCode,
				Name:        name,
				Description: info.Description,
				Value:       *props.Value,
				Unit:        unit,
				Time:        props.Time,
			},
		)

		if props.ParameterCode == "00060" {
			hasStreamflow[props.MonitoringLocationID] = true

			station.Value = *props.Value

			if unit != "" {
				station.Value += " " + unit
			}

			station.Time = props.Time
		}
	}

	stations := make([]Station, 0)

	for id, station := range stationMap {
		if hasStreamflow[id] {
			stations = append(stations, *station)
		}
	}

	log.Printf("Loaded %d streamflow stations", len(stations))

	cachedStations = stations
	lastCacheTime = time.Now()

	writeStationResponse(w, stations)
}

func fetchParameterNames() (map[string]ParameterInfo, error) {
	url := parameterURL + "?f=json&limit=10000"

	resp, err := httpClient.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("parameter API returned %s", resp.Status)
	}

	var data ParameterResponse

	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, err
	}

	parameters := make(map[string]ParameterInfo)

	for _, feature := range data.Features {
		props := feature.Properties

		parameters[props.ID] = ParameterInfo{
			Name:        props.ParameterName,
			Description: props.ParameterDescription,
		}
	}

	return parameters, nil
}

func writeStationResponse(w http.ResponseWriter, stations []Station) {
	w.Header().Set("Content-Type", "application/json")

	if err := json.NewEncoder(w).Encode(
		StationResponse{
			Stations: stations,
		},
	); err != nil {
		log.Printf("Failed to encode response: %v", err)
	}
}
