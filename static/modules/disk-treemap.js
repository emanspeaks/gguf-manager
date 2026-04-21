// Non-modal disk-usage treemap dialog

import { esc, formatBytes } from './utils.js';

let dialogEl = null;
let dialogCleanup = null;

export async function openDiskTreemap() {
  if (dialogEl) {
    dialogEl.classList.add('focused');
    return;
  }

  let data;
  try {
    const resp = await fetch('/api/disk-usage');
    if (!resp.ok) throw new Error('fetch failed');
    data = await resp.json();
  } catch (e) {
    return;
  }

  dialogEl = document.createElement('div');
  dialogEl.className = 'disk-treemap-dialog';
  dialogEl.innerHTML = `
    <div class="dtm-header">
      <span class="dtm-title">Disk usage</span>
      <span class="dtm-summary"></span>
      <button class="dtm-close" title="Close">×</button>
    </div>
    <div class="dtm-body"></div>
    <div class="dtm-tooltip" style="display:none"></div>
  `;

  const summary = dialogEl.querySelector('.dtm-summary');
  summary.textContent =
    formatBytes(data.usedBytes) + ' used / ' + formatBytes(data.totalBytes) +
    ' (' + formatBytes(data.freeBytes) + ' free, ' +
    formatBytes(data.modelsDirBytes) + ' in models)';

  dialogEl.querySelector('.dtm-close').addEventListener('click', closeDiskTreemap);

  const detachDrag = setupDragAndResize(dialogEl);

  document.body.appendChild(dialogEl);

  const items = buildItems(data);
  const body = dialogEl.querySelector('.dtm-body');
  const tooltip = dialogEl.querySelector('.dtm-tooltip');

  function render() {
    renderTreemap(body, items, tooltip);
  }

  render();
  const ro = new ResizeObserver(render);
  ro.observe(body);

  dialogCleanup = () => {
    ro.disconnect();
    detachDrag();
  };
}

export function closeDiskTreemap() {
  if (!dialogEl) return;
  if (dialogCleanup) { dialogCleanup(); dialogCleanup = null; }
  dialogEl.remove();
  dialogEl = null;
}

function buildItems(data) {
  const items = [];
  items.push({ kind: 'free', label: 'Free space', size: data.freeBytes });
  if (data.systemBytes > 0) {
    items.push({ kind: 'system', label: '(System files)', size: data.systemBytes });
  }
  for (const f of data.files) {
    if (!f.size || f.size <= 0) continue;
    items.push({ kind: 'file', label: f.path, size: f.size });
  }
  items.sort((a, b) => b.size - a.size);
  return items;
}

function colorFor(item) {
  if (item.kind === 'free') return '#1e293b';
  if (item.kind === 'system') return '#475569';
  // File: color by top-level path segment
  const seg = item.label.split('/')[0] || item.label;
  let h = 0;
  for (let i = 0; i < seg.length; i++) h = (h * 31 + seg.charCodeAt(i)) >>> 0;
  const hue = h % 360;
  return `hsl(${hue}, 55%, 40%)`;
}

function renderTreemap(container, items, tooltip) {
  container.innerHTML = '';
  const w = container.clientWidth;
  const h = container.clientHeight;
  if (w <= 0 || h <= 0 || items.length === 0) return;

  const total = items.reduce((s, it) => s + it.size, 0);
  if (total <= 0) return;

  const rects = squarify(items, 0, 0, w, h, total);

  for (const r of rects) {
    const el = document.createElement('div');
    el.className = 'dtm-block dtm-block-' + r.item.kind;
    el.style.left = r.x + 'px';
    el.style.top = r.y + 'px';
    el.style.width = r.w + 'px';
    el.style.height = r.h + 'px';
    el.style.background = colorFor(r.item);
    const showLabel = r.w >= 60 && r.h >= 22;
    if (showLabel) {
      const shortLabel = r.item.label.split('/').pop();
      el.innerHTML =
        `<span class="dtm-label">${esc(shortLabel)}</span>` +
        `<span class="dtm-size">${esc(formatBytes(r.item.size))}</span>`;
    }
    el.addEventListener('mousemove', (ev) => {
      tooltip.style.display = 'block';
      tooltip.textContent = r.item.label + ' — ' + formatBytes(r.item.size);
      const rect = container.getBoundingClientRect();
      let tx = ev.clientX - rect.left + 12;
      let ty = ev.clientY - rect.top + 12;
      const maxX = container.clientWidth - tooltip.offsetWidth - 4;
      const maxY = container.clientHeight - tooltip.offsetHeight - 4;
      if (tx > maxX) tx = maxX;
      if (ty > maxY) ty = maxY;
      tooltip.style.left = tx + 'px';
      tooltip.style.top = ty + 'px';
    });
    el.addEventListener('mouseleave', () => {
      tooltip.style.display = 'none';
    });
    container.appendChild(el);
  }
}

// Squarified treemap (Bruls, Huijsen, van Wijk).
// items: sorted desc by size. Returns array of {item, x, y, w, h}.
function squarify(items, x, y, w, h, totalSize) {
  const results = [];
  const remaining = items.slice();
  // Scale factor: pixel area per byte
  const scale = (w * h) / totalSize;
  const values = remaining.map((it) => it.size * scale);

  layoutLoop(values, remaining, { x, y, w, h }, results);
  return results;
}

function layoutLoop(values, items, rect, results) {
  let i = 0;
  while (i < values.length) {
    const shorter = Math.min(rect.w, rect.h);
    const row = [values[i]];
    const rowItems = [items[i]];
    let j = i + 1;
    let bestWorst = worst(row, shorter);
    while (j < values.length) {
      row.push(values[j]);
      const candWorst = worst(row, shorter);
      if (candWorst > bestWorst) {
        row.pop();
        break;
      }
      rowItems.push(items[j]);
      bestWorst = candWorst;
      j++;
    }
    const rowSum = row.reduce((s, v) => s + v, 0);
    if (rect.w >= rect.h) {
      // lay out row as a column on the left
      const colW = rowSum / rect.h;
      let yy = rect.y;
      for (let k = 0; k < row.length; k++) {
        const hh = row[k] / colW;
        results.push({ item: rowItems[k], x: rect.x, y: yy, w: colW, h: hh });
        yy += hh;
      }
      rect.x += colW;
      rect.w -= colW;
    } else {
      // lay out row as a row on the top
      const rowH = rowSum / rect.w;
      let xx = rect.x;
      for (let k = 0; k < row.length; k++) {
        const ww = row[k] / rowH;
        results.push({ item: rowItems[k], x: xx, y: rect.y, w: ww, h: rowH });
        xx += ww;
      }
      rect.y += rowH;
      rect.h -= rowH;
    }
    i = j;
  }
}

function worst(row, shorter) {
  let rmax = -Infinity;
  let rmin = Infinity;
  let sum = 0;
  for (const v of row) {
    if (v > rmax) rmax = v;
    if (v < rmin) rmin = v;
    sum += v;
  }
  const s2 = shorter * shorter;
  const sum2 = sum * sum;
  return Math.max((s2 * rmax) / sum2, sum2 / (s2 * rmin));
}

function setupDragAndResize(dlg) {
  const header = dlg.querySelector('.dtm-header');
  let dragging = false;
  let startX = 0, startY = 0, startLeft = 0, startTop = 0;
  function onDown(e) {
    if (e.target.classList.contains('dtm-close')) return;
    dragging = true;
    const rect = dlg.getBoundingClientRect();
    startX = e.clientX;
    startY = e.clientY;
    startLeft = rect.left;
    startTop = rect.top;
    dlg.style.left = startLeft + 'px';
    dlg.style.top = startTop + 'px';
    dlg.style.right = 'auto';
    dlg.style.bottom = 'auto';
    e.preventDefault();
  }
  function onMove(e) {
    if (!dragging) return;
    const nx = startLeft + (e.clientX - startX);
    const ny = startTop + (e.clientY - startY);
    dlg.style.left = Math.max(0, Math.min(window.innerWidth - 80, nx)) + 'px';
    dlg.style.top = Math.max(0, Math.min(window.innerHeight - 40, ny)) + 'px';
  }
  function onUp() { dragging = false; }
  header.addEventListener('mousedown', onDown);
  document.addEventListener('mousemove', onMove);
  document.addEventListener('mouseup', onUp);
  return () => {
    header.removeEventListener('mousedown', onDown);
    document.removeEventListener('mousemove', onMove);
    document.removeEventListener('mouseup', onUp);
  };
}
