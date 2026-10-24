import { defineConfig } from 'vite';
import preact from '@preact/preset-vite';
import { VitePWA } from 'vite-plugin-pwa';

const version = process.env.VITE_APP_VERSION || 'dev';

export default defineConfig({
  define: { __APP_VERSION__: JSON.stringify(version) },
  plugins: [
    preact(),
    // Workbox solo per la shell dell'app: i PDF li gestisce la sync scritta a mano.
    VitePWA({
      strategies: 'injectManifest',
      srcDir: 'src',
      filename: 'sw.js',
      injectRegister: false,
      manifest: false,
      injectManifest: { globPatterns: ['**/*.{js,css,html,woff2,svg,png,webmanifest,mjs}'] },
    }),
  ],
  build: { target: 'es2020', assetsDir: 'assets', sourcemap: false },
});
