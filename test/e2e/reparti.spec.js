// Numeri utili, avvisi, salvaschermo, reparti con più totem, monitoraggio.
import { test, expect, pair, syncNow, localDocs, PIN } from './harness.js';

async function ready(page, totem) {
  totem.addPdf('sds', 'Solventi/SDS_Acetone_rev3.pdf', 'sds', 'Acetone');
  totem.addPdf('emerg', 'Piano_di_evacuazione_rev5.pdf', 'plain', 'Piano di evacuazione');
  await totem.waitManifest((m) => m.documents.length === 2);
  await pair(page, totem);
  await expect.poll(async () => (await localDocs(page)).filter((d) => d.cached).length, { timeout: 40_000 }).toBe(2);
}

async function openMaint(page) {
  await page.getByTestId('pill').click();
  await page.getByTestId('maint-open').click();
  await page.keyboard.type(PIN);
  await page.keyboard.press('Enter');
  await expect(page.getByTestId('maint')).toBeVisible();
}

async function saveAndBack(page) {
  await page.getByTestId('save-extras').click();
  await expect(page.getByTestId('toast')).toContainText('Salvato');
  await page.getByTestId('back').click();
}

test('numeri utili e avvisi in Home, anche offline', async ({ page, context, totem }) => {
  await ready(page, totem);
  await openMaint(page);
  await page.getByTestId('open-contacts').click();
  await page.getByTestId('add-item').click();
  await page.fill('#cl-0', 'Emergenze');
  await page.fill('#cn-0', '112');
  await page.getByTestId('add-item').click();
  await page.fill('#cl-1', 'Addetto primo soccorso');
  await page.fill('#cn-1', 'abc');
  await page.getByTestId('save-extras').click();
  await expect(page.getByTestId('extras-err')).toContainText('Numero non valido');
  await page.fill('#cn-1', '3210');
  await saveAndBack(page);

  await page.getByTestId('open-notices').click();
  await page.getByTestId('add-item').click();
  await page.fill('#nt-0', 'Esercitazione antincendio giovedì alle 10');
  await page.getByRole('button', { name: 'Attenzione' }).click();
  await saveAndBack(page);
  await page.getByTestId('maint-exit').click();
  await syncNow(page);

  await expect(page.getByTestId('contacts')).toContainText('112');
  await expect(page.getByTestId('contacts')).toContainText('Addetto primo soccorso');
  await expect(page.getByTestId('notice')).toContainText('Attenzione: Esercitazione antincendio giovedì alle 10');

  await context.setOffline(true);
  await page.reload();
  await expect(page.getByTestId('contacts')).toContainText('3210');
  await expect(page.getByTestId('notice')).toBeVisible();
});

test('salvaschermo: compare dopo l\'inattività, il tocco lo chiude senza aprire nulla', async ({ page, totem }) => {
  await page.clock.install();
  await ready(page, totem);
  await openMaint(page);
  await page.getByTestId('open-contacts').click();
  await page.getByTestId('add-item').click();
  await page.fill('#cl-0', 'Emergenze');
  await page.fill('#cn-0', '112');
  await saveAndBack(page);
  await page.getByTestId('open-screensaver').click();
  await page.locator('#ss-on').click();
  await page.fill('#ss-min', '4');
  await saveAndBack(page);
  await page.getByTestId('maint-exit').click();
  await syncNow(page);

  await page.clock.fastForward('04:10');
  const ss = page.getByTestId('screensaver');
  await expect(ss).toBeVisible();
  await expect(ss).toHaveAttribute('data-mode', 'info');
  await expect(ss).toContainText('112');
  // il tocco cade sopra una tile, ma chiude solo il salvaschermo
  const tile = await page.getByTestId('tile').first().boundingBox();
  await page.mouse.click(tile.x + 20, tile.y + 20);
  await expect(ss).toHaveCount(0);
  await expect(page.getByTestId('home')).toBeVisible();
});

test('reparti: un secondo totem vede solo le sue sezioni; sostituzione del totem', async ({ page, browser, totem }) => {
  await ready(page, totem);
  await openMaint(page);
  await page.getByTestId('open-departments').click();
  await page.getByTestId('add-item').click();
  await page.fill('#dep-0', 'Magazzino');
  await page.getByTestId('extras-row').getByRole('button', { name: 'Piani di emergenza' }).click();
  await saveAndBack(page);

  await page.getByTestId('open-devices').click();
  await page.getByTestId('add-totem').click();
  await page.selectOption('#tot-rep', { label: 'Magazzino' });
  await page.getByTestId('gen-code').click();
  const code = await page.getByTestId('station-code').locator('[data-code]').getAttribute('data-code');

  const pairWith = async (c) => {
    const ctx = await browser.newContext({ viewport: { width: 1280, height: 800 } });
    const p = await ctx.newPage();
    await p.goto(totem.url);
    await p.getByTestId('start-setup').click();
    await p.getByTestId('setup-code').fill(c);
    await p.getByTestId('code-next').click();
    await expect(p.getByTestId('home')).toBeVisible({ timeout: 90_000 });
    return { ctx, p };
  };
  const t2 = await pairWith(code);
  await expect(t2.p.getByTestId('tile')).toHaveCount(1);
  await expect(t2.p.getByTestId('tile')).toContainText('Piani di emergenza');
  await expect(t2.p.locator('.topbar h1')).toContainText('Magazzino');
  expect((await localDocs(t2.p)).length).toBe(1);
  // il primo totem vede ancora tutto
  await page.getByTestId('maint-exit').click();
  await expect(page.getByTestId('tile')).toHaveCount(2);

  // sostituzione del totem del magazzino
  await openMaint(page);
  await page.getByTestId('open-devices').click();
  const row = page.getByTestId('device-row').filter({ hasText: 'totem Magazzino' });
  await expect(row).toHaveCount(1);
  await row.getByRole('button', { name: 'Sostituisci' }).click();
  await page.getByTestId('gen-code').click();
  const code2 = await page.getByTestId('station-code').locator('[data-code]').getAttribute('data-code');
  const t3 = await pairWith(code2);
  await expect(t3.p.getByTestId('tile')).toHaveCount(1);
  await t2.p.reload();
  await expect(t2.p.getByTestId('pill')).toHaveAttribute('data-kind', 'device');
  await t2.ctx.close();
  await t3.ctx.close();
});

test('monitoraggio: 200 se tutto va, 503 con una share non leggibile', async ({ page, request, totem }) => {
  await ready(page, totem);
  await openMaint(page);
  await page.getByTestId('open-monitoring').click();
  await expect(page.getByTestId('monitor-state')).toContainText('Monitoraggio non attivo');
  await page.getByTestId('monitor-gen').click();
  const token = (await page.getByTestId('monitor-token').locator('code.token').textContent()).trim();
  await expect(page.getByTestId('monitor-state')).toContainText('Monitoraggio attivo');

  const get = (q = '') => request.get(`${totem.url}/api/monitor${q}`, { headers: { Authorization: `Bearer ${token}` } });
  let r = await get();
  expect(r.status()).toBe(200);
  expect((await r.json()).ok).toBe(true);
  expect((await request.get(`${totem.url}/api/monitor`, { headers: { Authorization: 'Bearer sbagliato' } })).status()).toBe(401);

  totem.breakShare('emerg');
  await expect(async () => {
    await totem.kick();
    r = await get('?formato=testo');
    expect(r.status()).toBe(503);
  }).toPass({ timeout: 30_000 });
  const txt = await r.text();
  expect(txt.startsWith('TOTEM ERRORE')).toBe(true);
  expect(txt).toContain('Piani di emergenza');
});
