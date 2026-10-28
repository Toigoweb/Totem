// Interfono: spento invisibile; acceso, chiamata vera (audio finto di Chromium) tra totem e
// postazione, rifiuto con numero di ripiego, telefono abbinato con il codice del QR.
import { devices } from '@playwright/test';
import { test, expect, pair, PIN } from './harness.js';

test.use({ launchOptions: { args: ['--use-fake-ui-for-media-stream', '--use-fake-device-for-media-stream'] } });

async function openMaint(page) {
  await page.getByTestId('pill').click();
  await page.getByTestId('maint-open').click();
  await page.keyboard.type(PIN);
  await page.keyboard.press('Enter');
  await expect(page.getByTestId('maint')).toBeVisible();
}

async function pairWithCode(browser, totem, code, opts = {}) {
  const ctx = await browser.newContext({ viewport: { width: 1280, height: 800 }, ...opts });
  await ctx.grantPermissions(['microphone'], { origin: totem.url });
  const p = await ctx.newPage();
  await p.goto(totem.url);
  await p.getByTestId('start-setup').click();
  await p.getByTestId('setup-code').fill(code);
  await p.getByTestId('code-next').click();
  return { ctx, page: p };
}

test('interfono: invisibile da spento, chiamata tra totem e postazione, telefono con QR', async ({ page, context, browser, totem }) => {
  test.skip(test.info().project.name !== 'desktop', 'due browser e WebRTC: basta il profilo desktop');
  test.setTimeout(240_000);
  const icRequests = [];
  page.on('request', (r) => r.url().includes('/api/intercom/') && icRequests.push(r.url()));
  await context.grantPermissions(['microphone'], { origin: totem.url });
  totem.addPdf('emerg', 'Piano_di_evacuazione_rev5.pdf', 'plain', 'Piano di evacuazione');
  await totem.waitManifest((m) => m.documents.length === 1);
  await pair(page, totem);
  const st = await pairWithCode(browser, totem, await totem.stationCode('Portineria'));
  await expect(st.page.getByTestId('home')).toBeVisible({ timeout: 90_000 });

  // spento: nessun pulsante e nessuna richiesta all'interfono
  await expect(page.getByTestId('grid')).toBeVisible();
  await expect(page.getByTestId('intercom-open')).toHaveCount(0);
  expect(icRequests).toEqual([]);

  // acceso in manutenzione: generale, sui due dispositivi, un ruolo con la postazione
  await openMaint(page);
  await page.getByTestId('open-intercom-admin').click();
  await page.locator('#ic-on').click();
  const rows = page.getByTestId('ic-admin-device');
  await expect(rows).toHaveCount(2);
  for (const name of ['totem', 'Portineria']) {
    await rows.filter({ hasText: name }).getByRole('switch').click();
    await expect(rows.filter({ hasText: name }).getByLabel('Numero se non risponde')).toBeVisible();
  }
  await rows.filter({ hasText: 'Portineria' }).getByLabel('Numero se non risponde').fill('3210');
  await rows.filter({ hasText: 'Portineria' }).getByLabel('Numero se non risponde').blur();
  await page.getByTestId('ic-group-add').click();
  await page.getByLabel('Nome del ruolo').fill('Portineria di turno');
  await page.getByTestId('ic-admin-group').getByRole('button', { name: 'Portineria', exact: true }).click();
  await page.getByTestId('ic-save').click();
  await expect(page.getByTestId('toast')).toContainText('Impostazioni dell\'interfono salvate');
  await page.getByTestId('maint-exit').click();

  // il totem riprende il manifest da sé; la postazione al ricaricamento
  await expect(page.getByTestId('intercom-open')).toBeVisible({ timeout: 20_000 });
  await st.page.reload();
  await expect(st.page.getByTestId('intercom-open')).toBeVisible({ timeout: 20_000 });

  // chiamata al ruolo: squilla sulla postazione, risponde, l'audio si collega
  await page.getByTestId('intercom-open').click();
  const group = page.getByTestId('ic-group').filter({ hasText: 'Portineria di turno' });
  await expect(group).toContainText('1 disponibile ora');
  await group.click();
  await expect(page.getByTestId('call')).toHaveAttribute('data-phase', 'ringing');
  await expect(st.page.getByTestId('call')).toHaveAttribute('data-dir', 'in');
  await expect(st.page.getByTestId('call-name')).toContainText('totem');
  await st.page.getByTestId('call-answer').click();
  await expect(page.getByTestId('call')).toHaveAttribute('data-phase', 'active', { timeout: 20_000 });
  await expect(st.page.getByTestId('call')).toHaveAttribute('data-phase', 'active', { timeout: 20_000 });
  await st.page.getByTestId('call-mute').click();
  await expect(st.page.getByTestId('call-mute')).toHaveAttribute('aria-pressed', 'true');
  await page.getByTestId('call-hangup').click();
  await expect(st.page.getByTestId('call-ended')).toContainText('Chiamata terminata');
  await page.getByTestId('call-close').click();
  await expect(page.getByTestId('call')).toHaveCount(0);

  // il totem chiama la postazione, che rifiuta: compare il numero da chiamare
  await page.getByTestId('ic-device').filter({ hasText: 'Portineria' }).click();
  await st.page.getByTestId('call-reject').click();
  await expect(page.getByTestId('call-ended')).toContainText('Chiamata rifiutata');
  await expect(page.getByTestId('call-fallback')).toContainText('3210');
  await page.getByTestId('call-close').click();

  // telefono: codice dalla manutenzione, il QR apre l'indirizzo con il codice compilato
  await page.getByTestId('back').click();
  await openMaint(page);
  await page.getByTestId('open-intercom-admin').click();
  await page.getByLabel('Nome del telefono').fill('Telefono primo soccorso');
  await page.getByTestId('ic-phone-code').click();
  await expect(page.getByTestId('qr')).toBeVisible();
  const code = (await page.getByTestId('ic-code').textContent()).replace('-', '');
  const phoneCtx = await browser.newContext({ ...devices['Pixel 7'] });
  await phoneCtx.grantPermissions(['microphone'], { origin: totem.url });
  const phone = await phoneCtx.newPage();
  await phone.goto(`${totem.url}/?abbina=${code}`);
  await expect(phone.getByTestId('setup-code')).toHaveValue(`${code.slice(0, 4)}-${code.slice(4)}`);
  await phone.getByTestId('code-next').click();
  await expect(phone.getByTestId('phone-home')).toBeVisible({ timeout: 20_000 });
  await expect(phone.getByRole('switch')).toHaveAttribute('aria-checked', 'true');
  await expect(phone.getByTestId('ic-device').filter({ hasText: 'totem' })).toBeVisible();

  // il telefono (app aperta) riceve la chiamata del totem
  await page.getByTestId('maint-exit').click();
  await page.getByTestId('intercom-open').click();
  await page.getByTestId('ic-phone').filter({ hasText: 'Telefono primo soccorso' }).click();
  await expect(phone.getByTestId('call')).toHaveAttribute('data-dir', 'in', { timeout: 10_000 });
  await phone.getByTestId('call-answer').click();
  await expect(page.getByTestId('call')).toHaveAttribute('data-phase', 'active', { timeout: 20_000 });
  await phone.getByTestId('call-hangup').click();
  await expect(page.getByTestId('call-ended')).toBeVisible();

  // non disponibile: il totem lo vede non raggiungibile
  await page.getByTestId('call-close').click();
  await phone.getByTestId('call-close').click();
  await phone.getByRole('switch').click();
  await expect(page.getByTestId('ic-phone').filter({ hasText: 'Telefono primo soccorso' })).toContainText('Non raggiungibile', { timeout: 10_000 });

  // spento di nuovo: il pulsante sparisce dal totem
  await page.getByTestId('back').click();
  await openMaint(page);
  await page.getByTestId('open-intercom-admin').click();
  await page.locator('#ic-on').click();
  await page.getByTestId('maint-exit').click();
  await expect(page.getByTestId('intercom-open')).toHaveCount(0, { timeout: 20_000 });
  await phoneCtx.close();
  await st.ctx.close();
});
