# Totem documenti di sicurezza

> Per riprendere lo sviluppo (altra persona o altro assistente AI) partire da
> [AGENTS.md](AGENTS.md).

Totem offline-first per consultare in reparto le schede di sicurezza (SDS), le schede
prodotto e i piani di emergenza che stanno sulle share SMB aziendali, su OneDrive/SharePoint,
su WebDAV o caricati direttamente dall'area manutenzione.

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
   | Scheme / Forward Hostname / Port | `http` / IP del server Docker (o nome del container, se NPM è sulla stessa rete Docker) / il valore di `TOTEM_PORT` (default `8080`; `8080` se si usa il nome del container) |
   | Access List | `LAN` |
   | Block Common Exploits | attivo |
   | Cache Assets | **disattivato**: `sw.js` e `index.html` non devono essere messi in cache dal proxy, altrimenti gli aggiornamenti dell'app non arrivano |
   | Websockets Support | non serve |
   | SSL Certificate | il certificato creato al punto 2 |
   | Force SSL, HTTP/2 Support, HSTS Enabled | attivi |

> Il cookie del totem è `Secure`, quindi l'app funziona **solo in HTTPS**, cioè passando
> da NPM. L'unica eccezione è `http://localhost`, comodo per le prove sul server stesso.

Se NPM gira su un altro host, conviene limitare la porta del totem (`TOTEM_PORT`) al solo
indirizzo di NPM con il firewall (per esempio con `ufw`).

### 1.2 Accesso a GHCR (una sola volta)

```sh
echo "<PAT con read:packages>" | docker login ghcr.io -u <utente-github> --password-stdin
```

Le credenziali restano in `~/.docker/config.json` dell'utente che esegue `docker compose`.
Con Portainer il login si fa invece da **Registries** (vedi 1.6).

### 1.3 Cartelle e compose

```sh
sudo mkdir -p /opt/totem && cd /opt/totem
# copiare qui docker-compose.yml dal repository (e, se servono, le variabili: .env)
sudo mkdir data secrets
sudo chown -R 65532:65532 data secrets    # il container gira come utente non root 65532
docker compose up -d
```

`docker-compose.yml` è quello del repository e non va modificato: le impostazioni
dell'installazione sono variabili, in un file `.env` accanto al compose (modello in
[deploy/totem.env.example](deploy/totem.env.example)) oppure nello stack di Portainer.

| Variabile | Default | Uso |
|---|---|---|
| `TOTEM_PORT` | `8080` | Porta sul server Docker, quella a cui punta NPM. Accetta anche `indirizzo:porta` (es. `192.168.10.5:8090`) per esporla su una sola scheda di rete. Dentro il container la porta resta 8080. |
| `TOTEM_DATA_DIR` | `/opt/totem/data` | Cartella dei dati (percorso assoluto) |
| `TOTEM_SECRETS_DIR` | `/opt/totem/secrets` | Cartella della chiave di cifratura (percorso assoluto) |
| `TOTEM_TAG` | vuoto | Vuoto = la versione scritta nel compose, aggiornata a ogni rilascio (aggiornamento automatico con Portainer). `1.0.0` = versione bloccata. `1` = ultima 1.x con `docker compose pull`. |
| `TZ` | `Europe/Rome` | Fuso orario |

Il filesystem del container è in sola lettura; si scrive solo nelle due cartelle.

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


### 1.6 Con Portainer, con aggiornamento automatico

In alternativa ai punti 1.2-1.4, lo stack si crea in Portainer direttamente dal repository
GitHub privato. Portainer controlla il repository a intervalli regolari e ridistribuisce
lo stack quando cambia `docker-compose.yml`. A ogni rilascio (tag `vX.Y.Z`) la GitHub
Action, dopo aver pubblicato l'immagine, aggiorna da sola la versione scritta nel compose:
così ogni nuova versione arriva sul server senza interventi.

1. **Cartelle sul server Docker** (una volta sola; Portainer non le crea con i permessi
   giusti):
   ```sh
   sudo mkdir -p /opt/totem/data /opt/totem/secrets
   sudo chown -R 65532:65532 /opt/totem/data /opt/totem/secrets
   ```
2. **Token GitHub** (*Settings → Developer settings → Personal access tokens*). Basta un
   token *classic* con `repo` (lettura del repository privato) e `read:packages`
   (immagini su GHCR). In alternativa due token separati: uno *fine-grained* con
   *Contents: Read-only* sul solo repository Totem, più uno *classic* con `read:packages`.
3. **Registro GHCR**: *Registries → Add registry → Custom registry*, nome `GHCR`, URL
   `ghcr.io`, *Authentication* attiva, utente GitHub e token con `read:packages`.
4. **Stack**: *Stacks → Add stack*, nome `totem`, *Build method* **Repository**:

   | Campo | Valore |
   |---|---|
   | Repository URL | `https://github.com/Toigoweb/Totem` |
   | Repository reference | il branch principale, es. `refs/heads/claude/new-session-75lftp` |
   | Compose path | `docker-compose.yml` |
   | Authentication | attiva: utente GitHub e token con lettura del repository |
   | GitOps updates | attivo, *Mechanism* **Polling**, *Fetch interval* `5m` |
   | Environment variables | `TOTEM_PORT` e, se diverse dal default, le cartelle. **Non** impostare `TOTEM_TAG`, altrimenti la versione resta bloccata |

   *Re-pull image* e *Force redeployment* non servono: a ogni rilascio cambia il tag
   dell'immagine nel compose, quindi Portainer vede la modifica e scarica l'immagine nuova.
5. **Deploy the stack**, poi il codice di setup è nei log del container (*Containers →
   totem → Logs*), come al punto 1.4.

I comandi `docker compose exec totem …` del resto di questo README diventano, sul server,
`docker exec totem-totem-1 /totem …` (il nome del container è in *Containers*), oppure si
usa la *Console* del container in Portainer con il comando `/totem …`.

**Aggiornamenti:** il totem si aggiorna entro l'intervallo di polling dal rilascio. Per
bloccare una versione: variabile `TOTEM_TAG=1.2.3` nello stack. Per tornare indietro: la
stessa variabile con la versione precedente (si veda anche "Rollback e formato della
configurazione" al punto 4). Per riprendere gli aggiornamenti automatici: togliere la
variabile.
---

## 2. Gestione dei PDF sulle share (per chi mantiene i documenti)

Ogni sezione prende i PDF da una di quattro origini, scelta nel form della sezione
("Da dove arrivano i PDF"). Le regole qui sotto valgono per tutte: il totem riceve i
documenti sempre e solo dal server, e li conserva offline allo stesso modo.

| Origine | Quando usarla | Cosa serve |
|---|---|---|
| Cartella di rete (SMB) | File server o NAS aziendale | `\\server\condivisione\cartella`, utente e password in sola lettura |
| OneDrive / SharePoint | Documenti già gestiti in Microsoft 365 | Link della cartella, app registrata in Entra ID (vedi 2.1) |
| WebDAV | Nextcloud, ownCloud, Synology, QNAP… | URL WebDAV della cartella, utente e password |
| Caricamento diretto | Nessuna cartella condivisa disponibile, pochi documenti | Niente: i PDF si caricano dall'area manutenzione (vedi 2.3) |

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

### 2.1 OneDrive / SharePoint

Il server legge la cartella con Microsoft Graph, in sola lettura, a nome di un'app
registrata nel tenant (non di un utente: nessuna password personale, nessun MFA).
Una volta sola, un amministratore Microsoft 365:

1. [Entra ID](https://entra.microsoft.com) → **Registrazioni app** → **Nuova
   registrazione**, nome "Totem documenti di sicurezza", account solo di questa
   organizzazione. Annotare **ID applicazione (client)** e **ID directory (tenant)**.
2. **Certificati e segreti** → **Nuovo segreto client**. Annotare il **valore** (si vede
   una volta sola) e la scadenza: alla scadenza il totem mostra "Credenziali dell'app non
   valide" e il segreto va rinnovato e reinserito nella sezione.
3. **Autorizzazioni API** → **Aggiungi** → Microsoft Graph → **Autorizzazioni
   applicazione** → `Files.Read.All` (OneDrive e SharePoint) oppure `Sites.Read.All`
   (solo SharePoint). Poi **Concedi consenso amministratore**.
   Per limitare l'app a un solo sito SharePoint si può usare `Sites.Selected` e concedere
   la lettura del sito all'app (procedura Microsoft "Sites.Selected").
4. In OneDrive o SharePoint, clic destro sulla cartella → **Copia collegamento**.

Nel form della sezione: tipo "OneDrive / SharePoint", il link copiato, il tenant (ID o
dominio, es. `azienda.onmicrosoft.com`), l'ID applicazione e il segreto, poi "Prova
connessione". Il segreto è cifrato come le password SMB.

Il server deve poter uscire in HTTPS verso `login.microsoftonline.com` e
`graph.microsoft.com` (eventuale proxy: variabile `HTTPS_PROXY` nel compose). I PDF si
scaricano solo quando cambiano; un elenco ogni 2 minuti resta ampiamente sotto i limiti
di Graph, che comunque vengono rispettati (attesa `Retry-After`).

### 2.2 WebDAV (Nextcloud, NAS)

Indirizzo della cartella, per esempio:
- Nextcloud/ownCloud: `https://cloud.azienda.it/remote.php/dav/files/<utente>/SDS`
- Synology (WebDAV Server attivo): `https://nas.azienda.local:5006/HSE/SDS`
- QNAP: `https://nas.azienda.local:8081/HSE/SDS`

Per Nextcloud conviene una **password per applicazioni** (Impostazioni → Sicurezza) di un
utente con la cartella condivisa in sola lettura. `http://` è ammesso solo per server in
rete interna: la password viaggerebbe in chiaro.

### 2.3 Caricamento diretto

Per le sezioni di tipo "Caricamento diretto" i PDF si caricano dal totem o da una
postazione: **Manutenzione → Carica PDF** sulla riga della sezione (alla creazione si apre
da solo). Si possono scegliere più file insieme; la sottocartella facoltativa diventa un
filtro a chip. Un file con lo stesso nome di uno esistente lo sostituisce (nuova
revisione). Limite: 200 MB per file (impostazione `maxDimensionePDFMB`).

I PDF caricati stanno sul server in `/opt/totem/data/uploads/<sezione>/` e passano dagli
stessi controlli delle altre origini (PDF valido, completo). Eliminare la sezione o
cambiarne il tipo elimina anche i file caricati: vanno inclusi nel backup.

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
| "Serve l'indirizzo HTTPS del totem" (le versioni fino alla 1.0.0 mostravano una pagina bianca) | Aperto in `http://IP:porta`: senza HTTPS il browser non permette cache offline e service worker | Aprire `https://totem.azienda.it` (il proxy host di NPM). Per una prova sul server stesso va bene `http://localhost:<TOTEM_PORT>` |
| NPM risponde *502 Bad Gateway* | NPM non raggiunge il container | Nel proxy host: *Scheme* `http` (non https); *Forward Hostname* = IP del server Docker (non `localhost`/`127.0.0.1`, che dentro il container di NPM indicano NPM stesso); *Port* = `TOTEM_PORT`. Il nome del container con porta `8080` funziona solo se NPM è sulla stessa rete Docker dello stack. Prova dal server: `curl http://IP-del-server:<TOTEM_PORT>/api/health` |
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
sudo mkdir -p /opt/totem-test/data /opt/totem-test/secrets && cd /opt/totem-test
sudo chown -R 65532:65532 data secrets
# stesso docker-compose.yml del repository, con variabili diverse
cat > .env <<'ENV'
TOTEM_PORT=8081
TOTEM_DATA_DIR=/opt/totem-test/data
TOTEM_SECRETS_DIR=/opt/totem-test/secrets
#TOTEM_TAG=1.1.0     # per provare una versione precisa
ENV
docker compose up -d && docker compose logs totem   # codice di setup dell'istanza di prova
```

Con Portainer: un secondo stack (es. `totem-test`) dallo stesso repository, con queste
variabili.

In NPM aggiungere un secondo proxy host `totem-test.azienda.it` verso la porta `8081`,
con lo stesso certificato (o uno nuovo) e la stessa access list. L'istanza di prova ha
PIN, sezioni e dispositivi suoi: si abbina un PC di prova con la procedura guidata e si
usano cartelle di prova sulle share.

---

## Reparti, numeri utili, avvisi, salvaschermo, monitoraggio

Tutto si imposta da **Area manutenzione → Altre impostazioni** (sul totem o da una
postazione). I dati arrivano ai totem con la sincronizzazione e funzionano anche offline.

### Più totem e reparti

- **Reparti** (facoltativi): un reparto ha un nome e le sezioni che vede; senza sezioni
  scelte vede tutto. Senza reparti tutti i totem vedono tutto, come prima.
- **Aggiungere un totem:** Dispositivi → **Aggiungi totem** → scegliere il reparto →
  **Genera codice**. Sul nuovo dispositivo: indirizzo del totem → "Inizia configurazione" →
  codice. Dal server: `docker compose exec totem /totem abbina-totem --reparto "Magazzino"`.
  Tutti i totem usano lo stesso PIN.
- **Spostare un totem di reparto:** Dispositivi → menu "Reparto" sulla riga del totem.
- **Sostituire un totem** (guasto, passaggio da PC a tablet): Dispositivi → **Sostituisci**
  sulla riga del totem → codice da usare sul dispositivo nuovo, che eredita nome e reparto.
  Il vecchio smette di aggiornarsi (i documenti già scaricati restano leggibili).
- I reparti decidono **cosa si mostra**, non sono un confine di sicurezza: tutti i
  dispositivi abbinati sono considerati fidati.

### Numeri utili

Fino a 8 numeri (descrizione e numero), per tutti i reparti o solo per alcuni. Compaiono a
destra nella fascia rossa EMERGENZA della Home e nel salvaschermo. Se non ci sono sezioni
di emergenza, la fascia compare comunque con i soli numeri.

### Avvisi

Fino a 10 messaggi (massimo 200 caratteri) di tipo **Avviso** o **Attenzione**, con date di
inizio e fine incluse (facoltative) e reparti. Compaiono sotto la ricerca in Home; il
totem controlla le date da sé, anche offline. Gli avvisi scaduti spariscono da soli.

### Salvaschermo

Solo sui totem (non sulle postazioni). Dopo N minuti senza interazioni (default 5) mostra a
tutto schermo ora, numeri utili e avvisi. Facoltativo lo **schermo nero fuori orario**
(default 20:00-06:00) con la sola ora, attenuata. Il contenuto si sposta un poco ogni
minuto per non segnare lo schermo. Un tocco o un tasto lo chiude, senza attivare quello che
sta sotto. Per spegnere davvero la retroilluminazione usare gli orari di Fully Kiosk
(tablet) o di Windows (PC).

### Mappa incidenti

Mostra sul totem **dove** succedono gli eventi, per far vedere la situazione a tutti.
Da **Manutenzione → Mappa incidenti**:

1. **Planimetrie**: nome e immagine PNG o JPEG (pianta, foto o scansione; da un PDF va
   esportata come immagine), fino a 10, per tutti i reparti o solo per alcuni.
2. **Eventi**: si tocca la planimetria nel punto giusto e si indicano data e tipo:
   infortunio, medicazione, quasi incidente, danno a cose. Toccando un punto lo si
   modifica o elimina. La **nota** è facoltativa e resta in manutenzione e nell'export:
   non compare sul totem. Non scriverci nomi né dati sulla salute.
3. **Registrazione completa dal**: la data da cui tutti gli eventi sono segnati qui.
   Attiva il contatore **giorni senza infortuni** in Home, nella mappa e nel salvaschermo.
   Senza questa data il contatore non compare, perché non sarebbe affidabile.
4. **Esporta eventi (CSV)**: elenco completo con note, separato da `;`, si apre in Excel.

Sul totem, il riquadro "Situazione sicurezza" in Home apre la mappa di calore: periodo
(3 mesi, 12 mesi, 3 anni), tipi da includere (toccando i conteggi), punti con una forma
diversa per tipo. Gli infortuni pesano più dei quasi incidenti. Funziona offline.

La mappa non sostituisce il registro e le denunce di infortunio previsti dalla legge.

### Monitoraggio

Monitoraggio → **Genera token**: il token si mostra una volta sola. Poi:

```sh
curl -H "Authorization: Bearer <token>" https://totem.azienda.it/api/monitor               # JSON
curl -H "Authorization: Bearer <token>" "https://totem.azienda.it/api/monitor?formato=testo"
```

- **HTTP 200** se tutto va; **HTTP 503** se una sezione non è leggibile dal server o un
  totem non si fa vivo da più della soglia (default 30 minuti, modificabile). I documenti
  scartati compaiono come avvisi ma non danno errore.
- In formato testo la prima riga è `TOTEM OK` o `TOTEM ERRORE`: in **Uptime Kuma** basta un
  monitor HTTP(s) con l'header `Authorization` (o una parola chiave `TOTEM OK`).
- Il token si può passare anche come `?token=…`, ma finisce nei log del proxy: meglio l'header.
- Se i totem si spengono di notte, alzare la soglia o mettere il monitor in pausa nelle ore
  di spegnimento.
- Sul server, senza token: `docker compose exec totem /totem stato` (uscita 1 se c'è un
  errore, utile in uno script).

---

## 4. Aggiornamento e rollback

Le immagini si pubblicano su `ghcr.io/toigoweb/totem` con i tag `X.Y.Z`, `X.Y` e `X`. Il
compose usa la versione dell'ultimo rilascio, che la GitHub Action aggiorna da sola nel
repository dopo aver pubblicato l'immagine: con Portainer (1.6) l'aggiornamento è automatico.

| Operazione | Comando o azione |
|---|---|
| Aggiornamento con Portainer | Automatico entro l'intervallo di polling (1.6) |
| Aggiornamento con docker compose | Copiare il `docker-compose.yml` aggiornato dal repository, poi `docker compose up -d`. Oppure `TOTEM_TAG=1` nel `.env` e `docker compose pull && docker compose up -d` |
| Bloccare una versione | Variabile `TOTEM_TAG=1.2.3` (`.env` o stack Portainer) |
| Rollback | `TOTEM_TAG` con la versione precedente (per esempio `1.2.2`), poi *Update the stack* in Portainer o `docker compose up -d` |
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
| Chiave di cifratura | `/opt/totem/secrets/key` | Senza la chiave le password (SMB, WebDAV) e i segreti OneDrive vanno reinseriti dal totem. Conservarla separata dal backup di `data`. |
| Logo | `/opt/totem/data/logo.*` | Se presente |
| Mappa incidenti | `/opt/totem/data/safety.json` e `data/maps/` | **Da salvare**: planimetrie ed eventi esistono solo qui |
| PDF caricati | `/opt/totem/data/uploads/` | **Da salvare**: per le sezioni a caricamento diretto è l'unica copia |
| PDF delle altre origini | non serve | Si ricostruiscono da share, OneDrive o WebDAV alla prima scansione |

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
