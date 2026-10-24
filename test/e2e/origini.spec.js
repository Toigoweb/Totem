// Origini dei PDF diverse dalla share SMB: caricamento diretto e OneDrive/SharePoint
// (Microsoft Graph simulato in locale).
import { createServer } from 'node:http';
import { readFileSync } from 'node:fs';
import { test, expect, pair, syncNow, PIN } from './harness.js';

async function openMaint(page) {
  await page.getByTestId('pill').click();
  await page.getByTestId('maint-open').click();
  await page.keyboard.type(PIN);
  await page.keyboard.press('Enter');
  await expect(page.getByTestId('maint')).toBeVisible();
}

async function newSection(page, nome, tipo) {
  await page.getByTestId('add-section').click();
  await page.fill('#f-nome', nome);
  await page.getByTestId('source-type').locator(`[data-tipo="${tipo}"]`).click();
}

test('caricamento diretto: i PDF caricati dal browser arrivano al totem', async ({ page, totem }) => {
  totem.addPdf('sds', 'SDS_Acetone_rev3.pdf', 'sds', 'Acetone');
  await totem.waitManifest((m) => m.documents.length === 1);
  await pair(page, totem);
  await openMaint(page);

  await newSection(page, 'Procedure', 'upload');
  await expect(page.getByTestId('upload-info')).toBeVisible();
  await expect(page.getByTestId('test-conn')).toHaveCount(0);
  await page.getByTestId('save').click();
  // una nuova sezione a caricamento diretto porta subito al caricamento
  await expect(page.getByTestId('uploads')).toBeVisible();
  await expect(page.getByTestId('uploads-empty')).toBeVisible();

  const a = totem.makePdf('Procedura carrelli elevatori.pdf', 'plain', 'Procedura carrelli elevatori');
  const b = totem.makePdf('Lavori in quota.pdf', 'plain', 'Lavori in quota');
  const rotto = totem.makePdf('Rotto.pdf', 'truncated', 'Rotto');
  await page.fill('#u-cartella', 'Magazzino');
  await page.getByTestId('upload-input').setInputFiles([a, rotto]);
  await expect(page.getByTestId('upload-err')).toContainText('Rotto.pdf');
  await expect(page.getByTestId('upload-err')).toContainText('non è un PDF valido');
  await page.fill('#u-cartella', '');
  await page.getByTestId('upload-input').setInputFiles([b]);
  await expect(page.getByTestId('upload-row')).toHaveCount(2);
  await expect(page.getByTestId('upload-row').filter({ hasText: 'Procedura carrelli' })).toContainText('Magazzino');

  // l'elenco delle sezioni lo indica come "Caricamento diretto"
  await page.getByTestId('back').click();
  await expect(page.getByTestId('maint-row').filter({ hasText: 'Procedure' })).toContainText('Caricamento diretto');
  await page.getByTestId('maint-exit').click();

  await totem.waitManifest((m) => m.documents.length === 3);
  await syncNow(page);
  await page.getByTestId('tile').filter({ hasText: 'Procedure' }).click();
  await expect(page.getByTestId('doc-row')).toHaveCount(2);
  await page.getByTestId('doc-row').filter({ hasText: 'Lavori in quota' }).click();
  await expect(page.getByTestId('page-of')).toContainText('Pagina 1 di 3', { timeout: 20_000 });
  await page.getByTestId('back').click();
  await page.getByTestId('back').click();

  // eliminazione: sparisce dal totem
  await openMaint(page);
  await page.getByTestId('open-uploads').click();
  await page.getByTestId('upload-row').filter({ hasText: 'Lavori in quota' }).getByRole('button').click();
  await page.getByTestId('confirm-delete').click();
  await expect(page.getByTestId('upload-row')).toHaveCount(1);
  await page.getByTestId('maint-exit').click();
  await totem.waitManifest((m) => m.documents.length === 2);
  await syncNow(page);
  await page.getByTestId('tile').filter({ hasText: 'Procedure' }).click();
  await expect(page.getByTestId('doc-row')).toHaveCount(1);
});

/** Microsoft Graph simulato: token, /shares, children, content (con rinvio). */
function fakeGraph(files) {
  const link = 'https://azienda.sharepoint.com/:f:/s/HSE/EabcDEF123';
  const share = 'u!' + Buffer.from(link).toString('base64url');
  const srv = createServer((req, res) => {
    const u = new URL(req.url, 'http://x');
    const json = (code, o) => {
      res.writeHead(code, { 'Content-Type': 'application/json' });
      res.end(JSON.stringify(o));
    };
    if (req.method === 'POST' && u.pathname === '/login/azienda.onmicrosoft.com/oauth2/v2.0/token') {
      let body = '';
      req.on('data', (c) => (body += c));
      req.on('end', () => {
        const f = new URLSearchParams(body);
        if (f.get('client_secret') !== 'segreto-giusto') return json(401, { error: 'invalid_client' });
        json(200, { access_token: 'tok', expires_in: 3600 });
      });
      return;
    }
    if (u.pathname.startsWith('/login/')) return json(400, { error: 'invalid_tenant' });
    // il download (dopo il rinvio) non richiede il token
    const dl = u.pathname.match(/^\/download\/(\d+)$/);
    if (dl) {
      res.writeHead(200, { 'Content-Type': 'application/pdf' });
      return res.end(readFileSync(files[Number(dl[1])].path));
    }
    if (req.headers.authorization !== 'Bearer tok') return json(401, {});
    if (u.pathname === `/v1.0/shares/${share}/driveItem`) return json(200, { id: 'root', name: 'HSE', folder: {}, parentReference: { driveId: 'drv' } });
    if (u.pathname.startsWith('/v1.0/shares/')) return json(404, {});
    if (u.pathname === '/v1.0/drives/drv/items/root/children') {
      return json(200, { value: files.map((f, i) => ({ id: `f${i}`, name: f.name, size: readFileSync(f.path).length, lastModifiedDateTime: '2026-10-20T10:00:00Z', file: {} })) });
    }
    const m = u.pathname.match(/^\/v1\.0\/drives\/drv\/items\/f(\d+)\/content$/);
    if (m) {
      res.writeHead(302, { Location: `/download/${m[1]}` });
      return res.end();
    }
    json(404, {});
  });
  return { srv, link };
}

test('OneDrive / SharePoint: prova connessione con messaggi chiari, poi i PDF arrivano', async ({ page, totem }) => {
  const files = [{ name: 'Piano evacuazione HSE.pdf', path: totem.makePdf('od1.pdf', 'plain', 'Piano evacuazione HSE') }];
  const { srv, link } = fakeGraph(files);
  await new Promise((r) => srv.listen(0, '127.0.0.1', r));
  const base = `http://127.0.0.1:${srv.address().port}`;
  try {
    await totem.stop();
    totem.env = { TOTEM_GRAPH_URL: `${base}/v1.0`, TOTEM_LOGIN_URL: `${base}/login` };
    await totem.start();
    await pair(page, totem);
    await openMaint(page);
    await newSection(page, 'Sicurezza HSE', 'onedrive');
    await page.fill('#f-percorso', 'https://example.com/cartella');
    await expect(page.getByTestId('path-err')).toContainText('sharepoint.com');
    await page.fill('#f-percorso', link);
    await page.fill('#f-tenant', 'azienda.onmicrosoft.com');
    await page.fill('#f-utente', '1b2c3d4e-0000-1111-2222-333344445555');
    await page.fill('#f-password', 'segreto-sbagliato');
    await page.getByTestId('test-conn').click();
    await expect(page.getByTestId('test-err')).toContainText('Credenziali dell\'app non valide');
    await expect(page.getByTestId('save')).toBeDisabled();
    await page.fill('#f-password', 'segreto-giusto');
    await page.getByTestId('test-conn').click();
    await expect(page.getByTestId('test-ok')).toContainText('trovati 1 PDF');
    await page.getByTestId('save').click();
    await expect(page.getByTestId('maint-row').filter({ hasText: 'Sicurezza HSE' })).toContainText('OneDrive / SharePoint');
    await page.getByTestId('maint-exit').click();

    await totem.waitManifest((m) => m.documents.some((d) => d.title === 'Piano evacuazione HSE' || d.path === 'Piano evacuazione HSE.pdf'));
    await syncNow(page);
    await page.getByTestId('tile').filter({ hasText: 'Sicurezza HSE' }).click();
    await page.getByTestId('doc-row').filter({ hasText: 'Piano evacuazione HSE' }).click();
    await expect(page.getByTestId('page-of')).toContainText('Pagina 1 di 3', { timeout: 20_000 });
    // il segreto non finisce mai nei log
    expect(totem.log).not.toContain('segreto-giusto');
  } finally {
    srv.close();
  }
});
