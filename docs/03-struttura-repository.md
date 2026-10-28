# 3. Struttura del repository

```
.
├── AGENTS.md, CLAUDE.md         guida per chi riprende il progetto (persone e assistenti AI)
├── cmd/totem/main.go            entrypoint: serve (default) | healthcheck | stato | scan | reset-pairing [--reset-pin]
│                                | abbina-postazione --nome | abbina-totem --reparto | emergenza avvia|termina | version
├── internal/
│   ├── config/                  config.json: load, migrazioni con backup, salvataggio atomico, AES-GCM,
│   │                            reparti, numeri utili, avvisi, salvaschermo, monitoraggio
│   ├── smbsrc/                  sorgente SMB (go-smb2): parsing UNC, elenco PDF, apertura, classificazione errori
│   ├── davsrc/                  sorgente WebDAV (PROPFIND, Basic auth)
│   ├── graphsrc/                sorgente OneDrive/SharePoint (Microsoft Graph, client credentials)
│   ├── safety/                  mappa incidenti: planimetrie, eventi, safety.json
│   ├── kiosk/                   client dell'API FreeKiosk, controlli periodici, avvisi, schermo di notte, annuncio
│   ├── emergency/               stato della modalità emergenza (emergency.json)
│   ├── sources/                 scelta della sorgente per tipo; cartella dei caricamenti diretti
│   ├── scanner/                 scansione, stabilità, validazione PDF, blob store, manifest, GC
│   ├── auth/                    PIN argon2id, blocco tentativi, codice di setup, token dispositivo e manutenzione
│   ├── api/                     handler HTTP: setup, dispositivo, manifest per dispositivo, manutenzione,
│   │                            caricamenti, mappa incidenti, monitoraggio, listener interno
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
│       ├── screens/             Home, Ricerca, Sezione, Lettura, Stato, PIN, Manutenzione, form della sezione,
│       │                        Procedura guidata, Dispositivi, Extras (reparti, numeri, avvisi, salvaschermo,
│       │                        monitoraggio), Uploads, SafetyMap/SafetyAdmin (mappa incidenti), Insecure (HTTP),
│       │                        EmergencyAdmin (emergenza), KioskDevice (tablet FreeKiosk)
│       ├── heatmap.js           mappa di calore su canvas, filtri, giorni senza infortuni
│       ├── kiosk/               inattività, errori globali, aggiornamenti notturni, salvaschermo,
│       │                        emergenza (lettura periodica e schermata a tutto schermo)
│       └── sw.js                service worker (Workbox injectManifest, solo shell)
├── wireframes/index.html        wireframe statici navigabili (approvati)
├── deploy/windows/              watchdog del kiosk Chrome e script di installazione (fase A)
├── deploy/totem.env.example     variabili dell'installazione (porta, cartelle, versione)
├── test/
│   ├── integration/             test Go con tag `integration` contro Samba
│   ├── fixtures/genpdf.py       generatore di PDF di prova (SDS con 16 sezioni, generici, troncati)
│   └── e2e/                     Playwright: Chromium desktop + tablet Android emulato, server Go e Samba
│                                (resilienza, comportamenti, postazioni, reparti, origini, mappa, https, emergenza)
├── docs/                        architettura, formati, messa in servizio
├── Dockerfile                   node → go → distroless/static:nonroot
├── docker-compose.yml           l'unico compose di produzione (docker compose e Portainer)
├── docker-compose.test.yml      Samba + totem per i test di integrazione
├── .github/workflows/
│   ├── ci.yml                   push/PR: go vet, go test, integrazione Samba, npm test, e2e, build immagine
│   └── release.yml              tag vX.Y.Z → GHCR :X.Y.Z :X.Y :X :latest, poi versione nel compose
└── README.md
```

## GitHub Action di rilascio (`.github/workflows/release.yml`)

- Parte su `push` di tag `v*.*.*`.
- `docker/metadata-action` genera i tag `{{version}}`, `{{major}}.{{minor}}` e `{{major}}`.
- `docker/build-push-action` compila e pubblica su `ghcr.io/<owner>/totem` usando
  `GITHUB_TOKEN` con permesso `packages: write`. Il pacchetto è pubblico (impostazione
  del pacchetto su GitHub, indipendente dal repository).
- La versione passa al Dockerfile come `--build-arg VERSION=X.Y.Z` e finisce sia nel
  binario Go sia nella PWA.
- Il job `compose`, solo se la pubblicazione è riuscita, porta il default di
  `docker-compose.yml` a `${TOTEM_TAG:-X.Y.Z}` sul branch principale con un commit di
  `github-actions[bot]` (permesso `contents: write`). Gli stack Portainer collegati al
  repository vedono il commit e si aggiornano da soli.
