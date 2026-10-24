// Comportamenti della sezione 8 e interazioni principali dell'interfaccia.
import { test, expect, pair, syncNow, localDocs, openDoc, PIN } from './harness.js';

async function ready(page, totem) {
  totem.addPdf('sds', 'Solventi/SDS_Acetone_rev3.pdf', 'sds', 'Acetone');
  totem.addPdf('sds', 'Diluente_nitro_rev1.pdf', 'sds', 'Diluente nitro');
  totem.addPdf('emerg', 'Piano_di_evacuazione_rev5.pdf', 'plain', 'Piano di evacuazione');
  await totem.waitManifest((m) => m.documents.length === 3);
  await pair(page, totem);
  await expect.poll(async () => (await localDocs(page)).filter((d) => d.cached).length, { timeout: 40_000 }).toBe(3);
}

async function typePin(page, pin) {
  for (const d of pin) await page.getByTestId('pinpad').getByRole('button', { name: d, exact: true }).click();
  await page.getByTestId('pin-ok').click();
}

test('fascia emergenza: con un solo documento il pulsante lo apre direttamente', async ({ page, totem }) => {
  await ready(page, totem);
  await page.getByTestId('emergency').getByRole('button', { name: 'Piani di emergenza' }).click();
  await expect(page.getByTestId('reader')).toBeVisible();
  await expect(page.getByTestId('page-of')).toContainText('Pagina 1 di 3');
});

test('lettura SDS: accesso rapido, sezioni e cambio pagina', async ({ page, totem }) => {
  await ready(page, totem);
  await openDoc(page, 'Schede di sicurezza', 'Acetone');
  await page.getByRole('button', { name: 'ANTINCENDIO', exact: true }).click();
  await expect(page.getByTestId('page-of')).toContainText('Pagina 2 di 6');
  await page.getByRole('button', { name: '8 DPI' }).click();
  await expect(page.getByTestId('page-of')).toContainText('Pagina 3 di 6');
  await page.getByRole('button', { name: 'Pagina successiva' }).click();
  await expect(page.getByTestId('page-of')).toContainText('Pagina 4 di 6');
  await page.getByRole('button', { name: 'Ingrandisci' }).click();
  await expect(page.getByTestId('viewer')).toHaveAttribute('data-zoom', '1.25');
  await page.getByRole('button', { name: 'Adatta' }).click();
  await expect(page.getByTestId('viewer')).toHaveAttribute('data-zoom', '1.00');
});

test('ricerca locale con risultati raggruppati e nessun risultato', async ({ page, totem }) => {
  await ready(page, totem);
  await page.getByTestId('search-open').click();
  await page.getByTestId('search-input').fill('evacu');
  await expect(page.getByTestId('result')).toHaveCount(1);
  await expect(page.getByTestId('search').locator('mark')).toHaveText(/evacu/i);
  await page.getByTestId('search-input').fill('inesistente xyz');
  await expect(page.getByText('Nessun documento trovato')).toBeVisible();
  await page.getByRole('button', { name: 'Chiudi' }).click();
  await expect(page.getByTestId('home')).toBeVisible();
});

test('sezione eliminata: i suoi PDF spariscono alla sync successiva', async ({ page, totem }) => {
  await ready(page, totem);
  await page.getByTestId('pill').click();
  await page.getByTestId('maint-open').click();
  await typePin(page, PIN);
  await expect(page.getByTestId('maint')).toBeVisible();
  await page.getByTestId('maint-row').filter({ hasText: 'Piani di emergenza' }).getByRole('button', { name: 'Elimina' }).click();
  await expect(page.getByRole('dialog')).toContainText('Le cartelle di rete non vengono toccate');
  await page.getByTestId('confirm-delete').click();
  await expect(page.getByTestId('maint-row')).toHaveCount(1);
  await page.getByTestId('maint-exit').click();
  await totem.waitManifest((m) => m.sections.length === 1 && m.documents.length === 2);
  await syncNow(page);
  await expect(page.getByTestId('emergency')).toHaveCount(0);
  await expect(page.getByTestId('tile')).toHaveCount(1);
  expect((await localDocs(page)).length).toBe(2);
});

test('nuova versione di un documento: sostituita solo a download completato', async ({ page, totem }) => {
  await ready(page, totem);
  const old = (await localDocs(page)).find((d) => d.title === 'Diluente nitro');
  totem.addPdf('sds', 'Diluente_nitro_rev1.pdf', 'sds', 'Diluente nitro versione 2');
  await totem.waitManifest((m) => m.documents.some((d) => d.title === 'Diluente nitro' && d.sha256 !== old.sha256));
  await syncNow(page);
  const now = (await localDocs(page)).find((d) => d.title === 'Diluente nitro');
  expect(now.sha256).not.toBe(old.sha256);
  expect(now.cached).toBe(true);
  const oldCached = await page.evaluate(async (sha) => !!(await (await caches.open('totem-pdf-v1')).match(`/api/files/${sha}`)), old.sha256);
  expect(oldCached).toBe(false);
  await page.getByTestId('pill').click();
  await expect(page.getByTestId('sync-log')).toContainText('1 aggiornato');
});

test('credenziali errate nel form: messaggio per causa, Salva disattivato', async ({ page, totem }) => {
  await ready(page, totem);
  await page.getByTestId('pill').click();
  await page.getByTestId('maint-open').click();
  await typePin(page, '1111');
  await expect(page.getByTestId('pin-msg')).toContainText('PIN errato – restano 4 tentativi');
  await typePin(page, PIN);
  await page.getByTestId('add-section').click();
  await page.fill('#f-nome', 'Planimetrie');
  await page.fill('#f-percorso', 'server/cartella');
  await expect(page.getByTestId('path-err')).toBeVisible();
  await page.fill('#f-percorso', `\\\\127.0.0.1\\e2e\\${totem.id}\\sds`);
  await page.fill('#f-utente', 'totem');
  await page.fill('#f-password', 'sbagliata');
  await page.getByTestId('test-conn').click();
  await expect(page.getByTestId('test-err')).toContainText('Utente o password errati');
  await expect(page.getByTestId('save')).toBeDisabled();
  await page.fill('#f-percorso', `\\\\127.0.0.1\\e2e\\${totem.id}\\manca`);
  await page.fill('#f-password', 'Giusta-123');
  await page.getByTestId('test-conn').click();
  await expect(page.getByTestId('test-err')).toContainText('Cartella non trovata: controlla il percorso');
  await page.fill('#f-percorso', `\\\\127.0.0.1\\e2e\\${totem.id}\\sds`);
  await page.getByTestId('test-conn').click();
  await expect(page.getByTestId('test-ok')).toContainText('Connessione riuscita: trovati 2 PDF');
  await expect(page.getByTestId('save')).toBeEnabled();
  // cambiare la password dopo la prova riporta Salva disattivato
  await page.fill('#f-password', 'altra');
  await expect(page.getByTestId('save')).toBeDisabled();
});

test('token revocato e sostituzione del totem', async ({ page, browser, totem }) => {
  await ready(page, totem);
  const code = await totem.resetPairing();
  await page.reload();
  await expect(page.getByTestId('pill')).toHaveAttribute('data-kind', 'device');
  await openDoc(page, 'Piani di emergenza', 'Piano di evacuazione'); // i documenti restano leggibili

  // nuovo totem: procedura ridotta (codice, PIN attuale, prima sincronizzazione)
  const ctx = await browser.newContext({ viewport: { width: 1280, height: 800 } });
  const p2 = await ctx.newPage();
  await p2.goto(totem.url);
  await p2.getByTestId('start-setup').click();
  await p2.getByTestId('setup-code').fill(code.toLowerCase());
  await p2.getByTestId('code-next').click();
  await expect(p2.getByTestId('pinpad')).toContainText('Inserisci il PIN di manutenzione attuale');
  await expect(p2.getByTestId('pinpad')).toContainText('Passo 2 di 3');
  await p2.keyboard.type(PIN);
  await p2.keyboard.press('Enter');
  await expect(p2.getByTestId('wizard-sync')).toBeVisible();
  await expect(p2.getByTestId('home')).toBeVisible({ timeout: 90_000 });
  await expect(p2.getByTestId('tile')).toHaveCount(2);
  expect((await localDocs(p2)).filter((d) => d.cached).length).toBe(3);
  await ctx.close();
});

test('inattività: dalla lettura si torna alla Home e la manutenzione si chiude', async ({ page, totem }) => {
  await page.clock.install();
  await ready(page, totem);
  await openDoc(page, 'Schede di sicurezza', 'Acetone');
  await page.clock.fastForward('03:10');
  await expect(page.getByTestId('home')).toBeVisible();
  await page.getByTestId('pill').click();
  await page.getByTestId('maint-open').click();
  await typePin(page, PIN);
  await expect(page.getByTestId('maint')).toBeVisible();
  await page.clock.fastForward('03:10');
  await expect(page.getByTestId('home')).toBeVisible();
  await expect(page.getByTestId('maint')).toHaveCount(0);
});

test('pressione lunga sul logo con il mouse apre il PIN', async ({ page, totem }, testInfo) => {
  test.skip(testInfo.project.name !== 'desktop', 'mouse solo su desktop');
  await ready(page, totem);
  const box = await page.getByTestId('logo').boundingBox();
  await page.mouse.move(box.x + 10, box.y + 10);
  await page.mouse.down();
  await page.waitForTimeout(800);
  await page.mouse.up(); // troppo breve: niente
  await expect(page.getByTestId('pinpad')).toHaveCount(0);
  await page.mouse.down();
  await page.waitForTimeout(2200);
  await page.mouse.up();
  await expect(page.getByTestId('pinpad')).toBeVisible();
  await page.keyboard.press('Escape');
  await expect(page.getByTestId('home')).toBeVisible();
});

test('A+: 100%, 125%, 150% e ricordata', async ({ page, totem }) => {
  await ready(page, totem);
  const scale = () => page.evaluate(() => document.documentElement.dataset.scale);
  await page.getByTestId('scale').click();
  await expect.poll(scale).toBe('125');
  await page.getByTestId('scale').click();
  await expect.poll(scale).toBe('150');
  await page.reload();
  await expect.poll(scale).toBe('150');
  await page.getByTestId('scale').click();
  await expect.poll(scale).toBe('100');
});
