
#!/usr/bin/env python3
"""Read-only USGS station matching preview for an AquaNY display group."""

import argparse
import csv
import json
import os
import re
from pathlib import Path

import psycopg
from pyproj import Transformer
from shapely.geometry import Point, shape
from shapely.ops import transform

# Project longitude/latitude into US Albers coordinates (meters).
projector = Transformer.from_crs(
    "EPSG:4326", "EPSG:5070", always_xy=True
)

# Explicit main-river references, not place names such as
# "Croton-on-Hudson" or "Hudson Falls".
HUDSON_NAME = re.compile(
    r"\bHUDSON\s+(?:R\b|RIVER\b)",
    re.IGNORECASE,
)

SITE_TYPES = (
    "Stream",
    "Estuary",
    "Tidal stream",
    "Canal",
)

GROUP_SQL = """
SELECT w.id, w.name, w.segments::text
FROM waterbody_display_members m
JOIN waterbodies_v2 w ON w.id = m.waterbody_id
WHERE m.group_id = %s
ORDER BY w.name, w.id
"""

STATIONS_SQL = """
SELECT
    station_id,
    COALESCE(station->>'MonitoringLocationName', ''),
    COALESCE(station->>'SiteType', ''),
    (station->'Geometry'->>0)::float8,
    (station->'Geometry'->>1)::float8
FROM stations
WHERE station->>'SiteType' = ANY(%s)
  AND jsonb_typeof(station->'Geometry') = 'array'
  AND jsonb_array_length(station->'Geometry') >= 2
"""


def connect():
    url = os.getenv("DATABASE_URL")

    if url:
        return psycopg.connect(url)

    return psycopg.connect(
        host=os.getenv("PGHOST", "127.0.0.1"),
        port=os.getenv("PGPORT", "5432"),
        user=os.environ["PGUSER"],
        password=os.environ["PGPASSWORD"],
        dbname=os.environ["PGDATABASE"],
    )


def load_geometries(rows):
    geometries = []

    for waterbody_id, waterbody_name, raw_segments in rows:
        segments = json.loads(raw_segments)

        for segment in segments:
            geometry = segment.get("geometry")

            if not geometry:
                continue

            try:
                geom = shape(geometry)

                if geom.is_empty or not geom.is_valid:
                    continue

                metric_geom = transform(
                    projector.transform, geom
                )

                geometries.append({
                    "waterbody_id": waterbody_id,
                    "waterbody_name": waterbody_name,
                    "segment_id": str(
                        segment.get("segmentId") or ""
                    ),
                    "geometry": metric_geom,
                    "bounds": metric_geom.bounds,
                })

            except (ValueError, TypeError) as exc:
                print(
                    f"Skipping invalid geometry "
                    f"for {waterbody_id}: {exc}"
                )

    return geometries


def nearby_segment(point, geometries, max_distance):
    best = None

    for item in geometries:
        minx, miny, maxx, maxy = item["bounds"]

        # Avoid expensive distance calculations for
        # stations outside a segment's expanded bounds.
        if not (
            minx - max_distance <= point.x <= maxx + max_distance
            and miny - max_distance <= point.y <= maxy + max_distance
        ):
            continue

        distance = point.distance(item["geometry"])

        if best is None or distance < best["distance"]:
            best = {
                "item": item,
                "distance": distance,
            }

    return best


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument(
        "--group", default="hudson-river"
    )
    parser.add_argument(
        "--max-distance-m", type=float, default=200
    )
    parser.add_argument(
        "--output",
        type=Path,
        default=Path("tools/station_match_preview.csv"),
    )
    args = parser.parse_args()

    if args.max_distance_m <= 0:
        parser.error("--max-distance-m must be positive")

    with connect() as conn:
        with conn.cursor() as cur:
            cur.execute("SET TRANSACTION READ ONLY")

            cur.execute(GROUP_SQL, (args.group,))
            group_rows = cur.fetchall()

            if not group_rows:
                parser.error(
                    f"No DEC segments found for {args.group}"
                )

            cur.execute(STATIONS_SQL, (list(SITE_TYPES),))
            stations = cur.fetchall()

    geometries = load_geometries(group_rows)

    if not geometries:
        parser.error(
            "No usable DEC geometries found for this group"
        )

    print(
        f"Loaded {len(geometries)} DEC geometries "
        f"and {len(stations)} candidate stations."
    )

    results = []

    for station_id, name, site_type, lon, lat in stations:
        if lon is None or lat is None:
            continue

        try:
            point = transform(
                projector.transform, Point(lon, lat)
            )
        except (ValueError, TypeError):
            continue

        nearest = nearby_segment(
            point,
            geometries,
            args.max_distance_m,
        )

        if nearest is None:
            continue

        distance = nearest["distance"]

        if distance > args.max_distance_m:
            continue

        # Name compatibility is only supporting evidence.
        # It is not proof that this station monitors Hudson River.
        name_matches = bool(HUDSON_NAME.search(name))

        status = (
            "STRONG_CANDIDATE"
            if name_matches
            else "REVIEW_NAME"
        )

        segment = nearest["item"]

        results.append({
            "status": status,
            "station_id": station_id,
            "station_name": name,
            "site_type": site_type,
            "longitude": lon,
            "latitude": lat,
            "distance_m": round(distance, 2),
            "dec_waterbody": segment["waterbody_name"],
            "dec_segment_id": segment["segment_id"],
            "name_matches": name_matches,
        })

    results.sort(
        key=lambda row: (
            row["status"] != "STRONG_CANDIDATE",
            row["distance_m"],
            row["station_id"],
        )
    )

    args.output.parent.mkdir(
        parents=True, exist_ok=True
    )

    fields = [
        "status",
        "station_id",
        "station_name",
        "site_type",
        "longitude",
        "latitude",
        "distance_m",
        "dec_waterbody",
        "dec_segment_id",
        "name_matches",
    ]

    with args.output.open(
        "w", newline="", encoding="utf-8-sig"
    ) as file:
        writer = csv.DictWriter(
            file, fieldnames=fields
        )
        writer.writeheader()
        writer.writerows(results)

    strong = sum(
        row["status"] == "STRONG_CANDIDATE"
        for row in results
    )

    print(f"Matched candidates: {len(results)}")
    print(f"Strong name/geography candidates: {strong}")
    print(f"Require name review: {len(results) - strong}")
    print(f"CSV: {args.output.resolve()}")
    print("No database changes were made.")
    print(
        "All candidates still require review of "
        "waterbody identity and measurement availability."
    )


if __name__ == "__main__":
    main()
