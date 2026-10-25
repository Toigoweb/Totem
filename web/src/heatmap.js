// Mappa di calore degli eventi di sicurezza, su canvas, senza librerie.
//
// 1. ogni evento lascia una "macchia" sfumata in scala di grigi, più intensa se grave
// 2. le macchie si sommano (le zone con più eventi diventano più scure)
// 3. l'intensità si colora con una scala trasparente → giallo → arancio → rosso

export const EVENT_TYPES = ['infortunio', 'medicazione', 'quasi', 'danno'];
export const WEIGHT = { infortunio: 1, medicazione: 0.6, quasi: 0.35, danno: 0.35 };
export const PERIODS = { '3m': 3, '12m': 12, all: 36 };

function monthsBack(today, months) {
  const [y, m, d] = today.split('-').map(Number);
  const t = new Date(Date.UTC(y, m - 1 - months, d));
  return t.toISOString().slice(0, 10);
}

/** Eventi di una planimetria nel periodo (date AAAA-MM-GG, estremi inclusi) e dei tipi scelti. */
export function filterEvents(events, { map, period = '12m', types = EVENT_TYPES, today }) {
  const from = monthsBack(today, PERIODS[period] || 12);
  const ok = new Set(types);
  return (events || []).filter((e) => e.map === map && ok.has(e.type) && e.date >= from && e.date <= today);
}

export function countByType(events) {
  const c = Object.fromEntries(EVENT_TYPES.map((t) => [t, 0]));
  for (const e of events) if (e.type in c) c[e.type]++;
  return c;
}

/**
 * Giorni senza infortuni: dall'ultimo infortunio o, se non ce ne sono, dall'inizio della
 * registrazione. null se la registrazione non ha una data di inizio (dato non affidabile).
 */
export function daysWithoutInjury(lastInjury, since, today) {
  if (!since) return null;
  const ref = lastInjury && lastInjury > since ? lastInjury : since;
  const d = (s) => Date.UTC(...s.split('-').map((x, i) => Number(x) - (i === 1 ? 1 : 0)));
  return Math.max(0, Math.round((d(today) - d(ref)) / 86400000));
}

// scala di colori: 256 valori RGBA
let palette;
function getPalette() {
  if (palette) return palette;
  const c = document.createElement('canvas');
  c.width = 256;
  c.height = 1;
  const g = c.getContext('2d');
  const grad = g.createLinearGradient(0, 0, 256, 0);
  grad.addColorStop(0, 'rgba(255, 220, 0, 0)');
  grad.addColorStop(0.2, 'rgba(255, 220, 0, 0.55)');
  grad.addColorStop(0.5, 'rgba(255, 140, 0, 0.75)');
  grad.addColorStop(1, 'rgba(200, 16, 46, 0.9)');
  g.fillStyle = grad;
  g.fillRect(0, 0, 256, 1);
  palette = g.getImageData(0, 0, 256, 1).data;
  return palette;
}

/** Disegna la mappa di calore su canvas (dimensioni già impostate). */
export function drawHeat(canvas, events) {
  const w = canvas.width;
  const h = canvas.height;
  const g = canvas.getContext('2d', { willReadFrequently: true });
  g.clearRect(0, 0, w, h);
  if (!events.length) return;
  const r = Math.max(12, Math.round(Math.min(w, h) * 0.06));
  // intensità relativa: il massimo atteso cresce con il numero di eventi, ma meno che
  // linearmente, così un singolo evento resta visibile e molti eventi non saturano tutto
  const scale = 1 / Math.max(1, Math.sqrt(events.length) * 0.6);
  for (const e of events) {
    const x = e.x * w;
    const y = e.y * h;
    const grad = g.createRadialGradient(x, y, 0, x, y, r);
    grad.addColorStop(0, `rgba(0,0,0,${Math.min(1, (WEIGHT[e.type] || 0.3) * scale + 0.15)})`);
    grad.addColorStop(1, 'rgba(0,0,0,0)');
    g.fillStyle = grad;
    g.fillRect(x - r, y - r, 2 * r, 2 * r);
  }
  const img = g.getImageData(0, 0, w, h);
  const px = img.data;
  const pal = getPalette();
  for (let i = 0; i < px.length; i += 4) {
    const a = px[i + 3];
    if (!a) continue;
    const k = a * 4;
    px[i] = pal[k];
    px[i + 1] = pal[k + 1];
    px[i + 2] = pal[k + 2];
    px[i + 3] = pal[k + 3];
  }
  g.putImageData(img, 0, 0);
}
