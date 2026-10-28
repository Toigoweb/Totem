# Guida per chi riprende il progetto (persone e assistenti AI)

Questo file basta per continuare lo sviluppo da un altro account, un'altra persona o un
altro assistente AI senza la cronologia delle conversazioni precedenti. Va letto per
primo; i dettagli stanno nei documenti indicati.

## 1. Che cos'è

**Totem documenti di sicurezza**: un PC Windows in kiosk (fase A) o un tablet Android con
Fully Kiosk (fase B) mostra in reparto schede di sicurezza (SDS), schede prodotto, piani di
emergenza e procedure. I PDF arrivano da cartelle condivise o servizi cloud, il server li
verifica e li pubblica, il totem li conserva **offline** e li mostra anche senza rete.

- Server: **Go 1.24**, un solo binario con la PWA incorporata (`go:embed`), immagine
  Docker distroless non root, pubblicata su `ghcr.io/toigoweb/totem`.
- App del totem: **PWA Preact + Vite**, pdf.js self-hosted, IndexedDB + Cache Storage.
- HTTPS lo fa **Nginx Proxy Manager** (NPM) davanti al container; il container parla HTTP
  sulla porta 8080 interna.
- Installazione in produzione: **stack Portainer** collegato a questo repository
  (pubblico), aggiornamento automatico (README 1.6).
- Lingua: **solo italiano**, sia l'interfaccia sia commenti, documentazione e messaggi
  di commit.

## 2. Dove sono le cose

| Argomento | Documento |
|---|---|
| Installazione, NPM, Portainer, variabili, diagnostica, backup, aggiornamenti, sviluppo | [README.md](README.md) |
| Architettura, scelte e motivazioni, impostazioni avanzate | [docs/01-architettura.md](docs/01-architettura.md) |
| Formati: manifest, config.json, safety.json, file in /data | [docs/02-formati.md](docs/02-formati.md) |
| Mappa del repository e workflow di rilascio | [docs/03-struttura-repository.md](docs/03-struttura-repository.md) |
| Checklist di messa in servizio (Windows, Android) | [docs/04-messa-in-servizio.md](docs/04-messa-in-servizio.md) |
| Wireframe approvati | [wireframes/index.html](wireframes/index.html) |

## 3. Funzioni presenti (versione 1.2.x)

- **Sezioni** (massimo 8) con icona, ordine, flag emergenza; origine dei PDF per sezione:
  cartella **SMB**, **OneDrive/SharePoint** (Microsoft Graph, app Entra ID), **WebDAV**,
  **caricamento diretto** dall'area manutenzione.
- **Scanner**: polling, stabilità su due scansioni, controllo `%PDF`/`%%EOF`, archivio per
  SHA-256 in `/data/blobs`, manifest versionato, documenti scartati.
- **Totem**: Home con fascia EMERGENZA, ricerca (MiniSearch), elenco per sezione con chip
  per sottocartella, lettore PDF con accesso rapido alle 16 sezioni SDS, "aggiornati di
  recente", pillola di stato, schermata Stato, testo più grande, inattività → Home.
- **Sync offline**: incrementale, verifica SHA-256, commit atomico in IndexedDB, controllo
  dello spazio, heartbeat. Aggiornamenti dell'app solo di notte e in inattività.
- **Sicurezza**: procedura guidata con codice di setup monouso (nei log), PIN di
  manutenzione argon2id con blocco, token del dispositivo in cookie HttpOnly Secure
  SameSite=Strict, password delle sorgenti cifrate AES-GCM (chiave in `/secrets/key`).
- **Postazioni** (altri PC con accesso alla manutenzione), **più totem per reparto**,
  sostituzione di un totem, revoca dei dispositivi.
- **Numeri utili**, **avvisi** con date, **salvaschermo** (con schermo nero notturno).
- **Monitoraggio**: `GET /api/monitor` con token (200/503, JSON o testo), `totem stato`.
- **Mappa incidenti**: planimetrie, eventi (infortunio, medicazione, quasi incidente,
  danno), mappa di calore sul totem, giorni senza infortuni, export CSV.
- **Pagina in HTTP**: aperta senza HTTPS spiega che serve l'indirizzo di NPM.
- **Emergenza**: evacuazione o avviso urgente a tutto schermo su tutti i dispositivi, da
  manutenzione, API con token o `totem emergenza avvia`; resta anche offline.
- **Controllo dei dispositivi**: dall'app (batteria, browser, cosa mostra; comandi
  ricarica/Home/sincronizza) e, sui PC Windows, dall'**agente** `totem-agente.ps1` (stato
  del PC, comandi, screenshot, schermo di notte, annuncio vocale di emergenza).
- **Tablet con FreeKiosk**: il server legge stato e posizione, segnala corrente,
  batteria, temperatura e spostamenti, invia comandi, spegne lo schermo di notte e fa
  leggere ad alta voce l'emergenza (README, "Tablet Android con FreeKiosk").
- **Interfono** (spento di default, invisibile finché non si accende): chiamate a voce
  WebRTC in rete interna tra totem, postazioni e telefoni Android abbinati con QR; ruoli
  con squillo di gruppo, 30 s poi numero di ripiego, notifiche Web Push ai telefoni
  (`internal/intercom`, `internal/webpush`, `web/src/intercom/`). README, "Interfono".
- **Fase 2 (telefoni con i documenti)**: nel modello e nelle API, spenta (`globali.fase2=false`).

## 4. Regole da rispettare

1. **Offline prima di tutto.** Il totem deve funzionare con il server spento: tutto ciò
   che mostra arriva nel manifest o in Cache Storage. Una sync fallita non deve mai
   lasciare l'indice a metà o una sezione vuota.
2. **Niente CDN, niente telemetria, niente servizi esterni** dal browser del totem. Font,
   pdf.js e icone sono nel pacchetto. CSP rigida in `internal/api/server.go`.
3. **Segreti**: password e client secret mai nei log, mai nella PWA, mai in chiaro su
   disco. Le note della mappa incidenti non vanno mai nel manifest. Niente nomi di persone
   nei dati del totem. La chiave API dei tablet resta sul server.
   **Mai fotocamera o posizione** dei tablet e dei telefoni, e mai il **microfono** fuori
   da una chiamata dell'interfono iniziata o accettata con un tocco: sarebbero controllo
   a distanza dei lavoratori (art. 4 L. 300/1970). Le chiamate non si registrano e il
   server non ne tiene la cronologia.
4. **Sorgenti in sola lettura**: il server non scrive mai su SMB, OneDrive o WebDAV.
5. **Interfaccia**: tutte le stringhe in `web/src/strings.js`, in italiano. Pulsanti
   grandi (uso con i guanti), contrasto alto, stato mai solo a colori (icona + testo).
6. **Formati**: `config.json` ha `schemaVersion` (oggi 2). Ogni cambio di formato richiede
   una migrazione con backup in `internal/config` e l'aggiornamento di docs/02.
7. **Stile del codice**: commenti brevi in italiano che spiegano il perché; Go formattato
   con `gofmt`; nessuna dipendenza nuova senza un motivo forte.
8. **Script PowerShell** (`deploy/windows/*.ps1`): salvati in UTF-8 **con BOM**,
   altrimenti PowerShell 5.1 rovina le lettere accentate (c'è un controllo in CI).
   Compatibili con Windows PowerShell 5.1 (quello di Windows 11), non solo con pwsh 7.
   L'agente esegue solo l'elenco chiuso di comandi: non aggiungere esecuzioni arbitrarie.
9. **Test**: ogni funzione nuova con test Go e/o Vitest e, se tocca il totem, un test
   end-to-end Playwright. La CI deve restare verde.

## 5. Sviluppo e test

```sh
go vet ./... && go test -race ./...           # server
cd web && npm ci && npm test && npm run build  # PWA (Node 22)
cd test/e2e && npm ci && npx playwright test   # end-to-end (serve Docker per Samba)
docker compose -f docker-compose.test.yml up -d && go test -tags integration -count=1 ./test/integration/...
```

- Il binario Go incorpora `web/dist`: **dopo ogni modifica alla PWA rifare
  `npm run build`** prima di `go build`, altrimenti si prova l'interfaccia vecchia.
- Gli end-to-end (`test/e2e`) compilano PWA e server da soli, avviano Samba in Docker
  (porta 1446) e un server Go per test, con due profili: desktop e tablet 2560×1600.
  `TOTEM_SKIP_BUILD=1` salta la compilazione se è già fatta.
- Variabili solo per i test: `TOTEM_SMB_PORT`, `TOTEM_GRAPH_URL`, `TOTEM_LOGIN_URL`.
- Prova rapida in locale: `TOTEM_DATA=./data TOTEM_SECRETS=./secrets TOTEM_ADDR=127.0.0.1:8080 go run ./cmd/totem`
  e aprire `http://localhost:8080` (in HTTP l'app funziona solo su localhost).

## 6. Rilascio e aggiornamento in produzione

1. Tutto il lavoro va sul branch principale del repository, oggi
   **`claude/new-session-75lftp`** (è il branch di default; si può rinominare in `main`,
   ma allora va aggiornata la *Repository reference* dello stack Portainer).
2. La CI (`ci.yml`) gira a ogni push: go, integration, web, e2e, windows-kiosk, image.
3. Per pubblicare: su GitHub **Releases → Draft a new release**, tag `vX.Y.Z` sul branch
   principale, Publish. Il workflow `release.yml`:
   - compila e pubblica l'immagine `ghcr.io/toigoweb/totem` con i tag `X.Y.Z`, `X.Y`,
     `X` e `latest` (il pacchetto è pubblico);
   - poi aggiorna la versione predefinita in `docker-compose.yml`
     (`${TOTEM_TAG:-X.Y.Z}`) con un commit di `github-actions[bot]`.
4. Portainer (GitOps, polling ogni 5 minuti) vede il commit e ridistribuisce lo stack.
   Il totem applica la nuova versione dell'app di notte.
5. Dopo un rilascio fare `git pull` prima di lavorare: il commit del bot è sul branch.

Versioni: `MAJOR.MINOR.PATCH`. Una versione che cambia `config.json` in modo
incompatibile va annunciata nelle note della release (il rollback richiede il backup in
`data/backup/`).

## 7. Installazione in produzione

Il repository è pubblico: indirizzi interni, IP, utenti e token **non** vanno scritti qui
né altrove nel repository. Teneteli in un posto privato (per esempio le note dello stack
in Portainer o la "Scheda del totem" di docs/04) e passateli a chi riprende il lavoro.

| Voce | Dove si trova |
|---|---|
| Indirizzo del totem | proxy host in NPM (`https://totem.<dominio interno>`) |
| Server Docker | quello che ospita Portainer |
| Stack Portainer | repository `https://github.com/Toigoweb/Totem`, compose `docker-compose.yml`, variabile `TOTEM_PORT` |
| Dati | `/opt/totem/data` e `/opt/totem/secrets` (da includere nel backup) |

**Proxy host in NPM**: *Scheme* `http`. Se NPM e il totem sono sulla stessa rete Docker,
*Forward Hostname* = nome o IP del container e *Port* = **8080** (porta interna, non
`TOTEM_PORT`). Altrimenti *Forward Hostname* = IP del server Docker e *Port* =
`TOTEM_PORT`. *Cache Assets* disattivato.

## 8. Limiti dell'ambiente degli assistenti AI (sessioni cloud)

Nelle sessioni cloud di Claude Code usate finora:
- si poteva fare push solo sul branch principale; **tag e release no** (errore 403):
  la release la crea una persona da GitHub;
- il demone Docker a volte si ferma tra un comando e l'altro: `dockerd &` e riprovare;
- il traffico esce da un proxy: per provare con un browser un dominio locale serve
  escluderlo dal proxy (`NO_PROXY` o `bypass` di Playwright).

## 9. Idee proposte e non ancora sviluppate

1. Segnalazione di pericoli e quasi incidenti dal totem (anonima, da verificare in
   manutenzione prima di entrare nella mappa).
2. ~~Modalità emergenza~~ fatta (v1.2): resta da valutare un ingresso a contatto pulito
   della centrale antincendio tramite un modulo di rete (relè/IO) che chiami l'API.
3. Pittogrammi GHS (frasi H) e DPI estratti dalle SDS, ricerca per pericolo.
4. Codici QR da stampare per armadi, macchine e reparti (apertura del documento da
   telefono abbinato, fase 2).
5. Scadenzario della sicurezza (estintori, verifiche, prove di evacuazione) con avvisi
   nella pagina di monitoraggio.

Scartato per ora: totem "di macchina" con ordini di produzione e dati dei PLC. Se
servirà, sarà un progetto separato in un altro repository.
