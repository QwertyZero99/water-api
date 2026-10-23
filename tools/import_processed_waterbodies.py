
import json
import getpass
from pathlib import Path

import psycopg
from psycopg.types.json import Jsonb


ROOT = Path(__file__).resolve().parents[2]
DATA_FILE = ROOT / "data" / "raw" / "waterbodies-processed.jsonl"

EXPECTED_WATERBODIES = 4609
EXPECTED_SEGMENTS = 4689


def main():
    if not DATA_FILE.exists():
        raise SystemExit(f"Missing processed dataset: {DATA_FILE}")

    print("Importing processed DEC waterbodies.")
    print("The original waterbodies and grouping tables will not be modified.")
    print()

    password = getpass.getpass("PostgreSQL password: ")

    with psycopg.connect(
        host="localhost",
        port=5432,
        dbname="postgres",
        user="postgres",
        password=password,
    ) as conn:

        # All changes occur in one transaction.
        # An error rolls back the import.
        with conn.transaction():

            with conn.cursor() as cur:
                cur.execute("""
                    CREATE TABLE IF NOT EXISTS waterbodies_v2 (
                        id TEXT PRIMARY KEY,
                        name TEXT NOT NULL,
                        waterbody_type TEXT NOT NULL,
                        grouped BOOLEAN NOT NULL DEFAULT FALSE,
                        segment_count INTEGER NOT NULL,
                        segments JSONB NOT NULL
                    )
                """)

                # Reimporting replaces only v2 data.
                # Existing DEC data stays untouched.
                cur.execute("DELETE FROM waterbodies_v2")

                count = 0
                segment_total = 0

                with DATA_FILE.open(
                    "r", encoding="utf-8"
                ) as file:

                    for line_number, line in enumerate(file, 1):
                        if not line.strip():
                            continue

                        record = json.loads(line)

                        segments = record["segments"]

                        if record["segmentCount"] != len(segments):
                            raise ValueError(
                                f"Invalid segment count on line {line_number}"
                            )

                        cur.execute("""
                            INSERT INTO waterbodies_v2 (
                                id,
                                name,
                                waterbody_type,
                                grouped,
                                segment_count,
                                segments
                            )
                            VALUES (%s, %s, %s, %s, %s, %s)
                        """, (
                            record["id"],
                            record["name"],
                            record["type"],
                            record["grouped"],
                            len(segments),
                            Jsonb(segments),
                        ))

                        count += 1
                        segment_total += len(segments)

                        if count % 500 == 0:
                            print(
                                f"Imported {count:,} waterbodies...",
                                flush=True
                            )

                if count != EXPECTED_WATERBODIES:
                    raise ValueError(
                        f"Expected {EXPECTED_WATERBODIES} "
                        f"waterbodies, imported {count}"
                    )

                if segment_total != EXPECTED_SEGMENTS:
                    raise ValueError(
                        f"Expected {EXPECTED_SEGMENTS} "
                        f"segments, imported {segment_total}"
                    )

                cur.execute("""
                    CREATE INDEX IF NOT EXISTS
                        waterbodies_v2_name_idx
                    ON waterbodies_v2 (lower(name))
                """)

                cur.execute("""
                    CREATE INDEX IF NOT EXISTS
                        waterbodies_v2_type_idx
                    ON waterbodies_v2 (waterbody_type)
                """)

                print()
                print("=== IMPORT COMPLETE ===")
                print(f"Waterbodies: {count:,}")
                print(f"Original segments: {segment_total:,}")
                print("Table: waterbodies_v2")

    print("Transaction committed successfully.")


if __name__ == "__main__":
    main()
