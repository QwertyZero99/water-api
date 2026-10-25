package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
)

const (
	UsgsBaseURL = "https://api.waterdata.usgs.gov/ogcapi/v1"
)

var (
	cfg          Config
	stationStore StationStore
)

// Config stores application configuration, which is extracted from environment variables
type Config struct {
	Port               string
	APIKey             string
	DataBasePort       string
	DataBaseUsername   string
	DataBasePassword   string
	UpdateStationsFlag string
}

// loadConfig loads the configuration from environment variables
func loadConfig() (Config, error) {
	cfg := Config{
		Port:               os.Getenv("PORT"),
		APIKey:             os.Getenv("API_KEY"),
		DataBasePort:       os.Getenv("DATABASE_PORT"),
		DataBaseUsername:   os.Getenv("DATABASE_USERNAME"),
		DataBasePassword:   os.Getenv("DATABASE_PASSWORD"),
		UpdateStationsFlag: os.Getenv("UPDATE_STATIONS"),
	}

	if cfg.Port == "" {
		return Config{}, fmt.Errorf("PORT is required")
	}
	if cfg.APIKey == "" {
		return Config{}, fmt.Errorf("API_KEY is required")
	}
	if cfg.DataBasePort == "" {
		log.Println("DATABASE_PORT not set, assuming 5432")
		cfg.DataBasePort = "5432"
	}
	if cfg.DataBaseUsername == "" {
		log.Println("DATABASE_USERNAME not set, assuming \"postgres\"")
		cfg.DataBasePort = "postgres"
	}
	if cfg.DataBasePassword == "" {
		return Config{}, fmt.Errorf("DATABASE_PASSWORD is required")
	}
	if cfg.UpdateStationsFlag == "" {
		log.Println("UPDATE_STATIONS not set, assuming \"YES\"")
		cfg.DataBasePort = "YES"
	}

	return cfg, nil
}

func main() {
	var err error
	cfg, err = loadConfig()
	if err != nil {
		log.Fatalf("unable to load config: %v", err)
	}

	db, err := initStationDatabase()
	if err != nil {
		log.Fatalf("failed to initialize station database: %v", err)
	}
	log.Println("Successfully connected to the database.")
	defer db.Close()

	stationStore = *NewStationStore(db)

	if cfg.UpdateStationsFlag == "YES" {
		log.Println("UPDATE_STATIONS set to \"YES\", will periodically update stations.")
		startStationUpdater(
			&stationStore,
			stationStoreUpdateInterval,
		)
	} else {
		log.Println("UPDATE_STATIONS not set to \"YES\", will NOT periodically update stations.")
	}

	/* HANDLERS  Everytime you add go file or api add handler here*/
	fs := http.FileServer(http.Dir("../static"))
	http.Handle("/static/", http.StripPrefix("/static/", fs))

	imageFs := http.FileServer(http.Dir("../images"))
	http.Handle("/images/", http.StripPrefix("/images/", imageFs))

	jsFs := http.FileServer(http.Dir("../js"))
	http.Handle("/js/", http.StripPrefix("/js/", jsFs))
	http.HandleFunc(
		"GET /api/waterbodies/{id}",
		WaterbodyByID,
	)

	http.HandleFunc("GET /api/waterbodies/nearby", WaterbodiesNearby)
	http.HandleFunc(
		"GET /api/waterbodies/search",
		WaterbodySearch,
	)

	drinking := NewDrinkingWaterHandler(db)
	http.HandleFunc(
		"GET /api/drinking-water/search",
		drinking.Search,
	)
	http.HandleFunc("GET /api/drinking-water/by-zip", drinking.SearchByZIP)

	http.HandleFunc(
		"GET /api/drinking-water/systems/{id}",
		drinking.System,
	)

	http.HandleFunc(
		"GET /api/drinking-water/systems/{id}/contaminants",
		drinking.Contaminants,
	)

	pfasHandler := NewPFASHandler(db)
	http.HandleFunc(
		"GET /api/waterbodies/{id}/pfas",
		pfasHandler.WaterbodyPFAS,
	)
	http.HandleFunc("GET /api/waterbodies/{id}/epa-pfas", WaterbodyEPAPFAS)

	stationHandlers := NewStationHandlers(&stationStore)
	http.HandleFunc("/api/stations", stationHandlers.StationsInBoundingBox)
	http.HandleFunc(
		"GET /api/stations/availability",
		MeasurementAvailability,
	)

	http.HandleFunc(
		"GET /api/stations/{id}/measurements",
		stationHandlers.StationMeasurements,
	)
	http.HandleFunc(
		"GET /api/waterbodies/{id}/conditions",
		WaterbodyConditions,
	)
	http.HandleFunc("/api/stations/", stationHandlers.StationByID)

	// Water quality forecast API (backed by the internal Python service).
	forecastHandlers := NewForecastHandlers(
		os.Getenv("WQ_API_URL"),
	)

	http.HandleFunc(
		"GET /api/stations/{id}/forecast",
		forecastHandlers.ForecastByStation,
	)

	http.HandleFunc("/", homeHandler)
	http.HandleFunc("/map", mapHandler)
	http.HandleFunc("/info", infoHandler)

	// This runs server, I added running confirmation message
	fmt.Printf("Server running at http://localhost:%s\n", cfg.Port)
	log.Fatal(http.ListenAndServe(":"+cfg.Port, nil))
}

// this is for homepage (index.html)
func homeHandler(w http.ResponseWriter, r *http.Request) {
	log.Println("Served /")
	http.ServeFile(w, r, "../html/index.html")
}

// mappage(map.html)
func mapHandler(w http.ResponseWriter, r *http.Request) {
	log.Println("Served /map")
	http.ServeFile(w, r, "../html/map.html")
}

// info page(info.html)
func infoHandler(w http.ResponseWriter, r *http.Request) {
	log.Println("Served /info")
	http.ServeFile(w, r, "../html/info.html")
}
