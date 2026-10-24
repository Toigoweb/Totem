// IndexedDB: indice dei documenti, metadati della sync e mappe delle sezioni SDS.
// L'indice è la fonte di verità: un PDF è visibile solo se è qui E nella Cache Storage.

const DB_NAME = 'totem';
const DB_VERSION = 1;

function req(r) {
  return new Promise((resolve, reject) => {
    r.onsuccess = () => resolve(r.result);
    r.onerror = () => reject(r.error);
  });
}

function done(tx) {
  return new Promise((resolve, reject) => {
    tx.oncomplete = () => resolve();
    tx.onerror = () => reject(tx.error);
    tx.onabort = () => reject(tx.error || new Error('transazione annullata'));
  });
}

export function openDB(idb = globalThis.indexedDB) {
  return new Promise((resolve, reject) => {
    const r = idb.open(DB_NAME, DB_VERSION);
    r.onupgradeneeded = () => {
      const db = r.result;
      if (!db.objectStoreNames.contains('docs')) db.createObjectStore('docs', { keyPath: 'id' });
      if (!db.objectStoreNames.contains('meta')) db.createObjectStore('meta');
      if (!db.objectStoreNames.contains('sds')) db.createObjectStore('sds', { keyPath: 'sha256' });
    };
    r.onsuccess = () => resolve(wrap(r.result));
    r.onerror = () => reject(r.error);
  });
}

function wrap(db) {
  return {
    raw: db,
    async getMeta(key) {
      return req(db.transaction('meta').objectStore('meta').get(key));
    },
    async setMeta(key, value) {
      const tx = db.transaction('meta', 'readwrite');
      tx.objectStore('meta').put(value, key);
      return done(tx);
    },
    async allDocs() {
      return req(db.transaction('docs').objectStore('docs').getAll());
    },
    async allSds() {
      return req(db.transaction('sds').objectStore('sds').getAll());
    },
    async getSds(sha) {
      return req(db.transaction('sds').objectStore('sds').get(sha));
    },
    // commit sostituisce l'indice in un'unica transazione: o tutto o niente.
    async commit({ docs, meta, sds, keepSds }) {
      const tx = db.transaction(['docs', 'meta', 'sds'], 'readwrite');
      const d = tx.objectStore('docs');
      d.clear();
      for (const doc of docs) d.put(doc);
      const m = tx.objectStore('meta');
      for (const [k, v] of Object.entries(meta)) m.put(v, k);
      const s = tx.objectStore('sds');
      for (const rec of sds) s.put(rec);
      if (keepSds) {
        const cur = s.openCursor();
        cur.onsuccess = () => {
          const c = cur.result;
          if (!c) return;
          if (!keepSds.has(c.key)) c.delete();
          c.continue();
        };
      }
      return done(tx);
    },
    close() {
      db.close();
    },
  };
}
