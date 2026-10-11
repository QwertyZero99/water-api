
function getWaterCategory(siteTypeCode) {
  const code = String(siteTypeCode ?? "")
    .trim()
    .toUpperCase();

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

const NY_BBOX = [-79.8, 40.4, -71.8, 45.1];

const NY_BOUNDARY_URL =
  "https://raw.githubusercontent.com/PublicaMundi/MappingAPI/master/data/geojson/us-states.json";

const NEARBY_RADIUS_MILES = 20;
const MAX_NEARBY_STATIONS = 5;

// --------------------------------------
// GEOGRAPHIC HELPERS
// --------------------------------------

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
  if (!rings.length || !pointInRing(point, rings[0])) {
    return false;
  }

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
  const R = 3958.8;
  const radians = value => value * Math.PI / 180;

  const dLat = radians(lat2 - lat1);
  const dLng = radians(lng2 - lng1);

  const a =
    Math.sin(dLat / 2) ** 2 +
    Math.cos(radians(lat1)) *
    Math.cos(radians(lat2)) *
    Math.sin(dLng / 2) ** 2;

  return 2 * R * Math.asin(Math.min(1, Math.sqrt(a)));
}

async function loadNYBoundary() {
  const response = await fetch(NY_BOUNDARY_URL);

  if (!response.ok) {
    throw new Error(
      `NY boundary request failed: HTTP ${response.status}`
    );
  }

  const data = await response.json();

  const feature = data.features?.find(feature =>
    feature.properties?.name === "New York" ||
    feature.properties?.NAME === "New York"
  );

  if (
    !feature?.geometry ||
    !["Polygon", "MultiPolygon"].includes(feature.geometry.type)
  ) {
    throw new Error("New York boundary not found");
  }

  return feature.geometry;
}

// --------------------------------------
// CREATE MAP
// --------------------------------------

export async function createMap() {
  const INITIAL_CENTER = [-75, 43];
  const INITIAL_ZOOM = 7;
  const INITIAL_PITCH = 45;
  const INITIAL_BEARING = -8;

  let viewMode = "3d";
  let activePopup = null;
  let zipMarker = null;

  let availableStations = [];
  let nearbyStations = [];
  let searchVersion = 0;

  const map = new maplibregl.Map({
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
          layout: {
            visibility: "visible",
          },
          paint: {
            "hillshade-exaggeration": 0.25,
          },
        },
      ],

      terrain: {
        source: "terrainSource",
        exaggeration: 1.15,
      },
    },
  });

  map.addControl(
    new maplibregl.NavigationControl({
      showZoom: true,
      showCompass: true,
      visualizePitch: true,
    }),
    "top-left",
  );

  await new Promise(resolve => {
    map.once("load", resolve);
  });

  // --------------------------------------
  // STATION SOURCES
  // --------------------------------------

  const emptyGeoJSON = () => ({
    type: "FeatureCollection",
    features: [],
  });

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

  // --------------------------------------
  // CLUSTER LAYERS
  // --------------------------------------

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

  // --------------------------------------
  // INDIVIDUAL STATIONS
  // --------------------------------------

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

  // --------------------------------------
  // NEARBY STATION HIGHLIGHTS
  // --------------------------------------

  // This layer sits above the regular stations.
  // It displays the five selected stations in amber.


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


  // --------------------------------------
  // STATION DETAIL REQUEST
  // --------------------------------------

  async function fetchStationDetails(id) {
    const response = await fetch(
      `/api/stations/${encodeURIComponent(id)}`,
      {
        headers: {
          Accept: "application/json",
        },
      }
    );

    if (!response.ok) {
      throw new Error(
        `Station request failed: HTTP ${response.status}`
      );
    }

    return response.json();
  }


function getStationName(station) {
  const value =
    station.MonitoringLocationName ??
    station.monitoring_location_name ??
    station.monitoringLocationName ??
    station.Name ??
    station.name ??
    station.station_name ??
    station.StationName;

  return typeof value === "string" && value.trim()
    ? value.trim()
    : null;
}


  function getWaterbodyName(station) {
    const value =
      station.waterbody_name ??
      station.waterbodyName ??
      station.waterBodyName ??
      station.waterbody ??
      station.waterBody ??
      station.properties?.waterbody_name;

    return typeof value === "string" && value.trim()
      ? value.trim()
      : null;
  }

  // --------------------------------------
  // STATION POPUP
  // --------------------------------------

  async function showStationPopup(id, coordinates) {
    if (activePopup) {
      activePopup.remove();
    }

    const popup = new maplibregl.Popup({
      offset: 15,
      closeButton: true,
      closeOnClick: false,
      maxWidth: "300px",
    })
      .setLngLat(coordinates)
      .setText(`Loading station ${id}...`)
      .addTo(map);

    activePopup = popup;

    try {
      const details = await fetchStationDetails(id);

      if (activePopup !== popup || !popup.isOpen()) {
        return;
      }

      const container = document.createElement("div");
      container.className = "station-popup";

      const title = document.createElement("h3");
      title.textContent =
        getWaterbodyName(details) ??
        getStationName(details) ??
        "Water Monitoring Station";

      const stationName = getStationName(details);

      container.append(title);

      if (
        stationName &&
        stationName !== title.textContent
      ) {
        const subtitle = document.createElement("p");
        subtitle.textContent = stationName;
        container.append(subtitle);
      }

      const stationID = document.createElement("p");
      stationID.textContent =
        `Station ID: ${details.ID ?? details.id ?? id}`;

      container.append(stationID);

      popup.setDOMContent(container);

    } catch (error) {
      console.error("Station popup failed:", error);

      if (activePopup === popup && popup.isOpen()) {
        popup.setText(
          "Unable to load station information."
        );
      }
    }
  }

  // --------------------------------------
  // MAP CLICK HANDLING
  // --------------------------------------

  map.on("click", async event => {
    const features = map.queryRenderedFeatures(
      event.point,
      {
        layers: [
          "nearby-station-highlights",
          "station-circles",
          "station-clusters",
        ],
      }
    );

    const feature = features[0];

    if (!feature) return;

    const id = feature.properties?.id;

    // Individual station or highlighted station.
    if (id) {
      await showStationPopup(
        id,
        feature.geometry.coordinates.slice()
      );
      return;
    }

    // Numbered cluster.
    const clusterId = feature.properties?.cluster_id;

    if (clusterId == null) return;

    try {
      const source = map.getSource("stations");

      const zoom = await source.getClusterExpansionZoom(
        clusterId
      );

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

  // --------------------------------------
  // LOAD AND FILTER NEW YORK STATIONS
  // --------------------------------------

  async function loadStations() {
    const bbox = NY_BBOX.join(",");

    try {
      const [response, nyBoundary] = await Promise.all([
        fetch(
          `/api/stations?bbox=${encodeURIComponent(bbox)}`,
          {
            headers: {
              Accept: "application/json",
            },
          }
        ),
        loadNYBoundary(),
      ]);

      if (!response.ok) {
        throw new Error(
          `Station API returned HTTP ${response.status}`
        );
      }

      const stations = await response.json();

      if (!Array.isArray(stations)) {
        throw new Error("Invalid station response");
      }

      const features = [];

      let excluded = 0;
      let invalid = 0;

      for (const station of stations) {
        if (
          !station?.id ||
          !Array.isArray(station.location) ||
          station.location.length < 2
        ) {
          invalid++;
          continue;
        }

        const lng = Number(station.location[0]);
        const lat = Number(station.location[1]);

        if (
          !Number.isFinite(lng) ||
          !Number.isFinite(lat)
        ) {
          invalid++;
          continue;
        }

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
    id: station.id,
    name: station.name ?? "",
    siteType: station.siteType ?? "",
    siteTypeCode: station.siteTypeCode ?? "",
    category: getWaterCategory(station.siteTypeCode),
  },
});

      }

      availableStations = features;

      map.getSource("stations").setData({
        type: "FeatureCollection",
        features,
      });

      console.log("AquaNY stations loaded");
      console.log("Total records:", stations.length);
      console.log("NY stations:", features.length);
      console.log("Outside NY:", excluded);
      console.log("Invalid:", invalid);

    } catch (error) {
      console.error("Failed loading stations:", error);
      throw error;
    }
  }

  // --------------------------------------
  // FIND NEARBY STATIONS
  // --------------------------------------


async function highlightNearbyStations(lat, lng, version) {
  const resultsDiv = document.getElementById(
    "nearbyStationResults"
  );

  if (resultsDiv) {
    resultsDiv.textContent =
      "Finding nearby water monitoring stations...";
  }

  // Calculate distance for each station.
  const nearby = availableStations
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
    .filter(station =>
      station.distance <= NEARBY_RADIUS_MILES
    )
    .sort((a, b) => a.distance - b.distance);

  if (version !== searchVersion) return;

  // Separate groundwater and surface water.
  const groundwater = nearby
    .filter(station => station.category === "groundwater")
    .slice(0, MAX_NEARBY_STATIONS);

  const surfaceWater = nearby
    .filter(station => station.category === "surface")
    .slice(0, MAX_NEARBY_STATIONS);

  // Keep other station types available on the map,
  // but don't assign them to the wrong category.
  const selected = [
    ...surfaceWater,
    ...groundwater,
  ];

  nearbyStations = selected;

  // Highlight selected stations on the map.
  map.getSource("nearby-stations").setData({
    type: "FeatureCollection",
    features: selected.map(station => station.feature),
  });

  if (!resultsDiv) return;

  resultsDiv.replaceChildren();

  const heading = document.createElement("h3");
  heading.textContent = "Water Monitoring Near You";
  resultsDiv.append(heading);

  if (selected.length === 0) {
    const empty = document.createElement("p");
    empty.textContent =
      "No groundwater or surface-water monitoring stations " +
      `found within ${NEARBY_RADIUS_MILES} miles.`;

    resultsDiv.append(empty);
    return;
  }

  // Render a separate group for each water category.
  function renderGroup(title, stations) {
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
      type.textContent =
        station.siteType || title;

      const distance = document.createElement("span");
      distance.textContent =
        `${station.distance.toFixed(1)} mi`;

      item.append(name, type, distance);

      item.addEventListener("click", () => {
        const coordinates =
          station.feature.geometry.coordinates;

        map.flyTo({
          center: coordinates,
          zoom: 13,
          speed: 1.2,
        });

        showStationPopup(station.id, coordinates);
      });

      section.append(item);
    }

    resultsDiv.append(section);
  }

  renderGroup("Surface Water", surfaceWater);
  renderGroup("Groundwater", groundwater);

  console.log("Nearby surface-water stations:", surfaceWater.length);
  console.log("Nearby groundwater stations:", groundwater.length);
}


  // --------------------------------------
  // ZIP CODE SEARCH
  // --------------------------------------

  const zipForm = document.getElementById(
    "zipSearchForm"
  );

  const zipInput = document.getElementById(
    "zipInput"
  );

  const zipStatus = document.getElementById(
    "zipSearchStatus"
  );

  const zipButton = document.getElementById(
    "zipSearchButton"
  );

  async function searchZipCode(zip) {
    const version = ++searchVersion;

    if (!/^\d{5}$/.test(zip)) {
      zipStatus.textContent =
        "Please enter a valid 5-digit ZIP code.";
      return;
    }

    zipButton.disabled = true;
    zipStatus.textContent = "Finding your location...";

    try {
      const response = await fetch(
        `https://api.zippopotam.us/us/${zip}`
      );

      if (response.status === 404) {
        throw new Error("ZIP code not found.");
      }

      if (!response.ok) {
        throw new Error(
          "ZIP lookup is temporarily unavailable."
        );
      }

      const data = await response.json();

      if (version !== searchVersion) return;

      const place = data.places?.find(
        place =>
          place["state abbreviation"] === "NY"
      );

      if (!place) {
        throw new Error(
          "Please enter a New York State ZIP code."
        );
      }

      const lng = Number(place.longitude);
      const lat = Number(place.latitude);

      if (
        !Number.isFinite(lng) ||
        !Number.isFinite(lat)
      ) {
        throw new Error("Unable to locate this ZIP.");
      }

      // Update ZIP location marker.
      if (zipMarker) {
        zipMarker.remove();
      }

      zipMarker = new maplibregl.Marker({
        color: "#ef4444",
      })
        .setLngLat([lng, lat])
        .setPopup(
          new maplibregl.Popup({ offset: 20 })
            .setText(
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

      // Highlight closest stations and list them.
      await highlightNearbyStations(
        lat,
        lng,
        version
      );

    } catch (error) {
      if (version === searchVersion) {
        zipStatus.textContent =
          error.message ?? "Unable to search ZIP.";

        console.error("ZIP search failed:", error);
      }

    } finally {
      if (version === searchVersion) {
        zipButton.disabled = false;
      }
    }
  }

  if (zipForm && zipInput && zipStatus && zipButton) {
    zipInput.addEventListener("input", () => {
      zipInput.value = zipInput.value
        .replace(/\D/g, "")
        .slice(0, 5);
    });

    zipForm.addEventListener("submit", event => {
      event.preventDefault();
      searchZipCode(zipInput.value.trim());
    });
  }

  // --------------------------------------
  // 2D / 3D VIEW SWITCHING
  // --------------------------------------

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

  // --------------------------------------
  // RESET VIEW
  // --------------------------------------

  function resetView() {
    map.stop();

    const is3D = viewMode === "3d";

    map.easeTo({
      center: INITIAL_CENTER,
      zoom: INITIAL_ZOOM,
      pitch: is3D ? INITIAL_PITCH : 0,
      bearing: is3D ? INITIAL_BEARING : 0,
      duration: 900,
    });
  }

  // --------------------------------------
  // INITIALIZE
  // --------------------------------------

  try {
    await loadStations();
  } catch (error) {
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
    resetView,
    setViewMode,
  };
}
