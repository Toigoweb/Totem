# 2. Formato di manifest.json e config.json

## manifest.json

Si scarica da `GET /manifest.json` con il token di dispositivo. Il file sta su disco in
`/data/manifest.json` e si scrive in modo atomico (`manifest.json.tmp` → `fsync` → `rename`).

```json
{
  "schemaVersion": 1,
  "version": 128,
  "hash": "9d0c1c0e6b5e…(sha256 esadecimale)",
  "generatedAt": "2026-10-24T08:15:02+02:00",
  "settings": {
    "syncIntervalSec": 300,
    "heartbeatIntervalSec": 300,
    "staleDays": 3,
    "recentDays": 30,
    "idleMinutes": 3,
    "nightWindow": "02:00-05:00",
    "updateIdleMinutes": 15,
    "spaceMarginMB": 50
  },
  "sections": [
    { "id": "s_k3v9q2", "name": "Schede di sicurezza", "icon": "flacone", "order": 1, "emergency": false },
    { "id": "s_p81mza", "name": "Piani di emergenza",  "icon": "uscita",  "order": 2, "emergency": true  }
  ],
  "documents": [
    {
      "id": "s_k3v9q2/Solventi/Acetone_rev3.pdf",
      "section": "s_k3v9q2",
      "path": "Solventi/Acetone_rev3.pdf",
      "folder": "Solventi",
      "title": "Acetone",
      "rev": "3",
      "size": 412883,
      "mtime": "2026-10-20T11:02:40Z",
      "sha256": "3a7bd3e2360a3d29eea436fcfb7e44c735d117c42d1c1835420b6b9942dd4f1b",
      "firstSeen": "2025-03-02T09:00:11Z",
      "updated": "2026-10-20T11:06:12Z"
    }
  ]
}
```

| Campo | Significato |
|---|---|
| `version` | Intero monotono. Cresce di 1 solo quando cambia `hash`. |
| `hash` | SHA-256 della serializzazione canonica di `sections` + `documents`, ordinati per `id`. `settings` e `generatedAt` sono esclusi. |
| `id` documento | `<sezione>/<percorso relativo>`. Resta stabile tra le versioni dello stesso file. |
| `path` | Relativo alla cartella della share, con `/` come separatore. Non contiene mai server, nome della share o UNC. |
| `folder` | Prima sottocartella (vuota se il file sta alla radice). Serve per i chip "cartella". |
| `title` | Nome leggibile: senza estensione, `-` e `_` diventano spazi, revisione tolta. |
| `rev` | Revisione estratta dal nome con `regexRevisione`; stringa vuota se assente. |
| `firstSeen` / `updated` | Prima comparsa e ultimo cambio di SHA visti dal server. La PWA ne ricava i badge NUOVO e AGGIORNATO. |

I PDF si scaricano da `GET /api/files/<sha256>`, con `ETag: "<sha256>"`, `Range` e
`Cache-Control: private, max-age=31536000, immutable`.

Lo stato delle scansioni (share non raggiungibili, errori, ultime sync) **non** sta nel
manifest, perché cambierebbe l'hash a ogni scansione. Si legge da `GET /api/status`:

```json
{
  "serverVersion": "1.0.0",
  "now": "2026-10-24T08:16:00+02:00",
  "manifestVersion": 128,
  "sections": [
    { "id": "s_k3v9q2", "name": "Schede di sicurezza", "documents": 42,
      "scan": { "ok": true, "at": "2026-10-24T08:15:01+02:00", "error": "", "since": null } },
    { "id": "s_p81mza", "name": "Piani di emergenza", "documents": 3,
      "scan": { "ok": false, "at": "2026-10-24T08:15:01+02:00",
                "error": "unreachable", "since": "2026-10-24T07:40:00+02:00" } }
  ],
  "syncLog": [
    { "at": "2026-10-24T08:15:02+02:00", "added": 3, "updated": 1, "removed": 0, "errors": 0 }
  ]
}
```

Codici di `scan.error`: `auth`, `not_found`, `access_denied`, `unreachable`, `timeout`,
`invalid_path`, `no_password`, `io`.

## config.json

Sta in `/data/config.json`. Si scrive in modo atomico; prima di ogni migrazione di
`schemaVersion` si salva una copia in `/data/backup/config-<data>-v<n>.json`.
Le chiavi sono in italiano perché è l'unico file pensato per essere letto da una persona.

```json
{
  "schemaVersion": 2,
  "sezioni": [
    {
      "id": "s_k3v9q2",
      "nome": "Schede di sicurezza",
      "icona": "flacone",
      "ordine": 1,
      "emergenza": false,
      "share": {
        "percorso": "\\\\fileserver\\HSE\\SDS",
        "utente": "AZIENDA\\svc-totem",
        "password": "v1:6b1c…(nonce+ciphertext base64)",
        "sottocartelle": true
      }
    }
  ],
  "globali": {
    "pinHash": "$argon2id$v=19$m=65536,t=3,p=2$…",
    "fase2": false,
    "dispositivi": [
      {
        "id": "d_x1y2z3",
        "nome": "totem",
        "ruolo": "totem",
        "piattaforma": "windows",
        "tokenHash": "e3b0c442…(sha256 del token)",
        "creato": "2026-10-24T07:00:00+02:00",
        "ultimoContatto": "2026-10-24T08:15:30+02:00"
      }
    ]
  },
  "reparti": [
    { "id": "r_m4g4zz", "nome": "Magazzino", "sezioni": ["s_p81mza"] }
  ],
  "impostazioni": {
    "intervalloScansione": "2m",
    "giorniAvviso": 3
  }
}
```

| Campo | Regole |
|---|---|
| `sezioni[].icona` | Una tra: `documento`, `flacone`, `scudo`, `fiamma`, `croce`, `casco`, `uscita`, `mappa`, `ingranaggio`, `cartella`. |
| `sezioni[].nome` | Obbligatorio, 1-30 caratteri. |
| `share.tipo` | `smb` (predefinito se assente), `onedrive`, `webdav`, `upload`. |
| `share.percorso` | SMB: `\\server\condivisione[\cartella…]`, nessun `..` né caratteri di controllo. OneDrive: link https `*.sharepoint.com`, `onedrive.live.com` o `1drv.ms`. WebDAV: URL http(s) senza credenziali. Upload: vuoto (i file stanno in `/data/uploads/<id>`). |
| `share.utente` | SMB: `DOMINIO\utente`, `utente@dominio` oppure `utente`. WebDAV: utente. OneDrive: ID applicazione (GUID). |
| `share.tenant` | Solo OneDrive: ID (GUID) o dominio del tenant Microsoft 365. |
| `share.password` | Password SMB/WebDAV o segreto client OneDrive: `v1:` + base64(nonce 12 byte ‖ ciphertext AES-256-GCM). Associated data = id della sezione. |
| `globali.dispositivi[].ruolo` | `totem` (uno solo), `postazione` (altri PC con accesso alla manutenzione) o `telefono` (fase 2). |
| `globali.dispositivi[].tokenHash` | SHA-256 esadecimale del token del cookie `totem_device`. |
| `globali.dispositivi[].ultimoContatto` | Aggiornato dall'heartbeat, al massimo una volta al minuto. |
| `impostazioni` | Facoltativo. Solo le chiavi elencate in `docs/01-architettura.md`; le chiavi sconosciute si ignorano con un avviso nel log. |

Il dettaglio dell'ultimo heartbeat (versione dell'app, spazio libero, documenti, ultima
sync) sta in `/data/state/heartbeat.json`, per non riscrivere config.json a ogni contatto.

## Altri file in /data

```
/data/config.json            configurazione
/data/backup/                copie automatiche prima delle migrazioni
/data/manifest.json          ultimo manifest pubblicato
/data/manifest.prev.json     manifest precedente (i suoi blob restano per un ciclo)
/data/state/index.json       cache hash, candidati in attesa, firstSeen/updated, stato scansioni
/data/state/heartbeat.json   ultimo heartbeat ricevuto
/data/blobs/<sha256>         PDF validati
/data/blobs/tmp/             download in corso, ripuliti all'avvio
/data/logs/sync.log          log JSON per riga, rotazione a 5 MB × 5 file
/data/uploads/<sezione>/     PDF caricati direttamente (sezioni di tipo upload)
/data/emergency.json         emergenza in corso (o l'ultima terminata)
/data/safety.json            mappa incidenti: planimetrie ed eventi
/data/maps/<sha256>          immagini delle planimetrie (PNG o JPEG)
/secrets/key                 32 byte casuali, permessi 0600
```

## Novità della schemaVersion 2 di config.json

Campi aggiunti (la migrazione da 1 a 2 crea il backup e non trasforma nulla):

```json
{
  "reparti": [{ "id": "r_m4g4zz", "nome": "Magazzino", "sezioni": ["s_p81mza"] }],
  "globali": {
    "dispositivi": [{ "id": "d_x1y2z3", "ruolo": "totem", "reparto": "r_m4g4zz", "…": "…" }],
    "contatti": [{ "id": "c_1a2b3c", "etichetta": "Emergenze", "numero": "112", "reparti": [] }],
    "avvisi": [{ "id": "a_9z8y7x", "testo": "Esercitazione giovedì alle 10", "livello": "attenzione",
                 "dal": "2026-10-27", "al": "2026-10-30", "reparti": [] }],
    "salvaschermo": { "attivo": true, "dopoMinuti": 5, "notte": true, "orarioNero": "20:00-06:00" },
    "monitor": { "tokenHash": "…sha256…", "sogliaMinuti": 30 }
  }
}
```

| Campo | Regole |
|---|---|
| `reparti[].sezioni` | Id di sezioni esistenti; vuoto = tutte. Nomi unici, 1-30 caratteri, massimo 20 reparti. |
| `dispositivi[].reparto` | Solo per i totem; vuoto = tutte le sezioni. Postazioni e telefoni vedono tutto. |
| `contatti` | Massimo 8; numero con cifre, spazi e `+ / . -`; `reparti` vuoto = tutti. |
| `avvisi` | Massimo 10; testo 1-200 caratteri; `livello` `info` o `attenzione`; date `AAAA-MM-GG` incluse. |
| `monitor.tokenHash` | SHA-256 del token; vuoto = pagina di monitoraggio disattivata (404). |

## Campi aggiuntivi del manifest per dispositivo

Il server costruisce per ogni dispositivo una **vista** del manifest: sezioni e documenti
del suo reparto, più:

```json
{
  "role": "totem",
  "department": { "id": "r_m4g4zz", "name": "Magazzino" },
  "contacts": [{ "label": "Emergenze", "number": "112" }],
  "notices": [{ "id": "a_9z8y7x", "text": "Esercitazione giovedì alle 10", "level": "attenzione", "from": "2026-10-27", "to": "2026-10-30" }],
  "screensaver": { "enabled": true, "afterMinutes": 5, "night": true, "nightHours": "20:00-06:00" }
}
```

`hash` è calcolato sulla vista (contenuti e dati del reparto): se cambia un avviso, il
totem lo riprende alla sync successiva. `version` resta quella globale del server.

## Mappa incidenti

`/data/safety.json` (separato da config.json perché cresce nel tempo):

```json
{
  "schemaVersion": 1,
  "inizio": "2026-01-01",
  "mappe": [
    { "id": "m_a1b2c3", "nome": "Stabilimento", "immagine": "<sha256>", "formato": "image/png",
      "byte": 182340, "larghezza": 2400, "altezza": 1600, "reparti": [] }
  ],
  "eventi": [
    { "id": "e_d4e5f6", "mappa": "m_a1b2c3", "x": 0.31, "y": 0.42, "data": "2026-10-15",
      "tipo": "quasi", "nota": "muletto in retromarcia" }
  ]
}
```

| Campo | Regole |
|---|---|
| `inizio` | Data da cui la registrazione è completa. Senza, il contatore dei giorni senza infortuni non compare. |
| `mappe` | Massimo 10. Immagine PNG o JPEG, da 100 a 10 000 pixel per lato, massimo 15 MB. `reparti` vuoto = tutti. |
| `eventi[].x`, `y` | Posizione da 0 a 1 (frazione di larghezza e altezza), indipendente dalla risoluzione. |
| `eventi[].tipo` | `infortunio`, `medicazione`, `quasi` (quasi incidente), `danno` (danno a cose). |
| `eventi[].data` | AAAA-MM-GG, non nel futuro. Massimo 5000 eventi. |
| `eventi[].nota` | Facoltativa, massimo 200 caratteri. Solo manutenzione ed esportazione CSV: **non va mai nel manifest**. |

Nel manifest di ogni dispositivo, se ha almeno una planimetria visibile:

```json
"safety": {
  "maps": [{ "id": "m_a1b2c3", "name": "Stabilimento", "sha256": "…", "type": "image/png", "size": 182340, "width": 2400, "height": 1600 }],
  "events": [{ "map": "m_a1b2c3", "x": 0.31, "y": 0.42, "date": "2026-10-15", "type": "quasi" }],
  "lastInjury": "2026-09-01",
  "since": "2026-01-01"
}
```

Vanno al totem solo gli eventi degli ultimi 3 anni; `lastInjury` considera anche quelli più
vecchi. Le immagini si scaricano da `GET /api/maps/<sha256>` (cookie del dispositivo), con
verifica SHA-256, e restano in Cache Storage come i PDF.

## Tablet con FreeKiosk e emergenza

Un dispositivo in `globali.dispositivi[]` può avere il tablet collegato:

```json
"kiosk": {
  "tipo": "freekiosk",
  "indirizzo": "192.168.2.60:8080",
  "chiave": "v1:…(chiave API cifrata, associated data \"kiosk:<id del dispositivo>\")",
  "schermoNotte": true,
  "luminositaAuto": true,
  "posizione": [0.02, 9.79, 0.31]
}
```

`posizione` è il vettore di gravità di riferimento (primo controllo o "Posizione attuale
come riferimento"). Il token per avviare l'emergenza da sistemi esterni è in
`globali.emergenza.tokenHash` (SHA-256, come quello del monitoraggio).

`/data/emergency.json`:

```json
{ "attiva": true, "id": "em_k3v9q2", "tipo": "evacuazione", "testo": "Lasciare l'edificio…",
  "avviata": "2026-10-28T09:12:00Z", "da": "totem magazzino" }
```

`tipo`: `evacuazione` (testo predefinito se vuoto) o `avviso` (testo obbligatorio), massimo
200 caratteri. I dispositivi leggono `GET /api/emergency`:

```json
{ "active": true, "id": "em_k3v9q2", "type": "evacuazione", "title": "Evacuazione",
  "text": "Lasciare l'edificio…", "since": "2026-10-28T09:12:00Z" }
```

e lo conservano in IndexedDB (`meta.emergency`). Non passa dal manifest: deve arrivare in
pochi secondi, non alla sync successiva.

## Agente del PC e segnale periodico

Un dispositivo può avere l'agente del PC Windows:

```json
"agente": { "tokenHash": "e3b0c442…(sha256 del token)", "schermoNotte": true }
```

L'agente chiama `POST /api/agent/report` con `Authorization: Bearer <token>`:

```json
{ "versione": "1.0", "chrome": true, "rete": true, "batteria": null, "uptimeMin": 1440,
  "discoLiberoMB": 51200, "riavvioInAttesa": false, "windows": "Windows 11 Pro 24H2 (26100)",
  "schermoSpento": false, "fatti": [{ "id": "c_x1", "ok": true }] }
```

e riceve i comandi in coda (consegnati una volta sola), lo stato voluto dello schermo e
l'eventuale emergenza:

```json
{ "comandi": [{ "id": "c_x2", "azione": "screenshot" }], "schermoSpento": false, "intervalloSec": 30,
  "emergenza": { "id": "em_k3v9q2", "titolo": "Evacuazione", "testo": "Lasciare l'edificio…" } }
```

`azione`: `riavvia-chrome`, `riavvia-pc`, `schermo-on`, `schermo-off`, `screenshot` (il PNG
va a `POST /api/agent/screenshot`), `annuncio` (con `testo`). Lo stato degli agenti resta in
memoria sul server.

Il segnale periodico dell'app (`POST /api/heartbeat`) porta in più, facoltativi:
`battery` (`{ "level": 0-100, "charging": bool }`, solo se il dispositivo ha una batteria),
`browser` (es. "Chrome 131") e `view` (es. "Documento: Acetone"). I comandi all'app
(`ricarica`, `home`, `sincronizza`) arrivano nel campo `commands` di `GET /api/emergency`.

