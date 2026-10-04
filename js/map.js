import { calculateScore, getColor } from "./score.js";

export async function createMap() {
  const INITIAL_CENTER = [-78, 43];
  const INITIAL_ZOOM = 6;
  const INITIAL_PITCH = 45;
  const INITIAL_BEARING = -8;

  let viewMode = "3d";
  let activePopup = null;
  let hoveredStationKey = null;

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

  const stationStore = new Map();
  const stationFeatures = new Map();

  const loadedTiles = new Set();
  const loadingTiles = new Set();

  const normalStationRadius = [
    "interpolate",
    ["linear"],
    ["zoom"],
    6,
    6,
    9,
    10,
    13,
    14,
  ];

  const hoverStationRadius = [
    "interpolate",
    ["linear"],
    ["zoom"],
    6,
    5,
    9,
    8.5,
    13,
    12,
  ];

  map.addSource("stations", {
    type: "geojson",

    data: {
      type: "FeatureCollection",
      features: [],
    },
  });

  map.addLayer({
    id: "station-pulse",

    type: "circle",

    source: "stations",

    paint: {
      "circle-radius": 9,

      "circle-color": ["get", "color"],

      "circle-opacity": 0.12,

      "circle-blur": 0.4,

      "circle-stroke-width": 0,
    },
  });

  map.addLayer({
    id: "station-circles",

    type: "circle",

    source: "stations",

    paint: {
      "circle-radius": normalStationRadius,

      "circle-color": ["get", "color"],

      "circle-opacity": 0.9,

      "circle-stroke-color": "#ffffff",

      "circle-stroke-width": 2,
    },
  });

  function animatePulse(timestamp) {
    const duration = 2200;

    const phase =
      (timestamp % duration) / duration;

    const wave =
      (Math.sin(
        phase * Math.PI * 2 - Math.PI / 2,
      ) +
        1) /
      2;

    const baseRadius =
      9 + wave * 7;

    const opacity =
      0.06 + wave * 0.1;

    if (map.getLayer("station-pulse")) {
      map.setPaintProperty(
        "station-pulse",
        "circle-radius",
        [
          "interpolate",
          ["linear"],
          ["zoom"],
          6,
          baseRadius,
          9,
          baseRadius + 4,
          13,
          baseRadius + 8,
        ],
      );

      map.setPaintProperty(
        "station-pulse",
        "circle-opacity",
        opacity,
      );
    }

    requestAnimationFrame(animatePulse);
  }

  requestAnimationFrame(animatePulse);

  map.on(
    "mousemove",
    "station-circles",
    (event) => {
      map.getCanvas().style.cursor =
        "pointer";

      if (
        !event.features ||
        event.features.length === 0
      ) {
        return;
      }

      const stationKey =
        event.features[0].properties.stationKey;

      if (
        hoveredStationKey === stationKey
      ) {
        return;
      }

      hoveredStationKey =
        stationKey;

      map.setPaintProperty(
        "station-circles",
        "circle-radius",
        [
          "case",

          [
            "==",
            ["get", "stationKey"],
            hoveredStationKey,
          ],

          hoverStationRadius,

          normalStationRadius,
        ],
      );
    },
  );

  map.on(
    "mouseleave",
    "station-circles",
    () => {
      map.getCanvas().style.cursor =
        "";

      hoveredStationKey = null;

      map.setPaintProperty(
        "station-circles",
        "circle-radius",
        normalStationRadius,
      );
    },
  );

  map.on(
    "click",
    "station-circles",
    (event) => {
      if (
        !event.features ||
        event.features.length === 0
      ) {
        return;
      }

      const stationKey =
        event.features[0].properties.stationKey;

      const stored =
        stationStore.get(
          stationKey,
        );

      if (!stored) {
        return;
      }

      showStationPopup(
        event.lngLat,
        stored.station,
        stored.score,
      );
    },
  );

  async function loadStations() {
    const zoom =
      Math.floor(
        map.getZoom(),
      );

    const bounds =
      map.getBounds();

    const tiles =
      getVisibleTiles(
        bounds,
        zoom,
      );

    const requests = [];

    for (const tile of tiles) {
      const key =
        `${zoom}/${tile.y}/${tile.x}`;

      if (
        loadedTiles.has(key) ||
        loadingTiles.has(key)
      ) {
        continue;
      }

      loadingTiles.add(key);

      requests.push(
        loadTile(
          zoom,
          tile.x,
          tile.y,
          key,
        ),
      );
    }

    await Promise.all(
      requests,
    );
  }

  async function loadTile(
    zoom,
    x,
    y,
    key,
  ) {
    try {
      const response =
        await fetch(
          `/data/${zoom}/${y}/${x}`,
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

      const chunk =
        await response.json();

      if (
        !chunk ||
        !Array.isArray(
          chunk.stations,
        )
      ) {
        throw new Error(
          "Invalid station response",
        );
      }

      for (
        const station
        of chunk.stations
      ) {
        addStation(station);
      }

      updateStationSource();

      loadedTiles.add(key);
    } catch (err) {
      console.error(
        `Failed loading station tile ${key}:`,
        err,
      );
    } finally {
      loadingTiles.delete(key);
    }
  }

  function addStation(station) {
    const stationKey =
      `${station.lat},${station.lng}`;

    if (
      stationFeatures.has(
        stationKey,
      )
    ) {
      return;
    }

    const score =
      calculateScore(station);

    const color =
      getColor(score);

    stationStore.set(
      stationKey,
      {
        station,
        score,
      },
    );

    stationFeatures.set(
      stationKey,
      {
        type: "Feature",

        geometry: {
          type: "Point",

          coordinates: [
            Number(station.lng),
            Number(station.lat),
          ],
        },

        properties: {
          stationKey,
          color,
          score,
          name:
            station.name ?? "",
          value:
            station.value ?? "",
          time:
            station.time ?? "",
        },
      },
    );
  }

  function updateStationSource() {
    const source =
      map.getSource(
        "stations",
      );

    if (!source) {
      return;
    }

    source.setData({
      type: "FeatureCollection",

      features:
        Array.from(
          stationFeatures.values(),
        ),
    });
  }

  function showStationPopup(
    lngLat,
    station,
    score,
  ) {
    if (activePopup) {
      activePopup.remove();
    }

    const container =
      document.createElement(
        "div",
      );

    container.className =
      "station-popup";

    const title =
      document.createElement(
        "h3",
      );

    title.textContent =
      station.name ||
      "USGS Station";

    container.appendChild(
      title,
    );

    const scoreText =
      document.createElement(
        "p",
      );

    scoreText.textContent =
      `AquaScore: ${Math.round(
        score * 100,
      )}/100`;

    container.appendChild(
      scoreText,
    );

    const heading =
      document.createElement(
        "strong",
      );

    heading.textContent =
      "Data currently collected";

    container.appendChild(
      heading,
    );

    if (
      station.parameters &&
      station.parameters.length > 0
    ) {
      const list =
        document.createElement(
          "ul",
        );

      for (
        const parameter
        of station.parameters
      ) {
        const item =
          document.createElement(
            "li",
          );

        let text =
          parameter.name ||
          `Parameter ${parameter.code}`;

        if (
          parameter.value !== undefined &&
          parameter.value !== null &&
          parameter.value !== ""
        ) {
          text +=
            `: ${parameter.value}`;
        }

        if (parameter.unit) {
          text +=
            ` ${parameter.unit}`;
        }

        item.textContent =
          text;

        list.appendChild(
          item,
        );
      }

      container.appendChild(
        list,
      );
    } else {
      const none =
        document.createElement(
          "p",
        );

      none.textContent =
        "No current measurement data available.";

      container.appendChild(
        none,
      );
    }

    if (station.time) {
      const updated =
        document.createElement(
          "p",
        );

      updated.textContent =
        `Updated: ${new Date(
          station.time,
        ).toLocaleString()}`;

      container.appendChild(
        updated,
      );
    }

    activePopup =
      new maplibregl.Popup({
        closeButton: true,
        closeOnClick: true,
        maxWidth: "350px",
      })
        .setLngLat(lngLat)
        .setDOMContent(
          container,
        )
        .addTo(map);
  }

  function setViewMode(mode) {
    if (
      mode !== "2d" &&
      mode !== "3d"
    ) {
      return;
    }

    viewMode = mode;

    map.stop();

    if (mode === "2d") {
      map.setTerrain(null);

      if (
        map.getLayer(
          "terrain-hillshade",
        )
      ) {
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
      source:
        "terrainSource",

      exaggeration: 1.15,
    });

    if (
      map.getLayer(
        "terrain-hillshade",
      )
    ) {
      map.setLayoutProperty(
        "terrain-hillshade",
        "visibility",
        "visible",
      );
    }

    map.easeTo({
      pitch:
        INITIAL_PITCH,

      bearing:
        INITIAL_BEARING,

      duration: 700,
    });
  }

  function resetView() {
    map.stop();

    const is3D =
      viewMode === "3d";

    map.easeTo({
      center:
        INITIAL_CENTER,

      zoom:
        INITIAL_ZOOM,

      pitch:
        is3D
          ? INITIAL_PITCH
          : 0,

      bearing:
        is3D
          ? INITIAL_BEARING
          : 0,

      duration: 900,
    });
  }

  map.on(
    "moveend",
    loadStations,
  );

  await loadStations();

  return {
    map,
    resetView,
    setViewMode,
  };
}

function getVisibleTiles(
  bounds,
  zoom,
) {
  const tiles = [];

  const tileCount =
    Math.pow(2, zoom);

  const north =
    Math.min(
      85.05112878,
      bounds.getNorth(),
    );

  const south =
    Math.max(
      -85.05112878,
      bounds.getSouth(),
    );

  const west =
    bounds.getWest();

  const east =
    bounds.getEast();

  const northWest =
    latLngToTile(
      north,
      west,
      zoom,
    );

  const southEast =
    latLngToTile(
      south,
      east,
      zoom,
    );

  if (west <= east) {
    for (
      let x = northWest.x;
      x <= southEast.x;
      x++
    ) {
      for (
        let y = northWest.y;
        y <= southEast.y;
        y++
      ) {
        if (
          x >= 0 &&
          x < tileCount &&
          y >= 0 &&
          y < tileCount
        ) {
          tiles.push({
            x,
            y,
          });
        }
      }
    }

    return tiles;
  }

  const firstNorthWest =
    latLngToTile(
      north,
      west,
      zoom,
    );

  const firstSouthEast =
    latLngToTile(
      south,
      180,
      zoom,
    );

  for (
    let x =
      firstNorthWest.x;
    x <=
    firstSouthEast.x;
    x++
  ) {
    for (
      let y =
        firstNorthWest.y;
      y <=
      firstSouthEast.y;
      y++
    ) {
      if (
        x >= 0 &&
        x < tileCount &&
        y >= 0 &&
        y < tileCount
      ) {
        tiles.push({
          x,
          y,
        });
      }
    }
  }

  const secondNorthWest =
    latLngToTile(
      north,
      -180,
      zoom,
    );

  const secondSouthEast =
    latLngToTile(
      south,
      east,
      zoom,
    );

  for (
    let x =
      secondNorthWest.x;
    x <=
    secondSouthEast.x;
    x++
  ) {
    for (
      let y =
        secondNorthWest.y;
      y <=
      secondSouthEast.y;
      y++
    ) {
      if (
        x >= 0 &&
        x < tileCount &&
        y >= 0 &&
        y < tileCount
      ) {
        tiles.push({
          x,
          y,
        });
      }
    }
  }

  return tiles;
}

function latLngToTile(
  lat,
  lng,
  zoom,
) {
  const tileCount =
    Math.pow(2, zoom);

  lat =
    Math.max(
      -85.05112878,
      Math.min(
        85.05112878,
        lat,
      ),
    );

  lng =
    normalizeLongitude(
      lng,
    );

  let x =
    Math.floor(
      ((lng + 180) / 360) *
        tileCount,
    );

  const y =
    Math.floor(
      (
        (
          1 -
          Math.asinh(
            Math.tan(
              (lat *
                Math.PI) /
                180,
            ),
          ) /
            Math.PI
        ) /
        2
      ) *
        tileCount,
    );

  x =
    Math.max(
      0,
      Math.min(
        tileCount - 1,
        x,
      ),
    );

  const clampedY =
    Math.max(
      0,
      Math.min(
        tileCount - 1,
        y,
      ),
    );

  return {
    x,
    y:
      clampedY,
  };
}

function normalizeLongitude(
  lng,
) {
  return (
    ((((lng + 180) % 360) +
      360) %
      360) -
    180
  );
}