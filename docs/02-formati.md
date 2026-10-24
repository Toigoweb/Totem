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
  "schemaVersion": 1,
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
| `share.percorso` | `\\server\condivisione[\cartella…]`. Nessun `..`, nessun carattere di controllo. |
| `share.utente` | `DOMINIO\utente`, `utente@dominio` oppure `utente`. |
| `share.password` | `v1:` + base64(nonce 12 byte ‖ ciphertext AES-256-GCM). Associated data = id della sezione. |
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
/secrets/key                 32 byte casuali, permessi 0600
```
