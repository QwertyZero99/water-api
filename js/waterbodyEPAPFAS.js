/* EPA Water Quality Portal PFAS samples. Historical only; independent of NYSDEC PFAS. */
function el(tag, text, cls) {
  const node = document.createElement(tag);
  if (text !== undefined && text !== null) node.textContent = String(text);
  if (cls) node.className = cls;
  return node;
}
function validNumber(value) {
  return value !== null && value !== undefined && value !== '' && Number.isFinite(Number(value));
}
function prettyDate(value) {
  if (!value) return 'Date unavailable';
  const d = new Date(`${value.slice(0, 10)}T12:00:00`);
  return Number.isNaN(d.getTime()) ? value : new Intl.DateTimeFormat('en-US', {dateStyle:'medium'}).format(d);
}

export function appendWaterbodyEPAPFAS(container, waterbodyId, signal) {
  // Starts hidden. Empty responses and failed requests leave the interface uncluttered.
  const section = el('section', null, 'waterbody-pfas wb-epa-pfas');
  section.hidden = true;
  const heading = el('div', null, 'waterbody-pfas-heading');
  heading.append(el('h5', 'Historical PFAS Samples'),
    el('span', 'EPA Water Quality Portal', 'waterbody-pfas-badge'));
  const summary = el('p', '', 'waterbody-pfas-summary');
  const list = el('div', null, 'waterbody-pfas-list');
  section.append(heading, summary, list);
  container.append(section);

  const url = `/api/waterbodies/${encodeURIComponent(waterbodyId)}/epa-pfas`;
  (async () => {
    try {
      const res = await fetch(url, {headers:{Accept:'application/json'}, signal});
      if (!res.ok) throw new Error(`HTTP ${res.status}`);
      if (!(res.headers.get('content-type') || '').includes('application/json'))
        throw new Error('EPA PFAS API did not return JSON');
      const data = await res.json();
      if (signal?.aborted || !section.isConnected) return;
      const samples = Array.isArray(data.samples) ? data.samples : [];
      if (!samples.length) return;
      // Only surface-water locations are admitted by the server.
      summary.textContent = `${samples.length} historical lab result${samples.length === 1 ? '' : 's'} at matched surface-water monitoring locations. Not current conditions.`;
      for (const sample of samples) {
        const card = el('article', null, 'waterbody-pfas-site');
        const title = el('h6', `${sample.chemical || sample.characteristic || 'PFAS'} · ${sample.stationName || sample.stationId}`);
        card.append(title);
        const value = sample.nondetect
          ? `Not detected${sample.reportingLimit ? ` (reported limit: ${sample.reportingLimit})` : ''}`
          : validNumber(sample.value) ? `${Number(sample.value)} ${sample.unit || ''}`.trim()
          : sample.detectionCondition || 'Result not quantified';
        card.append(el('p', `${value} · Sampled ${prettyDate(sample.sampleDate)}`));
        if (sample.detectionCondition && !sample.nondetect)
          card.append(el('small', `Reporting qualifier: ${sample.detectionCondition}`));
        if (sample.activityId)
          card.append(el('small', ` · EPA activity: ${sample.activityId}`));
        list.append(card);
      }
      section.hidden = false;
    } catch (error) {
      if (signal?.aborted || error?.name === 'AbortError') return;
      // No blank card or duplicate "No data" warning.
      console.warn('EPA historical PFAS unavailable:', error);
    }
  })();
  return section;
}
