package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"sync"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/paulmach/orb"
	"github.com/paulmach/orb/geojson"
)

const (
	// USGS recommends not repeatedly fetching the same data more frequently
	// than hourly.
	stationStoreUpdateInterval = 1 * time.Hour

	// Wait between paginated station requests.
	stationPageFetchInterval = 1 * time.Minute

	// Request timeout.
	httpRequestTimeout = 30 * time.Second

	// PostgreSQL connection.
	postgresDSN = "postgres://localhost:5432/postgres?sslmode=disable"
)

var (
	nyBoundingBox = BoundingBox{
		MinLong: -79.8,
		MinLat:  40.4,
		MaxLong: -71.8,
		MaxLat:  45.1,
	}

	httpClient = &http.Client{
		Timeout: httpRequestTimeout,
	}
)

type BoundingBox struct {
	MinLong float32
	MinLat  float32
	MaxLong float32
	MaxLat  float32
}

func (b BoundingBox) String() string {
	return fmt.Sprintf(
		"%f,%f,%f,%f",
		b.MinLong,
		b.MinLat,
		b.MaxLong,
		b.MaxLat,
	)
}

// Station represents a USGS monitoring location.
//
// Feature contains the original GeoJSON feature. Keeping the original
// feature means we don't lose properties or GeoJSON foreign/extra members
// that aren't explicitly represented by this struct.
type Station struct {
	ID string

	AgencyCode               string
	AgencyName               string
	MonitoringLocationNumber string
	MonitoringLocationName   string
	StateCode                string
	StateName                string
	CountyCode               string
	CountyName               string
	SiteTypeCode             string
	SiteType                 string

	Geometry orb.Geometry

	// Feature is the original GeoJSON feature, so we can access stuff like
	// ExtraFields.
	Feature *geojson.Feature
}

// StationStore provides access to stations stored in PostgreSQL.
//
// There is deliberately no in-memory station cache. PostgreSQL is the
// source of truth for both reads and writes.
type StationStore struct {
	db        *sql.DB
	saveMutex sync.Mutex
}

func NewStationStore(db *sql.DB) *StationStore {
	return &StationStore{
		db: db,
	}
}

// Add adds a specific station to the database.
func (s *StationStore) Add(station Station) error {
	jsonData, err := json.Marshal(station)
	if err != nil {
		return fmt.Errorf("marshal station: %w", err)
	}

	_, err = s.db.Exec(`
		INSERT INTO stations (station_id, station)
		VALUES ($1, $2::jsonb)
		ON CONFLICT (station_id)
		DO UPDATE SET
			station = EXCLUDED.station,
			updated_at = NOW()
	`, station.ID, jsonData)

	if err != nil {
		return fmt.Errorf("save station %q: %w", station.ID, err)
	}

	return nil
}

// Update replaces all stations in the database with the supplied stations.
//
// The operation is transactional, so readers either see the old complete
// snapshot or the new complete snapshot.
func (s *StationStore) Update(stations []Station) error {
	s.saveMutex.Lock()
	defer s.saveMutex.Unlock()

	return s.saveSnapshot(stations)
}

// All returns all stations directly from PostgreSQL.
//
// No station data is retained in memory after this method returns.
func (s *StationStore) All() ([]Station, error) {
	rows, err := s.db.Query(`
		SELECT station
		FROM stations
		ORDER BY station_id
	`)
	if err != nil {
		return nil, fmt.Errorf("query stations: %w", err)
	}
	defer func() {
		if err := rows.Close(); err != nil {
			log.Printf("unable to close station rows: %v", err)
		}
	}()

	stations := make([]Station, 0)

	for rows.Next() {
		var rawJSON []byte

		if err := rows.Scan(&rawJSON); err != nil {
			return nil, fmt.Errorf("scan station: %w", err)
		}

		var station Station

		if err := json.Unmarshal(rawJSON, &station); err != nil {
			return nil, fmt.Errorf("decode station: %w", err)
		}

		stations = append(stations, station)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate stations: %w", err)
	}

	return stations, nil
}

// Get returns a station by its ID.
func (s *StationStore) Get(id string) (Station, error) {
	var rawJSON []byte

	err := s.db.QueryRow(`
		SELECT station
		FROM stations
		WHERE station_id = $1
	`, id).Scan(&rawJSON)

	if err != nil {
		if err == sql.ErrNoRows {
			return Station{}, fmt.Errorf("station %q not found", id)
		}

		return Station{}, fmt.Errorf(
			"query station %q: %w",
			id,
			err,
		)
	}

	var station Station

	if err := json.Unmarshal(rawJSON, &station); err != nil {
		return Station{}, fmt.Errorf(
			"decode station %q: %w",
			id,
			err,
		)
	}

	return station, nil
}

// Count returns the number of stations currently stored.
func (s *StationStore) Count() (int, error) {
	var count int

	if err := s.db.QueryRow(`
		SELECT COUNT(*)
		FROM stations
	`).Scan(&count); err != nil {
		return 0, fmt.Errorf("count stations: %w", err)
	}

	return count, nil
}

// LastUpdate returns the time at which the most recent complete station
// snapshot was successfully written.
func (s *StationStore) LastUpdate() (time.Time, error) {
	var lastUpdate sql.NullTime

	err := s.db.QueryRow(`
		SELECT MAX(updated_at)
		FROM station_store_metadata
		WHERE key = 'last_update'
	`).Scan(&lastUpdate)

	if err != nil {
		return time.Time{}, fmt.Errorf(
			"query station last update: %w",
			err,
		)
	}

	if !lastUpdate.Valid {
		return time.Time{}, nil
	}

	return lastUpdate.Time, nil
}

// saveSnapshot atomically replaces the station database contents.
//
// The transaction ensures that an unsuccessful update does not leave the
// database containing a partial station list.
func (s *StationStore) saveSnapshot(stations []Station) error {
	if s.db == nil {
		return fmt.Errorf("station database is not initialized")
	}

	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin station snapshot transaction: %w", err)
	}

	defer func() {
		_ = tx.Rollback()
	}()

	if _, err := tx.Exec(`DELETE FROM stations`); err != nil {
		return fmt.Errorf("clear stations: %w", err)
	}

	stmt, err := tx.Prepare(`
		INSERT INTO stations (
			station_id,
			station
		)
		VALUES ($1, $2::jsonb)
	`)
	if err != nil {
		return fmt.Errorf("prepare station insert: %w", err)
	}
	defer stmt.Close()

	for _, station := range stations {
		jsonData, err := json.Marshal(station)
		if err != nil {
			return fmt.Errorf(
				"marshal station %q: %w",
				station.ID,
				err,
			)
		}

		if _, err := stmt.Exec(
			station.ID,
			jsonData,
		); err != nil {
			return fmt.Errorf(
				"insert station %q: %w",
				station.ID,
				err,
			)
		}
	}

	now := time.Now()

	if _, err := tx.Exec(`
		INSERT INTO station_store_metadata (
			key,
			updated_at
		)
		VALUES ('last_update', $1)
		ON CONFLICT (key)
		DO UPDATE SET
			updated_at = EXCLUDED.updated_at
	`, now); err != nil {
		return fmt.Errorf(
			"update station metadata: %w",
			err,
		)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf(
			"commit station snapshot: %w",
			err,
		)
	}

	return nil
}

// Save writes the supplied station snapshot to PostgreSQL.
//
// This method is retained as the persistence entry point used by the
// updater. It does not maintain an in-memory copy.
func (s *StationStore) Save(stations []Station) error {
	s.saveMutex.Lock()
	defer s.saveMutex.Unlock()

	return s.saveSnapshot(stations)
}

// Delete removes a station by ID.
func (s *StationStore) Delete(id string) error {
	result, err := s.db.Exec(`
		DELETE FROM stations
		WHERE station_id = $1
	`, id)

	if err != nil {
		return fmt.Errorf(
			"delete station %q: %w",
			id,
			err,
		)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf(
			"get delete result for station %q: %w",
			id,
			err,
		)
	}

	if rowsAffected == 0 {
		return fmt.Errorf("station %q not found", id)
	}

	return nil
}

// initStationDatabase opens PostgreSQL and creates the required tables.
func initStationDatabase() (*sql.DB, error) {
	db, err := sql.Open(
		"pgx",
		postgresConnectionString(),
	)
	if err != nil {
		return nil, fmt.Errorf(
			"open PostgreSQL connection: %w",
			err,
		)
	}

	if err := db.Ping(); err != nil {
		_ = db.Close()

		return nil, fmt.Errorf(
			"ping PostgreSQL: %w",
			err,
		)
	}

	if err := createStationTables(db); err != nil {
		_ = db.Close()
		return nil, err
	}

	return db, nil
}

func postgresConnectionString() string {
	password := cfg.DataBasePassword

	return fmt.Sprintf(
		"postgres://postgres:%s@localhost:5432/postgres?sslmode=disable",
		url.QueryEscape(password),
	)
}

// createStationTables creates the station storage schema.
func createStationTables(db *sql.DB) error {
	const createStationsTable = `
		CREATE TABLE IF NOT EXISTS stations (
			station_id TEXT PRIMARY KEY,
			station JSONB NOT NULL,
			updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)
	`

	if _, err := db.Exec(createStationsTable); err != nil {
		return fmt.Errorf(
			"create stations table: %w",
			err,
		)
	}

	const createMetadataTable = `
		CREATE TABLE IF NOT EXISTS station_store_metadata (
			key TEXT PRIMARY KEY,
			updated_at TIMESTAMPTZ
		)
	`

	if _, err := db.Exec(createMetadataTable); err != nil {
		return fmt.Errorf(
			"create station metadata table: %w",
			err,
		)
	}

	return nil
}

// startStationUpdater starts the hourly USGS station updater.
//
// The first update starts immediately. After the update finishes, the
// updater waits one hour before starting the next update.
func startStationUpdater(
	store *StationStore,
	updateInterval time.Duration,
) {
	go func() {
		log.Printf("starting station updater")

		for {
			start := time.Now()

			if err := store.fetchStations(); err != nil {
				log.Printf(
					"failed to update stations: %v",
					err,
				)
			}

			elapsed := time.Since(start)

			wait := max(updateInterval-elapsed, 0)

			log.Printf(
				"station updater waiting %s before next update",
				wait.Round(time.Second),
			)

			time.Sleep(wait)
		}
	}()
}

// fetchStations gets all stations from the USGS API.
//
// Each page is fetched sequentially. The first page is fetched immediately.
// After every successful page, the fetched stations are persisted to the
// database. The updater then waits one minute before fetching the next page.
//
// The database therefore always contains the most recently completed
// checkpoint, even if a later page fails.
func (s *StationStore) fetchStations() error {
	url := UsgsBaseURL +
		"/collections/monitoring-locations/items" +
		"?bbox=" + nyBoundingBox.String() +
		"&limit=50000&f=json"

	pageIndex := 0
	var stations []Station

	for url != "" {
		log.Printf(
			"fetching station page %d at %s",
			pageIndex,
			url,
		)

		pageStart := time.Now()

		pageStations, nextURL, err := fetchStationPage(url)
		if err != nil {
			return fmt.Errorf(
				"fetch station page %d: %w",
				pageIndex,
				err,
			)
		}

		stations = append(stations, pageStations...)

		// Save everything successfully fetched so far.
		snapshot := make([]Station, len(stations))
		copy(snapshot, stations)

		if err := s.Save(snapshot); err != nil {
			return fmt.Errorf(
				"save stations after page %d: %w",
				pageIndex,
				err,
			)
		}

		log.Printf(
			"completed station page %d: %d stations accumulated; "+
				"page processing took %s",
			pageIndex,
			len(stations),
			time.Since(pageStart).Round(time.Millisecond),
		)

		url = nextURL
		pageIndex++

		if url == "" {
			break
		}

		// Start the one-minute interval after the page request and
		// database checkpoint have completed.
		//
		// This means the next request is made one minute after this
		// page has finished processing.
		time.Sleep(stationPageFetchInterval)
	}

	log.Printf(
		"station update successful: %d stations",
		len(stations),
	)

	return nil
}

// fetchStationPage fetches and decodes one USGS GeoJSON FeatureCollection.
func fetchStationPage(
	url string,
) ([]Station, string, error) {
	req, err := http.NewRequest(
		http.MethodGet,
		url,
		nil,
	)
	if err != nil {
		return nil, "", fmt.Errorf(
			"create USGS request: %w",
			err,
		)
	}

	req.Header.Set(
		"Accept",
		"application/geo+json, application/json",
	)

	req.Header.Set(
		"X-Api-Key",
		cfg.APIKey,
	)

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf(
			"request USGS stations: %w",
			err,
		)
	}

	defer func() {
		if err := resp.Body.Close(); err != nil {
			log.Printf(
				"unable to close USGS response body: %v",
				err,
			)
		}
	}()

	if resp.StatusCode < http.StatusOK ||
		resp.StatusCode >= http.StatusMultipleChoices {

		body, _ := io.ReadAll(
			io.LimitReader(resp.Body, 4<<10),
		)

		return nil, "", fmt.Errorf(
			"USGS returned HTTP %s: %s",
			resp.Status,
			string(body),
		)
	}

	rawJSON, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", fmt.Errorf(
			"read USGS response: %w",
			err,
		)
	}

	fc, err := geojson.UnmarshalFeatureCollection(rawJSON)
	if err != nil {
		return nil, "", fmt.Errorf(
			"decode USGS GeoJSON: %w",
			err,
		)
	}

	result := make([]Station, 0, len(fc.Features))

	for _, feature := range fc.Features {
		if feature == nil {
			continue
		}

		station, err := stationFromFeature(feature)
		if err != nil {
			return nil, "", err
		}

		result = append(result, station)
	}

	nextURL, err := nextLink(fc)
	if err != nil {
		return nil, "", err
	}

	return result, nextURL, nil
}

// stationFromFeature converts a GeoJSON feature into a Station.
func stationFromFeature(
	feature *geojson.Feature,
) (Station, error) {
	if feature == nil {
		return Station{}, fmt.Errorf(
			"USGS returned a nil feature",
		)
	}

	station := Station{
		ID:       featureID(feature),
		Geometry: feature.Geometry,
		Feature:  feature,
	}

	station.AgencyCode = propertyString(
		feature,
		"agency_code",
	)

	station.AgencyName = propertyString(
		feature,
		"agency_name",
	)

	station.MonitoringLocationNumber =
		propertyString(
			feature,
			"monitoring_location_number",
		)

	station.MonitoringLocationName =
		propertyString(
			feature,
			"monitoring_location_name",
		)

	station.StateCode = propertyString(
		feature,
		"state_code",
	)

	station.StateName = propertyString(
		feature,
		"state_name",
	)

	station.CountyCode = propertyString(
		feature,
		"county_code",
	)

	station.CountyName = propertyString(
		feature,
		"county_name",
	)

	station.SiteTypeCode = propertyString(
		feature,
		"site_type_code",
	)

	station.SiteType = propertyString(
		feature,
		"site_type",
	)

	return station, nil
}

// propertyString gets a string property without panicking.
func propertyString(
	feature *geojson.Feature,
	key string,
) string {
	if feature == nil ||
		feature.Properties == nil {
		return ""
	}

	value, ok := feature.Properties[key]
	if !ok || value == nil {
		return ""
	}

	stringValue, ok := value.(string)
	if !ok {
		return ""
	}

	return stringValue
}

// featureID converts the GeoJSON feature ID to a string.
func featureID(feature *geojson.Feature) string {
	if feature == nil || feature.ID == nil {
		return ""
	}

	switch id := feature.ID.(type) {
	case string:
		return id
	case float64:
		return fmt.Sprintf("%g", id)
	case json.Number:
		return id.String()
	default:
		return fmt.Sprint(id)
	}
}

// nextLink extracts the href of the link whose rel is "next".
func nextLink(
	fc *geojson.FeatureCollection,
) (string, error) {
	if fc == nil || fc.ExtraMembers == nil {
		return "", nil
	}

	rawLinks, ok := fc.ExtraMembers["links"]
	if !ok || rawLinks == nil {
		return "", nil
	}

	links, ok := rawLinks.([]any)
	if !ok {
		return "", fmt.Errorf(
			"USGS GeoJSON links has unexpected type %T",
			rawLinks,
		)
	}

	for _, rawLink := range links {
		link, ok := rawLink.(map[string]any)
		if !ok {
			continue
		}

		rel, _ := link["rel"].(string)
		if rel != "next" {
			continue
		}

		href, _ := link["href"].(string)
		if href == "" {
			return "", fmt.Errorf(
				"USGS next link has no href",
			)
		}

		return href, nil
	}

	return "", nil
}
