const SAMPLE_URL = "/static/data/waterbodies-sample.geojson";
const SOURCE = "selected-waterbody";

let samplePromise;

function loadSamples() {
  if (!samplePromise) {
    samplePromise = fetch(SAMPLE_URL)
      .then(response => {
        if (!response.ok) {
          throw new Error(`Sample GeoJSON: HTTP ${response.status}`);
        }
        return response.json();
      })
      .then(data => {
        if (
          data.type !== "FeatureCollection" ||
          !Array.isArray(data.features)
        ) {
          throw new Error("Invalid waterbody sample");
        }
        return data.features;
      })
      .catch(error => {
        samplePromise = undefined;
        throw error;
      });
  }

  return samplePromise;
}

// Later, this will call /api/waterbodies/search?q=...
async function searchWaterbodies(query) {
  const features = await loadSamples();
  const term = query.trim().toLowerCase();

  if (!term) return [];

  return features.filter(feature =>
    String(feature.properties?.name ?? "")
      .toLowerCase()
      .includes(term)
  );
}

// Later, this will call /api/waterbodies/{id}
async function getWaterbody(id) {
  const features = await loadSamples();

  return features.find(
    feature => feature.properties?.id === id
  ) ?? null;
}

// Calculate geographic bounds without external libraries.
function geometryBounds(geometry) {
  const bounds = [
    [Infinity, Infinity],
    [-Infinity, -Infinity]
  ];

  function walk(node) {
    if (!Array.isArray(node)) return;

    if (
      typeof node[0] === "number" &&
      typeof node[1] === "number"
    ) {
      const [lng, lat] = node;

      if (Number.isFinite(lng) && Number.isFinite(lat)) {
        bounds[0][0] = Math.min(bounds[0][0], lng);
        bounds[0][1] = Math.min(bounds[0][1], lat);
        bounds[1][0] = Math.max(bounds[1][0], lng);
        bounds[1][1] = Math.max(bounds[1][1], lat);
      }
    } else {
      node.forEach(walk);
    }
  }

  walk(geometry?.coordinates);

  return Number.isFinite(bounds[0][0]) ? bounds : null;
}

function createTextElement(tag, value, className) {
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

// MapLibre layers used to highlight the selected waterbody.
function addWaterbodyLayers(map) {
  if (!map.getSource(SOURCE)) {
    map.addSource(SOURCE, {
      type: "geojson",
      data: {
        type: "FeatureCollection",
        features: []
      }
    });
  }

  if (!map.getLayer("waterbody-fill")) {
    map.addLayer({
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
    });
  }

  if (!map.getLayer("waterbody-boundary")) {
    map.addLayer({
      id: "waterbody-boundary",
      type: "line",
      source: SOURCE,
      paint: {
        "line-color": "#087b9f",
        "line-width": 3,
        "line-opacity": 0.95
      }
    });
  }
}

export function initWaterbodySearch(map) {
  const intro = document.querySelector(".map-intro");

  if (!intro) {
    console.warn("Waterbody search: .map-intro not found");
    return;
  }

  addWaterbodyLayers(map);

  // Search panel.
  const root = document.createElement("section");
  root.className = "waterbody-search";

  root.append(
    createTextElement("h3", "Find a waterbody")
  );

  root.append(
    createTextElement(
      "p",
      "Search sample New York rivers, lakes, and estuaries."
    )
  );

  const label = createTextElement(
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

  const status = createTextElement(
    "p",
    "Start typing to search.",
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

  // Insert before existing ZIP-code search.
  const zipSearch = intro.querySelector(".zip-search");

  if (zipSearch) {
    intro.insertBefore(root, zipSearch);
  } else {
    intro.append(root);
  }

  let searchSerial = 0;
  let detailSerial = 0;
  let debounce;

  function clearSelection() {
    detailSerial++;

    map.getSource(SOURCE).setData({
      type: "FeatureCollection",
      features: []
    });

    detail.hidden = true;
    detail.replaceChildren();
  }

  async function choose(id) {
    const serial = ++detailSerial;

    detail.hidden = false;
    detail.replaceChildren(
      createTextElement(
        "p",
        "Loading waterbody details…"
      )
    );

    try {
      const feature = await getWaterbody(id);

      if (serial !== detailSerial) return;

      if (!feature?.geometry) {
        throw new Error("Waterbody geometry unavailable");
      }

      // Highlight selected waterbody.
      map.getSource(SOURCE).setData({
        type: "FeatureCollection",
        features: [feature]
      });

      // Zoom to its geographic bounds.
      const bounds = geometryBounds(feature.geometry);

      if (bounds) {
        map.fitBounds(bounds, {
          padding: 65,
          maxZoom: 12,
          duration: 850
        });
      }

      const p = feature.properties ?? {};

      detail.replaceChildren();
      detail.append(createTextElement("h4", p.name));

      const fields = [
        ["Assessment", p.assessment],
        ["Last assessed", p.year],
        [
          "Water quality class",
          p.class ? String(p.class).toUpperCase() : null
        ],
        ["Segment ID", p.segmentId],
        ["Basin", p.basin],
        ["Segment description", p.description]
      ];

      for (const [key, value] of fields) {
        const line = document.createElement("div");
        line.className = "waterbody-detail-row";

        line.append(
          createTextElement("span", key),
          createTextElement("strong", value)
        );

        detail.append(line);
      }

      detail.append(
        createTextElement(
          "p",
          "Assessment status is historical, not a real-time safety rating. Nearby USGS stations are not yet verified as monitoring this exact segment.",
          "waterbody-detail-note"
        )
      );

      // Official factsheet link.
      if (
        typeof p.factsheet === "string" &&
        p.factsheet.startsWith(
          "https://extapps.dec.ny.gov/"
        )
      ) {
        const link = createTextElement(
          "a",
          "View NYS DEC factsheet ↗"
        );

        link.href = p.factsheet;
        link.target = "_blank";
        link.rel = "noopener noreferrer";

        detail.append(link);
      }

      const clear = createTextElement(
        "button",
        "Clear highlight",
        "waterbody-clear"
      );

      clear.type = "button";
      clear.addEventListener("click", clearSelection);

      detail.append(clear);

    } catch (error) {
      if (serial !== detailSerial) return;

      console.error("Waterbody detail failed:", error);

      detail.replaceChildren(
        createTextElement(
          "p",
          "Unable to load this waterbody."
        )
      );
    }
  }

  async function runSearch() {
    const serial = ++searchSerial;
    const query = input.value.trim();

    results.replaceChildren();

    if (!query) {
      status.textContent = "Start typing to search.";
      return;
    }

    status.textContent = "Searching waterbodies…";

    try {
      const matches = await searchWaterbodies(query);

      if (serial !== searchSerial) return;

      status.textContent =
        `${matches.length} matching sample segment` +
        (matches.length === 1 ? "" : "s");

      if (!matches.length) {
        results.append(
          createTextElement(
            "p",
            "No matches in this prototype sample."
          )
        );
        return;
      }

      for (const feature of matches.slice(0, 12)) {
        const p = feature.properties;

        const button = document.createElement("button");
        button.type = "button";
        button.className = "waterbody-result";

        button.append(
          createTextElement("strong", p.name)
        );

        button.append(
          createTextElement(
            "span",
            `${p.type} · ${p.segmentId ?? "No segment ID"} · ${p.assessment ?? "Unassessed"}`
          )
        );

        button.addEventListener("click", () => {
          void choose(p.id);
        });

        results.append(button);
      }

      if (matches.length > 12) {
        results.append(
          createTextElement(
            "p",
            "Showing first 12 matches. Refine your search."
          )
        );
      }

    } catch (error) {
      if (serial !== searchSerial) return;

      console.error("Waterbody search failed:", error);

      status.textContent =
        "Unable to load sample data. Check the sample file path.";
    }
  }

  input.addEventListener("input", () => {
    searchSerial++;
    clearTimeout(debounce);

    debounce = setTimeout(() => {
      void runSearch();
    }, 220);
  });
}