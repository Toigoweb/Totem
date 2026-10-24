import { execSync } from 'node:child_process';
import { ROOT } from './harness.js';

export default async function () {
  if (!process.env.TOTEM_KEEP_SAMBA) execSync('docker compose down', { cwd: `${ROOT}/test/e2e`, stdio: 'inherit' });
}
