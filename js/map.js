import { calculateScore, getColor } from "./score.js";

export async function createMap() {
  const map = L.map("map").setView([43, -75], 7);

  L.tileLayer("https://tile.openstreetmap.org/{z}/{x}/{y}.png", {
    attribution: "© OpenStreetMap",
  }).addTo(map);

  // Station markers currently displayed on the map.
  const markers = new Map();

  // Tiles that have successfully loaded.
  const loadedTiles = new Set();

  // Tiles currently being requested.
  const loadingTiles = new Set();

  async function loadStations() {
    // Leaflet can return fractional zoom levels depending on configuration.
    // The Go server expects an integer XYZ zoom.
    const zoom = Math.floor(map.getZoom());

    const bounds = map.getBounds();
    const tiles = getVisibleTiles(bounds, zoom);

    const requests = [];

    for (const tile of tiles) {
      const key = `${zoom}/${tile.y}/${tile.x}`;

      // Don't request a tile that we already have or are currently loading.
      if (loadedTiles.has(key) || loadingTiles.has(key)) {
        continue;
      }

      loadingTiles.add(key);

      requests.push(loadTile(zoom, tile.x, tile.y, key));
    }

    // Load all visible tiles concurrently.
    await Promise.all(requests);
  }

  async function loadTile(zoom, x, y, key) {
    try {
      const response = await fetch(`/data/${zoom}/${y}/${x}`, {
        headers: {
          Accept: "application/json",
        },
      });

      if (!response.ok) {
        throw new Error(
          `Server returned ${response.status} ${response.statusText}`,
        );
      }

      const chunk = await response.json();

      if (!chunk || !Array.isArray(chunk.stations)) {
        throw new Error("Invalid station response");
      }

      for (const station of chunk.stations) {
        addStationMarker(station);
      }

      // Only mark the tile as loaded after the request succeeded.
      loadedTiles.add(key);
    } catch (err) {
      console.error(`Failed loading station tile ${key}:`, err);
    } finally {
      loadingTiles.delete(key);
    }
  }

  function addStationMarker(station) {
    // Use coordinates as the marker ID.
    //
    // USGS stations normally have unique coordinates, and this also
    // prevents duplicate markers when neighboring tile bounding boxes
    // overlap at their edges.
    const id = `${station.lat},${station.lng}`;

    if (markers.has(id)) {
      return;
    }

    const score = calculateScore(station);
    const color = getColor(score);

    const marker = L.circleMarker([station.lat, station.lng], {
      radius: 12,
      color,
      fillColor: color,
      fillOpacity: 0.8,
      weight: 2,
    });

    marker.addTo(map);

    marker.on("click", () => {
      showStation(station, score);
    });

    markers.set(id, marker);
  }

  function showStation(station, score) {
    const stationDiv = document.getElementById("station");

    if (!stationDiv) {
      return;
    }

    stationDiv.innerHTML = "";

    const title = document.createElement("h2");
    title.textContent = station.name;

    const scoreTitle = document.createElement("h3");
    scoreTitle.textContent = `AquaScore: ${score * 100}/100`;

    const value = document.createElement("p");
    value.textContent = `Streamflow: ${station.value}`;

    const updated = document.createElement("p");

    if (station.time) {
      updated.textContent = `Updated: ${new Date(station.time).toLocaleString()}`;
    } else {
      updated.textContent = "Updated: Unknown";
    }

    stationDiv.append(title, scoreTitle, value, updated);
  }

  // Load stations whenever the map finishes moving or zooming.
  map.on("moveend", loadStations);

  // Initial load.
  await loadStations();
}

function getVisibleTiles(bounds, zoom) {
  const tiles = [];

  const tileCount = Math.pow(2, zoom);

  const north = Math.min(85.05112878, bounds.getNorth());
  const south = Math.max(-85.05112878, bounds.getSouth());

  const west = bounds.getWest();
  const east = bounds.getEast();

  const northWest = latLngToTile(north, west, zoom);

  const southEast = latLngToTile(south, east, zoom);

  /*
   * Normal case:
   *
   *     west ---------------- east
   *
   * The visible tiles are a simple rectangle.
   */
  if (west <= east) {
    for (let x = northWest.x; x <= southEast.x; x++) {
      for (let y = northWest.y; y <= southEast.y; y++) {
        if (x >= 0 && x < tileCount && y >= 0 && y < tileCount) {
          tiles.push({ x, y });
        }
      }
    }

    return tiles;
  }

  /*
   * Antimeridian case.
   *
   * Leaflet can return bounds where:
   *
   *     west > east
   *
   * This means the map crosses ±180° longitude.
   *
   * Split it into:
   *
   *     west → +180
   *     -180 → east
   */
  const firstNorthWest = latLngToTile(north, west, zoom);

  const firstSouthEast = latLngToTile(south, 180, zoom);

  for (let x = firstNorthWest.x; x <= firstSouthEast.x; x++) {
    for (let y = firstNorthWest.y; y <= firstSouthEast.y; y++) {
      if (x >= 0 && x < tileCount && y >= 0 && y < tileCount) {
        tiles.push({ x, y });
      }
    }
  }

  const secondNorthWest = latLngToTile(north, -180, zoom);

  const secondSouthEast = latLngToTile(south, east, zoom);

  for (let x = secondNorthWest.x; x <= secondSouthEast.x; x++) {
    for (let y = secondNorthWest.y; y <= secondSouthEast.y; y++) {
      if (x >= 0 && x < tileCount && y >= 0 && y < tileCount) {
        tiles.push({ x, y });
      }
    }
  }

  return tiles;
}

// Convert latitude/longitude to a standard XYZ tile.
function latLngToTile(lat, lng, zoom) {
  const tileCount = Math.pow(2, zoom);

  // Web Mercator cannot represent the poles.
  lat = Math.max(-85.05112878, Math.min(85.05112878, lat));

  // Normalize longitude to [-180, 180].
  lng = normalizeLongitude(lng);

  let x = Math.floor(((lng + 180) / 360) * tileCount);

  const y = Math.floor(
    ((1 - Math.asinh(Math.tan((lat * Math.PI) / 180)) / Math.PI) / 2) *
      tileCount,
  );

  // Longitude +180 can mathematically produce x=2^zoom.
  // XYZ tiles only go from 0 to 2^zoom - 1.
  x = Math.max(0, Math.min(tileCount - 1, x));

  const clampedY = Math.max(0, Math.min(tileCount - 1, y));

  return {
    x,
    y: clampedY,
  };
}

function normalizeLongitude(lng) {
  return ((((lng + 180) % 360) + 360) % 360) - 180;
}
