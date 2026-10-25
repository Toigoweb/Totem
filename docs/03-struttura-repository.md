# 3. Struttura del repository

```
.
├── cmd/totem/main.go            entrypoint: serve (default) | healthcheck | reset-pairing [--reset-pin]
├── internal/
│   ├── config/                  config.json: load, migrazioni con backup, salvataggio atomico, AES-GCM
│   ├── smbsrc/                  sorgente SMB (go-smb2): parsing UNC, elenco PDF, apertura, classificazione errori
│   ├── davsrc/                  sorgente WebDAV (PROPFIND, Basic auth)
│   ├── graphsrc/                sorgente OneDrive/SharePoint (Microsoft Graph, client credentials)
│   ├── safety/                  mappa incidenti: planimetrie, eventi, safety.json
│   ├── sources/                 scelta della sorgente per tipo; cartella dei caricamenti diretti
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
│       ├── app.js, store.js     stato dell'app, navigazione, derivati (pillola, recenti)
│       ├── sync/                db.js (IndexedDB), sync.js (manifest, download, SHA-256, commit),
│       │                        sds.js (sezioni SDS con pdf.js), manager.js (timer, online, heartbeat)
│       ├── viewer/              pdf.js self-hosted e visualizzatore
│       ├── components/          barra superiore, logo con pressione lunga, tastierino PIN, icone
│       ├── screens/             Home, Ricerca, Sezione, Lettura, Stato, PIN, Manutenzione, Form, Procedura guidata
│       ├── kiosk/               inattività, errori globali, aggiornamenti notturni
│       └── sw.js                service worker (Workbox injectManifest, solo shell)
├── wireframes/index.html        wireframe statici navigabili (approvati)
├── deploy/windows/              watchdog del kiosk Chrome e script di installazione (fase A)
├── test/
│   ├── integration/             test Go con tag `integration` contro Samba
│   ├── fixtures/genpdf.py       generatore di PDF di prova (SDS con 16 sezioni, generici, troncati)
│   └── e2e/                     Playwright: Chromium desktop + tablet Android emulato, server Go e Samba
├── docs/                        architettura, formati, messa in servizio
├── Dockerfile                   node → go → distroless/static:nonroot
├── docker-compose.yml           l'unico compose di produzione
├── docker-compose.test.yml      Samba + totem per i test di integrazione
├── .github/workflows/
│   ├── ci.yml                   push/PR: go vet, go test, integrazione Samba, npm test, e2e, build immagine
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
