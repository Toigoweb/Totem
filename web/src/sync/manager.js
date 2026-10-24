// Coordina sync e heartbeat: timer, evento "online", "Aggiorna ora". Non usa la
// Periodic Background Sync. Una sola sync alla volta; l'interfaccia non si blocca mai.

import { runSync, recordFailure, SyncError } from './sync.js';

export function detectPlatform(ua = globalThis.navigator ? navigator.userAgent : '') {
  if (/Android/i.test(ua)) return 'android';
  if (/Windows/i.test(ua)) return 'windows';
  if (/iPhone|iPad/i.test(ua)) return 'ios';
  return 'altro';
}

const DEFAULTS = { syncIntervalSec: 300, heartbeatIntervalSec: 300 };

export function createSyncManager(o) {
  const db = o.db;
  const doFetch = o.fetch || globalThis.fetch.bind(globalThis);
  const win = o.window || globalThis;
  const now = o.now || (() => new Date());
  const appVersion = o.appVersion || 'dev';
  const run = o.runSync || runSync;
  const estimate = o.estimate || (() => (navigator.storage && navigator.storage.estimate ? navigator.storage.estimate() : Promise.resolve({})));

  let state = { running: false, progress: null, lastResult: null, lastError: null, nextAt: null, online: true };
  const listeners = new Set();
  const set = (patch) => {
    state = { ...state, ...patch };
    for (const l of listeners) l(state);
  };

  let syncTimer = null;
  let hbTimer = null;
  let current = null;
  let settings = { ...DEFAULTS };

  async function loadSettings() {
    const s = (await db.getMeta('settings')) || {};
    settings = { ...DEFAULTS, ...s };
  }

  function schedule() {
    clearTimeout(syncTimer);
    const ms = settings.syncIntervalSec * 1000;
    set({ nextAt: new Date(now().getTime() + ms) });
    syncTimer = setTimeout(() => syncNow(), ms);
  }

  /** Avvia una sync; se una è già in corso restituisce quella. */
  function syncNow({ manual = false } = {}) {
    if (current) return current;
    current = (async () => {
      set({ running: true, progress: { phase: 'manifest', done: 0, total: 0, bytesDone: 0, bytesTotal: 0 } });
      try {
        if (manual) {
          // chiede al server di riscandire le share; se non risponde si prosegue
          await doFetch('/api/scan', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: '{}', credentials: 'same-origin' }).catch(() => {});
        }
        const r = await run({ db, fetch: doFetch, caches: o.caches, estimate, analyze: o.analyze, now, onProgress: (p) => set({ progress: p }) });
        await loadSettings();
        set({ lastResult: r, lastError: null, online: true });
        return r;
      } catch (e) {
        const err = e instanceof SyncError ? e : new SyncError('server', String(e && e.message));
        await recordFailure(db, err, now()).catch(() => {});
        set({ lastError: { code: err.code, message: err.message, needed: err.needed, free: err.free }, online: err.code !== 'offline' });
        return null;
      } finally {
        current = null;
        set({ running: false, progress: null });
        schedule();
      }
    })();
    return current;
  }

  async function heartbeat() {
    try {
      const est = await estimate().catch(() => ({}));
      const docs = await db.allDocs();
      const body = {
        appVersion,
        platform: detectPlatform(o.userAgent),
        lastSync: (await db.getMeta('lastSuccess')) || null,
        documents: docs.length,
        freeBytes: est && est.quota != null ? Math.max(0, est.quota - (est.usage || 0)) : 0,
        usedBytes: (est && est.usage) || 0,
      };
      await doFetch('/api/heartbeat', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body), credentials: 'same-origin' });
    } catch {
      // offline: si riprova al prossimo giro
    }
    clearTimeout(hbTimer);
    hbTimer = setTimeout(heartbeat, settings.heartbeatIntervalSec * 1000);
  }

  const onOnline = () => {
    set({ online: true });
    syncNow();
  };
  const onOffline = () => set({ online: false });

  return {
    async start() {
      await loadSettings();
      win.addEventListener('online', onOnline);
      win.addEventListener('offline', onOffline);
      heartbeat();
      return syncNow();
    },
    stop() {
      clearTimeout(syncTimer);
      clearTimeout(hbTimer);
      win.removeEventListener('online', onOnline);
      win.removeEventListener('offline', onOffline);
    },
    syncNow,
    heartbeat,
    getState: () => state,
    subscribe(fn) {
      listeners.add(fn);
      return () => listeners.delete(fn);
    },
  };
}

/**
 * Chiede la memoria persistente (non sfrattabile). Va chiesta presto; il risultato
 * si mostra nella schermata Stato. Vedi README per il comportamento dei browser.
 */
export async function requestPersistence(nav = globalThis.navigator) {
  if (!nav || !nav.storage || !nav.storage.persist) return 'unsupported';
  try {
    if (await nav.storage.persisted()) return 'granted';
    return (await nav.storage.persist()) ? 'granted' : 'denied';
  } catch {
    return 'unsupported';
  }
}
