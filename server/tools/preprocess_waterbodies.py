import csv
import hashlib
import json
import re
import sqlite3
import tempfile
from contextlib import closing
from pathlib import Path

import ijson

# Script location: water-api/server/tools/preprocess_waterbodies.py
ROOT = Path(__file__).resolve().parents[2]
DATA_DIR = ROOT / "data" / "raw"
OUTPUT = DATA_DIR / "waterbodies-processed.jsonl"
REPORT = DATA_DIR / "waterbody-grouping-report.csv"

DATASETS = {
    "-1024704460657879735": "estuary",
    "-3959545539646349000": "stream",
    "1475796532095625219": "lake",
}

VERIFIED_LAKES = {
    "Cayuga Lake": 4,
    "Seneca Lake": 3,
    "Lake Champlain": 5,
    "Chautauqua Lake": 2,
}

APPROVED_ESTUARIES = {
    "Arthur Kill": 2,
    "Flanders Bay": 2,
    "Hempstead Harbor": 2,
    "Jamaica Bay": 2,
    "Long Island Sound": 6,
    "Manhasset Bay": 3,
    "Port Jefferson Harbor": 2,
    "Shelter Island Sound": 2,
}

SECTION_WORDS = {
    "north", "south", "east", "west", "northern end", "southern end",
    "eastern end", "western end", "northern section", "southern section",
    "main lake", "main basin", "middle", "central", "upper", "lower",
    "mid-north", "mid-south", "mid-east", "mid-west", "north lake",
    "south lake", "north bay", "south bay",
}
STREAM_WORDS = {
    "main stem", "mainstem", "upper", "middle", "mid", "lower",
    "north", "south", "east", "west", "upper main stem",
    "middle main stem", "lower main stem", "upper mainstem",
    "middle mainstem", "lower mainstem",
}


def dataset_type(filename):
    for marker, kind in DATASETS.items():
        if marker in filename:
            return kind
    raise ValueError(f"Unrecognized DEC GeoJSON filename: {filename}")


def stable_id(value):
    return hashlib.sha256(value.encode("utf-8")).hexdigest()[:24]


def proposed_parent(name):
    if "," not in name:
        return None, None
    parent, suffix = name.split(",", 1)
    return parent.strip(), suffix.strip()


def eligible_section(suffix):
    if not suffix:
        return False
    parts = [re.sub(r"\s+", " ", part.strip().lower())
             for part in suffix.split(",")]
    return bool(parts) and all(part in SECTION_WORDS for part in parts)


def eligible_stream_section(parent, suffix):
    if not parent or not suffix or not parent.lower().endswith(" river"):
        return False
    text = re.sub(r"\s+", " ", suffix.strip().lower())
    if any(word in text for word in ("trib", "pond", "lake", "reservoir")):
        return False
    parts = [part.strip() for part in text.split(",")]
    return (all(part in STREAM_WORDS for part in parts)
            and ("main stem" in text or "mainstem" in text))


def make_record(name, kind, segments, grouped):
    identity = f"{kind}:{name}" if grouped else segments[0]["id"]
    return {
        "id": stable_id(identity),
        "name": name,
        "type": kind,
        "grouped": grouped,
        "segmentCount": len(segments),
        "segments": segments,
    }


def main():
    files = sorted(DATA_DIR.glob("*.geojson"))
    if not files:
        raise SystemExit(f"No GeoJSON files found in {DATA_DIR}")

    # All handles close in this order, including after an exception:
    # SQLite connection, then temporary directory.
    with tempfile.TemporaryDirectory() as temp_dir:
        with closing(sqlite3.connect(str(Path(temp_dir) / "processing.sqlite"))) as db:
            db.execute("""
                CREATE TABLE records (
                    id TEXT PRIMARY KEY,
                    name TEXT NOT NULL,
                    kind TEXT NOT NULL,
                    basin TEXT NOT NULL,
                    parent TEXT,
                    eligible INTEGER NOT NULL,
                    group_key TEXT,
                    data TEXT NOT NULL
                )
            """)

            total = 0
            for path in files:
                kind = dataset_type(path.name)
                print(f"Reading {path.name} ({kind})...", flush=True)
                with path.open("rb") as handle:
                    for index, feature in enumerate(
                        ijson.items(handle, "features.item", use_float=True)
                    ):
                        props = feature.get("properties") or {}
                        name = str(props.get("WATERBODY") or "").strip()
                        if not name:
                            raise ValueError(f"Missing WATERBODY in {path.name}, feature {index}")
                        basin = str(props.get("BASIN_NAME") or "").strip()
                        parent, suffix = proposed_parent(name)
                        eligible = int(
                            (kind in ("lake", "estuary") and eligible_section(suffix))
                            or (kind == "stream" and eligible_stream_section(parent, suffix))
                        )
                        feature_id = f"{kind}:{index}"
                        segment = {
                            "id": feature_id,
                            "name": name,
                            "segmentId": props.get("SEG_ID"),
                            "win": props.get("WIN"),
                            "description": props.get("DESCRIPT"),
                            "assessment": props.get("WATERBODY_CATEGORY"),
                            "classification": props.get("CLASS"),
                            "assessmentYear": props.get("CYCLE_LAST_ASSESSED"),
                            "basin": basin,
                            "factsheet": props.get("FACTSHEET"),
                            "properties": props,
                            "geometry": feature.get("geometry"),
                        }
                        db.execute("""
                            INSERT INTO records
                                (id, name, kind, basin, parent, eligible, data)
                            VALUES (?, ?, ?, ?, ?, ?, ?)
                        """, (feature_id, name, kind, basin, parent, eligible,
                              json.dumps(segment, separators=(",", ":"))))
                        total += 1
                        if total % 250 == 0:
                            db.commit()
                        if total % 500 == 0:
                            print(f"  {total:,} features processed", flush=True)
                db.commit()

            db.execute("CREATE INDEX idx_parent ON records(kind, parent)")
            candidates = db.execute("""
                SELECT kind, parent, COUNT(*), COUNT(DISTINCT basin),
                       SUM(eligible), GROUP_CONCAT(name, ' | ')
                FROM records
                WHERE parent IS NOT NULL
                GROUP BY kind, parent
                HAVING COUNT(*) > 1
                ORDER BY kind, parent
            """).fetchall()

            approved = []
            report_rows = []
            for kind, parent, count, basins, eligible_count, names in candidates:
                expected = (VERIFIED_LAKES.get(parent) if kind == "lake"
                            else APPROVED_ESTUARIES.get(parent) if kind == "estuary"
                            else None)
                if basins != 1:
                    decision = "REVIEW_MULTIPLE_BASINS"
                elif expected is not None and count == expected:
                    decision = "GROUP_VERIFIED"
                elif expected is not None and count != expected:
                    decision = "REVIEW_COUNT_MISMATCH"
                elif eligible_count == count:
                    decision = "GROUP_AUTOMATIC"
                elif kind == "stream":
                    decision = "REVIEW_STREAM_NETWORK"
                else:
                    decision = "REVIEW_AMBIGUOUS_NAMES"

                if decision.startswith("GROUP_"):
                    key = f"{kind}:{parent}"
                    db.execute("""
                        UPDATE records SET group_key = ?
                        WHERE kind = ? AND parent = ?
                    """, (key, kind, parent))
                    approved.append((key, kind, parent))

                report_rows.append({
                    "type": kind, "proposed_parent": parent,
                    "segments": count, "basins": basins,
                    "decision": decision, "names": names,
                })
            db.commit()

            # Replace the processed dataset only after writing it successfully.
            temporary_output = OUTPUT.with_suffix(".jsonl.tmp")
            try:
                written = 0
                with temporary_output.open("w", encoding="utf-8") as output:
                    cursor = db.execute("""
                        SELECT name, kind, data FROM records
                        WHERE group_key IS NULL ORDER BY kind, name, id
                    """)
                    for name, kind, data in cursor:
                        output.write(json.dumps(make_record(
                            name, kind, [json.loads(data)], False
                        ), separators=(",", ":")) + "\n")
                        written += 1

                    for key, kind, parent in approved:
                        segments = [json.loads(row[0]) for row in db.execute("""
                            SELECT data FROM records WHERE group_key = ?
                            ORDER BY name, id
                        """, (key,))]
                        output.write(json.dumps(make_record(
                            parent, kind, segments, True
                        ), separators=(",", ":")) + "\n")
                        written += 1
                temporary_output.replace(OUTPUT)
            finally:
                temporary_output.unlink(missing_ok=True)

            with REPORT.open("w", newline="", encoding="utf-8") as handle:
                writer = csv.DictWriter(handle, fieldnames=[
                    "type", "proposed_parent", "segments", "basins",
                    "decision", "names",
                ])
                writer.writeheader()
                writer.writerows(report_rows)

            print("\n=== PREPROCESSING COMPLETE ===")
            print(f"Original DEC features: {total:,}")
            print(f"Output waterbodies: {written:,}")
            print(f"Approved groups: {len(approved)}")
            print(f"New automatic groups: {sum(r['decision'] == 'GROUP_AUTOMATIC' for r in report_rows)}")
            print(f"Groups needing review: {sum(r['decision'].startswith('REVIEW_') for r in report_rows)}")
            print(f"Dataset: {OUTPUT}")
            print(f"Report: {REPORT}")


if __name__ == "__main__":
    main()
