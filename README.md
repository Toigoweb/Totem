# Totem documenti di sicurezza

Totem offline-first per consultare in reparto le schede di sicurezza (SDS), le schede
prodotto e i piani di emergenza che stanno sulle share SMB aziendali.

- Un container Docker legge le share, conserva l'ultima versione valida di ogni PDF e la
  serve al totem.
- Il totem (PC Windows oggi, tablet Android domani) tiene una copia locale di tutti i
  documenti e funziona anche senza rete o con il server spento.
- Tutto si configura dal totem, dietro PIN. Sul server non si modifica niente.

| Documento | Contenuto |
|---|---|
| [docs/01-architettura.md](docs/01-architettura.md) | Architettura e scelte motivate |
| [docs/02-formati.md](docs/02-formati.md) | Formato di `manifest.json` e `config.json` |
| [docs/03-struttura-repository.md](docs/03-struttura-repository.md) | Struttura del repository e GitHub Action |
| [docs/04-messa-in-servizio.md](docs/04-messa-in-servizio.md) | **Checklist di messa in servizio**: fase A (Windows), fase B (Android), passaggio dall'una all'altra |
| `wireframes/index.html` | Wireframe approvati, da aprire nel browser |

---

## 1. Installazione

### Requisiti

| Cosa | Note |
|---|---|
| Un server Linux con Docker e Docker Compose v2 | Bastano 1 CPU, 256 MB di RAM e lo spazio dei PDF (circa il doppio dei PDF presenti sulle share). |
| Nginx Proxy Manager (NPM) già in uso | Gestisce HTTPS e l'accesso limitato alla LAN. |
| Un nome DNS aziendale per il totem | Per esempio `totem.azienda.it`. Deve puntare all'IP **interno** del server NPM. |
| Un account di dominio di servizio per le share | Per esempio `AZIENDA\svc-totem`, con permesso di **sola lettura** sulle cartelle. Il totem non scrive mai sulle share. |
| Accesso al pacchetto GHCR privato | Un Personal Access Token (classic) con il solo permesso `read:packages`. |

### 1.1 DNS e certificato in Nginx Proxy Manager

Il certificato Let's Encrypt si ottiene con la challenge DNS, quindi il nome non deve essere
raggiungibile da Internet.

1. Nel DNS interno creare il record `totem.azienda.it` verso l'IP interno di NPM.
2. In NPM aprire **SSL Certificates → Add SSL Certificate → Let's Encrypt** e compilare:
   - dominio `totem.azienda.it`;
   - **Use a DNS Challenge** attivo;
   - il provider DNS del dominio aziendale con le sue credenziali API.
3. In NPM aprire **Access Lists → Add Access List** e creare la lista "LAN":
   - nella scheda **Access**, una riga *allow* per ogni subnet interna (per esempio
     `192.168.10.0/24`), più una riga finale *deny* `all`;
   - nessun utente in **Authorization**; *Satisfy Any* disattivato.
4. In NPM aprire **Hosts → Proxy Hosts → Add Proxy Host** e impostare:

   | Campo | Valore |
   |---|---|
   | Domain Names | `totem.azienda.it` |
   | Scheme / Forward Hostname / Port | `http` / IP del server Docker (o nome del container, se NPM è sulla stessa rete Docker) / `8080` |
   | Access List | `LAN` |
   | Block Common Exploits | attivo |
   | Cache Assets | **disattivato**: `sw.js` e `index.html` non devono essere messi in cache dal proxy, altrimenti gli aggiornamenti dell'app non arrivano |
   | Websockets Support | non serve |
   | SSL Certificate | il certificato creato al punto 2 |
   | Force SSL, HTTP/2 Support, HSTS Enabled | attivi |

> Il cookie del totem è `Secure`, quindi l'app funziona **solo in HTTPS**, cioè passando
> da NPM. L'unica eccezione è `http://localhost`, comodo per le prove sul server stesso.

Se NPM gira su un altro host, conviene limitare la porta 8080 del server Docker al solo
indirizzo di NPM con il firewall (per esempio con `ufw`).

### 1.2 Accesso a GHCR (una sola volta)

```sh
echo "<PAT con read:packages>" | docker login ghcr.io -u <utente-github> --password-stdin
```

Le credenziali restano in `~/.docker/config.json` dell'utente che esegue `docker compose`.

### 1.3 Cartelle e compose

```sh
sudo mkdir -p /opt/totem && cd /opt/totem
# copiare qui docker-compose.yml dal repository
sudo mkdir data secrets
sudo chown -R 65532:65532 data secrets    # il container gira come utente non root 65532
docker compose up -d
```

`docker-compose.yml` è quello del repository e non va modificato. Il filesystem del
container è in sola lettura; si scrive solo in `./data` e `./secrets`.

Se il container si riavvia in continuazione e nei log compare *cartella non scrivibile*,
mancano i permessi: ripetere il `chown`.

### 1.4 Primo avvio

```sh
docker compose logs totem
```

Nei log compare un riquadro con il codice monouso, per esempio:

```
====================================================
  CODICE DI SETUP DEL TOTEM:  K7QM-4XPA
  (monouso, da inserire nella procedura guidata)
====================================================
```

Il codice serve alla procedura guidata sul totem; si veda
[docs/04-messa-in-servizio.md](docs/04-messa-in-servizio.md). Finché nessun totem è
abbinato, a ogni riavvio del container si genera un codice nuovo. Il codice impedisce che
qualcun altro in rete si impossessi del server prima del totem.

### 1.5 Logo aziendale (facoltativo)

Copiare `logo.svg` o `logo.png` (massimo 2 MB, sfondo trasparente, chiaro su navy) in
`/opt/totem/data/`. Il totem lo scarica al successivo avvio dell'app e lo conserva per
l'uso offline. Senza logo compare la scritta SICUREZZA.

---

## 2. Gestione dei PDF sulle share (per chi mantiene i documenti)

- Basta copiare, sostituire o cancellare i PDF nelle cartelle: il server se ne accorge
  entro 2 minuti e il totem si aggiorna entro i 5 minuti successivi.
- **Revisione nel nome:** `Acetone_rev3.pdf`, `Acetone Rev. 3.pdf`, `Acetone-R03.pdf` e
  `Acetone revisione 3.pdf` diventano "Acetone" con il badge "Rev. 3". Trattini e
  underscore diventano spazi.
- **Sottocartelle:** se la sezione le include, diventano i filtri a chip nell'elenco
  (si usa la prima sottocartella).
- **File in fase di copia:** un PDF si pubblica solo quando dimensione e data restano
  uguali per due controlli consecutivi. Non serve fare nulla.
- **PDF danneggiati o troncati:** vengono scartati e il totem continua a mostrare la
  versione precedente. L'errore resta nel log di sync.
- I file nascosti, quelli temporanei di Office (`~$…`) e i file non PDF vengono ignorati.
- **SDS:** il totem riconosce le 16 sezioni standard se il testo del PDF contiene le
  intestazioni "SEZIONE n" o "SECTION n" a inizio riga. Le SDS scansionate come
  immagine non hanno testo: si leggono normalmente, ma senza accesso rapido.

---

## 3. Diagnostica

### Dove guardare

| Fonte | Come | Cosa dice |
|---|---|---|
| Schermata **Stato** sul totem | pillola in alto a destra o pulsante "Stato" | Ultima sync, share non raggiungibili, spazio, ultime 10 sincronizzazioni, versioni |
| Log del container | `docker compose logs --tail 200 totem` | Avvio, codice di setup, sezioni non leggibili (con codice di errore), PDF scartati, manifest pubblicati |
| Log di sync | `/opt/totem/data/logs/sync.log`, JSON con una riga per scansione, ruotato a 5 MB × 5 | Nuovi, aggiornati, rimossi ed errori, con sezione e percorso relativo |
| Ultimo contatto del totem | `/opt/totem/data/state/heartbeat.json` | Versione dell'app, piattaforma, ultima sync, documenti e spazio libero del totem |
| Salute del container | `docker compose ps` | Colonna `healthy` |
| Scansione immediata | `docker compose exec totem /totem scan` | Da usare dopo aver corretto una share |

Le password non compaiono mai nei log.

### Problemi frequenti

| Sintomo | Causa probabile | Cosa fare |
|---|---|---|
| Il container si riavvia, log *cartella non scrivibile* | Permessi di `data` o `secrets` | `sudo chown -R 65532:65532 data secrets` |
| Il browser dà *403 Forbidden* | Il totem non è in una subnet dell'access list "LAN" | Aggiungere la subnet in NPM |
| Il totem resta su "Totem non ancora configurato" | Non abbinato | Procedura guidata con il codice dei log |
| Pillola gialla "Abbinamento da rifare" | Dopo un `reset-pairing`, o con il cookie del browser cancellato | Procedura guidata (codice, PIN attuale). I documenti restano leggibili nel frattempo. |
| Pillola "Offline – documenti disponibili" | Il totem non raggiunge il server | Rete del totem, DNS, NPM, `docker compose ps` |
| Pillola gialla "Ultimo aggiornamento N giorni fa" | Nessuna sync riuscita da 3 giorni o più | Come sopra, poi "Aggiorna ora" dalla schermata Stato |
| Banner giallo "La cartella della sezione «X» non è raggiungibile…" | Il server non legge la share | Leggere il codice di errore nei log (tabella sotto), correggere, poi `/totem scan` |
| Banner rosso "Spazio insufficiente sul totem" | Il browser del totem non ha spazio per i nuovi PDF | Liberare disco sul PC o sul tablet; i documenti attuali restano |
| Un PDF nuovo non compare | File ancora in copia, danneggiato, oltre 200 MB o non `.pdf` | `sync.log`: cercare il percorso, codice `corrupt` o `too_large` |
| Il totem mostra "In download…" su un documento | Download non completato (rete, spazio) | Attendere la sync successiva o premere "Aggiorna ora" |
| Manutenzione: "Troppi tentativi. Riprova tra…" | 5 PIN errati | Attendere 5 minuti. PIN dimenticato: `reset-pairing --reset-pin` (vedi sotto) |
| Log *generata una nuova chiave… le password SMB salvate non sono più leggibili* | `secrets/key` persa | Reinserire le password delle sezioni dal totem (Modifica, nuova password, Prova connessione, Salva). Nessun altro dato è perso. |

Codici di errore delle sezioni, nei log e nella schermata Stato:

| Codice | Significato | Verifica |
|---|---|---|
| `auth` | Utente o password errati, account bloccato o scaduto | Account di servizio in AD |
| `not_found` | Share o cartella inesistente | Percorso `\\server\condivisione\cartella` |
| `access_denied` | Permessi NTFS o di condivisione mancanti | Sola lettura per l'account di servizio |
| `unreachable` | Server SMB non raggiungibile (DNS, firewall, porta 445) | `nc -zv fileserver 445` dal server Docker |
| `timeout` | Il server SMB non risponde in tempo | Carico o rete del file server |
| `no_password` | Password non decifrabile (chiave persa) | Reinserirla dal totem |

### Sostituzione del totem, PIN dimenticato

| Operazione | Comando |
|---|---|
| Invalidare i token e generare un nuovo codice di setup (PIN e sezioni invariati) | `docker compose exec totem /totem reset-pairing` |
| Come sopra, azzerando anche il PIN | `docker compose exec totem /totem reset-pairing --reset-pin` |
| Con il container fermo | `docker compose run --rm totem reset-pairing`; il codice compare nei log al prossimo avvio |

---

## Altri PC, documenti scartati, istanza di prova

### Accesso da altri PC (postazioni)

Oltre al totem si possono abbinare altri PC, per esempio in ufficio IT o per le prove.
Una **postazione** legge i documenti (anche offline) e può entrare in manutenzione con il
PIN. Non sostituisce il totem: in alto a sinistra compare l'etichetta POSTAZIONE.

1. Generare un codice monouso, valido 10 minuti, in uno di due modi:
   - dal totem o da un'altra postazione: Area manutenzione → **Dispositivi** →
     **Abbina un'altra postazione**, scrivere il nome e premere **Genera codice**;
   - dal server: `docker compose exec totem /totem abbina-postazione --nome "PC ufficio IT"`.
2. Sull'altro PC aprire `https://totem.azienda.it` (il PC deve essere in una subnet
   dell'access list "LAN"), premere **Inizia configurazione** e inserire il codice.
3. Dopo la prima sincronizzazione la postazione è pronta.

In Area manutenzione → **Dispositivi** si vedono tutti i dispositivi con ultimo contatto,
versione dell'app, documenti e spazio libero; da lì si **revocano** le postazioni.
`reset-pairing` invalida tutti i dispositivi, postazioni comprese.

### Documenti scartati

Area manutenzione → **Documenti scartati** elenca i PDF che il server ha rifiutato
(danneggiati, incompleti, oltre la dimensione massima), con sezione, percorso e data.
Dice anche se sul totem resta la versione precedente. Basta sostituire il file nella
cartella: al controllo successivo sparisce dall'elenco.

### Istanza di prova

Per provare nuove versioni, nuove sezioni o PDF particolari senza toccare il totem di
reparto conviene un secondo container, completamente separato:

```sh
sudo mkdir -p /opt/totem-test && cd /opt/totem-test
sudo mkdir data secrets && sudo chown -R 65532:65532 data secrets
cat > docker-compose.yml <<'YAML'
services:
  totem:
    image: ghcr.io/toigoweb/totem:1      # o la versione da provare, per esempio :1.1.0
    restart: unless-stopped
    read_only: true
    ports: ["8081:8080"]
    volumes: ["./data:/data", "./secrets:/secrets"]
    environment: ["TZ=Europe/Rome"]
    logging: { options: { max-size: "10m", max-file: "3" } }
YAML
docker compose up -d && docker compose logs totem   # codice di setup dell'istanza di prova
```

In NPM aggiungere un secondo proxy host `totem-test.azienda.it` verso la porta `8081`,
con lo stesso certificato (o uno nuovo) e la stessa access list. L'istanza di prova ha
PIN, sezioni e dispositivi suoi: si abbina un PC di prova con la procedura guidata e si
usano cartelle di prova sulle share.

---

## 4. Aggiornamento e rollback

Le immagini si pubblicano su `ghcr.io/toigoweb/totem` con i tag `X.Y.Z`, `X.Y` e `X`. Il
compose usa `:1`, cioè l'ultima versione 1.x.

| Operazione | Comando o azione |
|---|---|
| Aggiornamento | `docker compose pull && docker compose up -d` |
| Bloccare una versione | Nel compose sostituire `:1` con `:1.2.3` |
| Rollback | Rimettere il tag precedente (per esempio `:1.2.2`) nel compose e lanciare `docker compose up -d` |
| Pubblicare una versione (sviluppo) | `git tag v1.2.3 && git push origin v1.2.3`: la GitHub Action compila e pubblica |
| Provare prima una versione nuova | Nell'istanza di prova (vedi «Istanza di prova») impostare il tag nuovo, poi aggiornare il totem |
| Notifica delle nuove versioni | Facoltativa: Watchtower in modalità solo notifica (`WATCHTOWER_MONITOR_ONLY=true`), che deve poter leggere `~/.docker/config.json` per GHCR |

**Cosa succede sul totem:**
- Il nuovo service worker viene scaricato ma resta in attesa. Si applica solo nella
  finestra notturna (02:00-05:00), con la Home aperta e almeno 15 minuti senza
  interazioni. Una lettura in corso non viene mai interrotta.
- Anche il riavvio notturno del browser applica l'aggiornamento.

**Rollback e formato della configurazione:** se una versione nuova migra `config.json`
(`schemaVersion` più alto), prima della migrazione salva una copia in
`data/backup/config-<data>-v<n>.json`. Una versione vecchia rifiuta di partire con un
`config.json` più nuovo di lei, così non lo danneggia. Per tornare indietro in quel caso:
fermare il container, rimettere il backup come `data/config.json`, impostare il tag
vecchio e avviare.

---

## 5. Backup e ripristino

| Cosa | Dove | Note |
|---|---|---|
| Configurazione | `/opt/totem/data/config.json` (e `data/backup/`) | Sezioni, PIN (hash), dispositivi, impostazioni |
| Chiave di cifratura | `/opt/totem/secrets/key` | Senza la chiave le password SMB vanno reinserite dal totem. Conservarla separata dal backup di `data`. |
| Logo | `/opt/totem/data/logo.*` | Se presente |
| PDF | non serve | Si ricostruiscono dalle share alla prima scansione |

Ripristino su un server nuovo:
1. Installare come al punto 1.3.
2. Copiare `config.json` in `data/` e `key` in `secrets/`, poi rifare il `chown`.
3. Lanciare `docker compose up -d`.
4. Se il nome DNS è lo stesso, il totem continua a funzionare: il suo token è in
   `config.json`. Altrimenti eseguire `reset-pairing` e la procedura guidata ridotta.

---

## 6. Impostazioni avanzate (solo da file)

I valori di default sono nel codice. Si possono sovrascrivere solo da file, nell'oggetto
`impostazioni` di `config.json`. **Fermare il container prima di modificare il file**:
il server tiene la configurazione in memoria e la riscrive a ogni modifica dal totem.

```json
"impostazioni": { "intervalloScansione": "5m", "giorniAvviso": 2 }
```

| Chiave | Default | Effetto |
|---|---|---|
| `intervalloScansione` | `2m` | Ogni quanto il server legge le share |
| `ritardoStabilita` | `20s` | Attesa per la seconda lettura quando ci sono file in copia |
| `regexRevisione` | `(?i)(?:^\|[\s_.-]+)(?:rev(?:isione)?\|r)\.?[\s_-]*(\d{1,3})$` | Revisione nel nome del file (primo gruppo = numero) |
| `maxDimensionePDFMB` | `200` | PDF più grandi vengono scartati |
| `timeoutSMB` | `2m` | Tempo massimo per elencare una share |
| `tentativiPIN` / `bloccoPIN` | `5` / `5m` | Blocco dopo PIN errati |
| `scadenzaManutenzione` | `10m` | Uscita automatica dalla manutenzione (la fascia gialla riporta sempre "10 minuti") |
| `intervalloSync` / `intervalloHeartbeat` | `5m` / `5m` | Sync e heartbeat del totem |
| `giorniAvviso` | `3` | Dopo quanti giorni senza sync compare l'avviso giallo |
| `giorniRecenti` | `30` | Finestra di "Aggiornati di recente" |
| `inattivitaMinuti` | `3` | Ritorno alla Home per inattività |
| `finestraNotturna` | `02:00-05:00` | Quando si applicano gli aggiornamenti dell'app |
| `inattivitaAggiornamentoMinuti` | `15` | Inattività richiesta per aggiornare l'app |
| `margineSpazioMB` | `50` | Spazio che la sync lascia comunque libero sul totem |

Le chiavi sconosciute o i valori non validi si ignorano, con un avviso nei log all'avvio.
`globali.fase2` abilita le API di abbinamento dei telefoni, che in v1 non hanno ancora
un'interfaccia.

---

## 7. Sviluppo e test

```sh
# server (Go 1.24)
go test -race ./...

# integrazione contro Samba
docker compose -f docker-compose.test.yml up -d
go test -tags integration -count=1 ./test/integration/...
docker compose -f docker-compose.test.yml down

# PWA (Node 22)
cd web && npm ci && npm test && npm run build

# end-to-end: Playwright con Chromium desktop e tablet Android emulato, server Go vero e Samba in Docker
cd test/e2e && npm ci && npx playwright test

# immagine
docker build --build-arg VERSION=0.0.0-dev -t totem:dev .
```

La CI (`.github/workflows/ci.yml`) esegue tutto questo a ogni push. Il rilascio
(`release.yml`) parte dai tag `vX.Y.Z`.

Per provare il server in locale senza Docker:

```sh
cd web && npm run build && cd ..
TOTEM_DATA=./data TOTEM_SECRETS=./secrets TOTEM_ADDR=127.0.0.1:8080 go run ./cmd/totem
# poi aprire http://localhost:8080
```
