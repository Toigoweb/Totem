// Ambiente di prova: un server totem per test, con dati puliti, due sezioni già
// configurate (SDS ed emergenza) su sottocartelle del Samba di test.
import { test as base, expect } from '@playwright/test';
import { spawn, execFileSync } from 'node:child_process';
import { createCipheriv, randomBytes } from 'node:crypto';
import { mkdirSync, writeFileSync, readFileSync, existsSync, rmSync, chmodSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import net from 'node:net';

const here = dirname(fileURLToPath(import.meta.url));
export const ROOT = join(here, '..', '..');
export const WORK = join(here, '.work');
export const BIN = process.env.TOTEM_BIN || join(WORK, 'totem');
export const SMB_PORT = 1446;
export const PIN = '2468';

const KEY = Buffer.alloc(32, 7); // chiave fissa: serve solo a cifrare le password di prova

function encrypt(sectionId, plain) {
  const nonce = randomBytes(12);
  const c = createCipheriv('aes-256-gcm', KEY, nonce);
  c.setAAD(Buffer.from(sectionId));
  const ct = Buffer.concat([c.update(plain, 'utf8'), c.final()]);
  return 'v1:' + Buffer.concat([nonce, ct, c.getAuthTag()]).toString('base64');
}

function freePort() {
  return new Promise((resolve) => {
    const s = net.createServer();
    s.listen(0, '127.0.0.1', () => {
      const p = s.address().port;
      s.close(() => resolve(p));
    });
  });
}

let counter = 0;

export class Totem {
  constructor(name) {
    this.id = `${name.replace(/[^a-z0-9]+/gi, '-').slice(0, 40)}-${Date.now()}-${counter++}`;
    this.dir = join(WORK, 'run', this.id);
    this.share = join(WORK, 'share', this.id);
    this.log = '';
    this.env = {}; // variabili aggiuntive per il server (es. Graph simulato)
  }

  async setup() {
    for (const d of ['data', 'secrets']) mkdirSync(join(this.dir, d), { recursive: true });
    for (const d of ['sds', 'emerg']) mkdirSync(join(this.share, d), { recursive: true });
    writeFileSync(join(this.dir, 'secrets', 'key'), KEY, { mode: 0o600 });
    const sec = (id, nome, icona, ordine, emergenza, sub) => ({
      id, nome, icona, ordine, emergenza,
      share: { percorso: `\\\\127.0.0.1\\e2e\\${this.id}\\${sub}`, utente: 'totem', password: encrypt(id, 'Giusta-123'), sottocartelle: true },
    });
    const cfg = {
      schemaVersion: 1,
      sezioni: [sec('s_sds', 'Schede di sicurezza', 'flacone', 1, false, 'sds'), sec('s_emerg', 'Piani di emergenza', 'uscita', 2, true, 'emerg')],
      globali: { fase2: false, dispositivi: [] },
      // tempi brevi per i test: scansione ogni 10 s, stabilità dopo 1 s
      impostazioni: { intervalloScansione: '10s', ritardoStabilita: '1s', intervalloSync: '30s', intervalloHeartbeat: '30s' },
    };
    writeFileSync(join(this.dir, 'data', 'config.json'), JSON.stringify(cfg, null, 2));
    this.port = await freePort();
    this.internal = await freePort();
    this.url = `http://localhost:${this.port}`;
  }

  async start() {
    this.proc = spawn(BIN, ['serve'], {
      env: { ...process.env, TOTEM_DATA: join(this.dir, 'data'), TOTEM_SECRETS: join(this.dir, 'secrets'),
        TOTEM_ADDR: `127.0.0.1:${this.port}`, TOTEM_INTERNAL_ADDR: `127.0.0.1:${this.internal}`, TOTEM_SMB_PORT: String(SMB_PORT), TZ: 'Europe/Rome', ...this.env },
      stdio: ['ignore', 'pipe', 'pipe'],
    });
    this.proc.stdout.on('data', (b) => (this.log += b));
    this.proc.stderr.on('data', (b) => (this.log += b));
    const until = Date.now() + 10_000;
    while (Date.now() < until) {
      try {
        const r = await fetch(`${this.url}/api/health`);
        if (r.ok) return;
      } catch {
        // non ancora in ascolto
      }
      await new Promise((r) => setTimeout(r, 100));
    }
    throw new Error('il server non parte:\n' + this.log);
  }

  async stop() {
    if (!this.proc || this.proc.exitCode !== null) return;
    const done = new Promise((r) => this.proc.once('exit', r));
    this.proc.kill('SIGTERM');
    await done;
  }

  setupCode() {
    const m = [...this.log.matchAll(/CODICE DI SETUP DEL TOTEM:\s+(\S+)/g)];
    return m.length ? m[m.length - 1][1] : null;
  }

  /** Crea un PDF nella share (kind: sds | plain | truncated). */
  addPdf(folder, name, kind = 'sds', title = name) {
    const p = join(this.share, folder, name);
    mkdirSync(dirname(p), { recursive: true });
    execFileSync('python3', [join(ROOT, 'test', 'fixtures', 'genpdf.py'), p, kind, title, '3']);
    chmodSync(p, 0o644);
    return p;
  }

  /** Crea un PDF fuori dalla share (da caricare dal browser) e ne restituisce il percorso. */
  makePdf(name, kind = 'sds', title = name) {
    const p = join(this.dir, 'locali', name);
    mkdirSync(dirname(p), { recursive: true });
    execFileSync('python3', [join(ROOT, 'test', 'fixtures', 'genpdf.py'), p, kind, title, '3']);
    return p;
  }

  writeFile(folder, name, data) {
    writeFileSync(join(this.share, folder, name), data, { mode: 0o644 });
  }

  removePdf(folder, name) {
    rmSync(join(this.share, folder, name));
  }

  /** Rende la cartella della share illeggibile (share "persa" per il server). */
  breakShare(folder) {
    chmodSync(join(this.share, folder), 0o000);
  }

  fixShare(folder) {
    chmodSync(join(this.share, folder), 0o755);
  }

  manifest() {
    const p = join(this.dir, 'data', 'manifest.json');
    return existsSync(p) ? JSON.parse(readFileSync(p, 'utf8')) : null;
  }

  /** Aspetta che il server pubblichi un manifest che soddisfa pred. */
  async waitManifest(pred, ms = 40_000) {
    const until = Date.now() + ms;
    while (Date.now() < until) {
      const m = this.manifest();
      if (m && pred(m)) return m;
      await this.kick();
      await new Promise((r) => setTimeout(r, 1000));
    }
    throw new Error('manifest atteso non pubblicato:\n' + JSON.stringify(this.manifest(), null, 1));
  }

  /** Chiede una scansione immediata tramite il listener interno. */
  async kick() {
    try {
      await fetch(`http://127.0.0.1:${this.internal}/internal/scan`, { method: 'POST' });
    } catch {
      // ignorato
    }
  }

  /** Codice di postazione dal server (come "totem abbina-postazione"). */
  async stationCode(nome = 'Postazione') {
    const r = await fetch(`http://127.0.0.1:${this.internal}/internal/pair-station?nome=${encodeURIComponent(nome)}`, { method: 'POST' });
    return (await r.json()).code;
  }

  async resetPairing() {
    const r = await fetch(`http://127.0.0.1:${this.internal}/internal/reset-pairing`, { method: 'POST' });
    return (await r.json()).setupCode;
  }
}

// ---------------------------------------------------------------------------
// Azioni sulla pagina

/** Abbina il browser al server via API (codice + PIN), poi carica l'app. */
export async function pair(page, totem) {
  await page.goto(totem.url);
  const code = totem.setupCode();
  const ok = await page.evaluate(async ({ code, pin }) => {
    const j = (u, b) => fetch(u, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(b) }).then((r) => r.json());
    const a = await j('/api/setup/code', { code });
    const b = await j('/api/setup/pin', { setupToken: a.setupToken, pin, platform: 'windows' });
    return !!b.ok;
  }, { code, pin: PIN });
  expect(ok).toBe(true);
  await page.reload();
  await expect(page.getByTestId('home')).toBeVisible();
  // la shell dell'app è in cache e il service worker controlla la pagina
  await page.waitForFunction(() => !!navigator.serviceWorker.controller, null, { timeout: 30_000 });
}

/** "Aggiorna ora" dalla schermata Stato; torna alla Home quando la sync è finita. */
export async function syncNow(page) {
  if (!(await page.getByTestId('status').isVisible())) {
    await page.getByTestId('pill').click();
    await expect(page.getByTestId('status')).toBeVisible();
  }
  await expect(page.getByTestId('sync-now')).toBeEnabled({ timeout: 60_000 });
  await page.getByTestId('sync-now').click();
  await expect(page.getByTestId('pill')).not.toHaveAttribute('data-kind', 'syncing', { timeout: 60_000 });
  await expect(page.getByTestId('sync-now')).toBeEnabled({ timeout: 60_000 });
  await page.getByTestId('back').click();
}

/** Documenti nell'indice locale (IndexedDB), con disponibilità nella cache. */
export function localDocs(page) {
  return page.evaluate(async () => {
    const db = await new Promise((res, rej) => {
      const r = indexedDB.open('totem');
      r.onsuccess = () => res(r.result);
      r.onerror = () => rej(r.error);
    });
    const docs = await new Promise((res) => {
      const t = db.transaction('docs').objectStore('docs').getAll();
      t.onsuccess = () => res(t.result);
    });
    const cache = await caches.open('totem-pdf-v1');
    const out = [];
    for (const d of docs) out.push({ id: d.id, sha256: d.sha256, title: d.title, cached: !!(await cache.match(`/api/files/${d.sha256}`)) });
    db.close();
    return out;
  });
}

/** Apre un documento dalla Home passando dalla sezione. */
export async function openDoc(page, sectionName, title) {
  await page.getByTestId('tile').filter({ hasText: sectionName }).click();
  await page.getByTestId('doc-row').filter({ hasText: title }).click();
  await expect(page.getByTestId('reader')).toBeVisible();
  await expect(page.getByTestId('page-of')).toContainText('Pagina 1 di', { timeout: 20_000 });
}

export const test = base.extend({
  totem: async ({}, use, testInfo) => {
    const t = new Totem(`${testInfo.project.name}-${testInfo.title}`);
    await t.setup();
    await t.start();
    await use(t);
    await t.stop();
    try {
      t.fixShare('sds');
      t.fixShare('emerg');
    } catch {
      // già sistemata
    }
    if (testInfo.status !== testInfo.expectedStatus) await testInfo.attach('server.log', { body: t.log, contentType: 'text/plain' });
  },
});

export { expect };
