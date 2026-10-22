/* Independent AquaNY Drinking Water view. Call initDrinkingWater(intro, ambientRoot). */
function el(tag, text, className) {
  const n = document.createElement(tag);
  if (text !== undefined && text !== null) n.textContent = String(text);
  if (className) n.className = className;
  return n;
}
async function getJSON(url) {
  const r = await fetch(url, { headers: { Accept: 'application/json' } });
  if (!r.ok) throw new Error(`HTTP ${r.status}`);
  if (!(r.headers.get('content-type') || '').includes('application/json')) throw new Error('Expected JSON');
  return r.json();
}
function displayValue(raw, unit) {
  if (raw == null || !String(raw).trim()) return 'Not reported';
  if (String(raw).trim().toUpperCase() === 'ND') return 'Nondetect';
  const n = Number(raw);
  return `${Number.isFinite(n) ? n.toLocaleString('en-US', {maximumFractionDigits:4}) : raw}${unit ? ` ${unit}` : ''}`;
}
function formatReportingPeriod(period) {
  if (!period) return "Date unavailable";

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
      "Jan–Mar",
      "Apr–Jun",
      "Jul–Sep",
      "Oct–Dec"
    ];

    return `${months[quarter - 1]} ${year}`;
  }

  return value;
}
// Keep annual and quarterly summary records distinct. The newest YEAR wins;
// within that year prefer its annual summary, when one is available.
function latestSummary(rows) {
  if (!rows.length) return null;
  const withYears = rows.filter(r => /^\d{4}(?:-\d+)?$/.test(String(r.period || '')));
  const source = withYears.length ? withYears : rows;
  const latestYear = Math.max(...source.map(r => Number(String(r.period).slice(0, 4)) || 0));
  const withinYear = source.filter(r => (Number(String(r.period).slice(0, 4)) || 0) === latestYear);
  return withinYear.find(r => /^\d{4}$/.test(String(r.period))) ||
    withinYear.slice().sort((a,b) => String(b.period).localeCompare(String(a.period), undefined, { numeric: true }))[0];
}
function detected(record) {
  if (!record) return false;
  const nSamples = Number(record.numSamples), nND = Number(record.numNonDetects);
  // A positive summary number is NOT proof of detection when the source has
  // substituted values for nondetect samples.
  if (record.numSamples != null && record.numNonDetects != null &&
      Number.isFinite(nSamples) && Number.isFinite(nND) && nSamples > 0) {
    return nND < nSamples;
  }
  // With missing detection counts, accept only an explicitly numeric maximum.
  const max = String(record.max ?? '').trim();
  return max !== '' && max.toUpperCase() !== 'ND' && Number.isFinite(Number(max)) && Number(max) > 0;
}
function contaminantName(r) {
  return ({'2806': 'PFOA', '2805': 'PFOS', '1040': 'Nitrate', '1005': 'Arsenic'})[r.code] || r.contaminant || r.code || 'Unknown contaminant';
}
function recordCard(record, label, emphasized = false) {
  const card = el(
    'article',
    null,
    'dw-pfas-card dw-contaminant-card' +
      (emphasized ? ' dw-detected-card' : '')
  );

  card.append(el('strong', label));

  if (!record) {
    card.append(
      el('div', 'No published measurement', 'dw-value')
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

  card.append(
    el(
      'div',
      `Mean: ${displayValue(record.mean, record.unit)}`,
      'dw-value'
    )
  );

  card.append(
    el(
      'div',
      `Maximum: ${displayValue(record.max, record.unit)}`
    )
  );

  // Show the reporting period separately.
  card.append(
    el(
      'div',
      `Reporting period: ${formatReportingPeriod(record.period)}`,
      'dw-subtext'
    )
  );
  
  card.append(
    el(
      'small',
      `${record.numSamples ?? '?'} samples · ${record.numNonDetects ?? '?'} nondetects`,
      'dw-subtext'
    )
  );

  return card;
}
function summaryTable(records) {
  const table = el('table', null, 'dw-records');
  const header = el('tr');
  for (const title of ['Period', 'Contaminant', 'Mean', 'Maximum', 'Samples', 'Nondetects']) header.append(el('th', title));
  table.append(header);
  for (const r of records) {
    const row = el('tr');
    for (const value of [r.period, contaminantName(r), displayValue(r.mean, r.unit), displayValue(r.max, r.unit), String(r.numSamples ?? '—'), String(r.numNonDetects ?? '—')]) {
      row.append(el('td', value));
    }
    table.append(row);
  }
  return table;
}
function renderSystem(target, system, data) {
  target.replaceChildren();
  target.append(el('h4', system.name));
  target.append(el('p', `${system.county || 'County unspecified'} · ${system.id}`, 'dw-subtext'));
  target.append(el('p', 'Historical NYSDOH system-level summaries, not current tap-water measurements. Annual and quarterly summaries may overlap; some data may reflect untreated or out-of-service sources.', 'dw-note'));
  const records = Array.isArray(data.records) ? data.records : [];
  if (!records.length) {
    target.append(el('p', 'No published summaries are available for this system.'));
    return;
  }
  const byAnalyte = new Map();
  for (const r of records) {
    const id = r.code || r.contaminant;
    if (!id) continue;
    if (!byAnalyte.has(id)) byAnalyte.set(id, []);
    byAnalyte.get(id).push(r);
  }
  const standard = [['2806', 'PFOA'], ['2805', 'PFOS'], ['1040', 'Nitrate'], ['1005', 'Arsenic']];
  target.append(el('h5', 'Key contaminants', 'dw-section-heading'));
  target.append(el('p', 'The same four contaminants are shown for every system; missing records do not mean nondetection.', 'dw-subtext'));
  const grid = el('div', null, 'dw-card-grid');
  for (const [code, name] of standard) grid.append(recordCard(latestSummary(byAnalyte.get(code) || []), name));
  target.append(grid);

  // Only OTHER analytes detected in their latest available reporting year.
  const featured = [];
  for (const [id, rows] of byAnalyte) {
    if (standard.some(([code]) => code === id)) continue;
    const latest = latestSummary(rows);
    if (detected(latest)) featured.push(latest);
  }
  featured.sort((a,b) => {
    const date = String(b.period).slice(0,4).localeCompare(String(a.period).slice(0,4));
    return date || contaminantName(a).localeCompare(contaminantName(b));
  });
  target.append(el('h5', 'Other detected contaminants', 'dw-section-heading'));
  target.append(el('p', 'Additional substances with detections in their latest available system-level summary. Detection does not mean the water was unsafe.', 'dw-subtext'));
  if (featured.length) {
    const extras = el('div', null, 'dw-card-grid');
    for (const r of featured.slice(0,3)) extras.append(recordCard(r, contaminantName(r), true));
    target.append(extras);
    if (featured.length > 3) target.append(el('p', `Showing 3 of ${featured.length} additional detected contaminants. See full history below.`, 'dw-subtext'));
  } else {
    target.append(el('p', 'No additional detected contaminants identified in the latest available summaries. This does not establish that other contaminants are absent.', 'dw-subtext'));
  }
  const details = el('details');
  details.append(el('summary', `View all contaminant summaries (${records.length})`));
  details.append(el('p', 'Historical yearly and quarterly summary rows are shown separately. They may describe overlapping samples.', 'dw-subtext'));
  details.append(summaryTable(records));
  target.append(details);
}
export function initDrinkingWater(intro, ambientRoot) {
  if (intro.querySelector('.dw-toggle')) return;
  const wrapper = el('div',null,'dw-toggle');
  const ambient = el('button','Rivers & Lakes','dw-tab dw-tab-active');
  const drinking = el('button','Drinking Water','dw-tab');
  ambient.type = drinking.type = 'button';
  wrapper.append(ambient,drinking);
  const section = el('section',null,'dw-panel');
  section.hidden = true;
  section.append(el('h3','Find your public water system'));
  section.append(el('p','Search by system name, county, or NY public water system ID. Confirm your actual utility before interpreting results.'));
  const search = el('input'); search.type='search';search.placeholder='e.g. Albany City';search.setAttribute('aria-label','Public water system search');
  const matches = el('div',null,'dw-matches');
  const detail = el('div',null,'dw-detail');
  section.append(search,matches,detail);
  ambientRoot.before(wrapper);
  ambientRoot.after(section);
  function mode(isDrinking) {
    section.hidden = !isDrinking;
    ambientRoot.hidden = isDrinking;
    ambient.classList.toggle('dw-tab-active',!isDrinking);
    drinking.classList.toggle('dw-tab-active',isDrinking);
  }
  ambient.addEventListener('click',()=>mode(false));
  drinking.addEventListener('click',()=>mode(true));
  let seq=0, timer;
  search.addEventListener('input',()=>{
    const version=++seq;clearTimeout(timer);matches.replaceChildren();detail.replaceChildren();
    const q=search.value.trim();if(q.length<2)return;
    timer=setTimeout(async()=>{
      try {
        const systems=await getJSON(`/api/drinking-water/search?q=${encodeURIComponent(q)}`);
        if(version!==seq)return;
        for(const system of systems){
          const btn=el('button',`${system.name} · ${system.county || 'NY'} · ${system.id}`,'dw-match');btn.type='button';
          btn.addEventListener('click',async()=>{
            const selected=++seq;search.value=system.name;matches.replaceChildren();detail.replaceChildren(el('p','Loading system records…'));
            try {const records=await getJSON(`/api/drinking-water/systems/${encodeURIComponent(system.id)}/contaminants`);
              if(selected===seq)renderSystem(detail,system,records);
            } catch(e) {if(selected===seq)detail.replaceChildren(el('p',`Unable to load records (${e.message}).`));}
          });matches.append(btn);
        }
        if(!systems.length)matches.append(el('p','No matching public water systems found.'));
      } catch(e) {if(version===seq)matches.replaceChildren(el('p',`Search failed (${e.message}).`));}
    },300);
  });
}
