// Postazioni (altri PC con accesso alla manutenzione), elenco dei dispositivi e
// documenti scartati.
import { test, expect, pair, syncNow, localDocs, PIN } from './harness.js';

async function ready(page, totem) {
  totem.addPdf('sds', 'Solventi/SDS_Acetone_rev3.pdf', 'sds', 'Acetone');
  totem.addPdf('emerg', 'Piano_di_evacuazione_rev5.pdf', 'plain', 'Piano di evacuazione');
  await totem.waitManifest((m) => m.documents.length === 2);
  await pair(page, totem);
  await expect.poll(async () => (await localDocs(page)).filter((d) => d.cached).length, { timeout: 40_000 }).toBe(2);
}

async function typePin(page, pin) {
  for (const d of pin) await page.getByTestId('pinpad').getByRole('button', { name: d, exact: true }).click();
  await page.getByTestId('pin-ok').click();
}

async function openMaint(page) {
  await page.getByTestId('pill').click();
  await page.getByTestId('maint-open').click();
  await typePin(page, PIN);
  await expect(page.getByTestId('maint')).toBeVisible();
}

/** Procedura guidata su un altro browser con un codice di postazione. */
async function pairStation(browser, totem, code) {
  const ctx = await browser.newContext({ viewport: { width: 1280, height: 800 } });
  const p = await ctx.newPage();
  await p.goto(totem.url);
  await p.getByTestId('start-setup').click();
  await p.getByTestId('setup-code').fill(code);
  await p.getByTestId('code-next').click();
  await expect(p.getByTestId('steps')).toContainText('Passo 2 di 2');
  await expect(p.getByTestId('home')).toBeVisible({ timeout: 90_000 });
  return { ctx, page: p };
}

test('postazione abbinata dalla manutenzione: il totem resta abbinato', async ({ page, browser, totem }) => {
  await ready(page, totem);
  await openMaint(page);
  await page.getByTestId('open-devices').click();
  await expect(page.getByTestId('device-row')).toHaveCount(1);
  await page.getByRole('button', { name: "Abbina un'altra postazione" }).click();
  await page.fill('#st-nome', 'PC ufficio IT');
  await page.getByTestId('gen-code').click();
  const code = await page.getByTestId('station-code').locator('[data-code]').getAttribute('data-code');
  expect(code).toMatch(/^[A-Z0-9]{8}$/);

  const st = await pairStation(browser, totem, code);
  await expect(st.page.getByTestId('station-badge')).toHaveText('POSTAZIONE');
  expect((await localDocs(st.page)).filter((d) => d.cached).length).toBe(2);

  // il totem continua ad aggiornarsi
  await page.getByTestId('maint-exit').click();
  await syncNow(page);
  await expect(page.getByTestId('pill')).toHaveAttribute('data-kind', 'ok');

  // la postazione entra in manutenzione con il PIN e vede i dispositivi
  await openMaint(st.page);
  await st.page.getByTestId('open-devices').click();
  await expect(st.page.getByTestId('device-row')).toHaveCount(2);
  await expect(st.page.getByTestId('device-row').filter({ hasText: 'PC ufficio IT' })).toContainText('Questo dispositivo');
  await expect(st.page.getByTestId('device-row').filter({ hasText: 'Totem' })).toContainText('reset-pairing');

  // il totem revoca la postazione
  await openMaint(page);
  await page.getByTestId('open-devices').click();
  await page.getByTestId('device-row').filter({ hasText: 'PC ufficio IT' }).getByRole('button', { name: 'Revoca' }).click();
  await page.getByTestId('confirm-revoke').click();
  await expect(page.getByTestId('device-row')).toHaveCount(1);
  await st.page.getByTestId('maint-exit').click();
  await st.page.reload();
  await expect(st.page.getByTestId('pill')).toHaveAttribute('data-kind', 'device');
  await st.ctx.close();
});

test('postazione con il codice generato sul server', async ({ page, browser, totem }) => {
  await ready(page, totem);
  const code = await totem.stationCode('Prove');
  const st = await pairStation(browser, totem, code.toLowerCase());
  await expect(st.page.getByTestId('station-badge')).toBeVisible();
  // un codice sbagliato, con il totem già abbinato, spiega cosa serve
  const p3 = await st.ctx.browser().newContext().then((c) => c.newPage());
  await p3.goto(totem.url);
  await p3.getByTestId('start-setup').click();
  await p3.getByTestId('setup-code').fill('AAAAAAAA');
  await p3.getByTestId('code-next').click();
  await expect(p3.getByRole('alert')).toContainText('qui serve il codice di una postazione');
  await p3.context().close();
  await st.ctx.close();
});

test('documenti scartati visibili in manutenzione', async ({ page, totem }) => {
  await ready(page, totem);
  totem.addPdf('sds', 'Rotto_rev1.pdf', 'truncated', 'Rotto');
  for (let i = 0; i < 4; i++) {
    await totem.kick();
    await page.waitForTimeout(1500);
  }
  await openMaint(page);
  await page.getByTestId('open-problems').click();
  const row = page.getByTestId('problems-table').getByRole('row').filter({ hasText: 'Rotto_rev1.pdf' });
  await expect(row).toContainText('Schede di sicurezza');
  await expect(row).toContainText('PDF danneggiato o incompleto');
  await expect(row).toContainText('Non è sul totem');
});
