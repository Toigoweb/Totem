# Totem documenti di sicurezza

Totem offline-first per consultare in reparto le schede di sicurezza, le schede prodotto e i
piani di emergenza che stanno sulle share SMB aziendali.

> **Stato del lavoro.** Fatti: architettura, formati, wireframe, server (scansione SMB,
> archivio, manifest, API, autenticazione), immagine Docker, CI e la logica di sync della
> PWA. Mancano: interfaccia (in attesa dell'approvazione dei wireframe), test end-to-end
> Playwright, README completo e checklist di messa in servizio.

## Documenti

- [Architettura e scelte](docs/01-architettura.md)
- [Formato di manifest.json e config.json](docs/02-formati.md)
- [Struttura del repository](docs/03-struttura-repository.md)
- Wireframe navigabili: `wireframes/index.html` (si apre direttamente nel browser)

## Sviluppo

```sh
# server (Go 1.24)
go test -race ./...

# test di integrazione contro Samba
docker compose -f docker-compose.test.yml up -d
go test -tags integration -count=1 ./test/integration/...
docker compose -f docker-compose.test.yml down

# PWA (Node 22)
cd web && npm ci && npm test && npm run build

# immagine
docker build --build-arg VERSION=0.0.0-dev -t totem:dev .
```

## Installazione rapida (provvisoria)

```sh
mkdir totem && cd totem
# copiare docker-compose.yml dal repository
mkdir data secrets && sudo chown -R 65532:65532 data secrets   # l'immagine gira come utente 65532
docker login ghcr.io            # una volta, con un PAT in sola lettura (read:packages)
docker compose up -d
docker compose logs totem       # contiene il CODICE DI SETUP per la procedura guidata
```

Comandi utili:

| Operazione | Comando |
|---|---|
| Sostituzione o ripristino del totem | `docker compose exec totem /totem reset-pairing` |
| Come sopra, azzerando anche il PIN | `docker compose exec totem /totem reset-pairing --reset-pin` |
| Con il container fermo | `docker compose run --rm totem reset-pairing` (il codice compare nei log al prossimo avvio) |
| Stato del container | `docker compose ps` (colonna `healthy`) |
