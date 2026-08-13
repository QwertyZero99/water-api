package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

type Station struct {
	Name     string    `json:"name"`
	Lat      float64   `json:"lat"`
	Lng      float64   `json:"lng"`
	LastTime time.Time `json:"time"`
	Value    string    `json:"value"`
}

type StationChunk struct {
	Stations []Station `json:"stations"`
	LastTime time.Time `json:"time"`
}

type StationChunkIndex struct {
	TileZ int
	TileY int
	TileX int
}

type BoundingBox struct {
	West  float64
	South float64
	East  float64
	North float64
}

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

type CachedTile struct {
	Chunk    StationChunk
	CachedAt time.Time
}

const (
	usgsBaseURL = "https://waterservices.usgs.gov/nwis/iv/"

	// USGS recommends not repeatedly fetching the same data more
	// frequently than hourly.
	tileCacheDuration = 1 * time.Hour

	// Web Mercator maximum latitude.
	maxMercatorLatitude = 85.05112878

	// Maximum product of latitude range * longitude range
	// allowed by the USGS bBox filter.
	maxBBoxArea = 25.0
)

var (
	tileCache      = make(map[StationChunkIndex]CachedTile)
	tileCacheMutex sync.RWMutex

	// Prevent multiple simultaneous USGS requests for the same tile.
	tileFetchMutexes   = make(map[StationChunkIndex]*sync.Mutex)
	tileFetchMutexLock sync.Mutex

	httpClient = &http.Client{
		Timeout: 30 * time.Second,
	}
)

func main() {
	fs := http.FileServer(http.Dir("./static"))
	http.Handle("/static/", http.StripPrefix("/static/", fs))

	jsFS := http.FileServer(http.Dir("./js"))
	http.Handle("/js/", http.StripPrefix("/js/", jsFS))

	http.HandleFunc("/", homeHandler)
	http.HandleFunc("/data/", stationsHandler)

	fmt.Println("Server running at http://localhost:8080")

	log.Fatal(http.ListenAndServe(":8080", nil))
}

func homeHandler(w http.ResponseWriter, r *http.Request) {
	http.ServeFile(w, r, "index.html")
}

// GET /data/{z}/{y}/{x}
func stationsHandler(w http.ResponseWriter, r *http.Request) {
	log.Println("stations request:", r.URL.Path)

	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")

	if len(parts) != 4 || parts[0] != "data" {
		http.Error(
			w,
			"expected /data/{z}/{y}/{x}",
			http.StatusBadRequest,
		)
		return
	}

	z, err := strconv.Atoi(parts[1])
	if err != nil {
		http.Error(w, "invalid zoom level", http.StatusBadRequest)
		return
	}

	y, err := strconv.Atoi(parts[2])
	if err != nil {
		http.Error(w, "invalid tile y", http.StatusBadRequest)
		return
	}

	x, err := strconv.Atoi(parts[3])
	if err != nil {
		http.Error(w, "invalid tile x", http.StatusBadRequest)
		return
	}

	// Limit zoom levels to something reasonable.
	//
	// z=0..5 can require splitting the tile into many USGS
	// bounding boxes because USGS limits bBox area to 25 degrees².
	//
	// We still support these zooms, but the number of requests
	// can be larger.
	if z < 0 || z > 22 {
		http.Error(
			w,
			"zoom level must be between 0 and 22",
			http.StatusBadRequest,
		)
		return
	}

	tileCount := 1 << z

	if x < 0 || x >= tileCount {
		http.Error(
			w,
			"tile x is outside the valid range",
			http.StatusBadRequest,
		)
		return
	}

	if y < 0 || y >= tileCount {
		http.Error(
			w,
			"tile y is outside the valid range",
			http.StatusBadRequest,
		)
		return
	}

	key := StationChunkIndex{
		TileZ: z,
		TileY: y,
		TileX: x,
	}

	// Fast cache lookup.
	if cached, ok := getCachedTile(key); ok {
		writeStationChunk(w, cached.Chunk)
		return
	}

	// Make sure only one request for this exact tile refreshes
	// the USGS data at a time.
	mutex := getTileFetchMutex(key)

	mutex.Lock()
	defer mutex.Unlock()

	// Check cache again after waiting for another request.
	if cached, ok := getCachedTile(key); ok {
		writeStationChunk(w, cached.Chunk)
		return
	}

	// Convert XYZ tile to geographic bounds.
	bbox := tileToBoundingBox(x, y, z)

	log.Printf(
		"tile %d/%d/%d bbox: west=%f south=%f east=%f north=%f",
		z,
		x,
		y,
		bbox.West,
		bbox.South,
		bbox.East,
		bbox.North,
	)

	// Fetch stations for this geographic area.
	stations, err := fetchStationsForBoundingBox(bbox)
	if err != nil {
		log.Printf(
			"USGS request failed for tile %d/%d/%d: %v",
			z,
			x,
			y,
			err,
		)

		// If an old cache exists, use it as a fallback.
		if cached, ok := getCachedTileIgnoringExpiry(key); ok {
			log.Printf(
				"using stale cache for tile %d/%d/%d",
				z,
				x,
				y,
			)

			writeStationChunk(w, cached.Chunk)
			return
		}

		http.Error(
			w,
			"failed to fetch station data from USGS",
			http.StatusBadGateway,
		)
		return
	}

	// USGS bBox filtering should already return stations in the
	// requested area, but filter one more time against the exact
	// tile bounds to protect against edge cases.
	tileStations := make([]Station, 0, len(stations))

	for _, station := range stations {
		if station.Lat >= bbox.South &&
			station.Lat <= bbox.North &&
			station.Lng >= bbox.West &&
			station.Lng <= bbox.East {

			tileStations = append(tileStations, station)
		}
	}

	lastTime := latestStationTime(tileStations)

	chunk := StationChunk{
		Stations: tileStations,
		LastTime: lastTime,
	}

	// Store tile in cache.
	storeCachedTile(key, chunk)

	writeStationChunk(w, chunk)
}

// Returns a fresh cached tile.
func getCachedTile(key StationChunkIndex) (CachedTile, bool) {
	tileCacheMutex.RLock()
	defer tileCacheMutex.RUnlock()

	cached, ok := tileCache[key]

	if !ok {
		return CachedTile{}, false
	}

	if time.Since(cached.CachedAt) >= tileCacheDuration {
		return CachedTile{}, false
	}

	return cached, true
}

// Returns cached data even if it is expired.
// Used as a fallback when USGS is temporarily unavailable.
func getCachedTileIgnoringExpiry(key StationChunkIndex) (CachedTile, bool) {
	tileCacheMutex.RLock()
	defer tileCacheMutex.RUnlock()

	cached, ok := tileCache[key]

	return cached, ok
}

func storeCachedTile(key StationChunkIndex, chunk StationChunk) {
	tileCacheMutex.Lock()
	defer tileCacheMutex.Unlock()

	tileCache[key] = CachedTile{
		Chunk:    chunk,
		CachedAt: time.Now(),
	}
}

// Gets a mutex for an individual tile.
func getTileFetchMutex(key StationChunkIndex) *sync.Mutex {
	tileFetchMutexLock.Lock()
	defer tileFetchMutexLock.Unlock()

	mutex, ok := tileFetchMutexes[key]

	if !ok {
		mutex = &sync.Mutex{}
		tileFetchMutexes[key] = mutex
	}

	return mutex
}

// Fetches current USGS discharge stations within a bounding box.
//
// USGS requires:
//
//	bBox=west,south,east,north
//
// and limits the product of latitude range * longitude range
// to 25 degrees².
//
// Therefore large map tiles are split into smaller requests.
func fetchStationsForBoundingBox(bbox BoundingBox) ([]Station, error) {
	boxes := splitBoundingBox(bbox)

	log.Printf(
		"USGS tile requires %d bounding-box request(s)",
		len(boxes),
	)

	allStations := make(map[string]Station)

	for _, box := range boxes {
		stations, err := fetchUSGSBox(box)

		if err != nil {
			return nil, err
		}

		// Deduplicate stations by coordinate/name.
		for _, station := range stations {
			key := stationKey(station)

			allStations[key] = station
		}
	}

	result := make([]Station, 0, len(allStations))

	for _, station := range allStations {
		result = append(result, station)
	}

	return result, nil
}

// Performs one USGS bBox request.
func fetchUSGSBox(bbox BoundingBox) ([]Station, error) {
	query := url.Values{}

	query.Set("format", "json")
	query.Set(
		"bBox",
		fmt.Sprintf(
			"%.7f,%.7f,%.7f,%.7f",
			bbox.West,
			bbox.South,
			bbox.East,
			bbox.North,
		),
	)
	query.Set("parameterCd", "00060")

	requestURL := usgsBaseURL + "?" + query.Encode()

	log.Println("USGS request:", requestURL)

	req, err := http.NewRequest(
		http.MethodGet,
		requestURL,
		nil,
	)
	if err != nil {
		return nil, fmt.Errorf(
			"create USGS request: %w",
			err,
		)
	}

	req.Header.Set(
		"User-Agent",
		"USGS-Station-Map/1.0",
	)

	req.Header.Set(
		"Accept",
		"application/json",
	)

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf(
			"request USGS: %w",
			err,
		)
	}

	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf(
			"read USGS response: %w",
			err,
		)
	}

	log.Printf(
		"USGS response: %s content-type=%q bytes=%d",
		resp.Status,
		resp.Header.Get("Content-Type"),
		len(body),
	)

	if resp.StatusCode != http.StatusOK {
		preview := string(body)

		if len(preview) > 1000 {
			preview = preview[:1000]
		}

		return nil, fmt.Errorf(
			"USGS returned HTTP %d: %s",
			resp.StatusCode,
			preview,
		)
	}

	var data USGSResponse

	if err := json.Unmarshal(body, &data); err != nil {
		preview := string(body)

		if len(preview) > 1000 {
			preview = preview[:1000]
		}

		return nil, fmt.Errorf(
			"decode USGS JSON: %w; response begins with %q",
			err,
			preview,
		)
	}

	stations := make([]Station, 0)

	for _, ts := range data.Value.TimeSeries {
		lat := ts.SourceInfo.
			GeoLocation.
			GeogLocation.
			Latitude

		lng := ts.SourceInfo.
			GeoLocation.
			GeogLocation.
			Longitude

		// Ignore invalid coordinates.
		if lat < -90 ||
			lat > 90 ||
			lng < -180 ||
			lng > 180 {
			continue
		}

		station := Station{
			Name: ts.SourceInfo.SiteName,
			Lat:  lat,
			Lng:  lng,
		}

		if len(ts.Values) > 0 &&
			len(ts.Values[0].Value) > 0 {

			value := ts.Values[0].Value[0]

			station.Value = value.Value

			t, err := time.Parse(
				time.RFC3339Nano,
				value.DateTime,
			)

			if err != nil {
				log.Printf(
					"could not parse timestamp %q for station %q: %v",
					value.DateTime,
					station.Name,
					err,
				)
			} else {
				station.LastTime = t
			}
		}

		stations = append(stations, station)
	}

	log.Printf(
		"USGS returned %d stations for bbox",
		len(stations),
	)

	return stations, nil
}

// Splits a bounding box into smaller boxes that satisfy
// USGS's maximum 25 degree² bBox constraint.
func splitBoundingBox(bbox BoundingBox) []BoundingBox {
	latRange := bbox.North - bbox.South
	lngRange := bbox.East - bbox.West

	area := latRange * lngRange

	if area <= maxBBoxArea {
		return []BoundingBox{bbox}
	}

	// Determine how many subdivisions are needed in each direction.
	//
	// We choose approximately square subdivisions in degrees and
	// continue increasing the grid until every box satisfies the
	// USGS constraint.
	latParts := 1
	lngParts := 1

	for (latRange/float64(latParts))*
		(lngRange/float64(lngParts)) > maxBBoxArea {

		if latRange/float64(latParts) >=
			lngRange/float64(lngParts) {
			latParts *= 2
		} else {
			lngParts *= 2
		}
	}

	boxes := make([]BoundingBox, 0, latParts*lngParts)

	latStep := latRange / float64(latParts)
	lngStep := lngRange / float64(lngParts)

	for latIndex := 0; latIndex < latParts; latIndex++ {
		south := bbox.South +
			float64(latIndex)*latStep

		north := bbox.South +
			float64(latIndex+1)*latStep

		for lngIndex := 0; lngIndex < lngParts; lngIndex++ {
			west := bbox.West +
				float64(lngIndex)*lngStep

			east := bbox.West +
				float64(lngIndex+1)*lngStep

			boxes = append(
				boxes,
				BoundingBox{
					West:  west,
					South: south,
					East:  east,
					North: north,
				},
			)
		}
	}

	return boxes
}

// Converts an XYZ tile to its geographic bounding box.
func tileToBoundingBox(x, y, z int) BoundingBox {
	n := math.Exp2(float64(z))

	west := float64(x)/n*360.0 - 180.0
	east := float64(x+1)/n*360.0 - 180.0

	north := tileYToLatitude(float64(y), n)
	south := tileYToLatitude(float64(y+1), n)

	return BoundingBox{
		West:  west,
		South: south,
		East:  east,
		North: north,
	}
}

// Converts an XYZ tile Y coordinate into latitude.
func tileYToLatitude(y, n float64) float64 {
	mercator := math.Pi * (1 - 2*y/n)

	lat := 180.0 / math.Pi *
		math.Atan(math.Sinh(mercator))

	return math.Max(
		-maxMercatorLatitude,
		math.Min(maxMercatorLatitude, lat),
	)
}

// Returns a stable key for deduplicating stations.
func stationKey(station Station) string {
	return fmt.Sprintf(
		"%s|%.7f|%.7f",
		station.Name,
		station.Lat,
		station.Lng,
	)
}

// Finds the newest measurement timestamp in a group of stations.
func latestStationTime(stations []Station) time.Time {
	var latest time.Time

	for _, station := range stations {
		if station.LastTime.After(latest) {
			latest = station.LastTime
		}
	}

	return latest
}

func writeStationChunk(w http.ResponseWriter, chunk StationChunk) {
	w.Header().Set(
		"Content-Type",
		"application/json",
	)

	// This is the server-side cache duration. The browser can also
	// reuse the result for this amount of time.
	w.Header().Set(
		"Cache-Control",
		"public, max-age=3600",
	)

	if err := json.NewEncoder(w).Encode(chunk); err != nil {
		log.Printf(
			"failed to encode station response: %v",
			err,
		)
	}
}
