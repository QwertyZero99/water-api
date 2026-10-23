package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	_ "github.com/jackc/pgx/v5/stdlib"
)

const schema = `
CREATE TABLE IF NOT EXISTS waterbodies (
 feature_id TEXT PRIMARY KEY,
 segment_id TEXT NOT NULL DEFAULT '',
 name TEXT NOT NULL DEFAULT '',
 waterbody_type TEXT NOT NULL DEFAULT '',
 description TEXT NOT NULL DEFAULT '',
 basin TEXT NOT NULL DEFAULT '',
 classification TEXT NOT NULL DEFAULT '',
 assessment TEXT NOT NULL DEFAULT '',
 assessed_year INTEGER,
 factsheet TEXT NOT NULL DEFAULT '',
 geometry JSONB NOT NULL,
 updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS waterbodies_name_idx ON waterbodies (lower(name));
CREATE EXTENSION IF NOT EXISTS pg_trgm;
CREATE INDEX IF NOT EXISTS waterbodies_name_trgm_idx ON waterbodies USING gin (name gin_trgm_ops);
CREATE INDEX IF NOT EXISTS waterbodies_segment_idx ON waterbodies (segment_id);
`

func asString(v any) string {
	if v == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(v))
}
func yearValue(v any) any {
	if v == nil || asString(v) == "" {
		return nil
	}
	n, err := strconv.Atoi(asString(v))
	if err != nil {
		return nil
	}
	return n
}
func first(m map[string]any, keys ...string) any {
	for _, k := range keys {
		if v, ok := m[k]; ok && v != nil {
			return v
		}
	}
	return nil
}
func dsn() string {
	if v := os.Getenv("DATABASE_URL"); v != "" {
		return v
	}
	pwd := url.QueryEscape(os.Getenv("DATABASE_PASSWORD"))
	port := os.Getenv("DATABASE_PORT")
	if port == "" {
		port = "5432"
	}
	return fmt.Sprintf("postgres://waterapi:%s@localhost:%s/postgres?sslmode=disable", pwd, port)
}

// parseFeatureCollection uses streaming tokens so the 479MB stream file never
// needs to be loaded as one enormous []byte.
func importFile(db *sql.DB, path, kind string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	dec := json.NewDecoder(f)
	tok, err := dec.Token()
	if err != nil {
		return err
	}
	if tok != json.Delim('{') {
		return fmt.Errorf("%s: expected object", path)
	}
	count := 0
	for dec.More() {
		k, err := dec.Token()
		if err != nil {
			return err
		}
		if k != "features" {
			var discarded json.RawMessage
			if err := dec.Decode(&discarded); err != nil {
				return err
			}
			continue
		}
		tok, err = dec.Token()
		if err != nil {
			return err
		}
		if tok != json.Delim('[') {
			return fmt.Errorf("%s: expected features array", path)
		}
		for dec.More() {
			var feature struct {
				ID         any             `json:"id"`
				Geometry   json.RawMessage `json:"geometry"`
				Properties map[string]any  `json:"properties"`
			}
			if err := dec.Decode(&feature); err != nil {
				return fmt.Errorf("%s feature %d: %w", path, count, err)
			}
			p := feature.Properties
			if len(feature.Geometry) == 0 || string(feature.Geometry) == "null" {
				continue
			}
			oid := asString(first(p, "OBJECTID", "objectid"))
			if oid == "" {
				oid = asString(feature.ID)
			}
			if oid == "" {
				return fmt.Errorf("feature %d has no ID", count)
			}
			// Type + object ID makes identity stable, even when SEG_ID is duplicated.
			id := kind + ":" + oid
			_, err := db.Exec(`
    INSERT INTO waterbodies (feature_id,segment_id,name,waterbody_type,
      description,basin,classification,assessment,assessed_year,factsheet,geometry)
    VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11::jsonb)
    ON CONFLICT (feature_id) DO UPDATE SET
     segment_id=EXCLUDED.segment_id,name=EXCLUDED.name,
     waterbody_type=EXCLUDED.waterbody_type,description=EXCLUDED.description,
     basin=EXCLUDED.basin,classification=EXCLUDED.classification,
     assessment=EXCLUDED.assessment,assessed_year=EXCLUDED.assessed_year,
     factsheet=EXCLUDED.factsheet,geometry=EXCLUDED.geometry,updated_at=NOW()`,
				id, asString(first(p, "SEG_ID", "seg_id")),
				asString(first(p, "WATERBODY", "waterbody")), kind,
				asString(first(p, "DESCRIPT", "description")),
				asString(first(p, "BASIN_NAME", "basin_name")),
				asString(first(p, "CLASS", "class")),
				asString(first(p, "WATERBODY_CATEGORY", "waterbody_category")),
				yearValue(first(p, "CYCLE_LAST_ASSESSED", "cycle_last_assessed")),
				asString(first(p, "FACTSHEET", "factsheet")), string(feature.Geometry))
			if err != nil {
				return fmt.Errorf("insert %s: %w", id, err)
			}
			count++
			if count%250 == 0 {
				log.Printf("%s: %d imported", kind, count)
			}
		}
		if _, err := dec.Token(); err != nil {
			return err
		} // ]
	}
	if _, err := dec.Token(); err != nil && err != io.EOF {
		return err
	} // }
	log.Printf("Imported %d %s features from %s", count, kind, filepath.Base(path))
	return nil
}

func main() {
	if len(os.Args) != 4 {
		log.Fatal("usage: go run ./tools/import_waterbodies <streams.geojson> <lakes.geojson> <estuaries.geojson> (run from server/)")
	}
	db, err := sql.Open("pgx", dsn())
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()
	if err := db.Ping(); err != nil {
		log.Fatal(err)
	}
	if _, err := db.Exec(schema); err != nil {
		log.Fatal("schema: ", err)
	}
	for i, item := range []string{"stream", "lake", "estuary"} {
		if err := importFile(db, os.Args[i+1], item); err != nil {
			log.Fatal(err)
		}
	}
	log.Println("Waterbody import complete")
}
