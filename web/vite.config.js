import { defineConfig } from 'vite';
import preact from '@preact/preset-vite';
import { VitePWA } from 'vite-plugin-pwa';
import { cpSync, writeFileSync } from 'node:fs';

const version = process.env.VITE_APP_VERSION || 'dev';

export default defineConfig({
  define: { __APP_VERSION__: JSON.stringify(version) },
  plugins: [
    preact(),
    // web/dist/.keep è nel repository: senza, go:embed fallisce su un checkout pulito.
    { name: 'keep-dist', closeBundle() { writeFileSync('dist/.keep', ''); } },
    // risorse di pdf.js self-hosted (font standard, cmaps, decoder wasm, profili ICC)
    {
      name: 'pdfjs-assets',
      writeBundle() {
        // esclusi: fallback senza wasm (la CSP ammette il wasm) e QuickJS (niente script nei PDF)
        // esclusi: fallback senza wasm (la CSP ammette il wasm) e QuickJS (niente script nei PDF)
        for (const d of ['cmaps', 'standard_fonts', 'wasm', 'iccs']) {
          cpSync(`node_modules/pdfjs-dist/${d}`, `dist/pdfjs/${d}`, { recursive: true, filter: (f) => !/LICENSE|\.md$|nowasm_fallback|quickjs/.test(f) });
        }
      },
    },
    // Workbox solo per la shell dell'app: i PDF li gestisce la sync scritta a mano.
    VitePWA({
      strategies: 'injectManifest',
      srcDir: 'src',
      filename: 'sw.js',
      injectRegister: false,
      manifest: false,
      injectManifest: {
        globPatterns: ['**/*.{js,css,html,woff2,svg,png,webmanifest,mjs,bcmap,pfb,ttf,wasm,icc}'],
        maximumFileSizeToCacheInBytes: 5 * 1024 * 1024,
      },
    }),
  ],
  build: { target: 'es2020', assetsDir: 'assets', sourcemap: false },
  test: { environment: 'node' },
});
