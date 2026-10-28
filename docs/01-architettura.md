# 1. Architettura e scelte motivate

```
 Share SMB (Windows)                 Server Docker (un container)                         Totem
 ─────────────────────               ───────────────────────────────────────────          ──────────────────────────
 \\srv\sds        ──┐                ┌─────────────────────────────────────────┐          Chromium --kiosk (Fase A)
 \\srv\prodotti   ──┼─ SMB 2.1/3.x ─▶│ Scanner (polling)                       │          Fully Kiosk (Fase B)
 \\srv\emergenza  ──┘  sola lettura  │  ├ stabilità 2 scansioni                │          ┌────────────────────────┐
                       go-smb2       │  ├ verifica %PDF … %%EOF                │  HTTPS   │ PWA Preact             │
                                     │  ├ SHA-256 (cache size+mtime)           │◀────────▶│  Service worker        │
                                     │  └ copia in /data/blobs/<sha256>        │  NPM     │   (solo shell app)     │
                                     │ Publisher                               │  (LE DNS)│  Sync PDF scritta a    │
                                     │  ├ manifest.json (tmp+rename)           │          │   mano → Cache Storage │
                                     │  └ GC blob non referenziati             │          │  Indice → IndexedDB    │
                                     │ HTTP :8080                              │          │  MiniSearch, pdf.js    │
                                     │  ├ /            PWA (go:embed)          │          └────────────────────────┘
                                     │  ├ /manifest.json, /api/files/<sha>     │
                                     │  ├ /api/heartbeat, /api/status          │
                                     │  └ /api/maint/*  (PIN → token 10 min)   │
                                     │ 127.0.0.1:8081 interno                  │◀── docker compose exec totem /totem reset-pairing
                                     │  └ healthcheck, reset-pairing           │◀── HEALTHCHECK /totem healthcheck
                                     └──── /data (config.json, blobs, log) ────┘
                                           /secrets (key AES-GCM)
```

## Flusso dei dati

1. **Scansione** (ogni 2 min di default). Per ogni sezione lo scanner elenca i PDF della
   share. Un file entra solo dopo due scansioni consecutive con dimensione e data di
   modifica invariate. Lo SHA-256 si ricalcola solo se cambiano dimensione o data di
   modifica. Il file si legge **una sola volta**: mentre lo si copia nel blob temporaneo
   se ne calcola l'hash e si controllano intestazione `%PDF` e marcatore `%%EOF`.
   Se il controllo fallisce si tiene la versione precedente e si registra l'errore.
2. **Pubblicazione.** Se l'insieme dei documenti è cambiato, il server scrive un nuovo
   `manifest.json` (file temporaneo e poi rename) e incrementa `version`. Solo dopo
   elimina i blob che non sono più referenziati né dal nuovo manifest né dal precedente.
   Il precedente resta in vita per un ciclo, così un totem che sta scaricando con il
   vecchio manifest non resta a metà.
3. **Sync del totem** (ogni 5 min, all'evento `online` e con "Aggiorna ora"). La PWA
   scarica il manifest e lo confronta con l'indice locale. Scarica nella Cache Storage
   solo i PDF nuovi o cambiati, verifica lo SHA-256 e poi aggiorna l'indice in
   IndexedDB con un'unica transazione. Solo dopo elimina i PDF che non servono più.
4. **Lettura.** Avviene solo dalla Cache Storage, quindi è indipendente da rete e server.

## Scelte motivate

| Scelta | Motivo |
|---|---|
| Go + go-smb2 nello stesso processo | SMB in user space: niente mount CIFS, niente `CAP_SYS_ADMIN`, niente kernel. Il binario statico sta su distroless/static. |
| Archivio per hash `/data/blobs/<sha256>` | Il server serve i PDF anche con la share spenta. La deduplicazione è gratuita, l'ETag è lo SHA, il cambio di versione è atomico perché cambia solo il riferimento nel manifest. |
| Polling invece di watcher | Le notifiche SMB (change notify) sono fragili su share Windows e si perdono alle disconnessioni. Il polling è prevedibile e la cache degli hash lo rende economico. |
| Stabilità su due scansioni | Copre i PDF in fase di scrittura (salvataggio da Office, copia lenta) senza lock né euristiche sui nomi. Quando ci sono file in attesa, la scansione successiva parte dopo 20 s e non dopo l'intervallo pieno. |
| Manifest unico, versionato, con hash complessivo | Il totem capisce con una sola richiesta se c'è qualcosa da fare. L'hash complessivo copre documenti e sezioni, non lo stato delle scansioni, che passa da `/api/status`. |
| Sync PDF scritta a mano, Workbox solo per la shell | Workbox non sa fare sostituzioni atomiche con verifica SHA-256 né stime di spazio. Le tiene a parte: aggiornare l'app non tocca i documenti e viceversa. |
| Cache Storage per i PDF, IndexedDB per l'indice | Cache Storage conserva i `Response` binari in modo efficiente ed è leggibile anche dal service worker. IndexedDB permette la transazione atomica sull'indice (il "commit" della sync). |
| Indice locale come unica fonte di verità | Un PDF è visibile solo se il suo SHA è nell'indice **e** il blob è nella cache. L'indice si aggiorna solo a download completati, quindi nessun documento resta a metà e nessuna sezione si svuota per una sync fallita. |
| Token di dispositivo in cookie HttpOnly | Non leggibile da JS, viaggia da solo anche nelle richieste di pdf.js e del service worker. In config si salva solo il suo SHA-256: il token ha 256 bit casuali, quindi non serve un hash lento. |
| PIN con argon2id lato server, token di manutenzione in memoria JS | Un ricaricamento della pagina chiude la manutenzione, così nessuno stato privilegiato sopravvive. Il blocco dopo 5 errori è lato server e resiste anche a un attacco da un altro client in LAN. |
| Codice di setup monouso nei log | Chi è in rete ma non ha accesso al server Docker non può impossessarsi del totem prima della messa in servizio. |
| Listener interno 127.0.0.1:8081 | `reset-pairing` e `healthcheck` girano con `docker compose exec` nello stesso namespace di rete. Il listener non è esposto all'esterno e non serve un secondo canale di autenticazione. Se il server non è in esecuzione, il comando modifica direttamente i file. |
| AES-GCM con chiave in `/secrets/key` | Le password SMB non stanno mai in chiaro su disco, nei log o nella PWA. La chiave è separata dai dati, quindi un backup di `/data` da solo non espone le password. |
| config.json, nessun database | Le sezioni sono una manciata. Un file JSON con backup automatico prima delle migrazioni si legge, si salva e si ripristina senza strumenti. |
| HTTP :8080 dietro NPM | Il certificato e l'access list LAN restano nel proxy che esiste già. Il container non gestisce certificati. |
| Aggiornamenti PWA solo di notte e in inattività | Il service worker nuovo resta in `waiting` e si attiva con `skipWaiting` solo nella finestra notturna dopo X minuti senza interazioni. Una lettura in corso non viene mai interrotta. |
| Postazioni | Altri PC abbinati con un codice monouso di 10 minuti (dal totem in manutenzione o con `totem abbina-postazione`). Leggono i documenti ed entrano in manutenzione con il PIN, ma non sostituiscono il totem; il token di manutenzione resta legato al dispositivo che l'ha ottenuto. |
| Più totem e reparti | Il manifest è una vista per dispositivo costruita a ogni richiesta: le share si leggono una volta sola, ogni totem scarica solo le sezioni del suo reparto. I reparti decidono cosa mostrare, non sono un confine di sicurezza. |
| Numeri utili, avvisi, salvaschermo nel manifest | Arrivano con la sync e restano in IndexedDB: funzionano offline; le date degli avvisi le controlla il totem. |
| Pagina di monitoraggio | Token dedicato (solo hash in config), HTTP 503 in caso di errore: si integra con qualsiasi strumento senza plugin. |
| Origini OneDrive, WebDAV, caricamento diretto | Stessa interfaccia `Source` (List/Open) della share SMB: scanner, stabilità, validazione e archivio non cambiano. OneDrive con Graph e flusso client credentials (nessun utente, nessun MFA, token in memoria); il link della cartella si risolve con `/shares`. I caricamenti finiscono in `/data/uploads/<sezione>` e lo scanner li tratta come una cartella locale. |
| Mappa incidenti in safety.json, non in config.json | Gli eventi crescono nel tempo (fino a 5000); config.json resta piccolo. Il totem riceve solo posizione, data e tipo (niente note, niente persone) e disegna la mappa di calore da sé su canvas, quindi filtri e periodo funzionano offline. Il contatore dei giorni senza infortuni compare solo con una data di inizio della registrazione. |
| Tablet controllati dal server, non dalla PWA | L'API di FreeKiosk è HTTP in LAN con una chiave: la chiama solo il server (la PWA in HTTPS non potrebbe, e la chiave finirebbe nel browser). Controllo ogni minuto; avvisi nel monitoraggio. Lo schermo di notte si spegne e si riaccende solo nei passaggi di fascia, così un'accensione a mano non viene annullata. Fotocamera, posizione e `/api/js` non si usano. |
| Emergenza fuori dal manifest | Deve comparire in secondi su tutti i dispositivi: endpoint dedicato letto ogni 10 s (5 s durante l'emergenza), stato salvato sul server (sopravvive ai riavvii) e sul totem (sopravvive alla perdita di rete). Si chiude solo dal server, con il PIN o con il token. |
| Fase 2 già nel modello | Un dispositivo ha `ruolo` (`totem`/`telefono`). Le API di abbinamento e revoca esistono, ma rispondono 404 finché `globali.fase2` è `false`. |

## Interfaccia e kiosk

- Navigazione a stati, senza URL: un kiosk non ha barra degli indirizzi.
- I PDF si leggono solo dalla Cache Storage; pdf.js, worker, font standard, cmaps e decoder
  wasm sono nella PWA e in precache, quindi la lettura non tocca mai la rete.
- Il service worker prende il controllo alla prima installazione (offline subito); gli
  aggiornamenti restano in attesa fino alla finestra notturna con la Home aperta e il
  totem inattivo.
- Inattività (default 3 min): ritorno alla Home, chiusura di ricerca, tastierini e
  manutenzione. La procedura guidata non si interrompe.
- Errore JavaScript non gestito: ricarica sulla Home (al massimo 3 volte in 5 minuti,
  poi una ogni minuto, per non entrare in un ciclo).
- Il logo aziendale si mette in `/data/logo.svg` o `/data/logo.png`; la PWA lo conserva in
  IndexedDB per l'uso offline. Senza logo compare la scritta SICUREZZA.

## Decisioni prese durante lo sviluppo

| Tema | Decisione | Motivo |
|---|---|---|
| Dimensione dell'immagine | 24,6 MB (6,4 MB compressi) invece di circa 20 | La differenza sono le risorse di pdf.js. Si tengono le cmaps (1,7 MB): senza, una SDS di un fornitore con caratteri cinesi, giapponesi o coreani non si leggerebbe bene, e sul totem di sicurezza ogni documento deve aprirsi. Tolti invece i fallback non-wasm, QuickJS e i font woff/non latini. |
| Totem non più abbinato | Pillola gialla "Abbinamento da rifare", documenti sempre leggibili | La specifica prevede quattro stati della pillola; questo quinto caso (dopo `reset-pairing` o cookie cancellato) deve restare distinguibile da "offline", perché si risolve solo con la procedura guidata. |
| Inattività durante la procedura guidata | Non riporta alla Home | Un tecnico non deve perdere la messa in servizio a metà. |
| Inattività in manutenzione | 10 minuti (fuori dalla manutenzione 3) | Coerente con la fascia gialla "uscita automatica dopo 10 minuti" e con la scadenza del token sul server. |
| Blocco del PIN | Globale, non per indirizzo IP | In LAN gli indirizzi si cambiano facilmente. |

## Nomi di file da tenere distinti

- `/manifest.json`: il manifest **dei documenti**, protetto dal token di dispositivo, come
  richiesto dalla specifica.
- `/app.webmanifest`: il web app manifest della PWA, pubblico.

## Valori di default (nel codice, sovrascrivibili solo da `config.json` → `impostazioni`)

| Chiave | Default | Dove |
|---|---|---|
| `intervalloScansione` | `2m` | server |
| `ritardoStabilita` | `20s` (scansione ravvicinata se ci sono file in attesa) | server |
| `regexRevisione` | `(?i)[ _-]*(?:rev|r)\.?[ _-]*(\d{1,3})$` | server |
| `intervalloSync` | `5m` | PWA (ricevuto dal manifest) |
| `intervalloHeartbeat` | `5m` | PWA |
| `giorniAvviso` | `3` | PWA |
| `giorniRecenti` | `30` | PWA |
| `inattivitaMinuti` | `3` (ritorno alla Home) | PWA |
| `finestraNotturna` | `02:00-05:00` | PWA |
| `inattivitaAggiornamentoMinuti` | `15` | PWA |
| `margineSpazioMB` | `50` | PWA |
| `maxDimensionePDFMB` | `200` | server |
| `tentativiPIN` / `bloccoPIN` | `5` / `5m` | server |
| `scadenzaManutenzione` | `10m` | server |

I valori che servono alla PWA arrivano nel manifest sotto `settings`, così anche
le impostazioni lato totem si cambiano solo da file sul server.
