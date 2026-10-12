// AquaNY waterbody search.
// Uses the Go/PostgreSQL API instead of the sample GeoJSON.

const SOURCE = "selected-waterbody";

const SEARCH_ENDPOINT = "/api/waterbodies/search";
const DETAIL_ENDPOINT = "/api/waterbodies";

// ==========================================
// API REQUESTS
// ==========================================

async function readJSON(url) {
  const response = await fetch(url, {
    headers: {
      Accept: "application/json"
    }
  });

  if (!response.ok) {
    throw new Error(`${url}: HTTP ${response.status}`);
  }

  return response.json();
}

async function searchWaterbodies(query) {
  return readJSON(
    `${SEARCH_ENDPOINT}?q=${encodeURIComponent(query)}`
  );
}

async function getWaterbody(id) {
  return readJSON(
    `${DETAIL_ENDPOINT}/${encodeURIComponent(id)}`
  );
}

// ==========================================
// GEOGRAPHIC HELPERS
// ==========================================

function boundsOf(geometry) {
  const result = [
    [Infinity, Infinity],
    [-Infinity, -Infinity]
  ];

  function visit(value) {
    if (!Array.isArray(value)) return;

    if (
      typeof value[0] === "number" &&
      typeof value[1] === "number"
    ) {
      const [x, y] = value;

      if (!Number.isFinite(x) || !Number.isFinite(y)) {
        return;
      }

      result[0][0] = Math.min(result[0][0], x);
      result[0][1] = Math.min(result[0][1], y);

      result[1][0] = Math.max(result[1][0], x);
      result[1][1] = Math.max(result[1][1], y);

    } else {
      value.forEach(visit);
    }
  }

  visit(geometry?.coordinates);

  return Number.isFinite(result[0][0])
    ? result
    : null;
}

// ==========================================
// DOM HELPERS
// ==========================================

function node(tag, value, className) {
  const element = document.createElement(tag);

  element.textContent =
    value == null || value === ""
      ? "Not listed"
      : String(value);

  if (className) {
    element.className = className;
  }

  return element;
}

function emptyCollection() {
  return {
    type: "FeatureCollection",
    features: []
  };
}

// ==========================================
// MAPLIBRE WATERBODY LAYERS
// ==========================================

function ensureWaterbodyLayers(map) {
  if (!map.getSource(SOURCE)) {
    map.addSource(SOURCE, {
      type: "geojson",
      data: emptyCollection()
    });
  }

  // Fill lakes and estuaries.
  if (!map.getLayer("waterbody-fill")) {
    map.addLayer(
      {
        id: "waterbody-fill",
        type: "fill",
        source: SOURCE,

        filter: [
          "==",
          ["geometry-type"],
          "Polygon"
        ],

        paint: {
          "fill-color": "#14a6c5",
          "fill-opacity": 0.35
        }
      },

      map.getLayer("station-clusters")
        ? "station-clusters"
        : undefined
    );
  }

  // Outline selected waterbodies.
  // Also highlights river and stream lines.
  if (!map.getLayer("waterbody-boundary")) {
    map.addLayer(
      {
        id: "waterbody-boundary",
        type: "line",
        source: SOURCE,

        paint: {
          "line-color": "#087b9f",
          "line-width": 4,
          "line-opacity": 0.95
        }
      },

      map.getLayer("station-clusters")
        ? "station-clusters"
        : undefined
    );
  }
}

// ==========================================
// INITIALIZE WATERBODY SEARCH
// ==========================================

export function initWaterbodySearch(map) {
  const intro = document.querySelector(".map-intro");

  if (!intro) {
    console.warn(
      "Waterbody search: .map-intro missing"
    );
    return;
  }

  ensureWaterbodyLayers(map);

  // ========================================
  // CREATE SEARCH INTERFACE
  // ========================================

  const root = document.createElement("section");
  root.className = "waterbody-search";

  root.append(
    node("h3", "Find a waterbody")
  );

  root.append(
    node(
      "p",
      "Search New York rivers, lakes, and estuaries."
    )
  );

  const label = node(
    "label",
    "Waterbody name",
    "waterbody-search-label"
  );

  label.htmlFor = "waterbodyQuery";

  const input = document.createElement("input");

  input.id = "waterbodyQuery";
  input.type = "search";
  input.placeholder = "e.g. Hudson River";
  input.autocomplete = "off";

  const status = node(
    "p",
    "Type at least two letters to search.",
    "waterbody-status"
  );

  status.setAttribute("role", "status");
  status.setAttribute("aria-live", "polite");

  const results = document.createElement("div");
  results.className = "waterbody-results";

  const detail = document.createElement("div");
  detail.className = "waterbody-detail";
  detail.hidden = true;

  root.append(
    label,
    input,
    status,
    results,
    detail
  );

  // Insert above the existing ZIP search.
  const zip = intro.querySelector(".zip-search");

  if (zip) {
    intro.insertBefore(root, zip);
  } else {
    intro.append(root);
  }

  let searchVersion = 0;
  let detailVersion = 0;
  let debounce;

  // ========================================
  // CLEAR SELECTED WATERBODY
  // ========================================

  function clearHighlight() {
    detailVersion++;

    map.getSource(SOURCE)?.setData(
      emptyCollection()
    );

    detail.hidden = true;
    detail.replaceChildren();
  }

  // ========================================
  // SELECT AND DISPLAY A WATERBODY
  // ========================================

  async function choose(id) {
    const version = ++detailVersion;

    detail.hidden = false;

    detail.replaceChildren(
      node("p", "Loading waterbody…")
    );

    try {
      const feature = await getWaterbody(id);

      if (version !== detailVersion) {
        return;
      }

      if (
        feature?.type !== "Feature" ||
        !feature.geometry
      ) {
        throw new Error(
          "Invalid waterbody geometry"
        );
      }

      // Highlight the selected waterbody.
      map.getSource(SOURCE).setData({
        type: "FeatureCollection",
        features: [feature]
      });

      // Find geographic bounds.
      const bounds = boundsOf(feature.geometry);

      if (bounds) {
        const [sw, ne] = bounds;

        const eps = 0.004;

        // Expand near-zero-width geometries
        // to prevent fitBounds issues.
        map.fitBounds(
          [
            [
              sw[0] === ne[0]
                ? sw[0] - eps
                : sw[0],

              sw[1] === ne[1]
                ? sw[1] - eps
                : sw[1]
            ],

            [
              sw[0] === ne[0]
                ? ne[0] + eps
                : ne[0],

              ne[1] === sw[1]
                ? ne[1] + eps
                : ne[1]
            ]
          ],

          {
            padding: 65,
            maxZoom: 12,
            duration: 850
          }
        );
      }

      // ====================================
      // DISPLAY WATERBODY INFORMATION
      // ====================================

      const p = feature.properties || {};

      detail.replaceChildren(
        node("h4", p.name)
      );

      const fields = [
        ["Type", p.type],

        ["Assessment", p.assessment],

        ["Last assessed", p.year],

        [
          "Water quality class",
          p.class
            ? String(p.class).toUpperCase()
            : null
        ],

        ["Segment ID", p.segmentId],

        ["Basin", p.basin],

        [
          "Segment description",
          p.description
        ]
      ];

      for (const [key, value] of fields) {
        const line = document.createElement("div");
        line.className = "waterbody-detail-row";

        line.append(
          node("span", key),
          node("strong", value)
        );

        detail.append(line);
      }

      // Explain historical assessment data.
      detail.append(
        node(
          "p",
          "Official assessment information is not a real-time safety rating. Nearby USGS monitoring stations have not yet been verified as belonging to this exact segment.",
          "waterbody-detail-note"
        )
      );

      // Link to the official factsheet.
      if (
        typeof p.factsheet === "string" &&
        /^https:\/\//i.test(p.factsheet)
      ) {
        const link = node(
          "a",
          "View official factsheet ↗"
        );

        link.href = p.factsheet;
        link.target = "_blank";
        link.rel = "noopener noreferrer";

        detail.append(link);
      }

      // Clear highlight button.
      const clear = node(
        "button",
        "Clear highlight",
        "waterbody-clear"
      );

      clear.type = "button";

      clear.addEventListener(
        "click",
        clearHighlight
      );

      detail.append(clear);

    } catch (error) {
      if (version !== detailVersion) {
        return;
      }

      console.error(
        "Waterbody details failed:",
        error
      );

      detail.replaceChildren(
        node(
          "p",
          "Could not load this waterbody. Please try again."
        )
      );
    }
  }

  // ========================================
  // RUN WATERBODY SEARCH
  // ========================================

  async function runSearch() {
    const version = ++searchVersion;

    const query = input.value.trim();

    results.replaceChildren();

    if (query.length < 2) {
      status.textContent =
        "Type at least two letters to search.";

      return;
    }

    status.textContent =
      "Searching waterbodies…";

    try {
      const matches = await searchWaterbodies(
        query
      );

      if (version !== searchVersion) {
        return;
      }

      if (!Array.isArray(matches)) {
        throw new Error(
          "Invalid search response"
        );
      }

      status.textContent =
        `${matches.length} matching segment` +
        `${matches.length === 1 ? "" : "s"}` +
        `${matches.length === 40 ? " (showing first 40)" : ""}`;

      if (matches.length === 0) {
        results.append(
          node(
            "p",
            "No matching assessed waterbodies were found."
          )
        );

        return;
      }

      // Create a button for each result.
      for (const p of matches) {
        const button = document.createElement(
          "button"
        );

        button.type = "button";
        button.className = "waterbody-result";

        button.append(
          node("strong", p.name)
        );

        button.append(
          node(
            "span",
            `${p.type || "Waterbody"} · ` +
            `${p.segmentId || "No segment ID"} · ` +
            `${p.assessment || "Not assessed"}`
          )
        );

        button.addEventListener(
          "click",
          () => void choose(p.id)
        );

        results.append(button);
      }

    } catch (error) {
      if (version !== searchVersion) {
        return;
      }

      console.error(
        "Waterbody search failed:",
        error
      );

      status.textContent =
        "Search unavailable. Check the Go server and database.";
    }
  }

  // ========================================
  // SEARCH INPUT LISTENER
  // ========================================

  input.addEventListener("input", () => {
    // Prevent previous searches from
    // overwriting newer results.
    ++searchVersion;

    clearTimeout(debounce);

    debounce = setTimeout(
      () => void runSearch(),
      250
    );
  });
}