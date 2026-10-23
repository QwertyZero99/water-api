package main

import (
	"crypto/sha256"
	"database/sql"
	"encoding/csv"
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"net/url"
	"os"
	"strings"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

const schema = `
CREATE TABLE IF NOT EXISTS beach_test_records (
 record_id TEXT PRIMARY KEY,
 region TEXT NOT NULL,
 beach_name TEXT NOT NULL,
 waterbody_name TEXT NOT NULL,
 category TEXT NOT NULL,
 sample_date DATE NOT NULL,
 results_modifier TEXT NOT NULL,
 result_raw TEXT NOT NULL,
 indicator_bacteria TEXT NOT NULL,
 beach_status TEXT NOT NULL,
 status_reason TEXT NOT NULL,
 imported_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS beach_tests_waterbody_date ON beach_test_records (lower(waterbody_name), sample_date DESC);
CREATE INDEX IF NOT EXISTS beach_tests_beach_date ON beach_test_records (beach_name, sample_date DESC);
`

func dsn() string {
	if s := os.Getenv("DATABASE_URL"); s != "" {
		return s
	}
	port := os.Getenv("DATABASE_PORT")
	if port == "" {
		port = "5432"
	}
	return fmt.Sprintf("postgres://postgres:%s@localhost:%s/postgres?sslmode=disable", url.QueryEscape(os.Getenv("DATABASE_PASSWORD")), port)
}
func key(header string) string {
	return strings.ToLower(strings.TrimSpace(strings.TrimPrefix(header, "\ufeff")))
}
func main() {
	if len(os.Args) != 2 {
		log.Fatalf("Usage (from server/): go run ./tools/import_beach_tests ../data/raw/beach-water-testing.csv")
	}
	f, err := os.Open(os.Args[1])
	if err != nil {
		log.Fatal(err)
	}
	defer f.Close()
	reader := csv.NewReader(f)
	reader.FieldsPerRecord = -1
	header, err := reader.Read()
	if err != nil {
		log.Fatal(err)
	}
	ix := map[string]int{}
	for i, c := range header {
		ix[key(c)] = i
	}
	cols := []string{"region", "state park beach", "waterbody", "category", "date sample taken", "results modifier", "results", "indicator bacteria", "beach status", "status reason"}
	for _, c := range cols {
		if _, ok := ix[c]; !ok {
			log.Fatalf("missing CSV column %q", c)
		}
	}
	db, err := sql.Open("pgx", dsn())
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()
	if err = db.Ping(); err != nil {
		log.Fatal("database connection: ", err)
	}
	if _, err = db.Exec(schema); err != nil {
		log.Fatal("schema: ", err)
	}
	stmt, err := db.Prepare(`INSERT INTO beach_test_records
 (record_id,region,beach_name,waterbody_name,category,sample_date,results_modifier,result_raw,indicator_bacteria,beach_status,status_reason)
 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11) ON CONFLICT (record_id) DO NOTHING`)
	if err != nil {
		log.Fatal(err)
	}
	defer stmt.Close()
	imported, skipped := 0, 0
	for line := 2; ; line++ {
		row, e := reader.Read()
		if e == io.EOF {
			break
		}
		if e != nil {
			log.Fatalf("line %d: %v", line, e)
		}
		get := func(col string) string {
			i := ix[col]
			if i >= len(row) {
				return ""
			}
			return strings.TrimSpace(row[i])
		}
		dt, e := time.Parse("01/02/2006", get("date sample taken"))
		if e != nil {
			log.Printf("line %d invalid date %q; skipping", line, get("date sample taken"))
			skipped++
			continue
		}
		// Stable content digest makes repeated imports idempotent. Exact duplicate CSV rows are intentionally collapsed.
		hash := sha256.Sum256([]byte(strings.Join(row, "\x1f")))
		id := hex.EncodeToString(hash[:])
		res, e := stmt.Exec(id, get("region"), get("state park beach"), get("waterbody"), get("category"), dt, get("results modifier"), get("results"), get("indicator bacteria"), get("beach status"), get("status reason"))
		if e != nil {
			log.Fatalf("line %d insert: %v", line, e)
		}
		n, _ := res.RowsAffected()
		if n > 0 {
			imported++
		} else {
			skipped++
		}
	}
	var total int
	_ = db.QueryRow(`SELECT COUNT(*) FROM beach_test_records`).Scan(&total)
	log.Printf("Import complete: inserted %d records, skipped %d (duplicates/invalid dates); database total %d", imported, skipped, total)
}
