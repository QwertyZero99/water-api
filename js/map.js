const MIN_STATION_ZOOM = 7;

export async function createMap() {
  const INITIAL_CENTER = [-75, 43];
  const INITIAL_ZOOM = 7;
  const INITIAL_PITCH = 45;
  const INITIAL_BEARING = -8;

  let viewMode = "3d";

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

  await new Promise((resolve) => {
    map.once("load", resolve);
  });

  /*
   * The API returns lightweight station objects:
   *
   * {
   *   "id": "USGS-01350000",
   *   "location": [-73.123, 42.456]
   * }
   *
   * We keep the API data separately from the GeoJSON features so that
   * clicking a marker can retrieve the full station from the API.
   */
  const stationStore = new Map();

  map.addSource("stations", {
    type: "geojson",

    data: {
      type: "FeatureCollection",
      features: [],
    },
  });

  map.addLayer({
    id: "station-circles",

    type: "circle",

    source: "stations",

    paint: {
      "circle-radius": [
        "interpolate",
        ["linear"],
        ["zoom"],
        6,
        5,
        9,
        8,
        13,
        12,
      ],

      "circle-color": "#1976d2",

      "circle-opacity": 0.85,

      "circle-stroke-color": "#ffffff",

      "circle-stroke-width": 2,
    },
  });

  map.on("mouseenter", "station-circles", () => {
    map.getCanvas().style.cursor = "pointer";
  });

  map.on("mouseleave", "station-circles", () => {
    map.getCanvas().style.cursor = "";
  });

  map.on("click", "station-circles", async (event) => {
    if (!event.features || event.features.length === 0) {
      return;
    }

    const feature = event.features[0];

    const id = feature.properties?.id;

    if (!id) {
      return;
    }

    await loadAndShowStation(id);
  });

  /*
   * Load stations whenever the map stops moving.
   *
   * The bbox API does the spatial filtering on the server, so the
   * browser only receives stations currently visible in the map.
   */
  async function loadStations() {
    if (map.getZoom() < MIN_STATION_ZOOM) {
      clearStations();

      return;
    }

    const bounds = map.getBounds();

    const west = bounds.getWest();
    const south = bounds.getSouth();
    const east = bounds.getEast();
    const north = bounds.getNorth();

    const bbox = [
      west,
      south,
      east,
      north,
    ].join(",");

    try {
      const response = await fetch(
        `/api/stations?bbox=${encodeURIComponent(bbox)}`,
        {
          headers: {
            Accept: "application/json",
          },
        },
      );

      if (!response.ok) {
        throw new Error(
          `Server returned ${response.status} ${response.statusText}`,
        );
      }

      const stations = await response.json();

      if (!Array.isArray(stations)) {
        throw new Error("Invalid station response");
      }

      updateStations(stations);
    } catch (err) {
      console.error("Failed loading stations:", err);
    }
  }

  function updateStations(stations) {
    stationStore.clear();

    const features = [];

    for (const station of stations) {
      if (!station || !station.id) {
        continue;
      }

      if (
        !Array.isArray(station.location) ||
        station.location.length < 2
      ) {
        continue;
      }

      const lng = Number(station.location[0]);
      const lat = Number(station.location[1]);

      if (!Number.isFinite(lng) || !Number.isFinite(lat)) {
        continue;
      }

      stationStore.set(station.id, station);

      features.push({
        type: "Feature",

        geometry: {
          type: "Point",

          coordinates: [lng, lat],
        },

        properties: {
          id: station.id,
        },
      });
    }

    const source = map.getSource("stations");

    if (!source) {
      return;
    }

    source.setData({
      type: "FeatureCollection",

      features,
    });
  }

  function clearStations() {
    stationStore.clear();

    const source = map.getSource("stations");

    if (!source) {
      return;
    }

    source.setData({
      type: "FeatureCollection",

      features: [],
    });
  }

  /*
   * Retrieve the complete station record only after the user clicks
   * a marker.
   */
  async function loadAndShowStation(id) {
    showStationLoading(id);

    try {
      const response = await fetch(
        `/api/stations/${encodeURIComponent(id)}`,
        {
          headers: {
            Accept: "application/json",
          },
        },
      );

      if (!response.ok) {
        if (response.status === 404) {
          throw new Error(`Station "${id}" was not found`);
        }

        throw new Error(
          `Server returned ${response.status} ${response.statusText}`,
        );
      }

      const station = await response.json();

      showStation(station);
    } catch (err) {
      console.error(`Failed loading station "${id}":`, err);

      showStationError(id, err);
    }
  }

  /*
   * The station detail UI is intentionally kept separate from the
   * API request. More fields can be added here later without
   * changing the map logic.
   */
  function showStation(station) {
    const stationDiv = document.getElementById("station");

    if (!stationDiv) {
      return;
    }

    stationDiv.innerHTML = "";

    const id = station.ID ?? station.id ?? "Unknown";

    const title = document.createElement("h2");

    title.textContent = id;

    stationDiv.append(title);

    /*
     * Add additional station information here later.
     *
     * For example:
     *
     * const name = document.createElement("p");
     * name.textContent = station.Name;
     * stationDiv.append(name);
     */
  }

  function showStationLoading(id) {
    const stationDiv = document.getElementById("station");

    if (!stationDiv) {
      return;
    }

    stationDiv.innerHTML = "";

    const message = document.createElement("p");

    message.textContent = `Loading station ${id}...`;

    stationDiv.append(message);
  }

  function showStationError(id, error) {
    const stationDiv = document.getElementById("station");

    if (!stationDiv) {
      return;
    }

    stationDiv.innerHTML = "";

    const message = document.createElement("p");

    message.textContent =
      error instanceof Error
        ? error.message
        : `Unable to load station ${id}`;

    stationDiv.append(message);
  }

  function setViewMode(mode) {
    if (mode !== "2d" && mode !== "3d") {
      return;
    }

    viewMode = mode;

    map.stop();

    if (mode === "2d") {
      map.setTerrain(null);

      if (map.getLayer("terrain-hillshade")) {
        map.setLayoutProperty(
          "terrain-hillshade",
          "visibility",
          "none",
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
        "visible",
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

    const is3D = viewMode === "3d";

    map.easeTo({
      center: INITIAL_CENTER,

      zoom: INITIAL_ZOOM,

      pitch: is3D ? INITIAL_PITCH : 0,

      bearing: is3D ? INITIAL_BEARING : 0,

      duration: 900,
    });
  }

  map.on("moveend", loadStations);

  await loadStations();

  return {
    map,
    resetView,
    setViewMode,
  };
}
