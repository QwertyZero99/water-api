
#!/usr/bin/env python3
"""Preview conservative DEC main-stem waterbody grouping candidates.

Read-only: this script never modifies PostgreSQL data.

Run from server/:
    python tools/group_waterbodies.py --preview

Requires psycopg (v3) or psycopg2 and DATABASE_URL
(or standard PG* environment variables).
"""

from __future__ import annotations

import argparse
import csv
import json
import math
import os
import re
from collections import defaultdict
from pathlib import Path

# Exact name matching: no fuzzy matching or automatic aliases.
MAIN_STEM = re.compile(r"\bmain\s+stem\b", re.IGNORECASE)

SQL = """
SELECT
    w.id,
    w.name,
    w.waterbody_type,
    w.segment_count,
    w.segments::text
FROM waterbodies_v2 AS w
WHERE w.waterbody_type = 'stream'
  AND w.name ILIKE '%main stem%'
  AND NOT EXISTS (
      SELECT 1
      FROM waterbody_display_members AS member
      WHERE member.waterbody_id = w.id
  )
ORDER BY w.name, w.id
"""

FIELDS = [
    "status",
    "name",
    "basin",
    "record_count",
    "segment_count",
    "minimum_pair_distance_m",
    "largest_connection_distance_m",
    "geometry_types",
    "member_ids",
    "reason",
]


def open_connection():
    url = os.getenv("DATABASE_URL")

    try:
        import psycopg

        if url:
            return psycopg.connect(url)

        return psycopg.connect("")

    except ImportError:
        try:
            import psycopg2

            if url:
                return psycopg2.connect(url)

            return psycopg2.connect("")

        except ImportError as exc:
            raise RuntimeError(
                "Install psycopg with: "
                "python -m pip install 'psycopg[binary]'"
            ) from exc


def parse_segments(raw):
    data = json.loads(raw) if isinstance(raw, str) else raw

    if not isinstance(data, list):
        raise ValueError("segments is not a JSON array")

    return data


def geometry_points(geom):
    """Collect GeoJSON vertices from line geometries."""
    if not isinstance(geom, dict):
        return None

    kind = geom.get("type")

    if kind == "GeometryCollection":
        parts = [
            geometry_points(g)
            for g in geom.get("geometries", [])
        ]

        if not parts or any(p is None for p in parts):
            return None

        return [
            point
            for part in parts
            for point in part
        ]

    if kind not in ("LineString", "MultiLineString"):
        return None

    coords = geom.get("coordinates")

    if kind == "LineString":
        paths = [coords]
    else:
        paths = coords

    if not isinstance(paths, list):
        return None

    points = []

    for path in paths:
        if not isinstance(path, list):
            return None

        for pair in path:
            if not isinstance(pair, list) or len(pair) < 2:
                return None

            lon, lat = pair[:2]

            if not isinstance(lon, (int, float)):
                return None

            if not isinstance(lat, (int, float)):
                return None

            if not (
                math.isfinite(lon)
                and math.isfinite(lat)
                and -180 <= lon <= 180
                and -90 <= lat <= 90
            ):
                return None

            points.append((lon, lat))

    # Cap vertex comparisons for large geometries.
    if len(points) > 300:
        stride = max(1, math.ceil(len(points) / 300))
        points = points[::stride][:300]

    return points or None


def distance_m(a, b):
    """Haversine distance between longitude/latitude pairs."""
    lon1, lat1 = map(math.radians, a)
    lon2, lat2 = map(math.radians, b)

    dlat = lat2 - lat1
    dlon = lon2 - lon1

    h = (
        math.sin(dlat / 2) ** 2
        + math.cos(lat1)
        * math.cos(lat2)
        * math.sin(dlon / 2) ** 2
    )

    return 2 * 6371000 * math.asin(
        min(1.0, math.sqrt(h))
    )


def min_vertex_distance_m(a, b):
    """
    Approximate proximity using sampled GeoJSON vertices.

    This is not a true line-to-line distance and is only
    suitable for screening candidates for review.
    """
    return min(
        distance_m(p, q)
        for p in a
        for q in b
    )


def assess(name, rows, threshold):
    ids = sorted(row[0] for row in rows)

    segment_total = sum(
        int(row[3])
        for row in rows
    )

    basins = set()
    shapes = []
    types = set()
    problems = []

    for row in rows:
        try:
            segments = parse_segments(row[4])

            if len(segments) != int(row[3]):
                problems.append(
                    "segment_count disagrees with segments JSON"
                )

            all_points = []

            for segment in segments:
                if not isinstance(segment, dict):
                    raise ValueError(
                        "segment entry is not an object"
                    )

                basin = (
                    segment.get("basin") or ""
                ).strip()

                if not basin:
                    problems.append("missing basin")
                else:
                    basins.add(basin.casefold())

                geom = segment.get("geometry")

                if isinstance(geom, dict):
                    types.add(
                        geom.get("type", "missing")
                    )
                else:
                    types.add("missing")

                pts = geometry_points(geom)

                if pts is None:
                    problems.append(
                        "missing, unsupported or invalid line geometry"
                    )
                else:
                    all_points.extend(pts)

            if not all_points:
                problems.append(
                    "record has no usable line geometry"
                )

            shapes.append(all_points)

        except (ValueError, TypeError) as exc:
            problems.append(
                f"invalid segment JSON: {exc}"
            )
            shapes.append([])

    if len(basins) > 1:
        problems.append("multiple basins")

    if not basins:
        problems.append("no known basin")

    distances = []

    if all(shapes):
        # Find the minimum-distance connections between records.
        n = len(shapes)
        edges = []

        for i in range(n):
            for j in range(i + 1, n):
                d = min_vertex_distance_m(
                    shapes[i],
                    shapes[j]
                )

                distances.append(d)
                edges.append((d, i, j))

        # Minimum spanning tree, allowing long rivers
        # to consist of a chain of adjacent segments.
        parent = list(range(n))

        def root(i):
            while parent[i] != i:
                parent[i] = parent[parent[i]]
                i = parent[i]

            return i

        max_link = 0.0

        for d, i, j in sorted(edges):
            ri = root(i)
            rj = root(j)

            if ri != rj:
                parent[ri] = rj
                max_link = max(max_link, d)

        if max_link > threshold:
            problems.append(
                "geographic gaps in nearest-neighbor chain "
                f"(largest {max_link:.0f} m)"
            )

    else:
        max_link = None

    # All groups require review before insertion.
    status = (
        "REVIEW_GEOGRAPHY"
        if problems
        else "CANDIDATE_REVIEW"
    )

    return {
        "status": status,
        "name": name,
        "basin": " | ".join(sorted(basins)) or "(missing)",
        "record_count": len(rows),
        "segment_count": segment_total,
        "minimum_pair_distance_m": (
            f"{min(distances):.1f}"
            if distances else ""
        ),
        "largest_connection_distance_m": (
            f"{max_link:.1f}"
            if max_link is not None else ""
        ),
        "geometry_types": " | ".join(sorted(types)),
        "member_ids": " | ".join(ids),
        "reason": (
            "; ".join(sorted(set(problems)))
            or "Exact name, single basin, close vertex "
               "connectivity; human review required"
        ),
    }


def main():
    parser = argparse.ArgumentParser(
        description=__doc__
    )

    parser.add_argument(
        "--preview",
        action="store_true",
        help="Generate a read-only CSV of proposed groups"
    )

    parser.add_argument(
        "--output",
        type=Path,
        default=Path(
            "tools/waterbody_group_preview.csv"
        )
    )

    parser.add_argument(
        "--max-gap-m",
        type=float,
        default=300,
        help="Flag geographic gaps larger than this distance"
    )

    args = parser.parse_args()

    if not args.preview:
        parser.error(
            "Only --preview is implemented. "
            "No --apply mode exists yet."
        )

    if args.max_gap_m <= 0:
        parser.error(
            "--max-gap-m must be positive"
        )

    try:
        conn = open_connection()

    except Exception as exc:
        parser.exit(
            1,
            f"Database connection failed: {exc}\n"
        )

    try:
        with conn.cursor() as cursor:
            cursor.execute(
                "SET TRANSACTION READ ONLY"
            )

            cursor.execute(SQL)
            records = cursor.fetchall()

    except Exception as exc:
        parser.exit(
            1,
            f"Database query failed: {exc}\n"
        )

    finally:
        conn.close()

    by_name = defaultdict(list)

    for record in records:
        if MAIN_STEM.search(record[1]):
            key = record[1].strip().casefold()
            by_name[key].append(record)

    previews = []

    for key, group in sorted(by_name.items()):
        if len(group) > 1:
            previews.append(
                assess(
                    group[0][1],
                    group,
                    args.max_gap_m
                )
            )

    args.output.parent.mkdir(
        parents=True,
        exist_ok=True
    )

    with args.output.open(
        "w",
        newline="",
        encoding="utf-8-sig"
    ) as file:
        writer = csv.DictWriter(
            file,
            fieldnames=FIELDS
        )

        writer.writeheader()
        writer.writerows(previews)

    print(
        f"Read {len(records)} ungrouped main-stem records."
    )

    print(
        f"Generated {len(previews)} candidate groups "
        "(no database changes)."
    )

    print(
        f"CSV: {args.output.resolve()}"
    )

    for item in previews:
        print(
            f"  [{item['status']}] "
            f"{item['name']} — "
            f"{item['record_count']} records; "
            f"{item['reason']}"
        )

    print(
        "All groups require review. "
        "Vertex-distance screening is not proof "
        "of hydrologic connectivity."
    )


if __name__ == "__main__":
    main()
