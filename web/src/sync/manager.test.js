import 'fake-indexeddb/auto';
import { IDBFactory } from 'fake-indexeddb';
import { describe, it, expect, beforeEach, vi } from 'vitest';
import { openDB } from './db.js';
import { createSyncManager, detectPlatform, requestPersistence } from './manager.js';
import { SyncError } from './sync.js';

let db;
beforeEach(async () => {
  globalThis.indexedDB = new IDBFactory();
  db = await openDB();
});

function fakeWindow() {
  const l = {};
  return {
    addEventListener: (t, f) => ((l[t] ||= []).push(f)),
    removeEventListener: (t, f) => (l[t] = (l[t] || []).filter((x) => x !== f)),
    fire: (t) => (l[t] || []).forEach((f) => f()),
  };
}

describe('gestore della sync', () => {
  it('una sola sync alla volta', async () => {
    let calls = 0;
    let release;
    const m = createSyncManager({ db, window: fakeWindow(), fetch: async () => new Response('{}'),
      runSync: () => { calls++; return new Promise((r) => (release = r)); } });
    const a = m.syncNow();
    const b = m.syncNow();
    expect(a).toBe(b);
    release({ added: 0 });
    await a;
    expect(calls).toBe(1);
    m.stop();
  });

  it('"Aggiorna ora" chiede prima una scansione al server', async () => {
    const urls = [];
    const m = createSyncManager({ db, window: fakeWindow(), fetch: async (u) => { urls.push(u); return new Response('{}'); }, runSync: async () => ({}) });
    await m.syncNow({ manual: true });
    expect(urls[0]).toBe('/api/scan');
    m.stop();
  });

  it('errore registrato e interfaccia avvisata, poi ripresa all\'evento online', async () => {
    const w = fakeWindow();
    let fail = true;
    const m = createSyncManager({ db, window: w, fetch: async () => new Response('{}'),
      runSync: async () => { if (fail) throw new SyncError('offline'); return { added: 1 }; } });
    const states = [];
    m.subscribe((s) => states.push(s));
    await m.start();
    expect(m.getState().lastError.code).toBe('offline');
    expect(m.getState().online).toBe(false);
    expect((await db.getMeta('lastError')).code).toBe('offline');
    fail = false;
    w.fire('online');
    await vi.waitFor(() => expect(m.getState().lastResult).toEqual({ added: 1 }));
    expect(m.getState().lastError).toBe(null);
    m.stop();
  });

  it('heartbeat con piattaforma, documenti e spazio', async () => {
    let body;
    const m = createSyncManager({ db, window: fakeWindow(), appVersion: '1.2.3', userAgent: 'Mozilla/5.0 (Linux; Android 14)',
      estimate: async () => ({ quota: 1000, usage: 250 }),
      fetch: async (u, init) => { if (u === '/api/heartbeat') body = JSON.parse(init.body); return new Response('{}'); } });
    await m.heartbeat();
    expect(body).toMatchObject({ appVersion: '1.2.3', platform: 'android', documents: 0, freeBytes: 750, usedBytes: 250 });
    m.stop();
  });
});

describe('utilità', () => {
  it('rileva la piattaforma', () => {
    expect(detectPlatform('Mozilla/5.0 (Windows NT 10.0; Win64; x64) Chrome/130')).toBe('windows');
    expect(detectPlatform('Mozilla/5.0 (Linux; Android 13; SM-X200) Chrome/130')).toBe('android');
    expect(detectPlatform('curl')).toBe('altro');
  });

  it('memoria persistente', async () => {
    expect(await requestPersistence({ storage: { persisted: async () => false, persist: async () => true } })).toBe('granted');
    expect(await requestPersistence({ storage: { persisted: async () => false, persist: async () => false } })).toBe('denied');
    expect(await requestPersistence({})).toBe('unsupported');
  });
});
