// Sync incrementale dei PDF, scritta a mano (niente Workbox per i documenti).
//
// 1. scarica il manifest e lo confronta con l'indice locale
// 2. stima lo spazio necessario; se non basta si ferma senza toccare nulla
// 3. scarica solo i PDF nuovi o cambiati e ne verifica lo SHA-256
// 4. sostituisce l'indice in un'unica transazione IndexedDB, solo a download completati
// 5. solo dopo elimina dalla cache i PDF non più referenziati
//
// Una sync fallita lascia l'indice com'era: nessun documento a metà, nessuna sezione vuota.

import { analyzePdf } from './sds.js';

export const CACHE_NAME = 'totem-pdf-v1';
export const fileURL = (sha) => `/api/files/${sha}`;

export class SyncError extends Error {
  /** @param {'offline'|'device'|'server'|'space'|'download'|'integrity'|'storage'|'busy'} code */
  constructor(code, message, extra = {}) {
    super(message || code);
    this.code = code;
    Object.assign(this, extra);
  }
}

async function sha256Hex(buf) {
  const d = await crypto.subtle.digest('SHA-256', buf);
  return Array.from(new Uint8Array(d), (b) => b.toString(16).padStart(2, '0')).join('');
}

function isQuota(e) {
  return e && (e.name === 'QuotaExceededError' || e.code === 22);
}

/**
 * @param {object} o
 * @param {ReturnType<import('./db.js').openDB> extends Promise<infer T> ? T : never} o.db
 * @param {CacheStorage} [o.caches]
 * @param {typeof fetch} [o.fetch]
 * @param {() => Promise<{quota?: number, usage?: number}>} [o.estimate]
 * @param {(buf: ArrayBuffer) => Promise<any>} [o.analyze] analisi SDS (pdf.js)
 * @param {(p: {phase: string, done: number, total: number, bytesDone: number, bytesTotal: number}) => void} [o.onProgress]
 * @param {() => Date} [o.now]
 */
export async function runSync(o) {
  const db = o.db;
  const cachesApi = o.caches || globalThis.caches;
  const doFetch = o.fetch || globalThis.fetch.bind(globalThis);
  const estimate = o.estimate || (() => (navigator.storage && navigator.storage.estimate ? navigator.storage.estimate() : Promise.resolve({})));
  const analyze = o.analyze || analyzePdf;
  const progress = o.onProgress || (() => {});
  const now = o.now || (() => new Date());
  const startedAt = now();

  // 1. manifest
  progress({ phase: 'manifest', done: 0, total: 0, bytesDone: 0, bytesTotal: 0 });
  let res;
  try {
    res = await doFetch('/manifest.json', { credentials: 'same-origin', cache: 'no-store' });
  } catch {
    throw new SyncError('offline', 'server non raggiungibile');
  }
  if (res.status === 401) throw new SyncError('device', 'totem non abbinato o token revocato');
  if (!res.ok) throw new SyncError('server', `manifest: HTTP ${res.status}`);
  let remote;
  try {
    remote = await res.json();
  } catch {
    throw new SyncError('server', 'manifest non valido');
  }
  if (!remote || remote.schemaVersion !== 1 || !Array.isArray(remote.documents) || !Array.isArray(remote.sections)) {
    throw new SyncError('server', 'manifest non valido');
  }

  const cache = await cachesApi.open(CACHE_NAME);
  const local = await db.allDocs();
  const localById = new Map(local.map((d) => [d.id, d]));
  const shas = [...new Set(remote.documents.map((d) => d.sha256))];

  // i nuovi documenti compaiono subito come "In download…" (non apribili)
  await db.setMeta('pending', { version: remote.version, documents: remote.documents.filter((d) => !localById.has(d.id)) });

  const missing = [];
  for (const sha of shas) {
    if (!(await cache.match(fileURL(sha)))) missing.push(sha);
  }
  const sizeOf = new Map(remote.documents.map((d) => [d.sha256, d.size]));
  const bytesTotal = missing.reduce((n, s) => n + (sizeOf.get(s) || 0), 0);

  // 2. spazio: prima di scaricare
  if (missing.length) {
    const est = await estimate().catch(() => ({}));
    if (est && typeof est.quota === 'number' && typeof est.usage === 'number') {
      const margin = ((remote.settings && remote.settings.spaceMarginMB) || 50) * 1024 * 1024;
      const free = est.quota - est.usage;
      if (bytesTotal + margin > free) {
        throw new SyncError('space', 'spazio insufficiente', { needed: bytesTotal, free });
      }
    }
  }

  // 3. download con verifica
  const sdsRecords = [];
  let bytesDone = 0;
  let done = 0;
  progress({ phase: 'download', done, total: missing.length, bytesDone, bytesTotal });
  for (const sha of missing) {
    let r;
    try {
      r = await doFetch(fileURL(sha), { credentials: 'same-origin', cache: 'no-store' });
    } catch {
      throw new SyncError('offline', 'connessione persa durante il download', { done, total: missing.length });
    }
    if (r.status === 401) throw new SyncError('device', 'token revocato');
    if (!r.ok) throw new SyncError('download', `PDF ${sha.slice(0, 12)}: HTTP ${r.status}`);
    let buf;
    try {
      buf = await r.arrayBuffer();
    } catch {
      throw new SyncError('offline', 'connessione persa durante il download', { done, total: missing.length });
    }
    if ((await sha256Hex(buf)) !== sha) throw new SyncError('integrity', `PDF ${sha.slice(0, 12)}: SHA-256 non corrispondente`);
    try {
      await cache.put(fileURL(sha), new Response(buf, { headers: { 'Content-Type': 'application/pdf', 'Content-Length': String(buf.byteLength), ETag: `"${sha}"` } }));
    } catch (e) {
      if (isQuota(e)) throw new SyncError('space', 'spazio esaurito durante il download', { needed: bytesTotal - bytesDone });
      throw new SyncError('storage', 'scrittura nella cache non riuscita');
    }
    // analisi SDS una volta sola, al download
    const a = await analyze(buf);
    sdsRecords.push({ sha256: sha, numPages: a.numPages || 0, recognized: !!a.recognized, sections: a.sections || {} });
    bytesDone += buf.byteLength;
    done++;
    progress({ phase: 'download', done, total: missing.length, bytesDone, bytesTotal });
  }

  // documenti già in cache ma senza analisi (es. dopo un aggiornamento dell'app)
  const known = new Set((await db.allSds()).map((s) => s.sha256));
  for (const sha of shas) {
    if (known.has(sha) || sdsRecords.some((r) => r.sha256 === sha)) continue;
    const hit = await cache.match(fileURL(sha));
    const a = await analyze(await hit.arrayBuffer());
    sdsRecords.push({ sha256: sha, numPages: a.numPages || 0, recognized: !!a.recognized, sections: a.sections || {} });
  }

  // 4. commit atomico dell'indice
  let added = 0;
  let updated = 0;
  const remoteIds = new Set();
  for (const d of remote.documents) {
    remoteIds.add(d.id);
    const old = localById.get(d.id);
    if (!old) added++;
    else if (old.sha256 !== d.sha256) updated++;
  }
  const removed = local.filter((d) => !remoteIds.has(d.id)).length;
  const finishedAt = now();
  const entry = { at: finishedAt.toISOString(), added, updated, removed, errors: 0 };
  const log = ((await db.getMeta('syncLog')) || []).slice(0, 9);
  await db.commit({
    docs: remote.documents,
    sds: sdsRecords,
    keepSds: new Set(shas),
    meta: {
      manifest: { version: remote.version, hash: remote.hash, generatedAt: remote.generatedAt },
      sections: remote.sections,
      settings: remote.settings || {},
      lastSuccess: finishedAt.toISOString(),
      lastAttempt: startedAt.toISOString(),
      lastError: null,
      pending: { version: remote.version, documents: [] },
      syncLog: [entry, ...log],
    },
  });

  // 5. pulizia della cache solo dopo il commit
  const keep = new Set(shas.map(fileURL));
  for (const k of await cache.keys()) {
    const path = new URL(k.url, 'http://x').pathname;
    if (!keep.has(path)) await cache.delete(k);
  }
  progress({ phase: 'done', done, total: missing.length, bytesDone, bytesTotal });
  return { ...entry, version: remote.version, downloaded: missing.length, bytes: bytesDone };
}

/** Registra un tentativo fallito senza toccare l'indice. */
export async function recordFailure(db, err, now = new Date()) {
  const code = err instanceof SyncError ? err.code : 'server';
  await db.setMeta('lastAttempt', now.toISOString());
  await db.setMeta('lastError', { code, at: now.toISOString(), message: String(err && err.message), needed: err.needed, free: err.free });
  if (code !== 'offline') {
    const log = ((await db.getMeta('syncLog')) || []).slice(0, 9);
    await db.setMeta('syncLog', [{ at: now.toISOString(), added: 0, updated: 0, removed: 0, errors: 1, failed: code }, ...log]);
  }
}

/**
 * Documenti visibili: quelli dell'indice con il blob in cache, più i nuovi del manifest
 * in arrivo marcati come non disponibili ("In download…").
 */
export async function visibleDocuments(db, cachesApi = globalThis.caches) {
  const cache = await cachesApi.open(CACHE_NAME);
  const docs = await db.allDocs();
  const out = [];
  for (const d of docs) {
    out.push({ ...d, available: !!(await cache.match(fileURL(d.sha256))) });
  }
  const pending = (await db.getMeta('pending')) || { documents: [] };
  const ids = new Set(docs.map((d) => d.id));
  for (const d of pending.documents || []) {
    if (!ids.has(d.id)) out.push({ ...d, available: false });
  }
  return out;
}
