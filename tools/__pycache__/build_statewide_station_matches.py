#!/usr/bin/env python3
"""AquaNY read-only statewide USGS-to-DEC station match preview.

Creates two CSVs; DOES NOT change PostgreSQL. Requires shapely >=2,
pyproj, and psycopg 3. Launch from the server directory.
"""
import argparse
import csv
import json
import os
import re
from collections import Counter, defaultdict
from pathlib import Path

import psycopg
from pyproj import Transformer
from shapely.geometry import Point, shape
from shapely.ops import transform
from shapely.strtree import STRtree

TO_METERS = Transformer.from_crs("EPSG:4326", "EPSG:5070", always_xy=True).transform
SITE_TYPES = ("Stream", "Estuary", "Tidal stream", "Canal", "Lake, Reservoir, Impoundment")

# Include standalone DEC profiles; group members are represented by their
# consolidated group rather than appearing twice in the website.
WATERBODIES_SQL = """
SELECT w.id, w.name, w.waterbody_type, w.segments::text
FROM waterbodies_v2 w
WHERE NOT EXISTS (
  SELECT 1 FROM waterbody_display_members m WHERE m.waterbody_id = w.id
)
"""
GROUPS_SQL = """
SELECT g.id, g.name, w.waterbody_type, w.segments::text
FROM waterbody_display_groups g
JOIN waterbody_display_members m ON m.group_id = g.id
JOIN waterbodies_v2 w ON w.id = m.waterbody_id
"""
STATIONS_SQL = """
SELECT station_id,
       COALESCE(station->>'MonitoringLocationName',''),
       COALESCE(station->>'SiteType',''),
       (station->'Geometry'->>0)::float8,
       (station->'Geometry'->>1)::float8
FROM stations
WHERE station->>'SiteType' = ANY(%s)
  AND jsonb_typeof(station->'Geometry') = 'array'
  AND jsonb_array_length(station->'Geometry') >= 2
"""


def connect():
    if os.getenv("DATABASE_URL"):
        return psycopg.connect(os.environ["DATABASE_URL"])
    return psycopg.connect(
        host=os.getenv("PGHOST", "127.0.0.1"),
        port=os.getenv("PGPORT", "5432"),
        user=os.getenv("PGUSER", "postgres"),
        password=os.getenv("PGPASSWORD", ""),
        dbname=os.getenv("PGDATABASE", "postgres"),
    )


def normalized_name(name):
    # DEC qualifiers like 'Hudson River (Class A)' and
    # 'Genesee River, Main Stem' are not part of the river's name.
    root = re.split(r"\s*\(|,", name, maxsplit=1)[0]
    root = re.sub(r"[^A-Z0-9 ]", " ", root.upper())
    root = re.sub(r"\s+", " ", root).strip()
    return root


def station_name_agrees(station_name, body_name):
    root = normalized_name(body_name)
    if len(root) < 5:
        return False
    variants = {root}
    for suffix, short in ((" RIVER", " R"), (" CREEK", " CK"), (" LAKE", " LK")):
        if root.endswith(suffix):
            variants.add(root[:-len(suffix)] + short)
    station = re.sub(r"[^A-Z0-9 ]", " ", station_name.upper())
    station = re.sub(r"\s+", " ", station).strip()
    # Only a leading waterbody name counts; excludes 'near Hudson NY'.
    return any(station == variant or station.startswith(variant + " ") for variant in variants)


def geometries_from_rows(rows):
    indexed = []
    for body_id, name, body_type, segments_json in rows:
        segments = json.loads(segments_json) if isinstance(segments_json, str) else segments_json
        if not isinstance(segments, list):
            continue
        for segment in segments:
            g = segment.get("geometry")
            if not g:
                continue
            if isinstance(g, str):
                try:
                    g = json.loads(g)
                except ValueError:
                    continue
            try:
                geom = shape(g)
                if geom.is_empty or not geom.is_valid:
                    continue
                projected = transform(TO_METERS, geom)
                indexed.append((body_id, name, body_type or "", str(segment.get("segmentId") or ""), projected))
            except (TypeError, ValueError, KeyError):
                continue
    return indexed


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--output", type=Path, default=Path("tools/statewide_station_matches.csv"))
    parser.add_argument("--review-output", type=Path, default=Path("tools/statewide_station_review.csv"))
    parser.add_argument("--max-distance-m", type=float, default=150)
    args = parser.parse_args()
    if args.max_distance_m <= 0:
        parser.error("--max-distance-m must be positive")

    with connect() as conn:
        with conn.cursor() as cur:
            cur.execute("SET TRANSACTION READ ONLY")
            cur.execute(WATERBODIES_SQL)
            rows = cur.fetchall()
            cur.execute(GROUPS_SQL)
            rows.extend(cur.fetchall())
            cur.execute(STATIONS_SQL, (list(SITE_TYPES),))
            stations = cur.fetchall()

    records = geometries_from_rows(rows)
    if not records:
        parser.error("No DEC geometries loaded; inspect segments JSON structure")
    geoms = [rec[4] for rec in records]
    tree = STRtree(geoms)  # Shapely 2: query returns integer indices.
    accepted, review = [], []
    counters = Counter()
    for station_id, station_name, site_type, lon, lat in stations:
        if lon is None or lat is None:
            continue
        try:
            pt = transform(TO_METERS, Point(lon, lat))
        except (TypeError, ValueError):
            continue
        # Candidate window is only a spatial index prefilter.
        nearby = tree.query(pt.buffer(args.max_distance_m))
        by_body = {}
        for idx in nearby:
            body_id, body_name, body_type, segment_id, geom = records[int(idx)]
            dist = pt.distance(geom)
            if dist > args.max_distance_m:
                continue
            # Tighter screening for lakes and ponds; polygon interiors still 0.
            threshold = 75 if any(w in body_type.lower() for w in ("lake", "pond", "reservoir")) else args.max_distance_m
            if dist > threshold:
                continue
            agrees = station_name_agrees(station_name, body_name)
            prev = by_body.get(body_id)
            if prev is None or dist < prev[0] or (dist == prev[0] and agrees and not prev[4]):
                by_body[body_id] = (dist, body_name, segment_id, body_type, agrees)
        if not by_body:
            continue
        matches = sorted(by_body.items(), key=lambda pair: (not pair[1][4], pair[1][0]))
        chosen_id, (dist, body_name, seg_id, _kind, agrees) = matches[0]
        other_strong = [body for body, value in matches[1:] if value[4]]
        if agrees and not other_strong:
            status = "STRONG_CANDIDATE"
            target = accepted
        else:
            status = "REVIEW_NAME" if not agrees else "REVIEW_AMBIGUOUS"
            target = review
        target.append({
            "waterbody_id": chosen_id,
            "waterbody_name": body_name,
            "status": status,
            "station_id": station_id,
            "station_name": station_name,
            "site_type": site_type,
            "longitude": lon,
            "latitude": lat,
            "distance_m": round(dist, 2),
            "dec_segment_id": seg_id,
            "alternatives": ";".join(body for body, _ in matches[1:6]),
        })
        counters[status] += 1

    columns = ["waterbody_id", "waterbody_name", "status", "station_id", "station_name", "site_type", "longitude", "latitude", "distance_m", "dec_segment_id", "alternatives"]
    for dest, data in ((args.output, accepted), (args.review_output, review)):
        dest.parent.mkdir(parents=True, exist_ok=True)
        with dest.open("w", encoding="utf-8-sig", newline="") as f:
            writer = csv.DictWriter(f, fieldnames=columns)
            writer.writeheader()
            writer.writerows(sorted(data, key=lambda v: (v["waterbody_name"], v["station_id"])))
    print(f"DEC geometries: {len(records)}; candidate USGS stations: {len(stations)}")
    print(f"Strong candidates: {len(accepted)} in {len({x['waterbody_id'] for x in accepted})} waterbodies")
    print(f"Review (not used by API): {len(review)}")
    print(f"Saved: {args.output.resolve()} and {args.review_output.resolve()}")
    print("Preview only: no PostgreSQL modifications. Strong means location/name agreement, not verified sensor availability.")


if __name__ == "__main__":
    main()
