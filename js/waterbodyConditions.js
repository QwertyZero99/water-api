
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

  // Do not create empty cards.
  if (!reading) return null;

  const card = elt('div', null, 'wb-condition-metric');

  card.append(
    elt('span', measure.label, 'wb-condition-label')
  );

  const value = Number(reading.value).toLocaleString('en-US', {
    maximumFractionDigits: 3,
  });

  const unit = reading.unit || measure.fallbackUnit;

  const output = elt('strong', null, 'wb-condition-value');
  output.append(elt('span', value));

  if (unit) {
    output.append(elt('small', ` ${unit}`));
  }

  card.append(output);

  const timestamp = elt(
    'time',
    formatTime(reading.time),
    'wb-condition-time'
  );

  if (reading.time) {
    timestamp.dateTime = reading.time;
  }

  card.append(timestamp);

  if (reading.approvalStatus) {
    card.append(
      elt('small', reading.approvalStatus, 'wb-condition-approval')
    );
  }

  return card;
}


function renderHistory(body, data) {
  const history = (Array.isArray(data.historicalReadings) ? data.historicalReadings : [])
    .filter(isValidReading)
    .sort((a, b) => new Date(b.time).getTime() - new Date(a.time).getTime());

  if (!history.length) {
    body.append(elt('p',
      'No older observations were returned by the USGS latest-continuous feed for matched stations. This is not a complete historical-data search.',
      'wb-conditions-message'));
    return;
  }

  const details = elt('details', null, 'wb-history');
  details.append(elt('summary', `Earlier USGS observations (${history.length} records)`,
    'wb-history-summary'));
  details.append(elt('p',
    'Historical observations — NOT current conditions. Values may represent different monitoring locations, depths, or sensors. These are snapshots, not a complete time series.',
    'wb-history-notice'));
  const list = elt('div', null, 'wb-history-list');

  for (const reading of history) {
    const card = elt('article', null, 'wb-history-record');
    card.append(elt('strong', reading.parameter || reading.parameterCode || 'Measurement'));
    const value = Number(reading.value).toLocaleString('en-US', { maximumFractionDigits: 3 });
    card.append(elt('span', `${value}${reading.unit ? ` ${reading.unit}` : ''}`));
    card.append(elt('small', reading.stationName || reading.stationId || 'USGS station'));
    const timestamp = elt('time', formatTime(reading.time));
    if (reading.time) timestamp.dateTime = reading.time;
    card.append(timestamp);
    if (reading.seriesId) card.append(elt('small', `Series: ${reading.seriesId}`));
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
      heading.append(elt('h6', station.name || station.id || 'USGS station'));

      if (station.longitude !== null && station.longitude !== undefined &&
          station.latitude !== null && station.latitude !== undefined &&
          Number.isFinite(Number(station.longitude)) &&
          Number.isFinite(Number(station.latitude)) &&
          map && typeof map.flyTo === 'function') {
        const locate = elt('button', 'Locate', 'wb-condition-locate');
        locate.type = 'button';
        locate.addEventListener('click', () => {
          map.flyTo({
            center: [Number(station.longitude), Number(station.latitude)],
            zoom: 12, duration: 750,
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
