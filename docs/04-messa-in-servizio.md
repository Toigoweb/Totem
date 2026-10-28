# 4. Messa in servizio del totem

Checklist operative. Il server deve essere già installato (README, sezione 1): DNS,
certificato e access list in NPM, container avviato, codice di setup visibile in
`docker compose logs totem`.

Prima di iniziare servono:

- l'URL del totem, per esempio `https://totem.azienda.it`;
- il **codice di setup**, dai log del server;
- il **PIN di manutenzione** da usare: 4-6 cifre, da conservare nel gestore password IT;
- per la prima sezione: percorso `\\server\condivisione\cartella`, utente `DOMINIO\utente`
  e password dell'account di servizio in sola lettura.

> **Da verificare sul posto.** Alcune voci dipendono dal dispositivo e dalla versione del
> software e non si possono provare in laboratorio: comportamento di `persist()`, nomi dei
> menu di Fully Kiosk, criteri di gruppo della specifica edizione di Windows. Le prove
> indicate con ☐ **Verifica** servono proprio a questo: annotare l'esito nella scheda del
> totem.

---

## Fase A – PC Windows

### A1. Hardware e sistema

- ☐ Windows 10/11 **Pro** o Enterprise (i criteri di gruppo locali non ci sono nella
  edizione Home).
- ☐ Rete cablata, se possibile. Dal PC `https://totem.azienda.it` risponde e il PC sta in
  una subnet dell'access list "LAN" di NPM.
- ☐ Nel BIOS/UEFI: **accensione automatica al ritorno della corrente** ("Restore on AC
  power loss: Power On"), così dopo un blackout il totem riparte da solo.
- ☐ Almeno 10 GB liberi su disco: il browser li usa per i PDF offline.
- ☐ Ora di sistema sincronizzata con il dominio o con NTP (data giusta = avvisi giusti).

### A2. Account

- ☐ Un account amministratore separato, per la manutenzione del PC.
- ☐ Un **utente locale standard** dedicato, per esempio `totem`, con password (serve ad
  Autologon). Non deve essere amministratore.

### A3. Browser

- ☐ Installare **Google Chrome** per tutti gli utenti (MSI enterprise), per mano
  dell'amministratore.
- ☐ ⚠ **Non usare la modalità kiosk di Edge con Accesso assegnato** (Assigned Access /
  "Configura un chiosco"): gira in InPrivate e a ogni sessione cancella Cache Storage,
  IndexedDB e cookie, cioè tutti i documenti offline e l'abbinamento. Per lo stesso motivo
  non usare profili ospite o InPrivate.
- Se si vuole comunque Edge al posto di Chrome: avviarlo dallo stesso watchdog con
  `msedge.exe --app=https://totem.azienda.it --start-fullscreen --user-data-dir=C:\TotemKiosk\profilo-edge`
  (profilo persistente, niente `--kiosk` e niente Accesso assegnato).
  - ☐ **Verifica:** in `edge://version` la riga "Percorso profilo" punta alla cartella
    indicata e non a un profilo InPrivate.
  - ☐ **Verifica:** dopo un riavvio del PC la schermata Stato mostra ancora tutti i
    documenti senza riscaricarli.

### A4. Script del kiosk

Da amministratore, nella cartella `deploy/windows` del repository:

```powershell
powershell -ExecutionPolicy Bypass -File installa-kiosk.ps1 -Url https://totem.azienda.it -Utente totem
```

Lo script:
- copia il watchdog `totem-kiosk.ps1` in `C:\TotemKiosk`;
- crea l'attività pianificata **"Totem - kiosk"**: all'accesso di `totem` avvia Chrome
  con `--kiosk` e il profilo persistente `C:\TotemKiosk\profilo`, e lo riapre entro 10
  secondi se si chiude;
- crea l'attività **"Totem - riavvio notturno browser"**: alle 03:30 chiude Chrome e il
  watchdog lo riapre. È anche il momento in cui si applicano gli aggiornamenti dell'app;
- imposta l'alimentazione: mai sospensione né ibernazione, schermo sempre acceso.

**Agente del PC (consigliato).** Con l'agente il server vede lo stato del PC e può
comandarlo (README, "PC Windows: agente e controllo"):
- ☐ Dopo la procedura guidata: Manutenzione → Dispositivi → **Controllo** sulla riga del
  totem → **Genera token dell'agente** e copiare il comando mostrato.
- ☐ Rilanciare da amministratore `installa-kiosk.ps1` con `-TokenAgente <token>`: crea
  l'attività **"Totem - agente"** e salva il token in `C:\TotemKiosk\agente.token`
  (leggibile solo da `totem` e dagli amministratori).
- ☐ Disconnettersi e riaccedere come `totem` (o riavviare): entro un minuto Controllo
  mostra Windows, Chrome "in esecuzione" e l'alimentazione.
- ☐ Prova **Screenshot**; con gli altoparlanti, prova di emergenza in orario concordato
  (annuncio in italiano: serve la voce italiana di Windows, presente sui Windows in
  italiano; altrimenti Impostazioni → Ora e lingua → Voce → Aggiungi voci → Italiano).
- ☐ Facoltativo: **Spegni lo schermo di notte** nella stessa schermata (al posto di
  orari con `powercfg`).

Il log dell'agente è in `C:\TotemKiosk\agente.log`.

Il log del watchdog è in `C:\TotemKiosk\watchdog.log`.

Gli script sono provati a ogni push dalla CI su un Windows vero (job `windows-kiosk`):
- sintassi e codifica leggibile da Windows PowerShell 5.1;
- installazione per un utente di prova, con le tre attività pianificate e i permessi del
  token dell'agente;
- agente contro il server vero (compilato sul momento): stato del PC ricevuto e
  segnalato, prova locale dello screenshot;
- avvio di Chrome con il profilo del totem e riapertura dopo la chiusura.

Restano da provare sul posto Autologon, i criteri di gruppo e il comportamento con uno
schermo touch.

### A5. Accesso automatico

- ☐ Scaricare **Sysinternals Autologon** e, da amministratore, eseguire
  `Autologon64.exe totem <NOMEPC> <password> /accepteula`. La password viene salvata
  cifrata come segreto LSA.
- ☐ Riavviare: il PC entra da solo come `totem` e dopo qualche secondo compare l'app a
  schermo intero.

### A6. Criteri di gruppo (solo per l'utente `totem`)

Da amministratore: `mmc` → File → Aggiungi/Rimuovi snap-in → **Editor oggetti Criteri di
gruppo** → Sfoglia → scheda **Utenti** → `totem` (oppure "Non amministratori"). Così i
blocchi non valgono per l'amministratore.

Configurazione utente → Modelli amministrativi:

| Percorso | Criterio | Valore |
|---|---|---|
| Sistema → Opzioni CTRL+ALT+CANC | Rimuovi Gestione attività | Attivato |
| Sistema → Opzioni CTRL+ALT+CANC | Rimuovi Blocca computer, Rimuovi Cambia password | Attivato |
| Componenti di Windows → Esplora file | Disattiva i tasti di scelta rapida di Windows (Win+X) | Attivato |
| Menu Start e barra delle applicazioni → Notifiche | Disattiva notifiche di tipo avviso popup | Attivato |
| Menu Start e barra delle applicazioni | Disattiva tutte le notifiche dei fumetti | Attivato |
| Componenti di Windows → Interfaccia utente bordi | Consenti scorrimento rapido dal bordo (schermi touch) | Disattivato |

Configurazione computer → Modelli amministrativi (questi valgono per tutto il PC):

| Percorso | Criterio | Valore |
|---|---|---|
| Sistema → Accesso | Nascondi punti di ingresso per Cambio rapido utente | Attivato |
| Componenti di Windows → Windows Update (→ Gestisci esperienza utente finale) | Configura aggiornamenti automatici | 4 – Download automatico e installazione pianificata, ogni giorno alle 03:00 |
| idem | Disattiva riavvio automatico per gli aggiornamenti durante l'orario di attività | Attivato, orario 06:00–23:00 |

- ☐ `gpupdate /force` e riavvio.
- ☐ **Verifica:** come `totem` non si aprono Gestione attività, Win+X, il cambio utente,
  le notifiche né lo scorrimento dal bordo.

### A7. Prima configurazione e prove

1. ☐ All'avvio compare "Totem non ancora configurato" → **Inizia configurazione**.
2. ☐ Passo 1: codice di setup. Passo 2: nuovo PIN e conferma. Passo 3: prima sezione con
   "Prova connessione" riuscita. Passo 4: prima sincronizzazione. La Home si apre da
   sola; le altre sezioni si aggiungono da **Area manutenzione**.
   - ☐ Sezioni **OneDrive / SharePoint**: app registrata con consenso amministratore
     (README 2.1), scadenza del segreto annotata nella scheda del totem, uscita HTTPS del
     server verso `login.microsoftonline.com` e `graph.microsoft.com`.
   - ☐ Sezioni a **caricamento diretto**: PDF caricati con "Carica PDF", cartella
     `/opt/totem/data/uploads/` inclusa nel backup.
3. ☐ **Prove funzionali:**
   - aprire una SDS e premere PRIMO SOCCORSO;
   - cercare un prodotto;
   - pressione lunga di 2 s sul logo → tastierino PIN → Annulla;
   - Stato → "Aggiorna ora".
4. ☐ **Prova offline:**
   - staccare il cavo di rete: la pillola diventa "Offline – documenti disponibili";
   - aprire un PDF;
   - **riavviare il PC senza rete**: l'app deve ripartire e mostrare i documenti.
5. ☐ **Verifica `persist()`:** nella schermata Stato leggere la riga "Memoria persistente"
   e annotarla (vedi sotto).
6. ☐ Riattaccare la rete e controllare che la pillola torni "Aggiornato alle …".
7. ☐ Sul server `cat /opt/totem/data/state/heartbeat.json` mostra `"platform": "windows"`
   e l'ora dell'ultimo contatto.

### A8. Uscire dal kiosk per manutenzione

- **Ctrl+Alt+Canc → Disconnetti**. Nella schermata di accesso tenere premuto **Maiusc**
  mentre Windows tenterebbe l'accesso automatico, poi entrare come amministratore.
- Per chiudere solo il browser (per esempio per una prova): Alt+F4. Il watchdog lo
  riapre entro 10 secondi.
- Per sospendere il kiosk a lungo: disattivare l'attività "Totem - kiosk" in Utilità di
  pianificazione, dall'account amministratore.

### A9. Limiti noti della fase A

- Senza schermo touch servono mouse e tastiera; l'app li supporta del tutto (pressione
  lunga del logo anche con il mouse, PIN da tastiera, frecce per le pagine).
- Ctrl+Alt+Canc non si può bloccare via criteri (è una sequenza di sicurezza di Windows):
  rimuovere Gestione attività e cambio utente limita cosa si raggiunge da lì.
- Gli aggiornamenti di Chrome si installano al riavvio del browser, quindi di notte con il
  riavvio delle 03:30.

---

## Memoria persistente (`navigator.storage.persist()`)

L'app la chiede all'avvio e mostra l'esito nella schermata Stato.

| Piattaforma | Comportamento atteso | Cosa vuol dire per il totem |
|---|---|---|
| Chrome / Edge su Windows | Nessuna domanda all'utente: il browser concede o nega con criteri propri (interazione con il sito, app installata, preferiti). In un profilo nuovo è normale vedere "non concessa" i primi giorni. | Anche se negata, i dati si cancellano solo quando il disco è quasi pieno, e per intero, non a metà. Con 10 GB liberi non succede. Se succedesse, la sync seguente riscarica tutto. |
| WebView di Fully Kiosk (Android) | `persist()` di solito non è concessa o non è significativa: i dati stanno nella memoria dell'app Fully e non subiscono lo sfratto di Chrome. | Si perdono solo cancellando i dati di Fully o attivando nelle sue impostazioni la pulizia di cache, archiviazione web o cookie (vedi B4). |

- ☐ **Verifica su ogni totem:** dopo un riavvio senza rete i documenti ci sono ancora, e
  la riga "Memoria persistente" della schermata Stato è annotata nella scheda del totem.

---

## Fase B – Tablet Android

### B1. Tablet

- ☐ Android 10 o successivo, schermo ≥ 10" orizzontale, Wi-Fi aziendale con DNS interno.
- ☐ **Android System WebView aggiornata** (Play Store) prima di installare Fully.
- ☐ Sempre in carica; se il modello lo prevede, **limite di carica all'85%** (su Samsung:
  Impostazioni → Batteria → Protezione batteria).
- ☐ **Aggiornamenti di sistema automatici disattivati**: Impostazioni → Aggiornamento
  software → disattivare il download automatico. Fully e WebView si aggiornano a mano,
  durante una manutenzione, ripetendo le prove di B5.
- ☐ Blocco schermo: nessuno (o scorrimento), così il riavvio non si ferma sul PIN di
  Android.

### B2. App kiosk: FreeKiosk (consigliata) o Fully Kiosk Browser Plus

**FreeKiosk** è gratuita e open source e ha l'API che il server usa per stato del tablet,
comandi, schermo spento di notte e annuncio vocale di emergenza. Installazione e
configurazione passo per passo: README, "Tablet Android con FreeKiosk". Checklist:

- ☐ IP fisso del tablet (prenotazione DHCP) annotato nella scheda del totem.
- ☐ Certificato della CA interna installato sul tablet.
- ☐ Account rimossi, APK installato, **Device Owner** attivo
  (`adb shell dpm list-owners` mostra `com.freekiosk`).
- ☐ FreeKiosk: URL del totem, PIN di FreeKiosk (diverso da quello del totem), **REST API**
  attiva con chiave lunga e diversa per ogni tablet, salvaschermo e pulizia di
  cache/cookie disattivati.
- ☐ Firewall: la porta dell'API del tablet raggiungibile **solo dal server Docker**.
- ☐ Dopo la procedura guidata: Manutenzione → Dispositivi → **Tablet** → indirizzo e
  chiave → Prova connessione → Salva. Lo stato mostra batteria "in carica".
- ☐ Prova: **Screenshot** dalla manutenzione di una postazione mostra la Home del totem.
- ☐ Prova di emergenza in orario concordato: Manutenzione → Emergenza → Avvia; il tablet
  accende lo schermo e legge il messaggio; poi Termina.

Con FreeKiosk i punti B4 (impostazioni di Fully) non servono; B1, B3, B5 e B6 valgono
uguali. **Con Fully Kiosk** (licenza PLUS, a pagamento) il totem funziona allo stesso modo
ma il server non può controllare il tablet:

- ☐ Installare Fully Kiosk Browser dal sito ufficiale o dal Play Store e attivare la
  licenza **PLUS** per il dispositivo.
- ☐ Concedere i permessi richiesti dalla modalità kiosk (app predefinita della Home,
  sovrapposizione ad altre app, amministratore del dispositivo dove richiesto).

### B3. Prima configurazione, ancora senza kiosk

Fare la procedura guidata **prima** di bloccare il kiosk, così si può ancora intervenire.

1. ☐ In Fully: Start URL `https://totem.azienda.it`.
2. ☐ Procedura guidata come in A7, oppure quella ridotta se si sostituisce il PC (vedi
   "Passaggio dalla fase A alla fase B").

### B4. Impostazioni di Fully

> I nomi delle voci cambiano tra le versioni di Fully: usare la ricerca nelle impostazioni.

| Area | Impostazione | Valore |
|---|---|---|
| Web Content | Start URL | `https://totem.azienda.it` |
| Web Content / Advanced Web Settings | JavaScript, cookie, archiviazione web (DOM storage), service worker | attivi (non disattivarli) |
| Advanced Web Settings | Pulizia di cache, cookie o archiviazione web a ogni ricarica, all'avvio o in base a orari | **tutte disattivate**: cancellerebbero i documenti offline e l'abbinamento |
| Web Auto Reload | Ricarica al ritorno della connessione, ricarica in inattività | disattivate (lo fa già l'app senza interrompere la lettura) |
| Web Browsing | Pull to refresh, navigazione indietro con gesto | disattivati |
| Web Zoom and Scaling | Zoom | disattivato (lo zoom c'è nel visualizzatore PDF) |
| Device Management | Avvio all'accensione (Launch on Boot) | attivo |
| Device Management / Power | Schermo sempre acceso, oppure orari di accensione e spegnimento | secondo il reparto |
| Device Management / Power | Riavvio programmato ogni notte (app o dispositivo, se il modello lo consente) | 03:30 |
| Other / Advanced | Riavvio automatico dell'app dopo un crash | attivo |
| Kiosk Mode (PLUS) | Modalità kiosk | attiva |
| Kiosk Mode | Blocco della barra di stato, tasti Home/Recenti/Indietro, pulsante di accensione | bloccati |
| Kiosk Mode | Uscita dal kiosk | protetta da **password/PIN di Fully** (diverso dal PIN del totem), conservato dall'IT |
| Remote Administration | Facoltativa: solo in LAN e con password | — |
| Advanced Web Settings | Debug dei contenuti WebView | disattivato in produzione |

### B5. Primo test sul tablet (service worker, Cache Storage, persist)

1. ☐ Dopo la prima sincronizzazione la schermata Stato mostra lo **spazio** (barra usato
   e disponibile) e la riga "Memoria persistente": annotarla.
2. ☐ **Service worker e Cache Storage:**
   - disattivare il Wi-Fi;
   - chiudere e riaprire Fully: l'app si apre e i PDF si leggono;
   - la pillola dice "Offline – documenti disponibili".
3. ☐ **Riavvio senza rete:** spegnere e riaccendere il tablet con il Wi-Fi spento. Fully
   riparte da solo, l'app si apre e i documenti ci sono.
4. ☐ Riattivare il Wi-Fi: entro pochi secondi la pillola torna "Aggiornato alle …".
5. ☐ **Tocco:**
   - pinch e swipe nel visualizzatore;
   - pressione lunga di 2 s sul logo → tastierino PIN (verificare che Fully non mostri un
     menu contestuale);
   - ricerca con la tastiera a schermo.
6. ☐ Sul server `heartbeat.json` mostra `"platform": "android"`.

### B6. Limiti noti della WebView rispetto a Chrome

- Il browser non chiede di "installare" la PWA e non serve: Fully è già a schermo intero.
- `persist()` non è significativa (vedi sopra); la sicurezza dei dati dipende dalle
  impostazioni di pulizia di Fully, che vanno lasciate disattivate.
- La WebView si aggiorna con Android System WebView: una versione molto vecchia può non
  avere le funzioni usate (service worker, Cache Storage, moduli ES). Tenerla aggiornata
  nelle manutenzioni.
- Lo spazio disponibile dipende dalla memoria libera del tablet: tenerne almeno 2-3 GB.

---

## Passaggio dalla fase A alla fase B

La sostituzione del dispositivo non tocca né la configurazione del server né le sezioni.

1. ☐ **Sul server:**
   ```sh
   cd /opt/totem
   docker compose exec totem /totem reset-pairing
   ```
   Il comando invalida i token di tutti i dispositivi (il PC smette di aggiornarsi e
   mostra "Abbinamento da rifare", ma i suoi documenti restano leggibili) e stampa un
   nuovo codice di setup. PIN e sezioni restano invariati.
2. ☐ **Sul tablet** (B1-B2 già fatti): Start URL in Fully, poi **Inizia configurazione**.
   La procedura guidata è ridotta: codice di setup, poi **PIN attuale**, poi prima
   sincronizzazione. Il passo "Prima sezione" si salta, quindi dopo il codice
   l'indicatore diventa "Passo 2 di 3".
3. ☐ **Verifica della prima sincronizzazione:**
   - la Home mostra tutte le sezioni con il numero corretto di documenti;
   - la schermata Stato dice "Tutto aggiornato";
   - `heartbeat.json` sul server mostra `"platform": "android"`;
   - eseguire le prove di B5, poi bloccare il kiosk (B4).
4. ☐ **Dismissione del PC:**
   - da amministratore: Utilità di pianificazione → eliminare "Totem - kiosk" e "Totem -
     riavvio notturno browser";
   - Autologon → Disable;
   - eliminare `C:\TotemKiosk` e l'utente `totem`;
   - rimuovere i criteri di gruppo creati in A6.

Se il tablet non va: ripetere `reset-pairing` e rifare la procedura ridotta sul PC (il suo
profilo ha ancora i documenti), in attesa di risolvere.

---

## Scheda del totem (da compilare)

| Voce | Valore |
|---|---|
| Dispositivo, modello, numero di serie | |
| Sistema operativo e versione | |
| Browser o Fully + WebView, versioni | |
| URL | `https://totem.azienda.it` |
| Data della messa in servizio, tecnico | |
| Memoria persistente (dalla schermata Stato) | |
| Prova "riavvio senza rete" superata (data) | |
| Dove è conservato il PIN di manutenzione | |
| Dove è conservata la password di uscita di Fully / l'account amministratore Windows | |
