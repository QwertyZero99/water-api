
/* EPA Water Quality Portal PFAS samples.
   Historical only; independent of NYSDEC PFAS. */

function el(tag, text, cls) {
  const node = document.createElement(tag);

  if (text !== undefined && text !== null) {
    node.textContent = String(text);
  }

  if (cls) node.className = cls;

  return node;
}

function validNumber(value) {
  return (
    value !== null &&
    value !== undefined &&
    value !== '' &&
    Number.isFinite(Number(value))
  );
}

function prettyDate(value) {
  if (!value) return 'Date unavailable';

  const d = new Date(`${value.slice(0, 10)}T12:00:00`);

  return Number.isNaN(d.getTime())
    ? value
    : new Intl.DateTimeFormat('en-US', {
        dateStyle: 'medium'
      }).format(d);
}

function sampleResultText(sample) {
  if (sample.nondetect) {
    return sample.reportingLimit
      ? `Nondetect (reporting limit: ${sample.reportingLimit})`
      : 'Nondetect';
  }

  if (validNumber(sample.value)) {
    return `${Number(sample.value)} ${sample.unit || ''}`.trim();
  }

  return sample.detectionCondition || 'Result not quantified';
}

function createEPAPFASCard(sample) {
  const card = el(
    'article',
    null,
    'waterbody-pfas-site wb-epa-pfas-card'
  );

  const chemical = sample.chemical ||
    sample.characteristic ||
    'PFAS';

  const station = sample.stationName ||
    sample.stationId ||
    'Unknown monitoring location';

  // Sample title
  const title = el(
    'h6',
    `${chemical} · ${station}`,
    'wb-epa-pfas-title'
  );

  card.append(title);

  // Measurement value
  const result = el(
    'div',
    sampleResultText(sample),
    'wb-epa-pfas-value'
  );

  card.append(result);

  // Sampling date
  const date = el(
    'div',
    `Sampled ${prettyDate(sample.sampleDate)}`,
    'wb-epa-pfas-date'
  );

  card.append(date);

  // Optional reporting qualifier
  if (sample.detectionCondition && !sample.nondetect) {
    card.append(
      el(
        'div',
        `Reporting qualifier: ${sample.detectionCondition}`,
        'wb-epa-pfas-extra'
      )
    );
  }

  // EPA activity identifier
  if (sample.activityId) {
    card.append(
      el(
        'div',
        `EPA activity: ${sample.activityId}`,
        'wb-epa-pfas-activity'
      )
    );
  }

  return card;
}

export function appendWaterbodyEPAPFAS(
  container,
  waterbodyId,
  signal
) {
  // Starts hidden. Empty or failed requests remain hidden.
  const section = el(
    'section',
    null,
    'waterbody-pfas wb-epa-pfas'
  );

  section.hidden = true;

  const heading = el(
    'div',
    null,
    'waterbody-pfas-heading'
  );

  heading.append(
    el('h5', 'Historical PFAS Samples'),
    el(
      'span',
      'EPA Water Quality Portal',
      'waterbody-pfas-badge'
    )
  );

  const summary = el(
    'p',
    '',
    'waterbody-pfas-summary'
  );

  const list = el(
    'div',
    null,
    'waterbody-pfas-list'
  );

  section.append(heading, summary, list);
  container.append(section);

  const url =
    `/api/waterbodies/${encodeURIComponent(waterbodyId)}/epa-pfas`;

  (async () => {
    try {
      const res = await fetch(url, {
        headers: {
          Accept: 'application/json'
        },
        signal
      });

      if (!res.ok) {
        throw new Error(`HTTP ${res.status}`);
      }

      const contentType =
        res.headers.get('content-type') || '';

      if (!contentType.includes('application/json')) {
        throw new Error('EPA PFAS API did not return JSON');
      }

      const data = await res.json();

      if (signal?.aborted || !section.isConnected) {
        return;
      }

      const samples = Array.isArray(data.samples)
        ? data.samples
        : [];

      if (!samples.length) return;

      summary.textContent =
        `${samples.length} historical lab result` +
        `${samples.length === 1 ? '' : 's'} at matched ` +
        `surface-water monitoring locations. ` +
        `Not current conditions.`;

      for (const sample of samples) {
        list.append(createEPAPFASCard(sample));
      }

      section.hidden = false;

    } catch (error) {
      if (
        signal?.aborted ||
        error?.name === 'AbortError'
      ) {
        return;
      }

      console.warn(
        'EPA historical PFAS unavailable:',
        error
      );
    }
  })();

  return section;
}
