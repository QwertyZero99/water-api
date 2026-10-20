
/*
 * AquaNY waterbody search
 *
 * DEC assessments, historical incidents, and ambient PFAS monitoring.
 *
 * API:
 *   /api/waterbodies/search?q=...
 *   /api/waterbodies/{id}
 *   /api/waterbodies/{id}/incidents
 *   /api/waterbodies/{id}/pfas
 */

const SOURCE = "selected-waterbody";
const PFAS_SOURCE = "selected-waterbody-pfas";
const DETAIL_ENDPOINT = "/api/waterbodies";
const SEARCH_ENDPOINT = "/api/waterbodies/search";

async function readJSON(url) {
  const response = await fetch(url, {
    headers: { Accept: "application/json" }
  });

  if (!response.ok) {
    throw new Error(`${url}: HTTP ${response.status}`);
  }

  return response.json();
}

function node(tag, value, className) {
  const element = document.createElement(tag);
  element.textContent =
    value == null || value === "" ? "Not listed" : String(value);

  if (className) element.className = className;
  return element;
}

function message(value, className) {
  return node("p", value, className);
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

function emptyCollection() {
  return { type: "FeatureCollection", features: [] };
}

function ensureWaterbodyLayers(map) {
  if (!map.getSource(SOURCE)) {
    map.addSource(SOURCE, {
      type: "geojson",
      data: emptyCollection()
    });
  }

  const before = map.getLayer("station-clusters")
    ? "station-clusters"
    : undefined;

  if (!map.getLayer("waterbody-fill")) {
    map.addLayer({
      id: "waterbody-fill",
      type: "fill",
      source: SOURCE,
      filter: ["==", ["geometry-type"], "Polygon"],
      paint: {
        "fill-color": "#14a6c5",
        "fill-opacity": 0.35
      }
    }, before);
  }

  if (!map.getLayer("waterbody-boundary")) {
    map.addLayer({
      id: "waterbody-boundary",
      type: "line",
      source: SOURCE,
      paint: {
        "line-color": "#087b9f",
        "line-width": 4,
        "line-opacity": 0.95
      }
    }, before);
  }
}

function ensurePFASLayers(map) {
  if (!map.getSource(PFAS_SOURCE)) {
    map.addSource(PFAS_SOURCE, {
      type: "geojson",
      data: emptyCollection()
    });
  }

  if (!map.getLayer("waterbody-pfas-halo")) {
    map.addLayer({
      id: "waterbody-pfas-halo",
      type: "circle",
      source: PFAS_SOURCE,
      paint: {
        "circle-radius": 12,
        "circle-color": "#f59e0b",
        "circle-opacity": 0.18
      }
    });
  }

  if (!map.getLayer("waterbody-pfas-points")) {
    map.addLayer({
      id: "waterbody-pfas-points",
      type: "circle",
      source: PFAS_SOURCE,
      paint: {
        "circle-radius": 7,
        "circle-color": "#e18a17",
        "circle-stroke-width": 2,
        "circle-stroke-color": "#ffffff"
      }
    });
  }
}

function clearPFASMarkers(map) {
  map.getSource(PFAS_SOURCE)?.setData(emptyCollection());
}

function showPFASMarkers(map, sites) {
  ensurePFASLayers(map);

  const features = sites
    .filter(site =>
      Number.isFinite(Number(site.latitude)) &&
      Number.isFinite(Number(site.longitude))
    )
    .map(site => ({
      type: "Feature",
      geometry: {
        type: "Point",
        coordinates: [
          Number(site.longitude),
          Number(site.latitude)
        ]
      },
      properties: {
        siteId: site.id,
        siteCode: site.siteCode,
        waterbodyName: site.waterbodyName,
        measurementCount: site.measurements?.length || 0
      }
    }));

  map.getSource(PFAS_SOURCE).setData({
    type: "FeatureCollection",
    features
  });
}

function boundsOfFeatures(features) {
  const result = [
    [Infinity, Infinity],
    [-Infinity, -Infinity]
  ];

  function visit(coordinates) {
    if (!Array.isArray(coordinates)) return;

    if (
      typeof coordinates[0] === "number" &&
      typeof coordinates[1] === "number"
    ) {
      const [lon, lat] = coordinates;
      if (!Number.isFinite(lon) || !Number.isFinite(lat)) return;

      result[0][0] = Math.min(result[0][0], lon);
      result[0][1] = Math.min(result[0][1], lat);
      result[1][0] = Math.max(result[1][0], lon);
      result[1][1] = Math.max(result[1][1], lat);
    } else {
      coordinates.forEach(visit);
    }
  }

  features.forEach(feature =>
    visit(feature.geometry?.coordinates)
  );

  return Number.isFinite(result[0][0]) ? result : null;
}

function highlightFeatures(map, features) {
  ensureWaterbodyLayers(map);

  map.getSource(SOURCE).setData({
    type: "FeatureCollection",
    features
  });

  const bounds = boundsOfFeatures(features);
  if (!bounds) return;

  const [sw, ne] = bounds;
  const epsilon = 0.004;

  map.fitBounds([
    [
      sw[0] === ne[0] ? sw[0] - epsilon : sw[0],
      sw[1] === ne[1] ? sw[1] - epsilon : sw[1]
    ],
    [
      sw[0] === ne[0] ? ne[0] + epsilon : ne[0],
      sw[1] === ne[1] ? ne[1] + epsilon : ne[1]
    ]
  ], {
    padding: 65,
    maxZoom: 12,
    duration: 850
  });
}

function formatDate(value) {
  if (!value) return "Date unavailable";

  const datePart = String(value).slice(0, 10);
  const parts = datePart.split("-").map(Number);

  if (
    parts.length !== 3 ||
    parts.some(part => !Number.isInteger(part))
  ) {
    return String(value);
  }

  const [year, month, day] = parts;
  const date = new Date(year, month - 1, day);

  if (
    date.getFullYear() !== year ||
    date.getMonth() !== month - 1 ||
    date.getDate() !== day
  ) {
    return String(value);
  }

  return new Intl.DateTimeFormat("en-US", {
    month: "short",
    day: "numeric",
    year: "numeric"
  }).format(date);
}

function createSegmentCard(segment) {
  const card = document.createElement("article");
  card.className = "waterbody-group-segment";

  card.append(node("h5", segment.name));

  const fields = [
    ["Assessment", segment.assessment],
    [
      "Water quality class",
      segment.classification
        ? String(segment.classification).toUpperCase()
        : null
    ],
    ["Last assessed", segment.assessmentYear],
    ["DEC segment ID", segment.segmentId],
    ["Basin", segment.basin],
    ["Area description", segment.description]
  ];

  fields.forEach(([label, value]) =>
    card.append(detailRow(label, value))
  );

  if (
    typeof segment.factsheet === "string" &&
    /^https?:\/\//i.test(segment.factsheet)
  ) {
    const link = node("a", "View official DEC factsheet ↗");
    link.href = segment.factsheet;
    link.target = "_blank";
    link.rel = "noopener noreferrer";
    card.append(link);
  }

  return card;
}

function createPastIncidentsSection() {
  const section = document.createElement("section");
  section.className = "waterbody-incidents";

  const header = document.createElement("div");
  header.className = "waterbody-incidents-header";
  header.append(
    node("h5", "Past Incidents"),
    node("span", "Historical records", "waterbody-incidents-badge")
  );

  const coverage = message("", "waterbody-incidents-coverage");
  const list = document.createElement("div");
  list.className = "waterbody-incidents-list";
  list.setAttribute("aria-live", "polite");

  section.append(
    header,
    message(
      "Historical water-quality concerns and beach closures reported " +
      "at associated New York State Park beaches. These records do not " +
      "establish current swimming safety.",
      "waterbody-incidents-description"
    ),
    coverage,
    list
  );

  return { section, coverage, list };
}

function createIncidentCard(incident) {
  const card = document.createElement("article");
  card.className = "waterbody-incident-card";

  const header = document.createElement("div");
  header.className = "waterbody-incident-card-header";

  const date = node(
    "time",
    formatDate(incident.date),
    "waterbody-incident-date"
  );

  if (incident.date) date.dateTime = incident.date;

  header.append(
    node("h6", incident.type || "Recorded water-quality concern"),
    date
  );

  const footer = document.createElement("div");
  footer.className = "waterbody-incident-footer";
  footer.append(
    node("span", incident.status || "Historical record", "waterbody-incident-status"),
    node("span", "Source: NYS Parks", "waterbody-incident-source")
  );

  card.append(
    header,
    message(
      incident.beach || "Beach not specified",
      "waterbody-incident-beach"
    ),
    message(
      incident.reason || "No additional reason was recorded.",
      "waterbody-incident-reason"
    ),
    footer
  );

  return card;
}

function renderPastIncidents(ui, data) {
  ui.coverage.textContent = data.coverageEnd
    ? `Available dataset through ${formatDate(data.coverageEnd)}.`
    : "Dataset coverage date unavailable.";

  const incidents = Array.isArray(data.incidents)
    ? data.incidents
    : [];

  ui.list.replaceChildren();

  if (!incidents.length) {
    ui.list.append(message(
      "No matching past incidents were found in the available " +
      "State Park beach records. This does not mean the waterbody " +
      "has never experienced an incident.",
      "waterbody-incidents-empty"
    ));
  } else {
    ui.list.append(message(
      `Showing ${incidents.length} historical record${incidents.length === 1 ? "" : "s"}.`,
      "waterbody-incidents-count"
    ));

    incidents.forEach(incident =>
      ui.list.append(createIncidentCard(incident))
    );
  }

  if (data.note) {
    ui.list.append(message(
      data.note,
      "waterbody-incidents-note"
    ));
  }
}

// ---------------------------------------------------------
// PFAS monitoring
// ---------------------------------------------------------

function createPFASSection() {
  const section = document.createElement("section");
  section.className = "waterbody-pfas";

  const heading = document.createElement("div");
  heading.className = "waterbody-pfas-heading";
  heading.append(
    node("h5", "PFAS Monitoring"),
    node("span", "NYSDEC ambient samples", "waterbody-pfas-badge")
  );

  const summary = message(
    "Loading monitoring results...",
    "waterbody-pfas-summary"
  );

  const list = document.createElement("div");
  list.className = "waterbody-pfas-list";
  list.setAttribute("aria-live", "polite");

  section.append(
    heading,
    message(
      "Historical laboratory results from specific sampling locations. " +
      "These measurements are not representative of an entire river or lake.",
      "waterbody-pfas-description"
    ),
    summary,
    list
  );

  return { section, summary, list };
}

function measurementText(measurement) {
  if (measurement.nondetect) return "Nondetect";

  if (measurement.concentration == null) {
    return "Not reported";
  }

  return `${measurement.concentration} ${measurement.unit || ""}`.trim();
}

function latestMeasurement(measurements, analyte) {
  return measurements
    .filter(m => String(m.analyte).toUpperCase() === analyte)
    .sort((a, b) =>
      String(b.sampledAt).localeCompare(String(a.sampledAt))
    )[0];
}

function createMeasurementTable(measurements) {
  const table = document.createElement("table");
  table.className = "waterbody-pfas-table";

  const thead = document.createElement("thead");
  const headings = document.createElement("tr");

  ["Date", "Compound", "Result", "Qualifier"].forEach(label => {
    headings.append(node("th", label));
  });

  thead.append(headings);
  table.append(thead);

  const tbody = document.createElement("tbody");

  const sorted = [...measurements].sort((a, b) => {
    const dateOrder = String(b.sampledAt).localeCompare(
      String(a.sampledAt)
    );

    if (dateOrder !== 0) return dateOrder;

    return String(a.analyte).localeCompare(String(b.analyte));
  });

  for (const measurement of sorted) {
    const row = document.createElement("tr");

    const result = node(
      "td",
      measurementText(measurement),
      measurement.nondetect ? "waterbody-pfas-nondetect" : ""
    );

    const extra = [];

    if (
      measurement.nondetect &&
      measurement.reportingDetectionLimit != null
    ) {
      extra.push(
        `Reporting limit: ${measurement.reportingDetectionLimit} ${measurement.unit}`
      );
    }

    if (measurement.methodDetectionLimit != null) {
      extra.push(
        `Method detection limit: ${measurement.methodDetectionLimit} ${measurement.unit}`
      );
    }

    if (measurement.quantitationLimit != null) {
      extra.push(
        `Quantitation limit: ${measurement.quantitationLimit} ${measurement.unit}`
      );
    }

    if (measurement.analyticalMethod) {
      extra.push(`Method: ${measurement.analyticalMethod}`);
    }

    if (extra.length) result.title = extra.join("\n");

    row.append(
      node("td", formatDate(measurement.sampledAt)),
      node("td", measurement.analyte),
      result,
      node("td", measurement.qualifier || "—")
    );

    tbody.append(row);
  }

  table.append(tbody);

  const wrapper = document.createElement("div");
  wrapper.className = "waterbody-pfas-table-scroll";
  wrapper.append(table);

  return wrapper;
}

function createPFASSiteCard(site, map) {
  const card = document.createElement("article");
  card.className = "waterbody-pfas-site";

  const measurements = Array.isArray(site.measurements)
    ? site.measurements
    : [];

  const heading = document.createElement("div");
  heading.className = "waterbody-pfas-site-heading";

  heading.append(
    node("h6", site.siteCode || "Monitoring site"),
    node(
      "span",
      `${measurements.length} lab result${measurements.length === 1 ? "" : "s"}`,
      "waterbody-pfas-site-count"
    )
  );

  card.append(heading);

  const dates = measurements
    .map(m => m.sampledAt)
    .filter(Boolean)
    .sort();

  if (dates.length) {
    card.append(message(
      `Sampled ${formatDate(dates[0])}` +
      (
        dates[0].slice(0, 10) !== dates[dates.length - 1].slice(0, 10)
          ? ` – ${formatDate(dates[dates.length - 1])}`
          : ""
      ),
      "waterbody-pfas-site-date"
    ));
  }

  const keyResults = document.createElement("div");
  keyResults.className = "waterbody-pfas-key-results";

  for (const analyte of ["PFOA", "PFOS"]) {
    const latest = latestMeasurement(measurements, analyte);
    if (!latest) continue;

    const item = document.createElement("div");
    item.className = "waterbody-pfas-key-result";

    item.append(
      node("strong", analyte),
      node("span", measurementText(latest)),
      node("small", formatDate(latest.sampledAt))
    );

    keyResults.append(item);
  }

  if (keyResults.childElementCount) {
    card.append(keyResults);
  }

  const controls = document.createElement("div");
  controls.className = "waterbody-pfas-controls";

  const locate = node(
    "button",
    "Show on map",
    "waterbody-pfas-locate"
  );
  locate.type = "button";

  locate.addEventListener("click", () => {
    const lat = Number(site.latitude);
    const lon = Number(site.longitude);

    if (!Number.isFinite(lat) || !Number.isFinite(lon)) {
      return;
    }

    map.flyTo({
      center: [lon, lat],
      zoom: 13,
      duration: 750
    });
  });

  controls.append(locate);

  card.append(controls);

  if (measurements.length) {
    const details = document.createElement("details");
    details.className = "waterbody-pfas-measurements";

    const summary = node(
      "summary",
      `View all ${measurements.length} laboratory results`
    );

    details.append(summary);

    const pfas = measurements.filter(m =>
      String(m.analyte).toLowerCase() !== "1,4-dioxane"
    );

    const other = measurements.filter(m =>
      String(m.analyte).toLowerCase() === "1,4-dioxane"
    );

    if (pfas.length) {
      details.append(createMeasurementTable(pfas));
    }

    if (other.length) {
      details.append(node(
        "h6",
        "Other emerging contaminants (not PFAS)"
      ));
      details.append(createMeasurementTable(other));
    }

    details.append(message(
      "Hover over a result to see available laboratory detection " +
      "limits and the analytical method. Nondetect does not mean zero.",
      "waterbody-pfas-table-note"
    ));

    card.append(details);
  }

  return card;
}

function renderPFAS(ui, data, map) {
  const sites = Array.isArray(data.sites)
    ? data.sites
    : [];

  ui.list.replaceChildren();

  if (!sites.length) {
    ui.summary.textContent = "No linked ambient monitoring sites.";

    ui.list.append(message(
      "No spatially matched NYSDEC monitoring sites are available " +
      "for this waterbody. This does not mean PFAS are absent.",
      "waterbody-pfas-empty"
    ));

    clearPFASMarkers(map);
    return;
  }

  ui.summary.textContent =
    `${sites.length} monitoring site${sites.length === 1 ? "" : "s"} · ` +
    `${data.measurementCount ?? sites.reduce(
      (total, site) => total + (site.measurements?.length || 0), 0
    )} laboratory results`;

  sites.forEach(site => {
    ui.list.append(createPFASSiteCard(site, map));
  });

  ui.list.append(message(
    "Source: NYSDEC. Sites are associated with waterbodies using " +
    "automated name and geographic matching, not individual manual " +
    "verification. Results are historical and do not establish " +
    "current drinking-water or recreational safety.",
    "waterbody-pfas-note"
  ));

  showPFASMarkers(map, sites);
}

// ---------------------------------------------------------
// Main search UI
// ---------------------------------------------------------

export function initWaterbodySearch(map) {
  const intro = document.querySelector(".map-intro");

  if (!intro) {
    console.warn("Waterbody search: .map-intro missing");
    return;
  }

  if (intro.querySelector(".waterbody-search")) return;

  ensureWaterbodyLayers(map);
  ensurePFASLayers(map);

  // MapLibre handlers are registered only once.
  map.on("click", "waterbody-pfas-points", event => {
    const feature = event.features?.[0];
    if (!feature) return;

    const properties = feature.properties || {};
    const coordinates = feature.geometry.coordinates.slice();

    const popupBody = document.createElement("div");
    popupBody.className = "waterbody-pfas-popup";

    popupBody.append(
      node("strong", properties.siteCode),
      message(properties.waterbodyName || "Ambient monitoring"),
      message(`${properties.measurementCount || 0} laboratory results`)
    );

    new maplibregl.Popup({ offset: 12 })
      .setLngLat(coordinates)
      .setDOMContent(popupBody)
      .addTo(map);
  });

  map.on("mouseenter", "waterbody-pfas-points", () => {
    map.getCanvas().style.cursor = "pointer";
  });

  map.on("mouseleave", "waterbody-pfas-points", () => {
    map.getCanvas().style.cursor = "";
  });

  const root = document.createElement("section");
  root.className = "waterbody-search";

  root.append(
    node("h3", "Find a waterbody"),
    message("Search New York rivers, lakes, and estuaries.")
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

  const zip = intro.querySelector(".zip-search");
  if (zip) intro.insertBefore(root, zip);
  else intro.append(root);

  let searchVersion = 0;
  let detailVersion = 0;
  let debounce;

  function clearSelection() {
    ++detailVersion;

    map.getSource(SOURCE)?.setData(emptyCollection());
    clearPFASMarkers(map);

    detail.hidden = true;
    detail.replaceChildren();
  }

  function createClearButton() {
    const button = node(
      "button",
      "Clear highlight",
      "waterbody-clear"
    );

    button.type = "button";
    button.addEventListener("click", clearSelection);

    return button;
  }

  function appendIncidents(id, version) {
    const ui = createPastIncidentsSection();
    detail.append(ui.section);

    ui.list.append(message(
      "Loading historical incidents...",
      "waterbody-incidents-loading"
    ));

    readJSON(
      `${DETAIL_ENDPOINT}/${encodeURIComponent(id)}/incidents`
    )
      .then(data => {
        if (version !== detailVersion) return;
        renderPastIncidents(ui, data);
      })
      .catch(error => {
        if (version !== detailVersion) return;

        console.error("Past incidents request failed:", error);

        ui.list.replaceChildren(message(
          "Historical incident records are currently unavailable.",
          "waterbody-incidents-error"
        ));
      });
  }

  function appendPFAS(id, version) {
    const ui = createPFASSection();
    detail.append(ui.section);

    readJSON(
      `${DETAIL_ENDPOINT}/${encodeURIComponent(id)}/pfas`
    )
      .then(data => {
        if (version !== detailVersion) return;
        renderPFAS(ui, data, map);
      })
      .catch(error => {
        if (version !== detailVersion) return;

        console.error("PFAS request failed:", error);

        ui.summary.textContent = "Monitoring data unavailable.";
        ui.list.replaceChildren(message(
          "Could not load PFAS monitoring results. " +
          "Please try again later.",
          "waterbody-pfas-error"
        ));

        clearPFASMarkers(map);
      });
  }

  async function chooseWaterbody(id) {
    const version = ++detailVersion;

    detail.hidden = false;
    detail.replaceChildren(
      message("Loading waterbody profile...")
    );

    map.getSource(SOURCE)?.setData(emptyCollection());
    clearPFASMarkers(map);

    try {
      const waterbody = await readJSON(
        `${DETAIL_ENDPOINT}/${encodeURIComponent(id)}`
      );

      if (version !== detailVersion) return;

      if (
        !waterbody ||
        !Array.isArray(waterbody.segments) ||
        !waterbody.segments.length
      ) {
        throw new Error("Invalid waterbody detail response");
      }

      const features = waterbody.segments
        .filter(segment =>
          segment.geometry && segment.geometry.type
        )
        .map(segment => ({
          type: "Feature",
          id: segment.id,
          geometry: segment.geometry,
          properties: {
            name: segment.name,
            segmentId: segment.segmentId,
            assessment: segment.assessment
          }
        }));

      if (features.length) {
        highlightFeatures(map, features);
      }

      detail.replaceChildren(
        node("h4", waterbody.name)
      );

      const count = waterbody.segments.length;

      detail.append(message(
        `${count} official DEC assessment segment${count === 1 ? "" : "s"}.`,
        "waterbody-detail-note"
      ));

      detail.append(message(
        count > 1
          ? "Different sections can have different assessment results. " +
            "Each assessment below applies to its specified DEC segment, " +
            "not necessarily to the entire waterbody. These assessments " +
            "do not establish current swimming safety."
          : "This DEC assessment applies to the section described below " +
            "and does not establish current swimming safety.",
        "waterbody-detail-note"
      ));

      if (features.length !== count) {
        detail.append(message(
          "Some DEC segment geometries are unavailable; " +
          "the map highlight may be incomplete.",
          "waterbody-detail-note"
        ));
      }

      const segments = document.createElement("div");
      segments.className = "waterbody-group-segments";

      waterbody.segments.forEach(segment => {
        segments.append(createSegmentCard(segment));
      });

      detail.append(segments);

      // Load independently: failure of either API won't
      // interfere with the assessment profile.
      appendPFAS(id, version);
      appendIncidents(id, version);

      detail.append(createClearButton());

    } catch (error) {
      if (version !== detailVersion) return;

      console.error("Waterbody detail request failed:", error);

      detail.replaceChildren(message(
        "Could not load this waterbody. Please try again."
      ));
    }
  }

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
      const matches = await readJSON(
        `${SEARCH_ENDPOINT}?q=${encodeURIComponent(query)}`
      );

      if (version !== searchVersion) return;
      if (!Array.isArray(matches)) {
        throw new Error("Invalid search response");
      }

      status.textContent =
        `${matches.length} matching waterbod` +
        `${matches.length === 1 ? "y" : "ies"}` +
        `${matches.length === 40 ? " (showing first 40)" : ""}`;

      if (!matches.length) {
        results.append(message(
          "No matching waterbodies were found."
        ));
        return;
      }

      for (const item of matches) {
        const button = document.createElement("button");
        button.type = "button";
        button.className = "waterbody-result";

        if (item.segmentCount > 1) {
          button.classList.add("waterbody-result-group");
        }

        button.append(node("strong", item.name));

        const count = Number(item.segmentCount) || 1;

        button.append(node(
          "span",
          `${item.type || "Waterbody"} · ` +
          `${count} DEC assessment segment${count === 1 ? "" : "s"}`
        ));

        button.addEventListener("click", () =>
          void chooseWaterbody(item.id)
        );

        results.append(button);
      }

    } catch (error) {
      if (version !== searchVersion) return;

      console.error("Waterbody search failed:", error);

      status.textContent =
        "Search unavailable. Check the Go server and database.";
    }
  }

  input.addEventListener("input", () => {
    ++searchVersion;
    clearTimeout(debounce);

    results.replaceChildren();

    if (input.value.trim().length < 2) {
      status.textContent =
        "Type at least two letters to search.";
      return;
    }

    debounce = setTimeout(() => void runSearch(), 250);
  });
}
