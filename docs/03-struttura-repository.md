# 3. Struttura del repository

```
.
├── cmd/totem/main.go            entrypoint: serve (default) | healthcheck | reset-pairing [--reset-pin]
├── internal/
│   ├── config/                  config.json: load, migrazioni con backup, salvataggio atomico, AES-GCM
│   ├── smbsrc/                  sorgente SMB (go-smb2): parsing UNC, elenco PDF, apertura, classificazione errori
│   ├── scanner/                 scansione, stabilità, validazione PDF, blob store, manifest, GC
│   ├── auth/                    PIN argon2id, blocco tentativi, codice di setup, token dispositivo e manutenzione
│   ├── api/                     handler HTTP pubblici, dispositivo, manutenzione, listener interno
│   ├── synclog/                 log JSON con rotazione, senza segreti
│   └── version/                 versione iniettata con -ldflags
├── web/                         PWA (Preact + Vite)
│   ├── embed.go                 //go:embed all:dist
│   ├── index.html
│   ├── public/                  icone, app.webmanifest
│   └── src/
│       ├── strings.js           unico file di stringhe (italiano)
│       ├── sync/                manifest, download, verifica SHA-256, commit IndexedDB, spazio
│       ├── db/                  IndexedDB (indice, mappa sezioni SDS, stato)
│       ├── search/              MiniSearch
│       ├── viewer/              pdf.js (worker self-hosted)
│       ├── kiosk/               inattività, errori globali, aggiornamenti notturni
│       ├── screens/             Home, Sezione, Lettura, Stato, PIN, Manutenzione, Form, Wizard
│       └── sw.js                service worker (Workbox injectManifest, solo shell)
├── wireframes/index.html        wireframe statici navigabili (da approvare)
├── test/
│   ├── integration/             test Go con tag `integration` contro Samba
│   ├── fixtures/                PDF di prova (validi, troncati, SDS con 16 sezioni)
│   └── e2e/                     Playwright: desktop Chromium + mobile emulato
├── docs/                        architettura, formati, messa in servizio
├── Dockerfile                   node → go → distroless/static:nonroot
├── docker-compose.yml           l'unico compose di produzione
├── docker-compose.test.yml      Samba + totem per i test di integrazione
├── .github/workflows/
│   ├── ci.yml                   push/PR: go vet, go test, npm test, build
│   └── release.yml              tag vX.Y.Z → GHCR :X.Y.Z :X.Y :X
└── README.md
```

## GitHub Action di rilascio (`.github/workflows/release.yml`)

- Parte su `push` di tag `v*.*.*`.
- `docker/metadata-action` genera i tag `{{version}}`, `{{major}}.{{minor}}` e `{{major}}`.
- `docker/build-push-action` compila e pubblica su `ghcr.io/<owner>/totem` usando
  `GITHUB_TOKEN` con permesso `packages: write`. Il pacchetto eredita la visibilità
  privata del repository.
- La versione passa al Dockerfile come `--build-arg VERSION=X.Y.Z` e finisce sia nel
  binario Go sia nella PWA.
