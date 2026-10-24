
/*
 * AquaNY — Drinking Water
 * Historical NYSDOH public water system contaminant summaries.
 *
 * Independent view:
 * initDrinkingWater(intro, ambientRoot)
 */

function el(tag, text, className) {
  const node = document.createElement(tag);

  if (text !== undefined && text !== null) {
    node.textContent = String(text);
  }

  if (className) {
    node.className = className;
  }

  return node;
}

/* ========================================
   Water system name formatting
   ======================================== */

function formatWaterSystemName(name) {
  if (!name) return 'Unknown water system';

  const smallWords = new Set([
    'of', 'the', 'and', 'at', 'in', 'on', 'for', 'to'
  ]);

  const abbreviations = new Set([
    'NY', 'NYC', 'SUNY', 'US', 'USA',
    'EPA', 'PWS', 'LLC', 'INC'
  ]);

  return String(name)
    .trim()
    .toLowerCase()
    .split(/\s+/)
    .map((word, index) => {
      const upper = word.toUpperCase();

      if (abbreviations.has(upper)) {
        return upper;
      }

      if (index > 0 && smallWords.has(word)) {
        return word;
      }

      return word.charAt(0).toUpperCase() + word.slice(1);
    })
    .join(' ');
}

/* ========================================
   API requests
   ======================================== */

async function getJSON(url) {
  const response = await fetch(url, {
    headers: { Accept: 'application/json' }
  });

  if (!response.ok) {
    throw new Error(`HTTP ${response.status}`);
  }

  const contentType = response.headers.get('content-type') || '';

  if (!contentType.includes('application/json')) {
    throw new Error('Expected JSON');
  }

  return response.json();
}

/* ========================================
   Formatting helpers
   ======================================== */

function displayValue(raw, unit) {
  if (raw === null || raw === undefined || !String(raw).trim()) {
    return 'Not reported';
  }

  if (String(raw).trim().toUpperCase() === 'ND') {
    return 'Nondetect';
  }

  const number = Number(raw);

  const formatted = Number.isFinite(number)
    ? number.toLocaleString('en-US', {
        maximumFractionDigits: 4
      })
    : String(raw);

  return `${formatted}${unit ? ` ${unit}` : ''}`;
}

function formatReportingPeriod(period) {
  if (!period) {
    return 'Date unavailable';
  }

  const value = String(period).trim();

  // Annual summary: 2024
  if (/^\d{4}$/.test(value)) {
    return value;
  }

  // Quarterly summary: 2024-3
  const match = value.match(/^(\d{4})-([1-4])$/);

  if (match) {
    const year = match[1];
    const quarter = Number(match[2]);

    const months = [
      'Jan–Mar',
      'Apr–Jun',
      'Jul–Sep',
      'Oct–Dec'
    ];

    return `${months[quarter - 1]} ${year}`;
  }

  return value;
}

/*
 * Preserve the existing selection logic:
 * Choose the newest reporting year.
 * Within that year, prefer the annual summary.
 */
function latestSummary(rows) {
  if (!rows.length) {
    return null;
  }

  const withYears = rows.filter((record) =>
    /^\d{4}(?:-\d+)?$/.test(String(record.period || ''))
  );

  const source = withYears.length ? withYears : rows;

  const latestYear = Math.max(
    ...source.map((record) =>
      Number(String(record.period).slice(0, 4)) || 0
    )
  );

  const withinYear = source.filter(
    (record) =>
      (Number(String(record.period).slice(0, 4)) || 0) === latestYear
  );

  return (
    withinYear.find((record) =>
      /^\d{4}$/.test(String(record.period))
    ) ||
    withinYear
      .slice()
      .sort((a, b) =>
        String(b.period).localeCompare(
          String(a.period),
          undefined,
          { numeric: true }
        )
      )[0]
  );
}

/*
 * A positive summary number does not necessarily
 * prove that a contaminant was detected.
 */
function detected(record) {
  if (!record) {
    return false;
  }

  const nSamples = Number(record.numSamples);
  const nND = Number(record.numNonDetects);

  if (
    record.numSamples != null &&
    record.numNonDetects != null &&
    Number.isFinite(nSamples) &&
    Number.isFinite(nND) &&
    nSamples > 0
  ) {
    return nND < nSamples;
  }

  const maximum = String(record.max ?? '').trim();

  return (
    maximum !== '' &&
    maximum.toUpperCase() !== 'ND' &&
    Number.isFinite(Number(maximum)) &&
    Number(maximum) > 0
  );
}

function contaminantName(record) {
  const standardNames = {
    '2806': 'PFOA',
    '2805': 'PFOS',
    '1040': 'Nitrate',
    '1005': 'Arsenic'
  };

  return (
    standardNames[record.code] ||
    record.contaminant ||
    record.code ||
    'Unknown contaminant'
  );
}

/* ========================================
   Contaminant cards
   ======================================== */


// AquaNY contextual measurement links: values navigate to the matching guide section.
const AQUANY_INFO_BASE = "/info.html";
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
const AQUANY_CONTAMINANT_GUIDE = {"PFOA":"pfoa","PFOS":"pfos","Nitrate":"nitrate","Arsenic":"arsenic"};

function recordCard(record, label, emphasized = false) {
  const card = el(
    'article',
    null,
    'dw-pfas-card dw-contaminant-card' +
      (emphasized ? ' dw-detected-card' : '')
  );

  card.append(
    el('strong', label, 'dw-card-title')
  );

  if (!record) {
    card.append(
      el('div', 'No published measurement', 'dw-card-empty')
    );

    card.append(
      el(
        'small',
        'No record for this system in this dataset.',
        'dw-subtext'
      )
    );

    return card;
  }

  const stats = el('div', null, 'dw-card-stats');

  for (const [name, rawValue] of [
    ['Mean', record.mean],
    ['Maximum', record.max]
  ]) {
    const row = el('div', null, 'dw-stat-row');

    row.append(
      el('span', name, 'dw-stat-label')
    );

    row.append(
      el(
        'span',
        displayValue(rawValue, record.unit),
        'dw-stat-value'
      )
    );

    const valueElement = row.lastElementChild;
    const guideSection = AQUANY_CONTAMINANT_GUIDE[label] || 'other-contaminants';
    if (valueElement) valueElement.replaceWith(aquanyInfoLink(valueElement, guideSection));
    stats.append(row);
  }

  card.append(stats);

  const footer = el('div', null, 'dw-clean-footer');

  footer.append(
    el(
      'div',
      `Reporting period: ${formatReportingPeriod(record.period)}`
    )
  );

  const samples = record.numSamples;
  const nondetects = record.numNonDetects;

  const sampleLabel =
    Number(samples) === 1 ? 'sample' : 'samples';

  const nondetectLabel =
    Number(nondetects) === 1 ? 'nondetect' : 'nondetects';

  footer.append(
    el(
      'div',
      `${samples ?? '?'} ${sampleLabel} · ` +
        `${nondetects ?? '?'} ${nondetectLabel}`
    )
  );

  card.append(footer);

  return card;
}

/* ========================================
   Historical summary table
   ======================================== */

function summaryTable(records) {
  const table = el('table', null, 'dw-records');

  const header = el('tr');

  const columns = [
    'Period',
    'Contaminant',
    'Mean',
    'Maximum',
    'Samples',
    'Nondetects'
  ];

  for (const title of columns) {
    header.append(el('th', title));
  }

  table.append(header);

  for (const record of records) {
    const row = el('tr');

    const values = [
      record.period,
      contaminantName(record),
      displayValue(record.mean, record.unit),
      displayValue(record.max, record.unit),
      String(record.numSamples ?? '—'),
      String(record.numNonDetects ?? '—')
    ];

    for (const value of values) {
      row.append(el('td', value));
    }

    table.append(row);
  }

  return table;
}

/* ========================================
   Selected public water system
   ======================================== */

function renderSystem(target, system, data) {
  target.replaceChildren();

  // Compact system heading
  const header = el('div', null, 'dw-system-header');

  header.append(
    el('h3', formatWaterSystemName(system.name))
  );

  header.append(
    el(
      'div',
      `${system.county || 'County unspecified'} · ${system.id}`,
      'dw-system-meta'
    )
  );

  // Consolidated explanation
  header.append(
    el(
      'p',
      'Historical NYSDOH data, not current tap-water results. ' +
        'Reporting periods may overlap, and some records may ' +
        'reflect untreated or inactive sources.',
      'dw-system-note'
    )
  );

  target.append(header);

  const records = Array.isArray(data.records)
    ? data.records
    : [];

  if (!records.length) {
    target.append(
      el(
        'p',
        'No published summaries are available for this system.'
      )
    );
    return;
  }

  // Group records by contaminant code.
  const byAnalyte = new Map();

  for (const record of records) {
    const id = record.code || record.contaminant;

    if (!id) {
      continue;
    }

    if (!byAnalyte.has(id)) {
      byAnalyte.set(id, []);
    }

    byAnalyte.get(id).push(record);
  }

  /* ----------------------------------------
     Four standard priority contaminants
     ---------------------------------------- */

  const standard = [
    ['2806', 'PFOA'],
    ['2805', 'PFOS'],
    ['1040', 'Nitrate'],
    ['1005', 'Arsenic']
  ];

  target.append(
    el('h5', 'Key contaminants', 'dw-section-heading')
  );

  target.append(
    el(
      'p',
      'Missing records are not nondetections.',
      'dw-subtext'
    )
  );

  const grid = el('div', null, 'dw-card-grid');

  for (const [code, name] of standard) {
    const latest = latestSummary(
      byAnalyte.get(code) || []
    );

    grid.append(
      recordCard(latest, name)
    );
  }

  target.append(grid);

  /* ----------------------------------------
     Additional detected contaminants
     ---------------------------------------- */

  const featured = [];

  for (const [id, rows] of byAnalyte) {
    if (standard.some(([code]) => code === id)) {
      continue;
    }

    const latest = latestSummary(rows);

    if (detected(latest)) {
      featured.push(latest);
    }
  }

  featured.sort((a, b) => {
    const date = String(b.period)
      .slice(0, 4)
      .localeCompare(String(a.period).slice(0, 4));

    return (
      date ||
      contaminantName(a).localeCompare(
        contaminantName(b)
      )
    );
  });

  target.append(
    el(
      'h5',
      'Other detected contaminants',
      'dw-section-heading'
    )
  );

  target.append(
    el(
      'p',
      'Additional substances detected in their latest available ' +
        'system-level summaries. Detection does not mean the water was unsafe.',
      'dw-subtext'
    )
  );

  if (featured.length) {
    const extras = el('div', null, 'dw-card-grid');

    for (const record of featured.slice(0, 3)) {
      extras.append(
        recordCard(
          record,
          contaminantName(record),
          true
        )
      );
    }

    target.append(extras);

    if (featured.length > 3) {
      target.append(
        el(
          'p',
          `Showing 3 of ${featured.length} additional detected ` +
            'contaminants. See full history below.',
          'dw-subtext'
        )
      );
    }
  } else {
    target.append(
      el(
        'p',
        'No additional detected contaminants identified in the ' +
          'latest available summaries. This does not establish ' +
          'that other contaminants are absent.',
        'dw-subtext'
      )
    );
  }

  /* ----------------------------------------
     Complete historical records
     ---------------------------------------- */

  const details = el('details');

  details.append(
    el(
      'summary',
      `View all contaminant summaries (${records.length})`
    )
  );

  details.append(
    el(
      'p',
      'Historical annual and quarterly summaries are listed separately. ' +
        'They may describe overlapping samples.',
      'dw-subtext'
    )
  );

  details.append(summaryTable(records));

  target.append(details);
}

/* ========================================
   Drinking Water mode
   ======================================== */

export function initDrinkingWater(intro, ambientRoot) {
  if (intro.querySelector('.dw-toggle')) {
    return;
  }

  const wrapper = el('div', null, 'dw-toggle');

  const ambient = el(
    'button',
    'Rivers & Lakes',
    'dw-tab dw-tab-active'
  );

  const drinking = el(
    'button',
    'Drinking Water',
    'dw-tab'
  );

  ambient.type = 'button';
  drinking.type = 'button';

  wrapper.append(ambient, drinking);

  const section = el('section', null, 'dw-panel');
  section.hidden = true;

  section.append(
    el('h3', 'Find your public water system')
  );

  section.append(
    el(
      'p',
      'Search by utility name, county, or system ID.',
      'dw-search-description'
    )
  );

  const search = el('input');
  search.type = 'search';
  search.placeholder = 'e.g. Albany City';
  search.setAttribute(
    'aria-label',
    'Public water system search'
  );

  const matches = el('div', null, 'dw-matches');
  const detail = el('div', null, 'dw-detail');

  section.append(search, matches, detail);

  ambientRoot.before(wrapper);
  ambientRoot.after(section);

  // Switch between Rivers & Lakes and Drinking Water.
  function mode(isDrinking) {
    section.hidden = !isDrinking;
    ambientRoot.hidden = isDrinking;

    ambient.classList.toggle(
      'dw-tab-active',
      !isDrinking
    );

    drinking.classList.toggle(
      'dw-tab-active',
      isDrinking
    );
  }

  ambient.addEventListener(
    'click',
    () => mode(false)
  );

  drinking.addEventListener(
    'click',
    () => mode(true)
  );

  // Search for public water systems.
  let seq = 0;
  let timer;

  search.addEventListener('input', () => {
    const version = ++seq;

    clearTimeout(timer);
    matches.replaceChildren();
    detail.replaceChildren();

    const query = search.value.trim();

    if (query.length < 2) {
      return;
    }

    timer = setTimeout(async () => {
      try {
        const systems = await getJSON(
          `/api/drinking-water/search?q=${encodeURIComponent(query)}`
        );

        if (version !== seq) {
          return;
        }

        for (const system of systems) {
          const button = el(
            'button',
            `${formatWaterSystemName(system.name)} · ` +
              `${system.county || 'NY'} · ${system.id}`,
            'dw-match'
          );

          button.type = 'button';

          button.addEventListener('click', async () => {
            const selected = ++seq;

            // Use a readable name in the search field.
            search.value = formatWaterSystemName(system.name);

            matches.replaceChildren();

            detail.replaceChildren(
              el('p', 'Loading system records…')
            );

            try {
              const records = await getJSON(
                `/api/drinking-water/systems/` +
                  `${encodeURIComponent(system.id)}/contaminants`
              );

              if (selected === seq) {
                renderSystem(detail, system, records);
              }
            } catch (error) {
              if (selected === seq) {
                detail.replaceChildren(
                  el(
                    'p',
                    `Unable to load records (${error.message}).`
                  )
                );
              }
            }
          });

          matches.append(button);
        }

        if (!systems.length) {
          matches.append(
            el(
              'p',
              'No matching public water systems found.'
            )
          );
        }
      } catch (error) {
        if (version === seq) {
          matches.replaceChildren(
            el(
              'p',
              `Search failed (${error.message}).`
            )
          );
        }
      }
    }, 300);
  });
}
