
"""Import NYSDEC ambient surface-water chemistry into PostgreSQL."""

import argparse
import csv
from collections import Counter
from datetime import datetime
from pathlib import Path

import psycopg


SCHEMA = """
CREATE TABLE IF NOT EXISTS pfas_sites (
    id BIGSERIAL PRIMARY KEY,
    site_code TEXT NOT NULL,
    waterbody_name TEXT NOT NULL,
    waterbody_type TEXT NOT NULL,
    basin TEXT NOT NULL,
    latitude DOUBLE PRECISION NOT NULL,
    longitude DOUBLE PRECISION NOT NULL,
    waterbody_id TEXT REFERENCES waterbodies_v2(id),
    match_method TEXT,
    UNIQUE (site_code, latitude, longitude)
);

CREATE INDEX IF NOT EXISTS pfas_sites_waterbody_idx
ON pfas_sites(waterbody_id);

CREATE INDEX IF NOT EXISTS pfas_sites_code_idx
ON pfas_sites(site_code);

CREATE TABLE IF NOT EXISTS pfas_results (
    objectid INTEGER PRIMARY KEY,
    site_id BIGINT NOT NULL REFERENCES pfas_sites(id),
    event_id TEXT NOT NULL,
    sampled_at TIMESTAMP NOT NULL,
    analyte TEXT NOT NULL,
    casrn TEXT,
    concentration NUMERIC,
    nondetect BOOLEAN NOT NULL,
    unit TEXT NOT NULL,
    qualifier TEXT,
    method_detection_limit NUMERIC,
    quantitation_limit NUMERIC,
    reporting_detection_limit NUMERIC,
    analytical_method TEXT,
    sample_design TEXT,
    source_organization TEXT,

    CHECK (
        (nondetect AND concentration IS NULL)
        OR
        (NOT nondetect AND concentration IS NOT NULL)
    )
);

CREATE INDEX IF NOT EXISTS pfas_results_site_idx
ON pfas_results(site_id);

CREATE INDEX IF NOT EXISTS pfas_results_analyte_idx
ON pfas_results(analyte);
"""


def number(value):
    value = (value or "").strip()

    if not value or value in ("#N/A", "N/A"):
        return None

    return float(value)


def sample_date(value):
    value = (value or "").strip()

    formats = (
        "%Y-%m-%d",                # 2023-09-19
        "%Y-%m-%d %H:%M:%S",       # 2023-09-19 14:30:00
        "%m/%d/%Y",                # 09/19/2023
        "%m/%d/%Y %I:%M:%S %p",   # 09/19/2023 02:30:00 PM
    )

    for fmt in formats:
        try:
            return datetime.strptime(value, fmt)
        except ValueError:
            continue

    raise ValueError(f"Unexpected sampling date: {value!r}")


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("csv_path", type=Path)
    parser.add_argument(
        "--dsn",
        default=(
            "host=localhost port=5432 "
            "dbname=postgres user=postgres"
        ),
    )
    args = parser.parse_args()

    if not args.csv_path.is_file():
        parser.error(f"File not found: {args.csv_path}")

    with args.csv_path.open(
        newline="",
        encoding="utf-8-sig",
    ) as file:
        records = list(csv.DictReader(file))

    if not records or "Site Code" not in records[0]:
        raise ValueError(
            "Expected NYSDEC Ambient Water Chemistry Results CSV"
        )

    print(f"Read {len(records):,} chemistry records")

    with psycopg.connect(args.dsn) as conn:
        with conn.cursor() as cur:
            cur.execute(SCHEMA)

            # Repeatable import. Only affects PFAS tables.
            cur.execute("DELETE FROM pfas_results")
            cur.execute("DELETE FROM pfas_sites")

            cur.execute("""
                SELECT id, lower(name), waterbody_type
                FROM waterbodies_v2
            """)

            by_name = {}

            for wid, name, kind in cur.fetchall():
                key = (name.strip(), kind)
                by_name.setdefault(key, []).append(wid)

            site_ids = {}
            match_counts = Counter()

            # Import distinct sampling locations.
            for record in records:
                code = record["Site Code"].strip()
                latitude = number(record["Latitude"])
                longitude = number(record["Longitude"])

                if (
                    latitude is None
                    or longitude is None
                    or not 39 <= latitude <= 46
                    or not -81 <= longitude <= -71
                ):
                    raise ValueError(
                        f"Invalid coordinates for site {code}"
                    )

                site_key = (code, latitude, longitude)

                if site_key in site_ids:
                    continue

                waterbody_name = record["Waterbody Name"].strip()

                kind = (
                    "stream"
                    if record["Waterbody Type"] == "river_stream"
                    else "lake"
                )

                candidates = by_name.get(
                    (waterbody_name.lower(), kind),
                    [],
                )

                # Exact-name matching only.
                # This is NOT spatial verification.
                waterbody_id = (
                    candidates[0]
                    if len(candidates) == 1
                    else None
                )

                match_method = (
                    "exact_name_unverified"
                    if waterbody_id
                    else None
                )

                cur.execute(
                    """
                    INSERT INTO pfas_sites (
                        site_code,
                        waterbody_name,
                        waterbody_type,
                        basin,
                        latitude,
                        longitude,
                        waterbody_id,
                        match_method
                    )
                    VALUES (
                        %s, %s, %s, %s,
                        %s, %s, %s, %s
                    )
                    RETURNING id
                    """,
                    (
                        code,
                        waterbody_name,
                        kind,
                        record["Basin Name"],
                        latitude,
                        longitude,
                        waterbody_id,
                        match_method,
                    ),
                )

                site_ids[site_key] = cur.fetchone()[0]

                if waterbody_id:
                    match_counts["linked by name"] += 1
                else:
                    match_counts["unmatched"] += 1

            # Import every individual chemistry result.
            for record in records:
                raw_result = record["Result Value"].strip()

                nondetect = raw_result.lower() == "nondetect"

                concentration = (
                    None if nondetect else number(raw_result)
                )

                if not nondetect and concentration is None:
                    raise ValueError(
                        "Unexpected measurement: "
                        f"{record['OBJECTID']} = {raw_result!r}"
                    )

                site_key = (
                    record["Site Code"].strip(),
                    number(record["Latitude"]),
                    number(record["Longitude"]),
                )

                unit = (
                    record["Unit"]
                    .replace("ng/l", "ng/L")
                    .replace("ug/l", "ug/L")
                )

                cur.execute(
                    """
                    INSERT INTO pfas_results (
                        objectid,
                        site_id,
                        event_id,
                        sampled_at,
                        analyte,
                        casrn,
                        concentration,
                        nondetect,
                        unit,
                        qualifier,
                        method_detection_limit,
                        quantitation_limit,
                        reporting_detection_limit,
                        analytical_method,
                        sample_design,
                        source_organization
                    )
                    VALUES (
                        %s, %s, %s, %s,
                        %s, %s, %s, %s,
                        %s, %s, %s, %s,
                        %s, %s, %s, %s
                    )
                    """,
                    (
                        int(record["OBJECTID"]),
                        site_ids[site_key],
                        record["Event ID"],
                        sample_date(record["Event Date and Time"]),
                        record["Parameter Name"],
                        record["CASRN"],
                        concentration,
                        nondetect,
                        unit,
                        record["Result Qualifier Description"],
                        number(record["Method Detection Limit"]),
                        number(record["Quantitation Limit"]),
                        number(record["Reporting Detection Limit"]),
                        record["Lab Analytical Method"],
                        record["Sample Design"],
                        record["Sample Organization"],
                    ),
                )

    print(
        f"Imported {len(site_ids):,} distinct "
        "site-coordinate records"
    )
    print(f"Imported {len(records):,} chemistry results")
    print(dict(match_counts))

    print(
        "Waterbody name matches are provisional. "
        "Spatial validation is still required."
    )


if __name__ == "__main__":
    main()
