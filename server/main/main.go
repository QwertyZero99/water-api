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

type Config struct {
	Port             string
	APIKey           string
	DataBasePort     string
	DataBasePassword string
}

// loadConfig loads the configuration from environment variables
func loadConfig() (Config, error) {
	cfg := Config{
		Port:             os.Getenv("PORT"),
		APIKey:           os.Getenv("API_KEY"),
		DataBasePort:     os.Getenv("DATABASE_PORT"),
		DataBasePassword: os.Getenv("DATABASE_PASSWORD"),
	}

	if cfg.Port == "" {
		return Config{}, fmt.Errorf("PORT is required")
	}
	if cfg.APIKey == "" {
		return Config{}, fmt.Errorf("API key is required")
	}

	return cfg, nil
}

func main() {
	/* CONFIG */
	var err error
	cfg, err = loadConfig()
	if err != nil {
		log.Fatalf("unable to load config: %v", err)
	}

	/* ROUTINES */
	db, err := initStationDatabase()
	if err != nil {
		log.Fatalf("failed to initialize station database: %v", err)
	}
	defer db.Close()

	stationStore = *NewStationStore(db)

	startStationUpdater(
		&stationStore,
		stationStoreUpdateInterval,
	)

	/* HANDLERS */
	// Static filesystems
	fs := http.FileServer(http.Dir("./static"))
	http.Handle("/static/", http.StripPrefix("/static/", fs))

	imageFs := http.FileServer(http.Dir("./images"))
	http.Handle("/images/", http.StripPrefix("/images/", imageFs))

	jsFS := http.FileServer(http.Dir("./js"))
	http.Handle("/js/", http.StripPrefix("/js/", jsFS))

	// The station API
	stationHandlers := NewStationHandlers(&stationStore)
	http.HandleFunc("/api/stations", stationHandlers.StationsInBoundingBox)
	http.HandleFunc("/api/stations/", stationHandlers.StationByID)

	// Handlers for page endpoints
	http.HandleFunc("/", homeHandler)
	http.HandleFunc("/map", mapHandler)

	// Run server
	fmt.Printf("Server running at http://localhost:%s\n", cfg.Port)
	fmt.Printf("API Key: %s\n", cfg.APIKey)
	log.Fatal(http.ListenAndServe(":"+cfg.Port, nil))
}

// homeHandler handles the homepage
func homeHandler(w http.ResponseWriter, r *http.Request) {
	http.ServeFile(w, r, "html/index.html")
}

// mapHandler handles the map page
func mapHandler(w http.ResponseWriter, r *http.Request) {
	http.ServeFile(w, r, "html/map.html")
}
