// Controllo dei dispositivi: comandi all'app (anche su un'altra postazione) e agente del
// PC Windows, simulato qui con le stesse chiamate di totem-agente.ps1.
import { test, expect, pair, localDocs, PIN } from './harness.js';

async function ready(page, totem) {
  totem.addPdf('sds', 'SDS_Acetone_rev3.pdf', 'sds', 'Acetone');
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

// PNG 1×1 valido
const PNG = Buffer.from('iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg==', 'base64');

/** Agente simulato: un giro di rapporto; esegue lo screenshot se richiesto. */
async function agentRound(url, token, done = []) {
  const report = { versione: '1.0', chrome: true, rete: false, batteria: 81, uptimeMin: 1440, discoLiberoMB: 50000,
    riavvioInAttesa: true, windows: 'Windows 11 Pro 24H2 (26100)', schermoSpento: false, fatti: done };
  const r = await fetch(`${url}/api/agent/report`, { method: 'POST', headers: { 'Content-Type': 'application/json', Authorization: `Bearer ${token}` }, body: JSON.stringify(report) });
  const body = await r.json();
  const fatti = [];
  for (const c of body.comandi || []) {
    if (c.azione === 'screenshot') {
      await fetch(`${url}/api/agent/screenshot`, { method: 'POST', headers: { 'Content-Type': 'image/png', Authorization: `Bearer ${token}` }, body: PNG });
    }
    fatti.push({ id: c.id, ok: true });
  }
  return { status: r.status, body, fatti };
}

test('controllo: comandi all\'app e agente del PC Windows', async ({ page, browser, totem }) => {
  await ready(page, totem);

  // una postazione su un'altra schermata riceve "Torna alla Home"
  const code = await totem.stationCode('PC ufficio');
  const ctx = await browser.newContext({ viewport: { width: 1280, height: 800 } });
  const st = await ctx.newPage();
  await st.goto(totem.url);
  await st.getByTestId('start-setup').click();
  await st.getByTestId('setup-code').fill(code);
  await st.getByTestId('code-next').click();
  await expect(st.getByTestId('home')).toBeVisible({ timeout: 90_000 });
  await st.getByTestId('search-open').click();
  await expect(st.getByTestId('home')).toHaveCount(0);

  await openMaint(page);
  await page.getByTestId('open-devices').click();
  await page.getByTestId('device-row').filter({ hasText: 'PC ufficio' }).getByTestId('open-tablet').click();
  await expect(page.getByTestId('ctl-app')).toBeVisible();
  await page.getByTestId('app-act-home').click();
  await expect(page.getByTestId('toast')).toContainText('entro 10 secondi');
  await expect(st.getByTestId('home')).toBeVisible({ timeout: 20_000 });
  await ctx.close();

  // agente del PC del totem
  await page.getByTestId('back').click();
  await page.getByTestId('device-row').filter({ hasText: 'totem' }).first().getByTestId('open-tablet').click();
  // su Android la sezione del PC si apre a mano
  if (await page.getByTestId('show-pc').count()) await page.getByTestId('show-pc').click();
  await expect(page.getByTestId('ctl-pc')).toContainText('Agente non collegato');
  await page.getByTestId('pc-token-new').click();
  const cmd = await page.getByTestId('pc-token').locator('pre').textContent();
  expect(cmd).toContain(`-Url ${totem.url}`);
  const token = cmd.trim().split(' ').pop();
  expect(token.length).toBeGreaterThan(30);

  expect((await agentRound(totem.url, 'sbagliato')).status).toBe(401);
  await agentRound(totem.url, token);
  await expect(page.getByTestId('pc-state')).toContainText('Windows 11 Pro 24H2', { timeout: 20_000 });
  await expect(page.getByTestId('pc-power')).toContainText('batteria · 81%');
  await expect(page.getByTestId('ctl-issue')).toContainText('aggiornamenti di Windows in attesa di riavvio');

  // screenshot: in coda, l'agente lo esegue al giro successivo
  await page.getByTestId('pc-shot').click();
  let fatti = [];
  await expect.poll(async () => {
    const r = await agentRound(totem.url, token, fatti);
    fatti = r.fatti;
    return page.getByTestId('pc-shot-img').count();
  }, { timeout: 30_000, intervals: [1500] }).toBe(1);

  // riavvio del PC con conferma: arriva come comando
  await page.getByTestId('pc-act-riavvia-pc').click();
  await page.getByTestId('confirm-pc').click();
  await expect(page.getByTestId('toast')).toContainText('entro 30 secondi');
  const r = await agentRound(totem.url, token, fatti);
  expect(r.body.comandi.map((c) => c.azione)).toEqual(['riavvia-pc']);

  // in Dispositivi il riepilogo
  await page.getByTestId('back').click();
  await expect(page.getByTestId('device-pc')).toContainText('aggiornamenti di Windows');
});
