package main

import (
	"fmt"
	"log"
	"net/http"
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

	// USGS recommends not repeatedly fetching the same data more
	// frequently than hourly.
	tileCacheDuration = 1 * time.Hour
)

var (
	httpClient = &http.Client{
		Timeout: 30 * time.Second,
	}
)

func main() {
	// Static filesystems
	fs := http.FileServer(http.Dir("./static"))
	http.Handle("/static/", http.StripPrefix("/static/", fs))

	jsFS := http.FileServer(http.Dir("./js"))
	http.Handle("/js/", http.StripPrefix("/js/", jsFS))

	// Handlers for page endpoints
	http.HandleFunc("/", homeHandler)
	http.HandleFunc("/map", mapHandler)

	// Run server
	fmt.Println("Server running at http://localhost:8080")
	log.Fatal(http.ListenAndServe(":8080", nil))
}

func homeHandler(w http.ResponseWriter, r *http.Request) {
	http.ServeFile(w, r, "html/index.html")
}

func mapHandler(w http.ResponseWriter, r *http.Request) {
	http.ServeFile(w, r, "html/map.html")
}
