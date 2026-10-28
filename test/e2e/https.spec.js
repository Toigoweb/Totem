// Aperto in http://indirizzo:porta il browser non offre cache offline né service worker:
// il totem deve spiegarlo, non restare su una pagina bianca.
import { networkInterfaces } from 'node:os';
import { test, expect } from './harness.js';

function lanIP() {
  for (const list of Object.values(networkInterfaces())) {
    for (const a of list || []) if (a.family === 'IPv4' && !a.internal) return a.address;
  }
  return null;
}

test('in HTTP su un indirizzo IP compare la richiesta di usare HTTPS', async ({ page, totem }) => {
  const ip = lanIP();
  test.skip(!ip, 'nessun indirizzo di rete non locale');
  await totem.stop();
  totem.env = { TOTEM_ADDR: `0.0.0.0:${totem.port}` };
  await totem.start();
  const errors = [];
  page.on('pageerror', (e) => errors.push(e.message));
  await page.goto(`http://${ip}:${totem.port}/`);
  await expect(page.getByTestId('insecure')).toContainText('Serve l\'indirizzo HTTPS del totem');
  await expect(page.getByTestId('insecure')).toContainText(`${ip}:${totem.port}`);
  await page.waitForTimeout(1500); // nessun ciclo di ricarica
  expect(errors).toEqual([]);
  await expect(page.getByTestId('insecure')).toBeVisible();
});
