
"""
Geographically validate PFAS monitoring-site associations.

Reads:
  pfas_sites
  waterbodies_v2

Default behavior:
  Produces a review CSV without changing the database.

With --apply:
  Clears provisional waterbody associations and stores only
  geographically verified, name-consistent matches.

Run from the server directory:
  python tools/match_pfas_waterbodies.py
  python tools/match_pfas_waterbodies.py --apply
"""

import argparse
import csv
import json
from collections import Counter
from pathlib import Path

import psycopg
from pyproj import Transformer
from shapely.geometry import Point, shape
from shapely.ops import transform
from shapely.strtree import STRtree


DEFAULT_DSN = (
    "host=localhost port=5432 "
    "dbname=postgres user=postgres"
)

# Distances in meters.
STREAM_TOLERANCE = 150
LAKE_TOLERANCE = 75

# Use a continental-US projection with meter units.
PROJECT = Transformer.from_crs(
    "EPSG:4326",
    "EPSG:5070",
    always_xy=True
).transform


def normalize_name(value):
    """Normalize punctuation and whitespace for name comparison."""
    import re

    value = (value or "").lower()
    value = value.replace("&", "and")
    value = re.sub(r"[^a-z0-9]+", " ", value)

    return " ".join(value.split())


def normalized_base_name(value):
    import re

    name = normalize_name(value)

    # Remove leading section descriptors.
    name = re.sub(
        r"^(upper|lower|middle)\s+",
        "",
        name
    )

    # Remove trailing tributary descriptors.
    name = re.sub(
        r"\s+(and\s+)?(minor\s+|mnr\s+)?tribs$",
        "",
        name
    )

    # Remove standard DEC section descriptors.
    name = re.sub(
        r"\s+(upper|lower|middle|upp|low|mid)"
        r"(\s+main\s+stem)?$",
        "",
        name
    )

    # Remove main-stem descriptors.
    name = re.sub(
        r"\s+main\s+stem$",
        "",
        name
    )

    return name.strip()


def compatible_names(site_name, dec_name):
    # Exact match after conservative normalization.
    return (
        normalized_base_name(site_name)
        == normalized_base_name(dec_name)
    )


def parse_geometry(raw):
    if isinstance(raw, str):
        raw = json.loads(raw)

    if not isinstance(raw, dict):
        return None

    try:
        geometry = shape(raw)

        if geometry.is_empty:
            return None

        return transform(PROJECT, geometry)
    except Exception:
        return None


def build_spatial_index(conn):
    """
    Index each original DEC segment geometry.

    Each indexed segment retains its parent waterbody ID.
    """
    geometries = []
    metadata = []

    sql = """
        SELECT
            w.id,
            w.name,
            w.waterbody_type,
            segment->'geometry'
        FROM waterbodies_v2 AS w
        CROSS JOIN LATERAL
            jsonb_array_elements(w.segments) AS segment
    """

    with conn.cursor(name="pfas_geometry_stream") as cur:
        cur.execute(sql)

        for waterbody_id, name, kind, raw_geometry in cur:
            geometry = parse_geometry(raw_geometry)

            if geometry is None:
                continue

            geometries.append(geometry)

            metadata.append({
                "id": waterbody_id,
                "name": name,
                "type": kind
            })

    print(f"Indexed {len(geometries):,} DEC segment geometries")

    return STRtree(geometries), geometries, metadata


def find_candidates(site, index, geometries, metadata):
    point = Point(site["longitude"], site["latitude"])
    point = transform(PROJECT, point)

    # Search once at the largest permitted tolerance.
    nearby_indices = index.query(
        point.buffer(max(STREAM_TOLERANCE, LAKE_TOLERANCE))
    )

    # Combine segments belonging to the same parent.
    parents = {}

    for position in nearby_indices:
        position = int(position)

        geometry = geometries[position]
        parent = metadata[position]

        if parent["type"] not in ("lake", "stream", "estuary"):
            continue

        if site["type"] == "lake" and parent["type"] != "lake":
            continue

        if site["type"] == "stream" and parent["type"] not in (
            "stream", "estuary"
        ):
            continue

        distance = point.distance(geometry)

        tolerance = (
            LAKE_TOLERANCE
            if parent["type"] == "lake"
            else STREAM_TOLERANCE
        )

        if distance > tolerance:
            continue

        waterbody_id = parent["id"]

        previous = parents.get(waterbody_id)

        if previous is None or distance < previous["distance"]:
            parents[waterbody_id] = {
                "id": waterbody_id,
                "name": parent["name"],
                "distance": distance,
                "name_match": compatible_names(
                    site["name"], parent["name"]
                )
            }

    return sorted(
        parents.values(),
        key=lambda item: item["distance"]
    )


def decide_match(candidates):
    """
    Accept only one parent with a compatible name.

    Other nearby geometries are retained in the report,
    but not used to override a unique name-consistent match.
    """
    good = [c for c in candidates if c["name_match"]]

    if len(good) == 1:
        return good[0]["id"], "verified_name_and_geography"

    if len(good) > 1:
        return None, "ambiguous_name_matches"

    if candidates:
        return None, "nearby_but_name_mismatch"

    return None, "no_nearby_geometry"


def main():
    parser = argparse.ArgumentParser()

    parser.add_argument(
        "--dsn",
        default=DEFAULT_DSN
    )

    parser.add_argument(
        "--apply",
        action="store_true",
        help="Update database after reviewing results"
    )

    parser.add_argument(
        "--report",
        type=Path,
        default=Path("../data/raw/pfas_matching_report.csv")
    )

    args = parser.parse_args()

    with psycopg.connect(args.dsn) as conn:
        index, geometries, metadata = build_spatial_index(conn)

        with conn.cursor() as cur:
            cur.execute("""
                SELECT
                    id,
                    site_code,
                    waterbody_name,
                    waterbody_type,
                    latitude,
                    longitude,
                    waterbody_id
                FROM pfas_sites
                ORDER BY id
            """)

            sites = [
                {
                    "id": row[0],
                    "code": row[1],
                    "name": row[2],
                    "type": row[3],
                    "latitude": row[4],
                    "longitude": row[5],
                    "previous_id": row[6]
                }
                for row in cur.fetchall()
            ]

        report = []
        updates = []
        statuses = Counter()

        for site in sites:
            candidates = find_candidates(
                site, index, geometries, metadata
            )

            matched_id, status = decide_match(candidates)

            statuses[status] += 1
            updates.append((matched_id, status, site["id"]))

            closest = candidates[0] if candidates else None

            report.append({
                "site_id": site["id"],
                "site_code": site["code"],
                "site_name": site["name"],
                "site_type": site["type"],
                "latitude": site["latitude"],
                "longitude": site["longitude"],
                "previous_waterbody_id": site["previous_id"],
                "matched_waterbody_id": matched_id or "",
                "status": status,
                "nearest_waterbody": (
                    closest["name"] if closest else ""
                ),
                "nearest_distance_m": (
                    round(closest["distance"], 2)
                    if closest else ""
                ),
                "candidate_count": len(candidates),
                "candidate_names": "; ".join(
                    c["name"] for c in candidates[:10]
                )
            })

        args.report.parent.mkdir(
            parents=True, exist_ok=True
        )

        with args.report.open(
            "w", newline="", encoding="utf-8-sig"
        ) as output:
            writer = csv.DictWriter(
                output,
                fieldnames=list(report[0].keys())
            )

            writer.writeheader()
            writer.writerows(report)

        print()
        print("Matching results:")
        for status, count in statuses.most_common():
            print(f"  {status}: {count}")

        print(f"\nReview report: {args.report.resolve()}")

        if args.apply:
            with conn.cursor() as cur:
                cur.execute("""
                    UPDATE pfas_sites
                    SET waterbody_id = NULL,
                        match_method = NULL
                """)

                cur.executemany("""
                    UPDATE pfas_sites
                    SET waterbody_id = %s,
                        match_method = %s
                    WHERE id = %s
                """, updates)

                print("Database associations updated.")
        else:
            print("DRY RUN: no database associations changed.")
            print("Review the CSV before running with --apply.")


if __name__ == "__main__":
    main()
