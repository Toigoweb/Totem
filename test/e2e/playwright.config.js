import { defineConfig, devices } from '@playwright/test';

// Test end-to-end della PWA contro il server Go vero e un Samba in Docker.
// Prerequisiti: Go, Node, Docker. La build della PWA e del binario la fa global-setup.
export default defineConfig({
  testDir: '.',
  testMatch: /.*\.spec\.js/,
  timeout: 120_000,
  expect: { timeout: 20_000 },
  workers: 1, // un solo Samba condiviso e scenari che fermano il server
  retries: 0,
  reporter: [['list']],
  globalSetup: './global-setup.js',
  globalTeardown: './global-teardown.js',
  use: { trace: 'retain-on-failure', screenshot: 'only-on-failure' },
  projects: [
    { name: 'desktop', use: { ...devices['Desktop Chrome'], viewport: { width: 1280, height: 800 } } },
    { name: 'tablet', use: { ...devices['Galaxy Tab S9 landscape'] } },
  ],
});
