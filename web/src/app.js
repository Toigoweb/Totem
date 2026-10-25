// Stato globale e azioni dell'app (navigazione, dati locali, manutenzione).
import { createStore } from './store.js';
import { openDB } from './sync/db.js';
import { visibleDocuments } from './sync/sync.js';
import { createSyncManager, requestPersistence, detectPlatform } from './sync/manager.js';
import { api } from './api.js';
import { daysSince } from './format.js';

export const APP_VERSION = typeof __APP_VERSION__ !== 'undefined' ? __APP_VERSION__ : 'dev';
export const PLATFORM = detectPlatform();

const SCALES = ['100', '125', '150'];
function loadScale() {
  try {
    const v = localStorage.getItem('totem.scale');
    return SCALES.includes(v) ? v : '100';
  } catch {
    return '100';
  }
}

export const app = createStore({
  ready: false,
  route: { name: 'home' },
  history: [],
  sections: [],
  docs: [],
  extras: { department: null, contacts: [], notices: [], screensaver: null, safety: null },
  settings: {},
  meta: { lastSuccess: null, lastError: null, syncLog: [] },
  sync: { running: false, progress: null, lastError: null, online: true, nextAt: null },
  paired: null, // null = sconosciuto
  role: 'totem', // 'totem' | 'postazione'
  deviceError: false,
  maint: null, // { token }
  toast: null,
  scale: loadScale(),
  persist: 'unsupported',
  serverVersion: null,
  lastInteraction: Date.now(),
});

let db;
let manager;

export const getDB = () => db;
export const getManager = () => manager;

// ---------------------------------------------------------------------------
// Navigazione: una pila semplice, la Home la azzera.

export function go(name, params = {}) {
  const s = app.get();
  if (name === 'home') {
    app.set({ route: { name: 'home' }, history: [] });
    return;
  }
  app.set({ route: { name, ...params }, history: [...s.history, s.route].slice(-20) });
}

/** Sostituisce la schermata corrente senza aggiungerla alla pila. */
export function replace(name, params = {}) {
  app.set({ route: { name, ...params } });
}

export function back() {
  const s = app.get();
  const h = [...s.history];
  const prev = h.pop() || { name: 'home' };
  app.set({ route: prev, history: h });
}

// ---------------------------------------------------------------------------
// Dati locali

export async function reloadData() {
  const [sections, settings, lastSuccess, lastError, syncLog, docs, extras] = await Promise.all([
    db.getMeta('sections'), db.getMeta('settings'), db.getMeta('lastSuccess'), db.getMeta('lastError'), db.getMeta('syncLog'),
    visibleDocuments(db), db.getMeta('extras'),
  ]);
  app.set({
    sections: (sections || []).slice().sort((a, b) => a.order - b.order),
    settings: settings || {},
    docs,
    extras: { department: null, contacts: [], notices: [], screensaver: null, safety: null, ...(extras || {}) },
    meta: { lastSuccess: lastSuccess || null, lastError: lastError || null, syncLog: syncLog || [] },
  });
}

export function setToast(text, ms = 4000) {
  app.set({ toast: text });
  setTimeout(() => {
    if (app.get().toast === text) app.set({ toast: null });
  }, ms);
}

export function cycleScale() {
  const i = (SCALES.indexOf(app.get().scale) + 1) % SCALES.length;
  const scale = SCALES[i];
  try {
    localStorage.setItem('totem.scale', scale);
  } catch {
    // memoria non disponibile: vale solo per questa sessione
  }
  app.set({ scale });
}

// ---------------------------------------------------------------------------
// Manutenzione

export function enterMaintenance(token) {
  app.set({ maint: { token } });
}

export async function exitMaintenance() {
  const m = app.get().maint;
  app.set({ maint: null });
  if (m) api('/api/maint/logout', { method: 'POST', body: {}, maint: m.token }).catch(() => {});
}

/** Chiamata di manutenzione: se il token è scaduto si torna alla Home. */
export async function maintApi(path, opts = {}) {
  const m = app.get().maint;
  if (!m) throw new Error('manutenzione non attiva');
  const r = await api(path, { ...opts, maint: m.token });
  if (r.status === 401 && r.data.error === 'maintenance') {
    app.set({ maint: null });
    go('home');
  }
  return r;
}

// ---------------------------------------------------------------------------
// Avvio

export async function startSync() {
  if (manager) return;
  manager = createSyncManager({ db, appVersion: APP_VERSION });
  let wasRunning = false;
  manager.subscribe((st) => {
    app.set({ sync: st });
    if (st.lastError && st.lastError.code === 'device') app.set({ deviceError: true });
    else if (st.lastResult && !st.running) app.set({ deviceError: false });
    // a fine sync (o durante, quando arriva il manifest) si rileggono i dati locali
    if (wasRunning && !st.running) reloadData();
    if (st.running && st.progress && st.progress.phase === 'download' && st.progress.done === 0) reloadData();
    wasRunning = st.running;
  });
  manager.start();
}

export async function init() {
  db = await openDB();
  await reloadData();
  const hasDocs = app.get().docs.length > 0 || app.get().sections.length > 0;
  const pairedMeta = await db.getMeta('paired');
  const roleMeta = await db.getMeta('role');
  app.set({ ready: true, paired: hasDocs || !!pairedMeta ? true : null, role: roleMeta || 'totem' });
  requestPersistence().then((persist) => app.set({ persist }));

  // il server dice se questo dispositivo è abbinato; offline si usa ciò che c'è in locale
  try {
    const r = await api('/api/setup/state', { timeoutMs: 6000 });
    if (r.ok) {
      app.set({ serverVersion: r.data.serverVersion || null });
      if (r.data.paired) {
        await db.setMeta('paired', true);
        await db.setMeta('role', r.data.role || 'totem');
        app.set({ paired: true, deviceError: false, role: r.data.role || 'totem' });
      } else if (app.get().paired) {
        app.set({ deviceError: true }); // token revocato: i documenti restano consultabili
      } else {
        app.set({ paired: false });
      }
    }
  } catch {
    if (app.get().paired === null) app.set({ paired: false });
  }
  if (app.get().paired) startSync();
}

export async function markPaired(role = 'totem') {
  await db.setMeta('paired', true);
  await db.setMeta('role', role);
  app.set({ paired: true, deviceError: false, role });
}

// ---------------------------------------------------------------------------
// Derivati

/** Stato da mostrare nella pillola in alto a destra. */
export function pillState(s, now = new Date()) {
  const p = s.sync.progress;
  if (s.sync.running) return { kind: 'syncing', done: p ? p.done : 0, total: p ? p.total : 0 };
  if (s.deviceError) return { kind: 'device' };
  const staleDays = s.settings.staleDays || 3;
  const days = daysSince(s.meta.lastSuccess, now);
  if (!s.meta.lastSuccess) return { kind: 'never' };
  if (days >= staleDays) return { kind: 'stale', days };
  const err = s.sync.lastError || s.meta.lastError;
  if (err && err.code === 'offline') return { kind: 'offline' };
  if (typeof navigator !== 'undefined' && navigator.onLine === false) return { kind: 'offline' };
  return { kind: 'ok', at: s.meta.lastSuccess };
}

export function docsBySection(docs) {
  const m = new Map();
  for (const d of docs) {
    if (!m.has(d.section)) m.set(d.section, []);
    m.get(d.section).push(d);
  }
  return m;
}

/** Documenti aggiornati negli ultimi N giorni (max 8), con badge NUOVO o AGGIORNATO. */
export function recentDocs(docs, recentDays = 30, now = new Date()) {
  const limit = now.getTime() - recentDays * 86400000;
  return docs
    .filter((d) => d.available && new Date(d.updated).getTime() >= limit)
    .sort((a, b) => new Date(b.updated) - new Date(a.updated))
    .slice(0, 8)
    .map((d) => ({ ...d, badge: new Date(d.updated).getTime() - new Date(d.firstSeen).getTime() < 60000 ? 'new' : 'updated' }));
}

/** Data locale "AAAA-MM-GG". */
export function localDay(d = new Date()) {
  const p = (n) => String(n).padStart(2, '0');
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())}`;
}

/** Avvisi da mostrare oggi (le date sono incluse; il controllo è locale, vale offline). */
export function activeNotices(notices, now = new Date()) {
  const today = localDay(now);
  return (notices || []).filter((n) => (!n.from || n.from <= today) && (!n.to || n.to >= today));
}
