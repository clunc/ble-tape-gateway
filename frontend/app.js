'use strict';

const statusEl    = document.getElementById('status');
const valueEl     = document.getElementById('value');
const valueCmEl   = document.getElementById('value-cm');
const timestampEl = document.getElementById('timestamp');
const deltaEl     = document.getElementById('delta');
const logBody     = document.querySelector('#log tbody');
const selectEl    = document.getElementById('body-part');
const logSection  = document.getElementById('log-section');
const logBtn      = document.getElementById('log-btn');

const MAX_ROWS = 50;

// last logged entry per body part — used for both live display delta and log table delta
const lastLoggedByBodyPart = new Map();

// most recent measurement — captured by Log button
let current = null;

logBtn.addEventListener('click', async () => {
  if (!current) return;
  const { mm, bp, ts, deviceId } = current;
  const prevLogged = lastLoggedByBodyPart.get(bp) ?? null;
  const diff = (mm > 0 && prevLogged !== null) ? mm - prevLogged : null;
  if (mm > 0) lastLoggedByBodyPart.set(bp, mm);

  try {
    await fetch('/measurements', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        device_id: deviceId,
        body_part: bp,
        circumference_mm: mm,
        timestamp_unix_ms: ts,
      }),
    });
  } catch (err) {
    console.warn('save failed', err);
  }

  const cls = deltaClass(diff);
  const tr  = document.createElement('tr');
  tr.innerHTML =
    `<td>${formatTime(ts)}</td>` +
    `<td class="body-part">${bp || '—'}</td>` +
    `<td>${mm.toFixed(1)}</td>` +
    `<td class="${cls}">${deltaText(diff)}</td>` +
    `<td><button class="delete-btn" title="Delete">×</button></td>`;
  tr.querySelector('.delete-btn').addEventListener('click', () => tr.remove());
  logBody.prepend(tr);
  while (logBody.rows.length > MAX_ROWS) logBody.deleteRow(logBody.rows.length - 1);

  logBtn.textContent = 'Logged ✓';
  setTimeout(() => { logBtn.textContent = 'Log'; }, 1000);
});

// --- Log toggle ---


// --- Body part selector ---

async function loadBodyParts() {
  try {
    const res = await fetch('/body-part');
    const data = await res.json();
    selectEl.innerHTML = '';
    data.body_parts.forEach(bp => {
      const opt = document.createElement('option');
      opt.value = bp;
      opt.textContent = bp;
      if (bp === data.body_part) opt.selected = true;
      selectEl.appendChild(opt);
    });
  } catch (err) {
    console.warn('could not load body parts', err);
  }
}

selectEl.addEventListener('change', async () => {
  try {
    await fetch('/body-part', {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ body_part: selectEl.value }),
    });
  } catch (err) {
    console.warn('could not update body part', err);
  }
});

// --- Status ---

function setStatus(connected) {
  statusEl.textContent = connected ? 'Live' : 'Disconnected';
  statusEl.className = 'status ' + (connected ? 'connected' : 'disconnected');
}

// --- Delta ---

function renderDelta(diff) {
  if (diff === null) {
    deltaEl.innerHTML = '';
    return null;
  }
  const sign  = diff > 0 ? '+' : '';
  const cls   = diff > 0.05 ? 'delta-up' : diff < -0.05 ? 'delta-down' : 'delta-same';
  const arrow = diff > 0.05 ? '↑' : diff < -0.05 ? '↓' : '=';
  deltaEl.innerHTML = `<span class="delta-pill ${cls}">${arrow} ${sign}${diff.toFixed(1)} mm</span>`;
  return cls;
}

function deltaClass(diff) {
  if (diff === null) return '';
  return diff > 0.05 ? 'delta-up' : diff < -0.05 ? 'delta-down' : 'delta-same';
}

function deltaText(diff) {
  if (diff === null) return '—';
  const sign = diff > 0 ? '+' : '';
  const arrow = diff > 0.05 ? '↑ ' : diff < -0.05 ? '↓ ' : '= ';
  return arrow + sign + diff.toFixed(1);
}

// --- Measurement display ---

function formatTime(unixMs) {
  return new Date(unixMs).toLocaleTimeString([], {
    hour12: false, hour: '2-digit', minute: '2-digit', second: '2-digit',
  });
}

function onMeasurement(m) {
  const mm = m.circumference_mm;
  const bp = m.body_part || '';

  const prev = lastLoggedByBodyPart.get(bp) ?? null;
  const diff = (mm > 0 && prev !== null) ? mm - prev : null;

  current = { mm, bp, diff, ts: m.timestamp_unix_ms, deviceId: m.device_id };
  logBtn.disabled = false;

  valueEl.textContent     = mm.toFixed(1);
  valueCmEl.textContent   = (mm / 10).toFixed(2);
  timestampEl.textContent = formatTime(m.timestamp_unix_ms);
  renderDelta(diff);

  // Sync dropdown if body part changed server-side
  if (bp && selectEl.value !== bp) {
    for (const opt of selectEl.options) {
      if (opt.value === bp) { opt.selected = true; break; }
    }
  }
}

// --- SSE ---

function connect() {
  const es = new EventSource('/ws');
  es.onopen    = () => setStatus(true);
  es.onmessage = (e) => {
    try { onMeasurement(JSON.parse(e.data)); }
    catch (err) { console.warn('bad message', e.data, err); }
  };
  es.onerror   = () => {
    setStatus(false);
    es.close();
    setTimeout(connect, 3000);
  };
}

async function loadTodayLog() {
  try {
    const res  = await fetch('/measurements/today');
    const data = await res.json();
    // Render oldest→newest so prepend order matches live entries
    data.forEach(m => {
      const bp   = m.body_part || '';
      const mm   = m.circumference_mm;
      const prev = lastLoggedByBodyPart.get(bp) ?? null;
      const diff = (mm > 0 && prev !== null) ? mm - prev : null;
      if (mm > 0) lastLoggedByBodyPart.set(bp, mm);

      const cls = deltaClass(diff);
      const tr  = document.createElement('tr');
      tr.innerHTML =
        `<td>${formatTime(m.timestamp_unix_ms)}</td>` +
        `<td class="body-part">${bp || '—'}</td>` +
        `<td>${mm.toFixed(1)}</td>` +
        `<td class="${cls}">${deltaText(diff)}</td>` +
        `<td><button class="delete-btn" title="Delete">×</button></td>`;
      tr.querySelector('.delete-btn').addEventListener('click', () => tr.remove());
      logBody.appendChild(tr);
    });
  } catch (err) {
    console.warn('could not load today log', err);
  }
}

loadBodyParts();
loadTodayLog();
connect();
