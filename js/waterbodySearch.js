
// ==========================================
// AQUANY — WATERBODY SEARCH & PROFILES
// ==========================================
//
// Features:
// - Search individual DEC waterbodies
// - Search grouped parent waterbodies
// - Hide grouped segments from duplicate results
// - Highlight selected GeoJSON geometries
// - Show official DEC assessment information
// - Show historical beach-related incidents
// - Preserve ungrouped waterbody functionality
//
// Required Go endpoints:
// GET /api/waterbodies/search?q=...
// GET /api/waterbodies/{id}
// GET /api/waterbodies/{id}/incidents
// GET /api/waterbody-groups/search?q=...
// GET /api/waterbody-groups/{id}
// GET /api/waterbody-groups/{id}/incidents
// ==========================================

const SOURCE = "selected-waterbody";

const SEARCH_ENDPOINT = "/api/waterbodies/search";
const DETAIL_ENDPOINT = "/api/waterbodies";

const GROUP_SEARCH_ENDPOINT = "/api/waterbody-groups/search";
const GROUP_DETAIL_ENDPOINT = "/api/waterbody-groups";

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

async function getWaterbody(id) {
  return readJSON(
    `${DETAIL_ENDPOINT}/${encodeURIComponent(id)}`
  );
}

async function getPastIncidents(id) {
  return readJSON(
    `${DETAIL_ENDPOINT}/${encodeURIComponent(id)}/incidents`
  );
}

async function getWaterbodyGroup(id) {
  return readJSON(
    `${GROUP_DETAIL_ENDPOINT}/${encodeURIComponent(id)}`
  );
}

async function getWaterbodyGroupIncidents(id) {
  return readJSON(
    `${GROUP_DETAIL_ENDPOINT}/${encodeURIComponent(id)}/incidents`
  );
}

// ==========================================
// SEARCH BOTH GROUPS AND INDIVIDUAL SEGMENTS
// ==========================================

async function searchWaterbodies(query) {
  const encoded = encodeURIComponent(query);

  const [groups, segments] = await Promise.all([
    readJSON(`${GROUP_SEARCH_ENDPOINT}?q=${encoded}`),
    readJSON(`${SEARCH_ENDPOINT}?q=${encoded}`)
  ]);

  if (!Array.isArray(groups) || !Array.isArray(segments)) {
    throw new Error("Invalid waterbody search response");
  }

  // Initial frontend deduplication for the explicitly
  // verified parent groups.
  //
  // This is not a substitute for database-backed group
  // membership filtering when we expand statewide.
  const groupNames = groups.map((group) =>
    String(group.name || "").toLowerCase().trim()
  );

  const ungroupedSegments = segments.filter((segment) => {
    const name = String(segment.name || "")
      .toLowerCase()
      .trim();

    return !groupNames.some(
      (parent) =>
        name === parent ||
        name.startsWith(`${parent},`)
    );
  });

  return [
    ...groups.map((group) => ({
      ...group,
      isGroup: true
    })),
    ...ungroupedSegments.map((segment) => ({
      ...segment,
      isGroup: false
    }))
  ];
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

function message(text, className) {
  return node("p", text, className);
}

function detailRow(label, value) {
  const row = document.createElement("div");
  row.className = "waterbody-detail-row";

  row.append(
    node("span", label),
    node("strong", value)
  );

  return row;
}

// ==========================================
// GEOGRAPHIC HELPERS
// ==========================================

function emptyCollection() {
  return {
    type: "FeatureCollection",
    features: []
  };
}

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

function boundsOfFeatures(features) {
  const result = [
    [Infinity, Infinity],
    [-Infinity, -Infinity]
  ];

  for (const feature of features) {
    const bounds = boundsOf(feature.geometry);

    if (!bounds) continue;

    result[0][0] = Math.min(result[0][0], bounds[0][0]);
    result[0][1] = Math.min(result[0][1], bounds[0][1]);
    result[1][0] = Math.max(result[1][0], bounds[1][0]);
    result[1][1] = Math.max(result[1][1], bounds[1][1]);
  }

  return Number.isFinite(result[0][0])
    ? result
    : null;
}

function zoomToFeatures(map, features) {
  const bounds = boundsOfFeatures(features);

  if (!bounds) return;

  const [sw, ne] = bounds;
  const epsilon = 0.004;

  map.fitBounds(
    [
      [
        sw[0] === ne[0] ? sw[0] - epsilon : sw[0],
        sw[1] === ne[1] ? sw[1] - epsilon : sw[1]
      ],
      [
        sw[0] === ne[0] ? ne[0] + epsilon : ne[0],
        sw[1] === ne[1] ? ne[1] + epsilon : ne[1]
      ]
    ],
    {
      padding: 65,
      maxZoom: 12,
      duration: 850
    }
  );
}

// ==========================================
// MAPLIBRE LAYERS
// ==========================================

function ensureWaterbodyLayers(map) {
  if (!map.getSource(SOURCE)) {
    map.addSource(SOURCE, {
      type: "geojson",
      data: emptyCollection()
    });
  }

  const beforeLayer = map.getLayer("station-clusters")
    ? "station-clusters"
    : undefined;

  // Highlight lake and estuary polygons.
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
      beforeLayer
    );
  }

  // Outline polygons and highlight river lines.
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
      beforeLayer
    );
  }
}

function highlightFeatures(map, features) {
  ensureWaterbodyLayers(map);

  map.getSource(SOURCE).setData({
    type: "FeatureCollection",
    features
  });

  zoomToFeatures(map, features);
}

// ==========================================
// HISTORICAL INCIDENTS — DATE FORMATTING
// ==========================================

function formatIncidentDate(dateString) {
  if (!dateString) return "Date unavailable";

  const parts = String(dateString).split("-").map(Number);

  if (
    parts.length !== 3 ||
    parts.some((value) => !Number.isFinite(value))
  ) {
    return String(dateString);
  }

  const [year, month, day] = parts;

  const date = new Date(year, month - 1, day);

  if (
    date.getFullYear() !== year ||
    date.getMonth() !== month - 1 ||
    date.getDate() !== day
  ) {
    return String(dateString);
  }

  return new Intl.DateTimeFormat("en-US", {
    month: "long",
    day: "numeric",
    year: "numeric"
  }).format(date);
}

// ==========================================
// HISTORICAL INCIDENTS — UI
// ==========================================

function createPastIncidentsSection() {
  const section = document.createElement("section");
  section.className = "waterbody-incidents";

  const header = document.createElement("div");
  header.className = "waterbody-incidents-header";

  header.append(
    node("h5", "Past Incidents"),
    node(
      "span",
      "Historical records",
      "waterbody-incidents-badge"
    )
  );

  const description = message(
    "Historical water-quality concerns and beach closures " +
      "reported at associated New York State Park beaches. " +
      "These records do not establish current swimming safety.",
    "waterbody-incidents-description"
  );

  const coverage = document.createElement("p");
  coverage.className = "waterbody-incidents-coverage";

  const list = document.createElement("div");
  list.className = "waterbody-incidents-list";
  list.setAttribute("aria-live", "polite");

  section.append(header, description, coverage, list);

  return { section, coverage, list };
}

function createIncidentCard(incident) {
  const card = document.createElement("article");
  card.className = "waterbody-incident-card";

  const header = document.createElement("div");
  header.className = "waterbody-incident-card-header";

  const title = node(
    "h6",
    incident.type || "Recorded water-quality concern"
  );

  const date = node(
    "time",
    formatIncidentDate(incident.date),
    "waterbody-incident-date"
  );

  if (incident.date) {
    date.dateTime = incident.date;
  }

  header.append(title, date);

  const beach = message(
    incident.beach || "Beach not specified",
    "waterbody-incident-beach"
  );

  const reason = message(
    incident.reason || "No additional reason was recorded.",
    "waterbody-incident-reason"
  );

  const footer = document.createElement("div");
  footer.className = "waterbody-incident-footer";

  footer.append(
    node(
      "span",
      incident.status || "Historical record",
      "waterbody-incident-status"
    ),
    node(
      "span",
      "Source: NYS Parks",
      "waterbody-incident-source"
    )
  );

  card.append(header, beach, reason, footer);

  return card;
}

function renderPastIncidents(container, data) {
  const { coverage, list } = container;

  list.replaceChildren();

  coverage.textContent = data.coverageEnd
    ? `Available dataset through ${formatIncidentDate(data.coverageEnd)}.`
    : "Dataset coverage date unavailable.";

  const incidents = Array.isArray(data.incidents)
    ? data.incidents
    : [];

  if (incidents.length === 0) {
    list.append(
      message(
        "No matching past incidents were found in the available " +
          "State Park beach records. This does not mean the " +
          "waterbody has never experienced an incident.",
        "waterbody-incidents-empty"
      )
    );

    if (data.note) {
      list.append(
        message(data.note, "waterbody-incidents-note")
      );
    }

    return;
  }

  list.append(
    message(
      `Showing ${incidents.length} historical record` +
        `${incidents.length === 1 ? "" : "s"}.`,
      "waterbody-incidents-count"
    )
  );

  for (const incident of incidents) {
    list.append(createIncidentCard(incident));
  }

  if (data.note) {
    list.append(
      message(data.note, "waterbody-incidents-note")
    );
  }
}

// ==========================================
// GROUPED WATERBODY — SEGMENT DISPLAY
// ==========================================

function createSegmentCard(segment) {
  const card = document.createElement("article");
  card.className = "waterbody-group-segment";

  card.append(
    node("h5", segment.name),
    detailRow(
      "Assessment",
      segment.assessment || "Not listed"
    ),
    detailRow(
      "Water quality class",
      segment.classification
        ? String(segment.classification).toUpperCase()
        : "Not listed"
    )
  );

  return card;
}

// ==========================================
// INITIALIZE WATERBODY SEARCH
// ==========================================

export function initWaterbodySearch(map) {
  const intro = document.querySelector(".map-intro");

  if (!intro) {
    console.warn("Waterbody search: .map-intro missing");
    return;
  }

  ensureWaterbodyLayers(map);

  // Prevent duplicate search panels if initialization
  // happens more than once.
  if (intro.querySelector(".waterbody-search")) {
    return;
  }

  // ========================================
  // CREATE SEARCH INTERFACE
  // ========================================

  const root = document.createElement("section");
  root.className = "waterbody-search";

  root.append(
    node("h3", "Find a waterbody"),
    message(
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
  input.placeholder = "e.g. Cayuga Lake";
  input.autocomplete = "off";

  const status = message(
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

  root.append(label, input, status, results, detail);

  // Insert above the existing ZIP code search.
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
  // CLEAR SELECTION
  // ========================================

  function clearHighlight() {
    ++detailVersion;

    map.getSource(SOURCE)?.setData(emptyCollection());

    detail.hidden = true;
    detail.replaceChildren();
  }

  function createClearButton() {
    const clear = node(
      "button",
      "Clear highlight",
      "waterbody-clear"
    );

    clear.type = "button";
    clear.addEventListener("click", clearHighlight);

    return clear;
  }

  // ========================================
  // LOAD HISTORICAL INCIDENTS
  // ========================================

  function appendIncidents(id, version, isGroup) {
    const incidentUI = createPastIncidentsSection();

    detail.append(incidentUI.section);

    incidentUI.list.append(
      message(
        "Loading historical incidents...",
        "waterbody-incidents-loading"
      )
    );

    const request = isGroup
      ? getWaterbodyGroupIncidents(id)
      : getPastIncidents(id);

    request
      .then((data) => {
        if (version !== detailVersion) return;

        renderPastIncidents(incidentUI, data);
      })
      .catch((error) => {
        if (version !== detailVersion) return;

        console.error(
          "Past incidents request failed:",
          error
        );

        incidentUI.list.replaceChildren(
          message(
            "Historical incident records are currently " +
              "unavailable. Please try again later.",
            "waterbody-incidents-error"
          )
        );
      });
  }

  // ========================================
  // SELECT AN INDIVIDUAL DEC SEGMENT
  // ========================================

  async function choose(id) {
    const version = ++detailVersion;

    detail.hidden = false;
    detail.replaceChildren(
      message("Loading waterbody...")
    );

    try {
      const feature = await getWaterbody(id);

      if (version !== detailVersion) return;

      if (
        feature?.type !== "Feature" ||
        !feature.geometry
      ) {
        throw new Error("Invalid waterbody geometry");
      }

      highlightFeatures(map, [feature]);

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
        ["Segment description", p.description]
      ];

      for (const [label, value] of fields) {
        detail.append(detailRow(label, value));
      }

      detail.append(
        message(
          "This is an official DEC assessment segment. " +
            "Assessment results are historical and do not " +
            "establish current swimming safety.",
          "waterbody-detail-note"
        )
      );

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

      // Existing endpoint for ungrouped segments.
      appendIncidents(id, version, false);

      detail.append(createClearButton());

    } catch (error) {
      if (version !== detailVersion) return;

      console.error(
        "Waterbody details failed:",
        error
      );

      detail.replaceChildren(
        message(
          "Could not load this waterbody. Please try again."
        )
      );
    }
  }

  // ========================================
  // SELECT A GROUPED PARENT WATERBODY
  // ========================================

  async function chooseGroup(id) {
    const version = ++detailVersion;

    detail.hidden = false;

    detail.replaceChildren(
      message("Loading waterbody profile...")
    );

    try {
      const group = await getWaterbodyGroup(id);

      if (version !== detailVersion) return;

      if (
        !group ||
        !Array.isArray(group.segments)
      ) {
        throw new Error("Invalid group response");
      }

      // Obtain each verified DEC segment's geometry.
      // One failed geometry request won't prevent
      // the rest of the profile from loading.
      const geometryResults = await Promise.allSettled(
        group.segments.map((segment) =>
          getWaterbody(segment.id)
        )
      );

      if (version !== detailVersion) return;

      const features = geometryResults
        .filter(
          (result) =>
            result.status === "fulfilled" &&
            result.value?.type === "Feature" &&
            result.value.geometry
        )
        .map((result) => result.value);

      if (features.length > 0) {
        highlightFeatures(map, features);
      } else {
        map.getSource(SOURCE)?.setData(
          emptyCollection()
        );
      }

      // ====================================
      // PARENT PROFILE HEADER
      // ====================================

      detail.replaceChildren(
        node("h4", group.name)
      );

      const count = group.segments.length;

      detail.append(
        message(
          `${count} official DEC assessment segment` +
            `${count === 1 ? "" : "s"}`,
          "waterbody-detail-note"
        ),
        message(
          "Water quality can differ between areas of " +
            "this waterbody. The assessments below apply " +
            "to individual segments, not necessarily " +
            "to the entire lake or river.",
          "waterbody-detail-note"
        )
      );

      if (features.length < count) {
        detail.append(
          message(
            "Some segment map geometries could not be " +
              "loaded. The highlighted area may be incomplete.",
            "waterbody-detail-note"
          )
        );
      }

      // ====================================
      // INDIVIDUAL DEC ASSESSMENTS
      // ====================================

      const segmentList = document.createElement("div");
      segmentList.className = "waterbody-group-segments";

      for (const segment of group.segments) {
        segmentList.append(
          createSegmentCard(segment)
        );
      }

      detail.append(segmentList);

      // ====================================
      // LAKE-WIDE HISTORICAL INCIDENTS
      // ====================================

      appendIncidents(id, version, true);

      detail.append(createClearButton());

    } catch (error) {
      if (version !== detailVersion) return;

      console.error(
        "Grouped waterbody failed:",
        error
      );

      detail.replaceChildren(
        message(
          "Could not load this waterbody profile. " +
            "Please try again."
        )
      );
    }
  }

  // ========================================
  // RUN SEARCH
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

    status.textContent = "Searching waterbodies...";

    try {
      const matches = await searchWaterbodies(query);

      if (version !== searchVersion) return;

      status.textContent =
        `${matches.length} matching waterbod` +
        `${matches.length === 1 ? "y" : "ies"}` +
        `${matches.length === 40 ? " (showing first 40)" : ""}`;

      if (matches.length === 0) {
        results.append(
          message(
            "No matching waterbodies were found."
          )
        );
        return;
      }

      for (const p of matches) {
        const button = document.createElement("button");

        button.type = "button";
        button.className = "waterbody-result";

        if (p.isGroup) {
          button.classList.add(
            "waterbody-result-group"
          );
        }

        button.append(
          node("strong", p.name)
        );

        const subtitle = p.isGroup
          ? `${p.type || "Waterbody"} · ` +
            `${p.segmentCount} assessment segments`
          : `${p.type || "Waterbody"} · ` +
            `${p.segmentId || "No segment ID"} · ` +
            `${p.assessment || "Not assessed"}`;

        button.append(
          node("span", subtitle)
        );

        button.addEventListener("click", () => {
          if (p.isGroup) {
            void chooseGroup(p.id);
          } else {
            void choose(p.id);
          }
        });

        results.append(button);
      }

    } catch (error) {
      if (version !== searchVersion) return;

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
    // Invalidate pending requests immediately,
    // rather than allowing stale results to render.
    ++searchVersion;

    clearTimeout(debounce);

    debounce = setTimeout(
      () => void runSearch(),
      250
    );
  });
}
