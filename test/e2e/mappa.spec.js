// Mappa degli incidenti: planimetria ed eventi dalla manutenzione, mappa di calore e
// giorni senza infortuni sul totem, anche offline.
import { writeFileSync } from 'node:fs';
import { join } from 'node:path';
import { deflateSync } from 'node:zlib';
import { test, expect, pair, syncNow, localDocs, PIN } from './harness.js';

/** PNG w×h grigio chiaro con un "muro" scuro, senza dipendenze. */
function makePng(path, w = 600, h = 400) {
  const crcTable = Array.from({ length: 256 }, (_, n) => {
    let c = n;
    for (let k = 0; k < 8; k++) c = c & 1 ? 0xedb88320 ^ (c >>> 1) : c >>> 1;
    return c >>> 0;
  });
  const crc = (b) => {
    let c = 0xffffffff;
    for (const x of b) c = crcTable[(c ^ x) & 0xff] ^ (c >>> 8);
    return (c ^ 0xffffffff) >>> 0;
  };
  const chunk = (type, data) => {
    const t = Buffer.from(type);
    const len = Buffer.alloc(4);
    len.writeUInt32BE(data.length);
    const c = Buffer.alloc(4);
    c.writeUInt32BE(crc(Buffer.concat([t, data])));
    return Buffer.concat([len, t, data, c]);
  };
  const ihdr = Buffer.alloc(13);
  ihdr.writeUInt32BE(w, 0);
  ihdr.writeUInt32BE(h, 4);
  ihdr[8] = 8; // profondità
  ihdr[9] = 0; // scala di grigi
  const raw = Buffer.alloc((w + 1) * h, 230);
  for (let y = 0; y < h; y++) {
    raw[y * (w + 1)] = 0;
    for (let x = 0; x < w; x++) if (x === Math.floor(w / 2) || y === 20) raw[y * (w + 1) + 1 + x] = 60;
  }
  writeFileSync(path, Buffer.concat([Buffer.from([137, 80, 78, 71, 13, 10, 26, 10]), chunk('IHDR', ihdr), chunk('IDAT', deflateSync(raw)), chunk('IEND', Buffer.alloc(0))]));
  return path;
}

function day(offset) {
  const d = new Date(Date.now() + offset * 86400000);
  const p = (n) => String(n).padStart(2, '0');
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())}`;
}

async function openMaint(page) {
  await page.getByTestId('pill').click();
  await page.getByTestId('maint-open').click();
  await page.keyboard.type(PIN);
  await page.keyboard.press('Enter');
  await expect(page.getByTestId('maint')).toBeVisible();
}

async function addEvent(page, fx, fy, tipo, date, nota = '') {
  await page.getByTestId('admin-map').scrollIntoViewIfNeeded();
  const box = await page.getByTestId('admin-map').boundingBox();
  await page.mouse.click(box.x + box.width * fx, box.y + box.height * fy);
  const dlg = page.getByTestId('event-dialog');
  await expect(dlg).toBeVisible();
  await page.fill('#ev-data', date);
  await dlg.locator(`[data-tipo="${tipo}"]`).click();
  if (nota) await page.fill('#ev-nota', nota);
  await page.getByTestId('event-save').click();
  await expect(dlg).toHaveCount(0);
}

test('mappa incidenti: dalla manutenzione al totem, anche offline', async ({ page, context, totem }) => {
  totem.addPdf('sds', 'SDS_Acetone_rev3.pdf', 'sds', 'Acetone');
  await totem.waitManifest((m) => m.documents.length === 1);
  await pair(page, totem);
  await expect.poll(async () => (await localDocs(page)).filter((d) => d.cached).length, { timeout: 40_000 }).toBe(1);
  await expect(page.getByTestId('safety-open')).toHaveCount(0);

  await openMaint(page);
  await page.getByTestId('open-safety-admin').click();
  await expect(page.getByTestId('safety-admin')).toBeVisible();
  // un file che non è un'immagine viene rifiutato
  const bad = join(totem.dir, 'non-immagine.png');
  writeFileSync(bad, 'ciao');
  await page.fill('#mp-nuovo', 'Stabilimento');
  await page.getByTestId('map-input').setInputFiles(bad);
  await expect(page.getByTestId('map-err')).toContainText('PNG o un JPEG');
  await page.getByTestId('map-input').setInputFiles(makePng(join(totem.dir, 'pianta.png')));
  await expect(page.getByTestId('map-row')).toHaveCount(1);
  await expect(page.getByTestId('admin-map').locator('img')).toBeVisible();

  await addEvent(page, 0.3, 0.4, 'infortunio', day(-10), 'Taglio alla mano');
  await addEvent(page, 0.32, 0.42, 'quasi', day(-3));
  await addEvent(page, 0.8, 0.7, 'danno', day(-400)); // fuori dai 12 mesi
  await expect(page.getByTestId('event-row')).toHaveCount(3);
  await expect(page.getByTestId('marker')).toHaveCount(3);
  // modifica dal punto sulla mappa
  await page.getByTestId('marker').first().click(); // il più recente: il quasi incidente
  await expect(page.getByTestId('event-dialog')).toBeVisible();
  await page.getByTestId('event-dialog').locator('[data-tipo="medicazione"]').click();
  await page.getByTestId('event-save').click();
  await expect(page.getByTestId('event-row').filter({ hasText: 'Medicazione' })).toHaveCount(1);
  // una data futura non è ammessa
  await page.getByTestId('admin-map').scrollIntoViewIfNeeded();
  const box = await page.getByTestId('admin-map').boundingBox();
  await page.mouse.click(box.x + box.width * 0.5, box.y + box.height * 0.5);
  await page.locator('#ev-data').evaluate((el) => { el.removeAttribute('max'); });
  await page.fill('#ev-data', day(5));
  await page.getByTestId('event-dialog').locator('[data-tipo="quasi"]').click();
  await page.getByTestId('event-save').click();
  await expect(page.getByTestId('event-err')).toContainText('futuro');
  await page.getByTestId('event-dialog').getByRole('button', { name: 'Annulla' }).click();

  await page.fill('#sf-inizio', day(-60));
  await page.getByTestId('save-start').click();
  await expect(page.getByTestId('toast')).toBeVisible();
  await page.getByTestId('back').click();
  await page.getByTestId('maint-exit').click();
  await syncNow(page);

  // Home: accesso alla mappa con il contatore
  await expect(page.getByTestId('home-days')).toContainText('10');
  await page.getByTestId('safety-open').click();
  await expect(page.getByTestId('days-counter')).toContainText('10');
  const counts = page.getByTestId('counts');
  await expect(counts.locator('[data-type="infortunio"]')).toContainText('1');
  await expect(counts.locator('[data-type="medicazione"]')).toContainText('1');
  await expect(counts.locator('[data-type="danno"]')).toContainText('0');
  // la mappa di calore è disegnata
  const heat = () => page.locator('[data-testid="map-view"] canvas').evaluate((c) => {
    const d = c.getContext('2d').getImageData(0, 0, c.width, c.height).data;
    let n = 0;
    for (let i = 3; i < d.length; i += 4) if (d[i] > 0) n++;
    return n;
  });
  await expect.poll(heat).toBeGreaterThan(100);
  await page.getByRole('button', { name: '3 anni' }).click();
  await expect(counts.locator('[data-type="danno"]')).toContainText('1');
  await page.getByTestId('show-points').check();
  await expect(page.getByTestId('map-view').getByTestId('marker')).toHaveCount(3);
  // escludendo tutti i tipi tranne i danni, la macchia resta una sola (piccola)
  for (const t of ['infortunio', 'medicazione', 'quasi']) await counts.locator(`[data-type="${t}"]`).click();
  await expect(page.getByTestId('map-view').getByTestId('marker')).toHaveCount(1);

  // offline: planimetria e dati dalla cache
  await context.setOffline(true);
  await page.reload();
  await page.getByTestId('safety-open').click();
  await expect.poll(() => page.locator('[data-testid="map-view"] img').evaluate((i) => i.complete && i.naturalWidth)).toBe(600);
  await expect.poll(heat).toBeGreaterThan(100);
});
