
import { initWaterbodySearch } from "./waterbodySearch.js?20261025-2";
const NY_BBOX = [-79.8, 40.4, -71.8, 45.1];

const INITIAL_CENTER = [-75, 43];
const INITIAL_ZOOM = 7;
const INITIAL_PITCH = 45;
const INITIAL_BEARING = -8;

const NEARBY_RADIUS_MILES = 20;
const MAX_NEARBY_STATIONS = 5;

const NY_BOUNDARY_URL =
  "https://raw.githubusercontent.com/PublicaMundi/MappingAPI/master/data/geojson/us-states.json";

const PARAMETER_NAMES = {
  "00010": "Water temperature",
  "00060": "Streamflow",
  "00065": "Water level (gage height)",
  "00095": "Specific conductance",
  "00300": "Dissolved oxygen",
  "00400": "pH",
  "63680": "Turbidity",
  "72019": "Depth to groundwater",
  "62619": "Water surface elevation (NGVD29)",
  "62620": "Water surface elevation (NAVD88)",
  "90860": "Salinity (at 25°C)",
  "00020": "Air temperature",
  "00021": "Air temperature (Fahrenheit)",
  "00025": "Barometric pressure",
  "00030": "Solar radiation",
  "00032": "Cloud cover",
  "00035": "Wind speed",
  "00036": "Wind direction",
  "00045": "Precipitation",
  "00061": "Instantaneous streamflow",
  "00062": "Reservoir water elevation",
  "00064": "Mean stream depth",
  "00072": "Stream stage",
  "00076": "Turbidity (NTU)",
  "00077": "Water transparency (Secchi disk)",
  "00078": "Water transparency (Secchi disk, meters)",
  "00080": "Water color",
  "00081": "Apparent water color",
  "00402": "Specific conductance (uncorrected)",
  "00480": "Salinity",
};

let activeMeasurementCoverage = "latest";
function getWaterCategory(siteTypeCode) {
  const code = String(siteTypeCode ?? "").trim().toUpperCase();

  if (code === "GW" || code.startsWith("GW-")) {
    return "groundwater";
  }

  if (
    code === "ST" || code.startsWith("ST-") ||
    code === "LK" || code.startsWith("LK-") ||
    code === "ES" || code.startsWith("ES-")
  ) {
    return "surface";
  }

  return "other";
}


// AquaNY contextual measurement links: values navigate to the matching guide section.
const AQUANY_INFO_BASE = "/info";
function aquanyInfoLink(valueNode, sectionId) {
  if (!sectionId) return valueNode;
  const anchor = document.createElement("a");
  anchor.href = `${AQUANY_INFO_BASE}#${sectionId}`;
  anchor.className = "aquany-reading-link";
  anchor.title = "Understand this measurement";
  anchor.setAttribute("aria-label", `Learn about ${sectionId.replaceAll('-', ' ')}: ${valueNode.textContent}`);
  anchor.append(valueNode);
  return anchor;
}
const AQUANY_USGS_GUIDE = {"00010": "water-temperature", "00011": "water-temperature", "00060": "streamflow", "00061": "streamflow", "00065": "gage-height", "00095": "specific-conductance", "00300": "dissolved-oxygen", "00400": "ph", "63680": "turbidity", "00076": "turbidity", "00480": "salinity", "90860": "salinity", "62619": "water-surface-elevation", "62620": "water-surface-elevation"};

function getParameterName(code) {
  return PARAMETER_NAMES[code] ?? null;
}

function formatMapStationName(name) {
  if (!name) return 'Water Monitoring Station';

  const smallWords = new Set([
    'at', 'of', 'the', 'and', 'in', 'on', 'near', 'to'
  ]);

  const abbreviations = new Set([
    'NY', 'US', 'USA', 'USGS', 'S', 'N', 'E', 'W',
    'NE', 'NW', 'SE', 'SW'
  ]);

  const formatted = String(name)
    .trim()
    .toLowerCase()
    .split(/\s+/)
    .map((word, index) => {
      const upper = word.toUpperCase();

      if (abbreviations.has(upper)) return upper;
      if (index > 0 && smallWords.has(word)) return word;

      return word.charAt(0).toUpperCase() + word.slice(1);
    })
    .join(' ');

  return formatted.replace(/\s+NY$/, ', NY');
}

function formatMapMeasurementUnit(unit) {
  if (!unit) return '';

  const normalized = String(unit).trim();

  const knownUnits = {
    'ft^3/s': 'ft³/s',
    'ft3/s': 'ft³/s',
    'ft³/s': 'ft³/s',
    'degc': '°C',
    'degf': '°F',
    'us/cm': 'µS/cm',
    'µs/cm': 'µS/cm',
    'μs/cm': 'µS/cm',
    'mg/l': 'mg/L',
    'ug/l': 'µg/L',
    '_fnu': 'FNU',
    '_ntu': 'NTU'
  };

  return knownUnits[normalized.toLowerCase()] ?? normalized;
}


function getStationName(station) {
  const value =
    station?.MonitoringLocationName ??
    station?.monitoring_location_name ??
    station?.monitoringLocationName ??
    station?.Name ??
    station?.name ??
    station?.station_name ??
    station?.StationName;

  return typeof value === "string" && value.trim()
    ? value.trim()
    : null;
}

function getWaterbodyName(station) {
  const value =
    station?.waterbody_name ??
    station?.waterbodyName ??
    station?.waterBodyName ??
    station?.waterbody ??
    station?.waterBody ??
    station?.properties?.waterbody_name ??
    station?.Feature?.properties?.waterbody_name ??
    station?.feature?.properties?.waterbody_name;

  return typeof value === "string" && value.trim()
    ? value.trim()
    : null;
}

function getStationId(station) {
  const value =
    station?.id ??
    station?.ID ??
    station?.station_id ??
    station?.MonitoringLocationNumber ??
    station?.monitoring_location_number;

  return value == null ? "" : String(value).trim();
}

function getStationLocation(station) {
  const location =
    station?.location ??
    station?.Location ??
    station?.geometry?.coordinates ??
    station?.Geometry?.coordinates;

  if (!Array.isArray(location) || location.length < 2) {
    return null;
  }

  const lng = Number(location[0]);
  const lat = Number(location[1]);

  if (!Number.isFinite(lng) || !Number.isFinite(lat)) {
    return null;
  }

  return [lng, lat];
}

function getSiteType(station) {
  return (
    station?.siteType ??
    station?.SiteType ??
    station?.site_type ??
    ""
  );
}

function getSiteTypeCode(station) {
  return (
    station?.siteTypeCode ??
    station?.SiteTypeCode ??
    station?.site_type_code ??
    ""
  );
}

// ==================================================
// 2. GEOGRAPHIC HELPERS
// ==================================================

function pointInRing(point, ring) {
  const [x, y] = point;
  let inside = false;

  for (let i = 0, j = ring.length - 1; i < ring.length; j = i++) {
    const [xi, yi] = ring[i];
    const [xj, yj] = ring[j];

    if (
      (yi > y) !== (yj > y) &&
      x < ((xj - xi) * (y - yi)) / (yj - yi) + xi
    ) {
      inside = !inside;
    }
  }

  return inside;
}

function pointInPolygon(point, rings) {
  if (!rings?.length || !pointInRing(point, rings[0])) {
    return false;
  }

  // Points inside a hole are not inside the polygon.
  for (let i = 1; i < rings.length; i++) {
    if (pointInRing(point, rings[i])) {
      return false;
    }
  }

  return true;
}

function pointInGeometry(point, geometry) {
  if (!geometry) return false;

  if (geometry.type === "Polygon") {
    return pointInPolygon(point, geometry.coordinates);
  }

  if (geometry.type === "MultiPolygon") {
    return geometry.coordinates.some(polygon =>
      pointInPolygon(point, polygon)
    );
  }

  return false;
}

function distanceMiles(lat1, lng1, lat2, lng2) {
  const earthRadiusMiles = 3958.8;
  const radians = value => value * Math.PI / 180;

  const dLat = radians(lat2 - lat1);
  const dLng = radians(lng2 - lng1);

  const a =
    Math.sin(dLat / 2) ** 2 +
    Math.cos(radians(lat1)) *
      Math.cos(radians(lat2)) *
      Math.sin(dLng / 2) ** 2;

  return 2 * earthRadiusMiles *
    Math.asin(Math.min(1, Math.sqrt(a)));
}

async function loadNYBoundary() {
  const response = await fetch(NY_BOUNDARY_URL);

  if (!response.ok) {
    throw new Error(
      `NY boundary request failed: HTTP ${response.status}`
    );
  }

  const data = await response.json();

  const feature = data.features?.find(item =>
    item.properties?.name === "New York" ||
    item.properties?.NAME === "New York"
  );

  if (
    !feature?.geometry ||
    !["Polygon", "MultiPolygon"].includes(feature.geometry.type)
  ) {
    throw new Error("New York boundary not found");
  }

  return feature.geometry;
}

// ==================================================
// 3. API REQUESTS
// ==================================================

async function fetchJSON(url, options = {}) {
  const response = await fetch(url, {
    ...options,
    headers: {
      Accept: "application/json",
      ...options.headers,
    },
  });

  if (!response.ok) {
    throw new Error(`Request failed: HTTP ${response.status}`);
  }

  return response.json();
}

async function fetchStationDetails(id) {
  return fetchJSON(`/api/stations/${encodeURIComponent(id)}`);
}

async function fetchStationMeasurements(id) {
  return fetchJSON(
    `/api/stations/${encodeURIComponent(id)}/measurements`
  );
}

// ==================================================
// 4. MAP SOURCES AND LAYERS
// ==================================================

function emptyGeoJSON() {
  return {
    type: "FeatureCollection",
    features: [],
  };
}

function addStationSources(map) {
  map.addSource("stations", {
    type: "geojson",
    data: emptyGeoJSON(),
    cluster: true,
    clusterMaxZoom: 12,
    clusterRadius: 60,
  });

  map.addSource("nearby-stations", {
    type: "geojson",
    data: emptyGeoJSON(),
  });
}

function addStationLayers(map) {
  map.addLayer({
    id: "station-clusters",
    type: "circle",
    source: "stations",
    filter: ["has", "point_count"],
    paint: {
      "circle-color": "#1976d2",
      "circle-opacity": 0.85,
      "circle-radius": [
        "step",
        ["get", "point_count"],
        18,
        20, 23,
        100, 30,
      ],
      "circle-stroke-color": "#ffffff",
      "circle-stroke-width": 2,
    },
  });

  map.addLayer({
    id: "station-cluster-count",
    type: "symbol",
    source: "stations",
    filter: ["has", "point_count"],
    layout: {
      "text-field": ["get", "point_count_abbreviated"],
      "text-size": 12,
      "text-allow-overlap": true,
      "text-ignore-placement": true,
    },
    paint: {
      "text-color": "#ffffff",
    },
  });

  map.addLayer({
    id: "station-circles",
    type: "circle",
    source: "stations",
    filter: ["!", ["has", "point_count"]],
    paint: {
      "circle-radius": [
        "interpolate",
        ["linear"],
        ["zoom"],
        6, 5,
        9, 8,
        13, 12,
      ],
      "circle-color": "#1976d2",
      "circle-opacity": 0.85,
      "circle-stroke-color": "#ffffff",
      "circle-stroke-width": 2,
    },
  });

  map.addLayer({
    id: "nearby-station-highlights",
    type: "circle",
    source: "nearby-stations",
    paint: {
      "circle-radius": 14,
      "circle-color": [
        "match",
        ["get", "category"],
        "groundwater", "#217842",
        "surface", "#1c7bb9",
        "#64748b",
      ],
      "circle-opacity": 0.95,
      "circle-stroke-color": "#ffffff",
      "circle-stroke-width": 3,
    },
  });
}

function addMapControls(map) {
  map.addControl(
    new maplibregl.NavigationControl({
      showZoom: true,
      showCompass: true,
      visualizePitch: true,
    }),
    "top-left"
  );
}

function createBaseMap() {
  return new maplibregl.Map({
    container: "map",
    center: INITIAL_CENTER,
    zoom: INITIAL_ZOOM,
    pitch: INITIAL_PITCH,
    bearing: INITIAL_BEARING,
    maxZoom: 18,
    maxPitch: 80,
    maxBounds: [
      [-79.8, 40.4],
      [-71.8, 45.1],
    ],
    renderWorldCopies: false,
    style: {
      version: 8,
      sources: {
        esriTopo: {
          type: "raster",
          tiles: [
            "https://server.arcgisonline.com/ArcGIS/rest/services/World_Topo_Map/MapServer/tile/{z}/{y}/{x}",
          ],
          tileSize: 256,
          attribution:
            "Esri, TomTom, Garmin, FAO, NOAA, USGS, OpenStreetMap contributors",
        },
        terrainSource: {
          type: "raster-dem",
          url: "https://tiles.mapterhorn.com/tilejson.json",
        },
        hillshadeSource: {
          type: "raster-dem",
          url: "https://tiles.mapterhorn.com/tilejson.json",
        },
      },
      layers: [
        {
          id: "esri-topographic",
          type: "raster",
          source: "esriTopo",
        },
        {
          id: "terrain-hillshade",
          type: "hillshade",
          source: "hillshadeSource",
          layout: { visibility: "visible" },
          paint: { "hillshade-exaggeration": 0.25 },
        },
      ],
      terrain: {
        source: "terrainSource",
        exaggeration: 1.15,
      },
    },
  });
}

// ==================================================
// 5. STATION POPUPS AND MEASUREMENTS
// ==================================================

function createPopupMessage(container, message, isError = false) {
  const messageElement = document.createElement("div");

  messageElement.className = isError
    ? "measurement-empty measurement-error"
    : "measurement-empty";

  messageElement.textContent = message;

  container.replaceChildren(messageElement);
}

function createStationPopupContent(details, id) {
  const container = document.createElement("div");
  container.className = "station-popup";

  const header = document.createElement("div");
  header.className = "station-popup-header";

  const eyebrow = document.createElement("div");
  eyebrow.className = "station-popup-eyebrow";
  eyebrow.textContent = "USGS Monitoring Station";

  const title = document.createElement("h3");
  
  title.textContent = formatMapStationName(
    getWaterbodyName(details) ??
    getStationName(details) ??
    "Water Monitoring Station"
  );
  header.append(eyebrow, title);

  const stationName = getStationName(details);

  if (stationName && stationName !== title.textContent) {
    const subtitle = document.createElement("p");
    subtitle.className = "station-popup-subtitle";
    subtitle.textContent = formatMapStationName(stationName);
    header.append(subtitle);
  }

  const stationID = document.createElement("p");
  stationID.className = "station-popup-id";
  stationID.textContent = `Station ID: ${getStationId(details) || id}`;

  header.append(stationID);

  const measurementsDiv = document.createElement("div");
  measurementsDiv.className = "station-measurements";
  measurementsDiv.setAttribute("aria-live", "polite");

  createPopupMessage(
    measurementsDiv,
    "Loading latest measurements…"
  );

  container.append(header, measurementsDiv);

  return { container, measurementsDiv };
}

async function loadPopupMeasurements(id, container) {
  try {
    const data = await fetchStationMeasurements(id);

    if (!container.isConnected) return;

    if (!Array.isArray(data?.measurements)) {
      console.warn("Unexpected measurement response:", data);

      createPopupMessage(
        container,
        "Measurement data could not be displayed.",
        true
      );

      return;
    }

    
    
// Keep valid readings for parameters we can identify.
const eligibleMeasurements = data.measurements.filter(measurement => {
  if (!measurement) return false;

  const code = String(measurement.parameterCode ?? "").trim();

  if (!getParameterName(code)) return false;

  return (
    measurement.value !== null &&
    measurement.value !== undefined &&
    String(measurement.value).trim() !== ""
  );
});

// Keep only the newest observation for each parameter code.
const latestByParameter = new Map();

for (const measurement of eligibleMeasurements) {
  const code = String(measurement.parameterCode).trim();
  const existing = latestByParameter.get(code);

  const timestamp = Date.parse(measurement.time ?? "");
  const existingTimestamp = existing
    ? Date.parse(existing.time ?? "")
    : NaN;

  if (
    !existing ||
    (Number.isFinite(timestamp) &&
      (!Number.isFinite(existingTimestamp) ||
        timestamp > existingTimestamp))
  ) {
    latestByParameter.set(code, measurement);
  }
}

  const validMeasurements = [...latestByParameter.values()].sort(
    (a, b) => {
      const timeA = Date.parse(a.time ?? "");
      const timeB = Date.parse(b.time ?? "");

      return (
        (Number.isFinite(timeB) ? timeB : -Infinity) -
        (Number.isFinite(timeA) ? timeA : -Infinity)
      );
    }
  );



    if (validMeasurements.length === 0) {
      createPopupMessage(
        container,
        "No measurements for supported parameters are available at this station."
      );

      return;
    }

    const fragment = document.createDocumentFragment();

    for (const measurement of validMeasurements) {
      const row = document.createElement("div");
      row.className = "measurement-row";

      const label = document.createElement("strong");
      label.textContent = getParameterName(
        String(measurement.parameterCode ?? "")
      );

      
      const value = document.createElement("span");
      value.className = "measurement-value";

      const formattedUnit = formatMapMeasurementUnit(measurement.unit);
      const numericValue = Number(measurement.value);

      if (formattedUnit === "°C" && Number.isFinite(numericValue)) {
        // Convert Celsius to Fahrenheit for display.
        const fahrenheit = numericValue * 9 / 5 + 32;

        value.textContent = `${fahrenheit.toLocaleString("en-US", {
          maximumFractionDigits: 1
        })}°F`;

        const secondary = document.createElement("small");

        secondary.textContent = ` (${numericValue.toLocaleString("en-US", {
          maximumFractionDigits: 1
        })}°C)`;

        value.append(secondary);
      } else {
        // Keep the original value for all other measurements.
        value.textContent = String(measurement.value);

        if (formattedUnit) {
          const unit = document.createElement("span");
          unit.className = "measurement-unit";
          unit.textContent = ` ${formattedUnit}`;

          value.append(unit);
        }
      }


      const timestamp = document.createElement("small");

      const date = measurement.time
        ? new Date(measurement.time)
        : null;

      timestamp.textContent =
        date && !Number.isNaN(date.getTime())
          ? `Observed ${date.toLocaleString()}`
          : "Observation time unavailable";

      const guideSection = AQUANY_USGS_GUIDE[String(measurement.parameterCode ?? "").trim()];
      row.append(label, aquanyInfoLink(value, guideSection), timestamp);
      fragment.append(row);
    }

    container.replaceChildren(fragment);

  } catch (error) {
    console.error("Measurement error:", error);

    if (container.isConnected) {
      createPopupMessage(
        container,
        "Unable to retrieve measurements right now.",
        true
      );
    }
  }
}

function createPopupController(map) {
  let activePopup = null;

  async function showStationPopup(id, coordinates) {
    activePopup?.remove();

    const popup = new maplibregl.Popup({
      offset: 15,
      closeButton: true,
      closeOnClick: false,
      maxWidth: "360px",
    })
      .setLngLat(coordinates)
      .setText(`Loading station ${id}...`)
      .addTo(map);

    activePopup = popup;

    try {
      const details = await fetchStationDetails(id);

      // Prevent a slow request from overwriting a newer popup.
      if (activePopup !== popup || !popup.isOpen()) return;

      const { container, measurementsDiv } =
        createStationPopupContent(details, id);

      popup.setDOMContent(container);
      void loadPopupMeasurements(id, measurementsDiv);
    } catch (error) {
      console.error("Station popup failed:", error);

      if (activePopup === popup && popup.isOpen()) {
        popup.setText("Unable to load station information.");
      }
    }
  }

  return {
    showStationPopup,
    close() {
      activePopup?.remove();
      activePopup = null;
    },
  };
}

// ==================================================
// 6. LOAD AND NORMALIZE STATIONS
// ==================================================

async function loadStations(map) {
  const bbox = NY_BBOX.join(",");

  const [stations, nyBoundary] = await Promise.all([
    fetchJSON(`/api/stations?bbox=${encodeURIComponent(bbox)}`),
    loadNYBoundary(),
  ]);

  if (!Array.isArray(stations)) {
    throw new Error("Invalid station response");
  }

  const features = [];
  let excluded = 0;
  let invalid = 0;

  for (const station of stations) {
    const id = getStationId(station);
    const location = getStationLocation(station);

    if (!id || !location) {
      invalid++;
      continue;
    }

    const [lng, lat] = location;

    if (!pointInGeometry([lng, lat], nyBoundary)) {
      excluded++;
      continue;
    }

    features.push({
      type: "Feature",
      geometry: {
        type: "Point",
        coordinates: [lng, lat],
      },
      properties: {
        id,
        name: getStationName(station) ??
          station.name ?? station.Name ?? "",
        siteType: getSiteType(station),
        siteTypeCode: getSiteTypeCode(station),
        category: getWaterCategory(getSiteTypeCode(station)),
      },
    });
  }


  console.log("AquaNY stations loaded");
  console.log("Total records:", stations.length);
  console.log("NY stations:", features.length);
  console.log("Outside NY:", excluded);
  console.log("Invalid:", invalid);

  return features;
}

// ==================================================
// 7. NEARBY STATION SEARCH AND RESULTS
// ==================================================

function findNearbyStations(stations, lat, lng) {
  return stations
    .map(feature => {
      const [stationLng, stationLat] =
        feature.geometry.coordinates;

      return {
        id: feature.properties.id,
        name: feature.properties.name,
        siteType: feature.properties.siteType,
        category: feature.properties.category,
        feature,
        distance: distanceMiles(
          lat,
          lng,
          stationLat,
          stationLng
        ),
      };
    })
    .filter(station => station.distance <= NEARBY_RADIUS_MILES)
    .sort((a, b) => a.distance - b.distance);
}

function renderNearbyStationGroup(
  resultsDiv,
  title,
  stations,
  onStationClick
) {
  const section = document.createElement("section");
  section.className = "water-category";

  const groupHeading = document.createElement("h4");
  groupHeading.textContent = `${title} (${stations.length})`;
  section.append(groupHeading);

if (stations.length === 0) {
  const message = document.createElement("p");
  message.className = "water-category-empty";

  message.textContent =
    "No nearby monitoring stations found in this category.";

  section.append(message);
}

  for (const station of stations) {
    const item = document.createElement("button");
    item.type = "button";
    item.className = "nearby-station-result";
    item.classList.add(
      station.category === "groundwater"
        ? "groundwater-result"
        : "surface-result"
    );

    const name = document.createElement("strong");
    name.textContent =
      station.name || `Monitoring station ${station.id}`;

    const type = document.createElement("span");
    type.textContent = station.siteType || title;

    const distance = document.createElement("span");
    distance.textContent = `${station.distance.toFixed(1)} mi`;

    item.append(name, type, distance);
    item.addEventListener("click", () => onStationClick(station));
    section.append(item);
  }

  resultsDiv.append(section);
}

function renderNearbyStationResults(
  resultsDiv,
  selected,
  onStationClick
) {
  resultsDiv.replaceChildren();

  const heading = document.createElement("h3");
  heading.textContent = "Water Monitoring Near You";
  resultsDiv.append(heading);

  if (selected.length === 0) {
    const empty = document.createElement("p");
    empty.textContent =
      `No groundwater or surface-water monitoring stations found within ${NEARBY_RADIUS_MILES} miles.`;
    resultsDiv.append(empty);
    return;
  }

  renderNearbyStationGroup(
    resultsDiv,
    "Surface Water",
    selected.filter(station => station.category === "surface"),
    onStationClick
  );

  renderNearbyStationGroup(
    resultsDiv,
    "Groundwater",
    selected.filter(station => station.category === "groundwater"),
    onStationClick
  );
}

function createNearbyStationController(
  map,
  getStations,
  popupController
) {
  let searchVersion = 0;
  let nearbyStations = [];

  async function highlightNearbyStations(lat, lng, version) {
    const resultsDiv =
      document.getElementById("nearbyStationResults");

    if (resultsDiv) {
      resultsDiv.textContent =
        "Finding nearby water monitoring stations...";
    }

    const nearby = findNearbyStations(getStations(), lat, lng);

    if (version !== searchVersion) return;

    const groundwater = nearby
      .filter(station => station.category === "groundwater")
      .slice(0, MAX_NEARBY_STATIONS);

    const surfaceWater = nearby
      .filter(station => station.category === "surface")
      .slice(0, MAX_NEARBY_STATIONS);

    nearbyStations = [...surfaceWater, ...groundwater];

    map.getSource("nearby-stations").setData({
      type: "FeatureCollection",
      features: nearbyStations.map(station => ({
        ...station.feature,
        properties: {
          ...station.feature.properties,
          category: station.category,
        },
      })),
    });

    if (!resultsDiv) return;

    renderNearbyStationResults(
      resultsDiv,
      nearbyStations,
      station => {
        const coordinates =
          station.feature.geometry.coordinates;

        map.flyTo({
          center: coordinates,
          zoom: 13,
          speed: 1.2,
        });

        void popupController.showStationPopup(
          station.id,
          coordinates
        );
      }
    );

    console.log(
      "Nearby surface-water stations:",
      surfaceWater.length
    );
    console.log(
      "Nearby groundwater stations:",
      groundwater.length
    );
  }

  return {
    nextSearchVersion() {
      searchVersion++;
      return searchVersion;
    },
    isCurrentSearch(version) {
      return version === searchVersion;
    },
    highlightNearbyStations,
    getNearbyStations() {
      return nearbyStations;
    },
  };
}

// ==================================================
// 8. ZIP-CODE SEARCH
// ==================================================

function bindZipSearch(map, nearbyController) {
  const zipForm = document.getElementById("zipSearchForm");
  const zipInput = document.getElementById("zipInput");
  const zipStatus = document.getElementById("zipSearchStatus");
  const zipButton = document.getElementById("zipSearchButton");

  let zipMarker = null;

  if (!zipForm || !zipInput || !zipStatus || !zipButton) {
    return;
  }

  zipInput.addEventListener("input", () => {
    zipInput.value = zipInput.value.replace(/\D/g, "").slice(0, 5);
  });

  async function searchZipCode(zip) {
    const version = nearbyController.nextSearchVersion();

    if (!/^\d{5}$/.test(zip)) {
      zipStatus.textContent =
        "Please enter a valid 5-digit ZIP code.";
      return;
    }

    zipButton.disabled = true;
    zipStatus.textContent = "Finding your location...";

    try {
      const response =
        await fetch(`https://api.zippopotam.us/us/${zip}`);

      if (response.status === 404) {
        throw new Error("ZIP code not found.");
      }

      if (!response.ok) {
        throw new Error("ZIP lookup is temporarily unavailable.");
      }

      const data = await response.json();

      if (!nearbyController.isCurrentSearch(version)) return;

      const place = data.places?.find(
        item => item["state abbreviation"] === "NY"
      );

      if (!place) {
        throw new Error(
          "Please enter a New York State ZIP code."
        );
      }

      const lng = Number(place.longitude);
      const lat = Number(place.latitude);

      if (!Number.isFinite(lng) || !Number.isFinite(lat)) {
        throw new Error("Unable to locate this ZIP.");
      }

      zipMarker?.remove();

      zipMarker = new maplibregl.Marker({ color: "#ef4444" })
        .setLngLat([lng, lat])
        .setPopup(
          new maplibregl.Popup({ offset: 20 }).setText(
            `${place["place name"]}, NY ${zip}`
          )
        )
        .addTo(map);

      map.flyTo({
        center: [lng, lat],
        zoom: 11,
        speed: 1.2,
        curve: 1.2,
      });

      zipStatus.textContent =
        `Showing monitoring stations near ${place["place name"]}, NY ${zip}`;

      await nearbyController.highlightNearbyStations(
        lat,
        lng,
        version
      );
    } catch (error) {
      if (nearbyController.isCurrentSearch(version)) {
        zipStatus.textContent =
          error.message ?? "Unable to search ZIP.";
        console.error("ZIP search failed:", error);
      }
    } finally {
      if (nearbyController.isCurrentSearch(version)) {
        zipButton.disabled = false;
      }
    }
  }

  zipForm.addEventListener("submit", event => {
    event.preventDefault();
    void searchZipCode(zipInput.value.trim());
  });
}

// ==================================================
// 9. MAP CLICK AND HOVER INTERACTIONS
// ==================================================

function bindStationInteractions(map, popupController) {
  map.on("click", async event => {
    const features = map.queryRenderedFeatures(event.point, {
      layers: [
        "nearby-station-highlights",
        "station-circles",
        "station-clusters",
      ],
    });

    const feature = features[0];
    if (!feature) return;

    const id = feature.properties?.id;

    if (id) {
      await popupController.showStationPopup(
        String(id),
        feature.geometry.coordinates.slice()
      );
      return;
    }

    const clusterId = feature.properties?.cluster_id;
    if (clusterId == null) return;

    try {
      const source = map.getSource("stations");
      const zoom =
        await source.getClusterExpansionZoom(clusterId);

      map.easeTo({
        center: feature.geometry.coordinates,
        zoom: Math.min(zoom + 0.3, map.getMaxZoom()),
        duration: 650,
      });
    } catch (error) {
      console.error("Cluster expansion failed:", error);
    }
  });

  for (const layerId of [
    "station-clusters",
    "station-circles",
    "nearby-station-highlights",
  ]) {
    map.on("mouseenter", layerId, () => {
      map.getCanvas().style.cursor = "pointer";
    });

    map.on("mouseleave", layerId, () => {
      map.getCanvas().style.cursor = "";
    });
  }
}

// ==================================================
// 10. 2D / 3D VIEW CONTROLS
// ==================================================

function createViewControls(map) {
  let viewMode = "3d";

  function setViewMode(mode) {
    if (mode !== "2d" && mode !== "3d") return;

    viewMode = mode;
    map.stop();

    if (mode === "2d") {
      map.setTerrain(null);

      if (map.getLayer("terrain-hillshade")) {
        map.setLayoutProperty(
          "terrain-hillshade",
          "visibility",
          "none"
        );
      }

      map.easeTo({
        pitch: 0,
        bearing: 0,
        duration: 700,
      });
      return;
    }

    map.setTerrain({
      source: "terrainSource",
      exaggeration: 1.15,
    });

    if (map.getLayer("terrain-hillshade")) {
      map.setLayoutProperty(
        "terrain-hillshade",
        "visibility",
        "visible"
      );
    }

    map.easeTo({
      pitch: INITIAL_PITCH,
      bearing: INITIAL_BEARING,
      duration: 700,
    });
  }


  function resetView() {
    map.stop();

    // Switch to 2D using the existing control logic.
    // This also disables terrain and hillshading.
    setViewMode("2d");

    // Fit all of New York State within the map.
    map.fitBounds(
  [
    [-79.9, 40.3],
    [-71.7, 45.2]
  ],
  {
    padding: 50,
    pitch: 0,
    bearing: 0,
    duration: 900
  }
);
  }


  return { setViewMode, resetView };
}


function createMeasurementFilter(map, getAllStations, onFiltered) {
  // Preserve the original default station filters.
  // No visible measurement filter panel is needed.
  const coverage = "latest";
  const recency = "all";

  let requestVersion = 0;

  async function apply() {
    const version = ++requestVersion;

    activeMeasurementCoverage = coverage;

    try {
      const params = new URLSearchParams({
        coverage,
        recency
      });

      const response = await fetchJSON(
        `/api/stations/availability?${params}`
      );

      if (version !== requestVersion) return;

      if (!Array.isArray(response.stationIds)) {
        throw new Error("Invalid availability response");
      }

      const matching = new Set(
        response.stationIds.map(String)
      );

      const stations = getAllStations().filter(feature =>
        matching.has(String(feature.properties.id))
      );

      const source = map.getSource("stations");

      if (source) {
        source.setData({
          type: "FeatureCollection",
          features: stations
        });
      }

      onFiltered(stations);

      console.log(
        `${stations.length.toLocaleString()} stations shown`
      );

    } catch (error) {
      if (version !== requestVersion) return;

      console.error("Availability filter failed:", error);
    }
  }

  return { apply };
}

export async function createMap() {
  const map = createBaseMap();

  addMapControls(map);

  await new Promise((resolve, reject) => {
    map.once("load", resolve);

    map.once("error", event => {
      if (!map.loaded()) {
        reject(
          event.error ?? new Error("Map failed to load")
        );
      }
    });
  });

  addStationSources(map);
  addStationLayers(map);
  initWaterbodySearch(map);

  const popupController = createPopupController(map);
  
  // Open the normal USGS station popup when Locate is clicked
  // in the Current Water Conditions section.
  map.on('aquany:station-locate', ({ stationId, coordinates }) => {
    if (
      !stationId ||
      !Array.isArray(coordinates) ||
      coordinates.length !== 2
    ) {
      return;
    }

    void popupController.showStationPopup(
      stationId,
      coordinates
    );
  });


  let availableStations = [];
  let allStations = [];

  const nearbyController = createNearbyStationController(
    map,
    () => availableStations,
    popupController
  );

  bindStationInteractions(map, popupController);
  bindZipSearch(map, nearbyController);

  const viewControls = createViewControls(map);

  const measurementFilter = createMeasurementFilter(
    map,
    () => allStations,
    stations => {
      availableStations = stations;

      popupController.close();

      map.getSource("nearby-stations").setData(
        emptyGeoJSON()
      );

      const resultsDiv = document.getElementById(
        "nearbyStationResults"
      );

      if (resultsDiv) {
        resultsDiv.replaceChildren();
      }
    }
  );

  try {
    allStations = await loadStations(map);

    await measurementFilter.apply();

  } catch (error) {
    console.error(
      "Failed loading stations:",
      error
    );

    const status = document.getElementById(
      "zipSearchStatus"
    );

    if (status) {
      status.textContent =
        "Monitoring stations could not be loaded.";
    }
  }

  return {
    map,
    resetView: viewControls.resetView,
    setViewMode: viewControls.setViewMode,
  };
}

