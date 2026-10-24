// Scenari di resilienza della specifica (sezione 10): perdita della share, del server,
// della rete, quota esaurita, PDF corrotto o parziale, salto di data di N giorni.
import { test, expect, pair, syncNow, localDocs, openDoc } from './harness.js';

async function ready(page, totem) {
  totem.addPdf('sds', 'Solventi/SDS_Acetone_rev3.pdf', 'sds', 'Acetone');
  totem.addPdf('sds', 'Diluente_nitro_rev1.pdf', 'sds', 'Diluente nitro');
  totem.addPdf('emerg', 'Piano_di_evacuazione_rev5.pdf', 'plain', 'Piano di evacuazione');
  await totem.waitManifest((m) => m.documents.length === 3);
  await pair(page, totem);
  await expect.poll(async () => (await localDocs(page)).filter((d) => d.cached).length, { timeout: 40_000 }).toBe(3);
  await expect(page.getByTestId('pill')).toHaveAttribute('data-kind', 'ok');
}

test('share persa: i documenti restano consultabili e lo Stato lo segnala', async ({ page, totem }) => {
  await ready(page, totem);
  totem.breakShare('emerg');
  await totem.kick();
  await expect(async () => {
    await syncNow(page);
    await page.getByTestId('pill').click();
    await expect(page.getByTestId('share-banner')).toContainText('«Piani di emergenza»', { timeout: 2000 });
  }).toPass({ timeout: 60_000 });
  await expect(page.getByTestId('share-banner')).toContainText("Viene mostrata l'ultima versione disponibile");
  await page.getByTestId('back').click();
  await expect(page.getByTestId('tile').filter({ hasText: 'Piani di emergenza' })).toContainText('1 documento');
  await openDoc(page, 'Piani di emergenza', 'Piano di evacuazione');
  expect((await localDocs(page)).length).toBe(3);
});

test('server spento: app e documenti funzionano, manutenzione bloccata', async ({ page, totem }) => {
  await ready(page, totem);
  await totem.stop();
  await page.reload(); // la shell arriva dal service worker
  await expect(page.getByTestId('home')).toBeVisible();
  await expect(page.getByTestId('pill')).toHaveAttribute('data-kind', 'offline');
  await expect(page.getByTestId('pill')).toContainText('Offline – documenti disponibili');
  await openDoc(page, 'Schede di sicurezza', 'Acetone');
  await expect(page.getByTestId('quick')).toBeVisible();
  await page.getByTestId('back').click();
  await page.getByTestId('back').click();
  await page.getByTestId('pill').click();
  await expect(page.getByTestId('status-main')).toContainText('Server non raggiungibile – il totem mostra i documenti già scaricati');
  await page.getByTestId('maint-open').click();
  await expect(page.getByTestId('pin-msg')).toContainText('Manutenzione disponibile solo con il server raggiungibile');
  await page.getByRole('button', { name: 'Annulla' }).click();
  // il server torna: la sync riprende
  await totem.start();
  await syncNow(page);
  await expect(page.getByTestId('pill')).toHaveAttribute('data-kind', 'ok');
});

test('rete persa: offline e ripresa automatica all\'evento online', async ({ page, context, totem }) => {
  await ready(page, totem);
  await context.setOffline(true);
  await page.reload();
  await expect(page.getByTestId('home')).toBeVisible();
  await expect(page.getByTestId('pill')).toHaveAttribute('data-kind', 'offline');
  await openDoc(page, 'Piani di emergenza', 'Piano di evacuazione');
  // un nuovo documento arriva mentre il totem è offline
  totem.addPdf('sds', 'Olio_da_taglio_rev4.pdf', 'sds', 'Olio da taglio');
  await totem.waitManifest((m) => m.documents.length === 4);
  await context.setOffline(false); // l'evento "online" fa partire la sync
  await expect.poll(async () => (await localDocs(page)).filter((d) => d.cached).length, { timeout: 40_000 }).toBe(4);
  await expect(page.getByTestId('pill')).toHaveAttribute('data-kind', 'ok');
});

test('quota esaurita: la sync si ferma, i documenti attuali restano', async ({ page, totem }) => {
  await ready(page, totem);
  const before = await localDocs(page);
  totem.addPdf('sds', 'Grande_rev1.pdf', 'sds', 'Prodotto grande');
  await totem.waitManifest((m) => m.documents.length === 4);
  // spazio quasi finito: la stima dice che il nuovo PDF non ci sta
  await page.addInitScript(() => {
    const real = navigator.storage.estimate.bind(navigator.storage);
    navigator.storage.estimate = async () => {
      const e = await real();
      return { ...e, quota: e.usage + 1024 * 1024 };
    };
  });
  await page.reload();
  await expect(page.getByTestId('home')).toBeVisible();
  await syncNow(page);
  await page.getByTestId('pill').click();
  await expect(page.getByTestId('space-banner')).toContainText('Spazio insufficiente sul totem');
  await expect(page.getByTestId('sync-log')).toContainText('interrotta: spazio insufficiente');
  const after = await localDocs(page);
  expect(after.map((d) => d.sha256).sort()).toEqual(before.map((d) => d.sha256).sort());
  expect(after.every((d) => d.cached)).toBe(true);
  // il nuovo documento si vede ma non è apribile
  await page.getByTestId('back').click();
  await page.getByTestId('tile').filter({ hasText: 'Schede di sicurezza' }).click();
  const row = page.getByTestId('doc-row').filter({ hasText: 'Grande' });
  await expect(row).toBeDisabled();
  await expect(row).toContainText('In download…');
});

test('PDF corrotto o parziale: resta la versione precedente', async ({ page, totem }) => {
  await ready(page, totem);
  const acetone = (await localDocs(page)).find((d) => d.title === 'SDS Acetone');
  const v = totem.manifest().version;
  // la nuova versione arriva troncata, e un PDF nuovo è parziale
  totem.addPdf('sds', 'Solventi/SDS_Acetone_rev3.pdf', 'truncated', 'Acetone');
  totem.addPdf('sds', 'Nuovo_parziale.pdf', 'truncated', 'Parziale');
  for (let i = 0; i < 4; i++) {
    await totem.kick();
    await page.waitForTimeout(1500);
  }
  expect(totem.log).toContain('PDF scartato');
  expect(totem.manifest().version).toBe(v);
  await syncNow(page);
  const docs = await localDocs(page);
  expect(docs.find((d) => d.title === 'SDS Acetone').sha256).toBe(acetone.sha256);
  expect(docs.find((d) => d.id.includes('Nuovo_parziale'))).toBeUndefined();
  await openDoc(page, 'Schede di sicurezza', 'Acetone');
  await expect(page.getByTestId('quick')).toBeVisible();
});

test('salto di data di 5 giorni: avviso di dati vecchi con testo e icona', async ({ page, totem }) => {
  await page.clock.install();
  await ready(page, totem);
  await totem.stop();
  await page.clock.fastForward(121 * 3600 * 1000);
  await expect(page.getByTestId('pill')).toHaveAttribute('data-kind', 'stale');
  await expect(page.getByTestId('pill')).toContainText('Ultimo aggiornamento 5 giorni fa');
  await page.getByTestId('pill').click();
  await expect(page.getByTestId('status-main')).toContainText("Ultimo aggiornamento riuscito 5 giorni fa – contattare l'ufficio IT");
  // i documenti restano consultabili
  await page.getByTestId('back').click();
  await openDoc(page, 'Schede di sicurezza', 'Diluente nitro');
});
