import { execSync } from 'node:child_process';
import { mkdirSync, rmSync } from 'node:fs';
import net from 'node:net';
import { WORK, ROOT, BIN, SMB_PORT } from './harness.js';

function waitPort(port, ms) {
  const until = Date.now() + ms;
  return new Promise((resolve, reject) => {
    const tryOnce = () => {
      const s = net.connect(port, '127.0.0.1');
      s.on('connect', () => { s.destroy(); resolve(); });
      s.on('error', () => (Date.now() > until ? reject(new Error(`porta ${port} chiusa`)) : setTimeout(tryOnce, 500)));
    };
    tryOnce();
  });
}

export default async function () {
  // dati dei test precedenti via; il binario resta (serve con TOTEM_SKIP_BUILD)
  for (const d of ['run', 'share']) rmSync(`${WORK}/${d}`, { recursive: true, force: true });
  mkdirSync(`${WORK}/share`, { recursive: true });
  const sh = (cmd, cwd = ROOT) => execSync(cmd, { cwd, stdio: 'inherit' });
  if (!process.env.TOTEM_SKIP_BUILD) {
    sh('npm run build', `${ROOT}/web`);
    sh(`go build -o ${BIN} ./cmd/totem`);
  }
  sh('docker compose up -d --force-recreate', `${ROOT}/test/e2e`);
  await waitPort(SMB_PORT, 60_000);
  await new Promise((r) => setTimeout(r, 3000)); // smbd pronto dopo l'apertura della porta
}
