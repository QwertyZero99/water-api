package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"time"
)

type USGSResponse struct {
	Value struct {
		TimeSeries []struct {
			SourceInfo struct {
				SiteName    string `json:"siteName"`
				GeoLocation struct {
					GeogLocation struct {
						Latitude  float64 `json:"latitude"`
						Longitude float64 `json:"longitude"`
					} `json:"geogLocation"`
				} `json:"geoLocation"`
			} `json:"sourceInfo"`

			Values []struct {
				Value []struct {
					Value    string `json:"value"`
					DateTime string `json:"dateTime"`
				} `json:"value"`
			} `json:"values"`
		} `json:"timeSeries"`
	} `json:"value"`
}

const (
	usgsBaseURL = "https://waterservices.usgs.gov/nwis/iv/"

	// USGS recommends not repeatedly fetching the same data more frequently than hourly.
	cacheDuration = 1 * time.Hour
)

var (
	cfg        Config
	httpClient = &http.Client{
		Timeout: 30 * time.Second,
	}
)

type Config struct {
	Port        string
	DatabaseURL string
	APIKey      string
}

func loadConfig() (Config, error) {
	cfg := Config{
		Port:        os.Getenv("PORT"),
		DatabaseURL: os.Getenv("DATABASE_URL"),
		APIKey:      os.Getenv("API_KEY"),
	}

	if cfg.Port == "" {
		return Config{}, fmt.Errorf("PORT is required")
	}

	/*
		if cfg.DatabaseURL == "" {
			return Config{}, fmt.Errorf("DATABASE_URL is required")
		}
	*/

	return cfg, nil
}

func main() {
	// Env variables
	var err error
	cfg, err = loadConfig()
	if err != nil {
		log.Fatalf("unable to load config: %v", err)
	}

	// Static filesystems
	fs := http.FileServer(http.Dir("./static"))
	http.Handle("/static/", http.StripPrefix("/static/", fs))

	imageFs := http.FileServer(http.Dir("./images"))
	http.Handle("/images/", http.StripPrefix("/images/", imageFs))

	jsFS := http.FileServer(http.Dir("./js"))
	http.Handle("/js/", http.StripPrefix("/js/", jsFS))

	// Handlers for page endpoints
	http.HandleFunc("/", homeHandler)
	http.HandleFunc("/map", mapHandler)

	// Run server
	fmt.Printf("Server running at http://localhost:%s\n", cfg.Port)
	log.Fatal(http.ListenAndServe(":"+cfg.Port, nil))
}

func homeHandler(w http.ResponseWriter, r *http.Request) {
	http.ServeFile(w, r, "html/index.html")
}

func mapHandler(w http.ResponseWriter, r *http.Request) {
	http.ServeFile(w, r, "html/map.html")
}
