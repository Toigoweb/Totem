// Modalità emergenza su totem e postazioni; tablet con FreeKiosk (API simulata).
import { createServer } from 'node:http';
import { test, expect, pair, syncNow, localDocs, PIN } from './harness.js';

async function ready(page, totem) {
  totem.addPdf('emerg', 'Piano_di_evacuazione_rev5.pdf', 'plain', 'Piano di evacuazione');
  await totem.waitManifest((m) => m.documents.length === 1);
  await pair(page, totem);
  await expect.poll(async () => (await localDocs(page)).filter((d) => d.cached).length, { timeout: 40_000 }).toBe(1);
}

async function openMaint(page) {
  await page.getByTestId('pill').click();
  await page.getByTestId('maint-open').click();
  await page.keyboard.type(PIN);
  await page.keyboard.press('Enter');
  await expect(page.getByTestId('maint')).toBeVisible();
}

async function pairStation(browser, totem) {
  const code = await totem.stationCode('PC ufficio');
  const ctx = await browser.newContext({ viewport: { width: 1280, height: 800 } });
  const p = await ctx.newPage();
  await p.goto(totem.url);
  await p.getByTestId('start-setup').click();
  await p.getByTestId('setup-code').fill(code);
  await p.getByTestId('code-next').click();
  await expect(p.getByTestId('home')).toBeVisible({ timeout: 90_000 });
  return { ctx, page: p };
}

test('emergenza: avvio dalla manutenzione, su tutti i dispositivi, anche offline, poi termina', async ({ page, context, browser, totem }) => {
  await ready(page, totem);
  const st = await pairStation(browser, totem);

  await openMaint(page);
  await page.getByTestId('open-emergency').click();
  await expect(page.getByTestId('emergency-admin')).toContainText('Nessun tablet con FreeKiosk');
  await page.getByTestId('emergency-admin').locator('[data-tipo="avviso"]').click();
  await expect(page.getByTestId('emergency-start')).toBeDisabled(); // un avviso senza testo non parte
  await page.fill('#em-testo', 'Fuga di gas in magazzino: non entrare e allontanarsi dalla zona.');
  await page.getByTestId('emergency-start').click();
  await page.getByTestId('confirm-emergency').click();
  await expect(page.getByTestId('emergency-active')).toContainText('Fuga di gas');
  // in manutenzione resta solo la fascia
  await expect(page.getByTestId('emergency-band')).toContainText('ATTENZIONE');
  await expect(page.getByTestId('emergency-screen')).toHaveCount(0);
  await page.getByTestId('maint-exit').click();
  await expect(page.getByTestId('emergency-screen')).toContainText('ATTENZIONE');
  await expect(page.getByTestId('emergency-text')).toContainText('Fuga di gas in magazzino');

  // la postazione la riceve entro pochi secondi
  await expect(st.page.getByTestId('emergency-screen')).toContainText('Fuga di gas', { timeout: 20_000 });

  // il piano di emergenza resta raggiungibile; dopo, la schermata torna
  await page.getByTestId('emergency-screen').getByRole('button', { name: 'Piani di emergenza' }).click();
  await expect(page.getByTestId('emergency-band')).toBeVisible();
  await expect(page.getByTestId('doc-row')).toHaveCount(1);
  await page.getByTestId('emergency-show').click();
  await expect(page.getByTestId('emergency-screen')).toBeVisible();

  // offline e dopo un ricaricamento resta a schermo
  await context.setOffline(true);
  await page.reload();
  await expect(page.getByTestId('emergency-screen')).toContainText('Fuga di gas', { timeout: 15_000 });
  await context.setOffline(false);

  // si termina con il PIN dalla schermata stessa
  await page.getByTestId('emergency-end').click();
  await page.keyboard.type(PIN);
  await page.keyboard.press('Enter');
  await expect(page.getByTestId('emergency-admin')).toBeVisible();
  await page.getByTestId('emergency-stop').click();
  await page.getByTestId('confirm-emergency').click();
  await expect(page.getByTestId('emergency-band')).toHaveCount(0, { timeout: 15_000 });
  await expect(st.page.getByTestId('emergency-screen')).toHaveCount(0, { timeout: 20_000 });
  await st.ctx.close();
});

/** API REST di FreeKiosk simulata: registra i comandi. */
function fakeFreeKiosk() {
  const calls = [];
  const srv = createServer((req, res) => {
    let body = '';
    req.on('data', (c) => (body += c));
    req.on('end', () => {
      if (req.headers['x-api-key'] !== 'chiave-tablet-1') {
        res.writeHead(401);
        return res.end('{"success":false}');
      }
      calls.push(`${req.method} ${req.url} ${body}`.trim());
      const json = (o) => {
        res.writeHead(200, { 'Content-Type': 'application/json' });
        res.end(JSON.stringify(o));
      };
      if (req.url === '/api/status') {
        return json({ success: true, data: {
          battery: { level: 72, charging: false, temperature: 33 }, screen: { on: true }, wifi: { connected: true, ssid: 'Reparto', rssi: -58 },
          device: { model: 'Tab A9+', manufacturer: 'samsung', android: '14' }, storage: { totalMB: 64000, availableMB: 30000 },
          sensors: { accelerometer: { x: 0, y: 9.8, z: 0 } }, autoBrightness: { enabled: true } } });
      }
      if (req.url === '/api/info') return json({ success: true, data: { version: '1.2.20', isDeviceOwner: true, kioskMode: true } });
      if (req.url === '/api/screenshot') {
        // PNG 1×1 valido
        res.writeHead(200, { 'Content-Type': 'image/png' });
        return res.end(Buffer.from('iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg==', 'base64'));
      }
      json({ success: true, data: {} });
    });
  });
  return { srv, calls };
}

test('tablet FreeKiosk: collegamento, stato, comandi, screenshot e annuncio di emergenza', async ({ page, totem }) => {
  const { srv, calls } = fakeFreeKiosk();
  await new Promise((r) => srv.listen(0, '127.0.0.1', r));
  const addr = `127.0.0.1:${srv.address().port}`;
  try {
    await ready(page, totem);
    await openMaint(page);
    await page.getByTestId('open-devices').click();
    await page.getByTestId('device-row').first().getByTestId('open-tablet').click();
    await expect(page.getByTestId('kiosk-device')).toBeVisible();
    await page.fill('#k-addr', 'http://x');
    await expect(page.getByTestId('kiosk-device')).toContainText('Formato: indirizzo');
    await page.fill('#k-addr', addr);
    await page.fill('#k-key', 'chiave-sbagliata');
    await page.getByTestId('kiosk-test').click();
    await expect(page.getByTestId('kiosk-test-err')).toContainText('Chiave API errata');
    await page.fill('#k-key', 'chiave-tablet-1');
    await page.getByTestId('kiosk-test').click();
    await expect(page.getByTestId('kiosk-test-ok')).toContainText('samsung Tab A9+, FreeKiosk 1.2.20');
    await page.getByTestId('kiosk-save').click();
    await expect(page.getByTestId('kiosk-battery')).toContainText('72% · non in carica', { timeout: 20_000 });

    await page.getByTestId('kiosk-act-ricarica').click();
    await expect(page.getByTestId('toast')).toContainText('Comando inviato');
    await expect.poll(() => calls.some((c) => c.startsWith('POST /api/reload'))).toBe(true);
    await page.getByTestId('kiosk-shot').click();
    await expect.poll(() => page.getByTestId('kiosk-shot-img').evaluate((i) => i.complete && i.naturalWidth)).toBe(1);

    // in Dispositivi il riepilogo del tablet
    await page.getByTestId('back').click();
    await expect(page.getByTestId('device-tablet')).toContainText('Batteria 72% (non in carica)');

    // emergenza: il tablet si sveglia, alza il volume e legge il messaggio
    await page.getByTestId('back').click();
    await page.getByTestId('open-emergency').click();
    await expect(page.getByTestId('emergency-admin')).toContainText('1 tablet con FreeKiosk riceverà');
    await page.getByTestId('emergency-start').click();
    await page.getByTestId('confirm-emergency').click();
    await expect(page.getByTestId('emergency-active')).toBeVisible();
    await expect.poll(() => calls.find((c) => c.startsWith('POST /api/tts')) || '').toContain('"language":"it-IT"');
    expect(calls.some((c) => c === 'POST /api/volume {"value":100}')).toBe(true);
    expect(calls.find((c) => c.startsWith('POST /api/tts'))).toContain('Evacuazione. Lasciare');
    await page.getByTestId('emergency-stop').click();
    await page.getByTestId('confirm-emergency').click();
    await expect(page.getByTestId('emergency-band')).toHaveCount(0, { timeout: 15_000 });
    // la chiave non arriva mai al browser
    const r = await page.evaluate(async () => (await fetch('/manifest.json')).text());
    expect(r).not.toContain('chiave-tablet-1');
  } finally {
    srv.close();
  }
});
