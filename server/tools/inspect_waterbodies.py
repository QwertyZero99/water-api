
from pathlib import Path
import json
import ijson

ROOT = Path(__file__).resolve().parents[2]
DATA_DIR = ROOT / "data" / "raw"

files = sorted(DATA_DIR.glob("*.geojson"))

if not files:
    print("No GeoJSON files found.")
    raise SystemExit(1)

for path in files:
    print("\n" + "=" * 60)
    print("FILE:", path.name)
    print("SIZE (MB):", round(path.stat().st_size / 1_000_000, 1))

    with path.open("rb") as f:
        first = next(ijson.items(f, "features.item"), None)

    if first is None:
        print("No features found.")
        continue

    properties = first.get("properties") or {}

    print("\nPROPERTY NAMES AND SAMPLE VALUES:")

    for key, value in properties.items():
        print(f"  {key}: {str(value)[:180]}")

    geometry = first.get("geometry") or {}
    print("\nGEOMETRY TYPE:", geometry.get("type"))
    print("FEATURE ID:", first.get("id"))
