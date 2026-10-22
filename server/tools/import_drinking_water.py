#!/usr/bin/env python3
"""Import the official NYSDOH drinking-water contaminant summaries into PostgreSQL.
Usage: python server/tools/import_drinking_water.py data/drinking_water/nys_drinking_water_contaminants.csv
Set DATABASE_URL (preferred) or PGHOST, PGPORT, PGUSER, PGDATABASE, PGPASSWORD.
Requires: pip install 'psycopg[binary]'
"""
import csv
import os
import sys
from pathlib import Path

import psycopg

SCHEMA = '''
CREATE TABLE IF NOT EXISTS drinking_water_systems (
 pws_id TEXT PRIMARY KEY,
 name TEXT NOT NULL,
 county TEXT,
 population INTEGER,
 fips TEXT
);
CREATE TABLE IF NOT EXISTS drinking_water_contaminants (
 source_row BIGINT PRIMARY KEY,
 pws_id TEXT NOT NULL REFERENCES drinking_water_systems(pws_id),
 contaminant_name TEXT NOT NULL,
 contaminant_code TEXT,
 summary_period TEXT NOT NULL,
 mean_raw TEXT,
 max_raw TEXT,
 range_of_mean TEXT,
 range_of_max TEXT,
 num_samples INTEGER,
 num_nondetects INTEGER,
 unit TEXT
);
CREATE INDEX IF NOT EXISTS drinking_water_systems_name_idx ON drinking_water_systems (lower(name));
CREATE INDEX IF NOT EXISTS drinking_water_contaminants_pws_period_idx ON drinking_water_contaminants (pws_id, summary_period DESC);
'''

def num(v):
    try:
        return int(v.strip()) if v and v.strip() else None
    except (ValueError, AttributeError):
        return None

def main():
    path = Path(sys.argv[1] if len(sys.argv) > 1 else 'data/drinking_water/nys_drinking_water_contaminants.csv')
    if not path.is_file():
        sys.exit(f'CSV not found: {path.resolve()}')
    dsn = os.environ.get('DATABASE_URL')
    if not dsn and not os.environ.get('PGDATABASE'):
        sys.exit('Set DATABASE_URL or PGDATABASE (+ PGHOST/PGUSER/PGPASSWORD) for the AquaNY database before importing.')
    with psycopg.connect(dsn or '') as con:
        with con.cursor() as cur:
            cur.execute(SCHEMA)
            cur.execute('''CREATE TEMP TABLE dw_import (
                source_row BIGINT, pws_id TEXT, name TEXT, county TEXT, population INTEGER,
                fips TEXT, contaminant_name TEXT, contaminant_code TEXT, summary_period TEXT,
                mean_raw TEXT, max_raw TEXT, range_of_mean TEXT, range_of_max TEXT,
                num_samples INTEGER, num_nondetects INTEGER, unit TEXT
            ) ON COMMIT DROP''')
            count = 0
            with path.open(newline='', encoding='utf-8-sig') as fh:
                reader = csv.DictReader(fh)
                required = {'PWSIDNumber','PWSName','contaminantname','SummaryTimePeriod','mean','max'}
                if not required.issubset(reader.fieldnames or []):
                    sys.exit(f'Missing CSV columns: {sorted(required - set(reader.fieldnames or []))}')
                with cur.copy('COPY dw_import FROM STDIN') as cp:
                    for index, row in enumerate(reader, 1):
                        pws_id = (row.get('PWSIDNumber') or '').strip()
                        if not pws_id: continue
                        code = (row.get('ContaminantCode') or '').strip()
                        unit = {'2805':'ng/L','2806':'ng/L','1040':'mg/L','4010':'pCi/L'}.get(code)
                        cp.write_row((index,pws_id,(row.get('PWSName') or '').strip(),
                            (row.get('county') or '').strip(),num(row.get('Population')),
                            (row.get('FIPS') or '').strip(),(row.get('contaminantname') or '').strip(),
                            code,(row.get('SummaryTimePeriod') or '').strip(),
                            (row.get('mean') or '').strip(),(row.get('max') or '').strip(),
                            (row.get('range_of_mean') or '').strip(),(row.get('range_of_max') or '').strip(),
                            num(row.get('NumSamples')),num(row.get('NumNonDetects')),unit))
                        count += 1
            # Favor the last valid system metadata row from the file.
            cur.execute('''INSERT INTO drinking_water_systems (pws_id,name,county,population,fips)
                SELECT DISTINCT ON (pws_id) pws_id,name,county,population,fips
                FROM dw_import ORDER BY pws_id,source_row DESC
                ON CONFLICT(pws_id) DO UPDATE SET name=EXCLUDED.name,county=EXCLUDED.county,
                population=EXCLUDED.population,fips=EXCLUDED.fips''')
            # Replace the dataset atomically to avoid retaining stale deleted rows.
            cur.execute('DELETE FROM drinking_water_contaminants')
            cur.execute('''INSERT INTO drinking_water_contaminants
                (source_row,pws_id,contaminant_name,contaminant_code,summary_period,mean_raw,max_raw,
                 range_of_mean,range_of_max,num_samples,num_nondetects,unit)
                SELECT source_row,pws_id,contaminant_name,contaminant_code,summary_period,mean_raw,
                       max_raw,range_of_mean,range_of_max,num_samples,num_nondetects,unit FROM dw_import''')
        con.commit()
        print(f'Imported {count:,} NYSDOH summary rows successfully.')
        with con.cursor() as cur:
            cur.execute('SELECT COUNT(*) FROM drinking_water_systems')
            print(f'Public water systems: {cur.fetchone()[0]:,}')
            cur.execute("SELECT COUNT(*) FROM drinking_water_contaminants WHERE contaminant_code IN ('2805','2806')")
            print(f'PFOA/PFOS summary rows: {cur.fetchone()[0]:,}')

if __name__ == '__main__': main()
