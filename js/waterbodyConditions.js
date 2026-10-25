
/* AquaNY Layer 1: recent and earlier USGS measurements for a selected waterbody.
 * This module is independent of the station-map implementation.
 */


const MEASURES = [
  { code: '00010', label: 'Water temperature', fallbackUnit: '°C' },
  { code: '00300', label: 'Dissolved oxygen', fallbackUnit: 'mg/L' },
  { code: '63680', label: 'Turbidity', fallbackUnit: '' },
  { code: '00060', label: 'Streamflow', fallbackUnit: 'ft³/s' },
  { code: '00065', label: 'Gage height', fallbackUnit: 'ft' },
  { code: '00095', label: 'Specific conductance', fallbackUnit: 'µS/cm' },
  { code: '00400', label: 'pH', fallbackUnit: '' },
];

const TURBIDITY_CODES = new Set(['63680', '00076']);

function matchesMeasure(measure, reading) {
  if (measure.code === '63680') {
    return TURBIDITY_CODES.has(reading.parameterCode);
  }

  return reading.parameterCode === measure.code;
}


function elt(tag, text, className) {
  const node = document.createElement(tag);
  if (text !== undefined && text !== null) node.textContent = String(text);
  if (className) node.className = className;
  return node;
}

function formatMeasurementValue(reading, fallbackUnit = '') {
  const value = Number(reading.value);
  const unit = formatMeasurementUnit(reading.unit || fallbackUnit);

  // Convert Celsius to Fahrenheit for display.
  if (unit === '°C') {
    const fahrenheit = value * 9 / 5 + 32;

    return {
      value: `${fahrenheit.toLocaleString('en-US', {
        maximumFractionDigits: 1,
      })}°F`,
      secondary: `(${value.toLocaleString('en-US', {
        maximumFractionDigits: 1,
      })}°C)`,
    };
  }

  return {
    value: `${value.toLocaleString('en-US', {
      maximumFractionDigits: 3,
    })}${unit ? ` ${unit}` : ''}`,
    secondary: '',
  };
}

function formatMeasurementUnit(unit) {
  if (!unit) return '';

  const normalized = String(unit).trim();

  const knownUnits = {
    'degc': '°C',
    'degf': '°F',
    'us/cm': 'µS/cm',
    'mg/l': 'mg/L',
    'ug/l': 'µg/L',
    'ft^3/s': 'ft³/s',
    'ft3/s': 'ft³/s',
    '_fnu': 'FNU',
    'fnu': 'FNU',
    '_ntu': 'NTU',
    'ntu': 'NTU',
    'ph': '',
    'ft³/s': 'ft³/s',
    'µs/cm': 'µS/cm',
    'μs/cm': 'µS/cm',
    '°c': '°C',
    '°f': '°F',
  };

  return knownUnits[normalized.toLowerCase()] ?? normalized;
}

function formatStationName(name) {
  if (!name) return 'USGS station';

  const smallWords = new Set(['at', 'of', 'the', 'and', 'in', 'on', 'near', 'to']);
  const abbreviations = new Set(['NY', 'US', 'USA', 'USGS']);

  const formatted = name
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

function formatTime(value) {
  if (!value) return 'Time unavailable';
  const date = new Date(value);
  if (!Number.isFinite(date.getTime())) return 'Time unavailable';
  return new Intl.DateTimeFormat('en-US', {
    dateStyle: 'medium', timeStyle: 'short',
  }).format(date);
}

function isValidReading(reading) {
  return reading && reading.value !== null && reading.value !== undefined &&
    reading.value !== '' && Number.isFinite(Number(reading.value));
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

function metricCard(measure, readings) {
  const matching = readings
    .filter((reading) => matchesMeasure(measure, reading))
    .filter(isValidReading)
    .sort(
      (a, b) =>
        new Date(b.time).getTime() -
        new Date(a.time).getTime()
    );

  const reading = matching[0];

  // Do not create cards for unavailable measurements.
  if (!reading) return null;

  const card = elt('div', null, 'wb-condition-metric');

  // Measurement name
  card.append(
    elt('span', measure.label, 'wb-condition-label')
  );

  // Format value and units.
  // Celsius readings display as Fahrenheit first, Celsius second.
  const formatted = formatMeasurementValue(
    reading,
    measure.fallbackUnit
  );

  const output = elt('strong', null, 'wb-condition-value');

  output.append(elt('span', formatted.value));

  if (formatted.secondary) {
    output.append(
      elt('small', ` ${formatted.secondary}`)
    );
  }

  card.append(aquanyInfoLink(output, AQUANY_USGS_GUIDE[String(reading.parameterCode ?? "").trim()]));

  // Observation timestamp
  const timestamp = elt(
    'time',
    formatTime(reading.time),
    'wb-condition-time'
  );

  if (reading.time) {
    timestamp.dateTime = reading.time;
  }

  card.append(timestamp);

  // USGS approval status (e.g. Provisional)
  if (reading.approvalStatus) {
    card.append(
      elt(
        'small',
        reading.approvalStatus,
        'wb-condition-approval'
      )
    );
  }

  return card;
}





function renderHistory(body, data) {
  const history = (
    Array.isArray(data.historicalReadings)
      ? data.historicalReadings
      : []
  )
    .filter(isValidReading)
    .sort(
      (a, b) =>
        new Date(b.time).getTime() -
        new Date(a.time).getTime()
    );

  // Do not display an empty historical section.
  if (!history.length) return;

  const details = elt('details', null, 'wb-history');

  details.append(
    elt(
      'summary',
      `Earlier USGS observations (${history.length} records)`,
      'wb-history-summary'
    )
  );

  details.append(
    elt(
      'p',
      'Older measurements from matched monitoring stations. ' +
        'These are historical observations, not current water conditions.',
      'wb-history-notice'
    )
  );

  const list = elt('div', null, 'wb-history-list');

  for (const reading of history) {
    const card = elt('article', null, 'wb-history-record');

    // Measurement name
    card.append(
      elt(
        'span',
        reading.parameter || 'Measurement',
        'wb-history-parameter'
      )
    );

    // Format measurement value and units.
    // Celsius temperatures display as Fahrenheit first.
    const formatted = formatMeasurementValue(reading);

    const output = elt('strong', null, 'wb-history-value');

    output.append(
      elt('span', formatted.value)
    );

    if (formatted.secondary) {
      output.append(
        elt('small', ` ${formatted.secondary}`)
      );
    }

    card.append(aquanyInfoLink(output, AQUANY_USGS_GUIDE[String(reading.parameterCode ?? "").trim()]));

    // Format monitoring station name.
    const stationName = formatStationName(
      reading.stationName || reading.stationId
    );

    card.append(
      elt('span', stationName, 'wb-history-station')
    );

    // Observation timestamp
    const timestamp = elt(
      'time',
      formatTime(reading.time),
      'wb-history-time'
    );

    if (reading.time) {
      timestamp.dateTime = reading.time;
    }

    card.append(timestamp);

    // Series IDs remain in the API data but are hidden from the UI.
    list.append(card);
  }

  details.append(list);
  body.append(details);
}



function renderConditions(section, data, map) {
  if (!data || typeof data !== 'object') {
    throw new Error('The conditions API returned an invalid response.');
  }

  const windowHours = Number(data.windowHours) > 0 ? Number(data.windowHours) : 48;
  const badge = section.querySelector('.wb-conditions-badge');
  if (badge) badge.textContent = `USGS · last ${windowHours} hours`;

  const readings = (Array.isArray(data.readings) ? data.readings : []).filter(isValidReading);
  const stations = Array.isArray(data.stations) ? data.stations : [];
  const body = section.querySelector('.wb-conditions-body');
  if (!body) throw new Error('Conditions section is missing its content container.');

  // Build the new view off-screen so a rendering exception doesn't remove the old view.
  const content = document.createDocumentFragment();

  if (!readings.length) {
    content.append(elt('p',
      `No recent measurements of the supported USGS parameters were found at matched monitoring stations within the past ${windowHours} hours. Other observations may exist.`,
      'wb-conditions-message'));
  } else {
    const stationIDs = new Set(readings.map((reading) => reading.stationId));
    content.append(elt('p',
      `${stationIDs.size} monitoring location${stationIDs.size === 1 ? '' : 's'} with recent measurements (past ${windowHours} hours).`,
      'wb-conditions-summary'));

    // Include readings even if a station was omitted from the station metadata list.
    const stationByID = new Map(stations.filter((s) => s && s.id).map((s) => [s.id, s]));
    for (const reading of readings) {
      if (!stationByID.has(reading.stationId)) {
        stationByID.set(reading.stationId, {
          id: reading.stationId, name: reading.stationName,
          longitude: reading.longitude, latitude: reading.latitude,
        });
      }
    }

    for (const station of stationByID.values()) {
      const stationReadings = readings.filter((reading) => reading.stationId === station.id);
      if (!stationReadings.length) continue;

      const stationCard = elt('article', null, 'wb-condition-station');
      const heading = elt('div', null, 'wb-condition-station-heading');
      heading.append(elt('h6', formatStationName(station.name || station.id)));


      if (station.longitude !== null && station.longitude !== undefined &&
          station.latitude !== null && station.latitude !== undefined &&
          Number.isFinite(Number(station.longitude)) &&
          Number.isFinite(Number(station.latitude)) &&
          map && typeof map.flyTo === 'function') {
        const locate = elt('button', 'Locate', 'wb-condition-locate');
        locate.type = 'button';
        
        locate.addEventListener('click', () => {
          const coordinates = [
            Number(station.longitude),
            Number(station.latitude)
          ];

          const stationId = String(
            station.id ?? stationReadings[0]?.stationId ?? ''
          ).trim();

          // Open the existing station popup once the map finishes moving.
          if (stationId) {
            map.once('moveend', () => {
              map.fire('aquany:station-locate', {
                stationId,
                coordinates
              });
            });
          }

          map.flyTo({
            center: coordinates,
            zoom: 12,
            duration: 750
          });
        });

        heading.append(locate);
      }
      stationCard.append(heading);

      
      const metrics = elt('div', null, 'wb-condition-metrics');

      for (const measure of MEASURES) {
        const card = metricCard(measure, stationReadings);

        if (card) {
          metrics.append(card);
        }
      }

      if (metrics.childElementCount === 0) {
        continue;
      }

      stationCard.append(metrics);
      content.append(stationCard);

    }

    content.append(elt('p',
      'Measurements apply only at the listed monitoring locations. Provisional values may change. These observations do not establish drinking or swimming safety.',
      'wb-condition-disclaimer'));
  }

  // Always render historical observations after recent measurements or the no-data message.
  const historyContainer = document.createElement('div');
  renderHistory(historyContainer, data);
  content.append(...historyContainer.childNodes);
  body.replaceChildren(content);
}

/**
 * Add conditions to a waterbody panel. An optional AbortSignal allows the
 * parent panel to cancel the request when the selection changes.
 */
export function appendWaterbodyConditions(container, waterbodyId, map, signal) {
  const section = elt('section', null, 'wb-conditions');
  section.setAttribute('aria-label', 'Water conditions and earlier USGS observations');

  const heading = elt('div', null, 'wb-conditions-heading');
  heading.append(elt('h5', 'Current Water Conditions'));
  heading.append(elt('span', 'USGS · loading', 'wb-conditions-badge'));
  section.append(heading);

  const body = elt('div', null, 'wb-conditions-body');
  body.setAttribute('aria-live', 'polite');
  body.append(elt('p', 'Loading USGS observations…', 'wb-conditions-message'));
  section.append(body);
  container.append(section);

  const endpoint = `/api/waterbodies/${encodeURIComponent(waterbodyId)}/conditions`;

  async function load() {
    let data;
    try {
      const response = await fetch(endpoint, {
        headers: { Accept: 'application/json' }, signal,
      });
      if (!response.ok) throw new Error(`HTTP ${response.status}`);
      const contentType = response.headers.get('content-type') || '';
      if (!contentType.includes('application/json')) {
        throw new Error(`Expected JSON, received ${contentType || 'an unknown content type'}`);
      }
      data = await response.json();
    } catch (error) {
      if (signal?.aborted || error?.name === 'AbortError' || !section.isConnected) return;
      console.error('Waterbody conditions API request failed:', endpoint, error);
      body.replaceChildren(elt('p',
        `Unable to retrieve USGS observations (${error.message}). Please try again.`,
        'wb-conditions-message'));
      return;
    }

    if (signal?.aborted || !section.isConnected) return;
    try {
      renderConditions(section, data, map);
    } catch (error) {
      if (signal?.aborted || !section.isConnected) return;
      console.error('Waterbody conditions rendering failed:', endpoint, error, data);
      body.replaceChildren(elt('p',
        `Unable to display USGS observations (${error.message}).`,
        'wb-conditions-message'));
    }
  }

  void load();
  return section;
}
