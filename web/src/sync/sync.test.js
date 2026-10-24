import 'fake-indexeddb/auto';
import { IDBFactory } from 'fake-indexeddb';
import { describe, it, expect, beforeEach } from 'vitest';
import { openDB } from './db.js';
import { runSync, recordFailure, visibleDocuments, SyncError, CACHE_NAME, fileURL } from './sync.js';
import { findSdsSections } from './sds.js';

// ---- simulazioni di Cache Storage e del server ----

class FakeCache {
  constructor(store) { this.store = store; }
  async match(url) { const b = this.store.get(String(url)); return b ? new Response(b.slice(0)) : undefined; }
  async put(url, res) {
    if (this.store.quota !== undefined) {
      const buf = await res.clone().arrayBuffer();
      if (this.store.used + buf.byteLength > this.store.quota) { const e = new Error('quota'); e.name = 'QuotaExceededError'; throw e; }
      this.store.used += buf.byteLength;
    }
    this.store.set(String(url), await res.arrayBuffer());
  }
  async keys() { return [...this.store.keys()].map((u) => ({ url: 'http://totem' + u })); }
  async delete(k) { return this.store.delete(new URL(k.url).pathname); }
}
class FakeCaches {
  constructor() { this.stores = new Map(); }
  async open(name) { if (!this.stores.has(name)) { const m = new Map(); m.used = 0; this.stores.set(name, m); } return new FakeCache(this.stores.get(name)); }
}

const enc = new TextEncoder();
async function sha(bytes) {
  const d = await crypto.subtle.digest('SHA-256', bytes);
  return Array.from(new Uint8Array(d), (b) => b.toString(16).padStart(2, '0')).join('');
}

class FakeServer {
  constructor() { this.files = new Map(); this.docs = []; this.version = 0; this.offline = false; this.status = 200; this.failFile = null; this.corrupt = null; this.requests = []; }
  async add(id, section, body) {
    const bytes = enc.encode(`%PDF-1.4\n${body}\n%%EOF\n`);
    const h = await sha(bytes);
    this.files.set(h, bytes);
    this.docs = this.docs.filter((d) => d.id !== id);
    this.docs.push({ id, section, path: id.split('/').slice(1).join('/'), folder: '', title: id, rev: '', size: bytes.byteLength, mtime: '2026-10-01T00:00:00Z', sha256: h, firstSeen: '2026-10-01T00:00:00Z', updated: '2026-10-01T00:00:00Z' });
    this.version++;
    return h;
  }
  remove(id) { this.docs = this.docs.filter((d) => d.id !== id); this.version++; }
  manifest() {
    return { schemaVersion: 1, version: this.version, hash: 'h' + this.version, generatedAt: '2026-10-24T08:00:00Z',
      settings: { spaceMarginMB: 0 }, sections: [{ id: 's_a', name: 'A', icon: 'flacone', order: 1, emergency: false }], documents: this.docs };
  }
  fetch = async (url) => {
    this.requests.push(url);
    if (this.offline) throw new TypeError('Failed to fetch');
    if (this.status !== 200) return new Response('{}', { status: this.status });
    if (url === '/manifest.json') return new Response(JSON.stringify(this.manifest()), { status: 200 });
    const h = url.split('/').pop();
    if (h === this.failFile) return new Response('', { status: 404 });
    const b = this.files.get(h);
    if (!b) return new Response('', { status: 404 });
    if (h === this.corrupt) return new Response(b.slice(0, b.byteLength - 3), { status: 200 });
    return new Response(b.slice(0), { status: 200 });
  };
}

const noAnalysis = async () => ({ numPages: 1, recognized: false, sections: {} });

let db, caches, srv;
beforeEach(async () => {
  globalThis.indexedDB = new IDBFactory();
  db = await openDB();
  caches = new FakeCaches();
  srv = new FakeServer();
});

const sync = (extra = {}) => runSync({ db, caches, fetch: srv.fetch, estimate: async () => ({}), analyze: noAnalysis, ...extra });

describe('sync incrementale', () => {
  it('scarica, verifica e indicizza', async () => {
    await srv.add('s_a/uno.pdf', 's_a', 'uno');
    await srv.add('s_a/due.pdf', 's_a', 'due');
    const r = await sync();
    expect(r).toMatchObject({ added: 2, updated: 0, removed: 0, downloaded: 2 });
    const vis = await visibleDocuments(db, caches);
    expect(vis.filter((d) => d.available)).toHaveLength(2);
    expect((await db.getMeta('syncLog'))[0]).toMatchObject({ added: 2 });
    expect(await db.getMeta('lastSuccess')).toBeTruthy();
  });

  it('non riscarica i PDF invariati', async () => {
    await srv.add('s_a/uno.pdf', 's_a', 'uno');
    await sync();
    srv.requests = [];
    const r = await sync();
    expect(r.downloaded).toBe(0);
    expect(srv.requests).toEqual(['/manifest.json']);
  });

  it('aggiorna e poi elimina la vecchia versione dalla cache', async () => {
    const v1 = await srv.add('s_a/uno.pdf', 's_a', 'v1');
    await sync();
    const v2 = await srv.add('s_a/uno.pdf', 's_a', 'v2 diversa');
    const r = await sync();
    expect(r).toMatchObject({ added: 0, updated: 1, removed: 0 });
    const c = await caches.open(CACHE_NAME);
    expect(await c.match(fileURL(v2))).toBeTruthy();
    expect(await c.match(fileURL(v1))).toBeUndefined();
  });

  it('rimuove i documenti tolti dal server, dopo il commit', async () => {
    await srv.add('s_a/uno.pdf', 's_a', 'uno');
    const h2 = await srv.add('s_a/due.pdf', 's_a', 'due');
    await sync();
    srv.remove('s_a/due.pdf');
    const r = await sync();
    expect(r.removed).toBe(1);
    expect((await db.allDocs()).map((d) => d.id)).toEqual(['s_a/uno.pdf']);
    expect(await (await caches.open(CACHE_NAME)).match(fileURL(h2))).toBeUndefined();
  });
});

describe('robustezza: una sync fallita non cambia nulla', () => {
  async function baseline() {
    await srv.add('s_a/uno.pdf', 's_a', 'uno');
    await sync();
    return JSON.stringify(await db.allDocs());
  }

  it('server spento', async () => {
    const before = await baseline();
    srv.offline = true;
    await expect(sync()).rejects.toMatchObject({ code: 'offline' });
    expect(JSON.stringify(await db.allDocs())).toBe(before);
    expect((await visibleDocuments(db, caches))[0].available).toBe(true);
  });

  it('token revocato', async () => {
    await baseline();
    srv.status = 401;
    await expect(sync()).rejects.toMatchObject({ code: 'device' });
  });

  it('download interrotto a metà: nessun documento a metà', async () => {
    const before = await baseline();
    await srv.add('s_a/due.pdf', 's_a', 'due');
    const h3 = await srv.add('s_a/tre.pdf', 's_a', 'tre');
    srv.failFile = h3;
    await expect(sync()).rejects.toMatchObject({ code: 'download' });
    expect(JSON.stringify(await db.allDocs())).toBe(before);
    // i nuovi compaiono come "In download…", non apribili
    const vis = await visibleDocuments(db, caches);
    expect(vis.filter((d) => !d.available).map((d) => d.id).sort()).toEqual(['s_a/due.pdf', 's_a/tre.pdf']);
    // alla sync successiva si riprende senza riscaricare quello già arrivato
    srv.failFile = null;
    srv.requests = [];
    const r = await sync();
    expect(r.downloaded).toBe(1);
    expect((await visibleDocuments(db, caches)).every((d) => d.available)).toBe(true);
  });

  it('PDF arrivato corrotto: SHA-256 non corrispondente', async () => {
    const before = await baseline();
    const h = await srv.add('s_a/uno.pdf', 's_a', 'nuova versione');
    srv.corrupt = h;
    await expect(sync()).rejects.toMatchObject({ code: 'integrity' });
    expect(JSON.stringify(await db.allDocs())).toBe(before);
    expect(await (await caches.open(CACHE_NAME)).match(fileURL(h))).toBeUndefined();
  });

  it('spazio insufficiente stimato prima di scaricare', async () => {
    const before = await baseline();
    await srv.add('s_a/grande.pdf', 's_a', 'x'.repeat(5000));
    srv.requests = [];
    const err = await sync({ estimate: async () => ({ quota: 10000, usage: 9000 }) }).catch((e) => e);
    expect(err).toBeInstanceOf(SyncError);
    expect(err.code).toBe('space');
    expect(err.needed).toBeGreaterThan(5000);
    expect(srv.requests).toEqual(['/manifest.json']); // nessun download tentato
    expect(JSON.stringify(await db.allDocs())).toBe(before);
    await recordFailure(db, err);
    expect(await db.getMeta('lastError')).toMatchObject({ code: 'space' });
  });

  it('quota esaurita durante il download', async () => {
    const before = await baseline();
    await srv.add('s_a/grande.pdf', 's_a', 'x'.repeat(5000));
    (await caches.open(CACHE_NAME)).store.quota = 1000;
    await expect(sync()).rejects.toMatchObject({ code: 'space' });
    expect(JSON.stringify(await db.allDocs())).toBe(before);
  });

  it('sezione svuotata solo se il server la svuota davvero', async () => {
    await baseline();
    srv.remove('s_a/uno.pdf');
    srv.offline = true;
    await expect(sync()).rejects.toBeTruthy();
    expect(await db.allDocs()).toHaveLength(1);
  });
});

describe('analisi SDS', () => {
  it('salva la mappa sezioni → pagina al download', async () => {
    const h = await srv.add('s_a/sds.pdf', 's_a', 'sds');
    await sync({ analyze: async () => ({ numPages: 12, recognized: true, sections: { 4: 3, 5: 4, 8: 6 } }) });
    expect(await db.getSds(h)).toMatchObject({ recognized: true, numPages: 12, sections: { 4: 3 } });
    srv.remove('s_a/sds.pdf');
    await sync();
    expect(await db.getSds(h)).toBeUndefined();
  });

  it('un errore di analisi non blocca la sync', async () => {
    await srv.add('s_a/strano.pdf', 's_a', 'x');
    const { analyzePdf } = await import('./sds.js');
    await sync({ analyze: (b) => analyzePdf(b, async () => { throw new Error('pdf.js'); }) });
    expect(await db.allDocs()).toHaveLength(1);
  });

  it('riconosce le intestazioni e ignora i rimandi', () => {
    const pages = [
      'SCHEDA DI DATI DI SICUREZZA\nSEZIONE 1: Identificazione\n1.1 Identificatore\nSEZIONE 2: Pericoli\nvedere la sezione 8 per i DPI\n',
      'SEZIONE 3: Composizione\nSEZIONE 4: Misure di primo soccorso\n',
      'SEZIONE 5: Misure antincendio\nSEZIONE 6. Rilascio\nSEZIONE 7 Manipolazione\n',
      'SEZIONE 8: Controllo dell\'esposizione/protezione individuale\nSEZIONE 9: Proprietà\n',
      'SECTION 10: Stability\nSEZIONE 11: Tossicologia\n',
    ];
    const r = findSdsSections(pages);
    expect(r.recognized).toBe(true);
    expect(r.sections).toMatchObject({ 1: 1, 2: 1, 3: 2, 4: 2, 5: 3, 8: 4, 10: 5, 11: 5 });
    expect(r.sections[12]).toBeUndefined();
  });

  it('un indice iniziale non sposta le sezioni', () => {
    const toc = Array.from({ length: 16 }, (_, i) => `SEZIONE ${i + 1} ....... ${i + 2}`).join('\n');
    const pages = [toc, ...Array.from({ length: 16 }, (_, i) => `SEZIONE ${i + 1}: testo\n`)];
    const r = findSdsSections(pages);
    expect(r.recognized).toBe(true);
    expect(r.sections[1]).toBe(2);
    expect(r.sections[4]).toBe(5);
    expect(r.sections[16]).toBe(17);
  });

  it('documento normale: non riconosciuto', () => {
    expect(findSdsSections(['Piano di evacuazione\nRiferimento alla sezione 4 del manuale']).recognized).toBe(false);
  });
});
