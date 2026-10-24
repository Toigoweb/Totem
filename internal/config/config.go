// Package config gestisce /data/config.json: caricamento, migrazioni con backup,
// validazione e salvataggio atomico. Nessun database: il file è piccolo e leggibile.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

// SchemaVersion è la versione corrente del formato di config.json.
const SchemaVersion = 2

// Icons è il set fisso delle 10 icone ammesse per le sezioni.
var Icons = []string{"documento", "flacone", "scudo", "fiamma", "croce", "casco", "uscita", "mappa", "ingranaggio", "cartella"}

// MaxSections è il limite della griglia in Home (4x2).
const MaxSections = 8

type Config struct {
	SchemaVersion int            `json:"schemaVersion"`
	Sezioni       []Section      `json:"sezioni"`
	Reparti       []Department   `json:"reparti"`
	Globali       Globals        `json:"globali"`
	Impostazioni  map[string]any `json:"impostazioni,omitempty"`
}

type Section struct {
	ID        string `json:"id"`
	Nome      string `json:"nome"`
	Icona     string `json:"icona"`
	Ordine    int    `json:"ordine"`
	Emergenza bool   `json:"emergenza"`
	Share     Share  `json:"share"`
}

type Share struct {
	Percorso      string `json:"percorso"`
	Utente        string `json:"utente"`
	Password      string `json:"password"` // cifrata: "v1:..." (vedi crypto.go)
	Sottocartelle bool   `json:"sottocartelle"`
}

type Globals struct {
	PinHash      string      `json:"pinHash,omitempty"`
	Fase2        bool        `json:"fase2"`
	Dispositivi  []Device    `json:"dispositivi"`
	Contatti     []Contact   `json:"contatti"`
	Avvisi       []Notice    `json:"avvisi"`
	Salvaschermo Screensaver `json:"salvaschermo"`
	Monitor      Monitor     `json:"monitor"`
}

// Department è un reparto: un gruppo di totem che vede solo alcune sezioni.
// Sezioni vuoto = tutte le sezioni.
type Department struct {
	ID      string   `json:"id"`
	Nome    string   `json:"nome"`
	Sezioni []string `json:"sezioni"`
}

// Contact è un numero utile mostrato nella fascia EMERGENZA.
// Reparti vuoto = tutti i reparti.
type Contact struct {
	ID        string   `json:"id"`
	Etichetta string   `json:"etichetta"`
	Numero    string   `json:"numero"`
	Reparti   []string `json:"reparti"`
}

// Notice è un avviso temporaneo in Home. Dal e Al sono date "AAAA-MM-GG" incluse,
// facoltative. Livello: "info" o "attenzione".
type Notice struct {
	ID      string   `json:"id"`
	Testo   string   `json:"testo"`
	Livello string   `json:"livello"`
	Dal     string   `json:"dal,omitempty"`
	Al      string   `json:"al,omitempty"`
	Reparti []string `json:"reparti"`
}

// Screensaver regola il salvaschermo dei totem (non delle postazioni).
type Screensaver struct {
	Attivo     bool   `json:"attivo"`
	DopoMinuti int    `json:"dopoMinuti"`
	Notte      bool   `json:"notte"`      // schermo nero nell'orario notturno
	OrarioNero string `json:"orarioNero"` // "HH:MM-HH:MM"
}

// Monitor è l'accesso alla pagina di stato per gli strumenti di monitoraggio.
type Monitor struct {
	TokenHash    string `json:"tokenHash,omitempty"`
	SogliaMinuti int    `json:"sogliaMinuti"` // totem in errore se tace da più minuti
}

type Device struct {
	ID             string    `json:"id"`
	Nome           string    `json:"nome"`
	Ruolo          string    `json:"ruolo"`             // "totem" | "postazione" | "telefono"
	Reparto        string    `json:"reparto,omitempty"` // solo totem; vuoto = tutte le sezioni
	Piattaforma    string    `json:"piattaforma"`
	TokenHash      string    `json:"tokenHash"`
	Creato         time.Time `json:"creato"`
	UltimoContatto time.Time `json:"ultimoContatto,omitempty"`
}

// Clone restituisce una copia profonda, così chi legge non vede scritture concorrenti.
func (c *Config) Clone() *Config {
	b, _ := json.Marshal(c)
	var out Config
	_ = json.Unmarshal(b, &out)
	if out.Sezioni == nil {
		out.Sezioni = []Section{}
	}
	if out.Globali.Dispositivi == nil {
		out.Globali.Dispositivi = []Device{}
	}
	if out.Reparti == nil {
		out.Reparti = []Department{}
	}
	if out.Globali.Contatti == nil {
		out.Globali.Contatti = []Contact{}
	}
	if out.Globali.Avvisi == nil {
		out.Globali.Avvisi = []Notice{}
	}
	return &out
}

// SortedSections restituisce le sezioni ordinate per Ordine (poi per id, per stabilità).
func (c *Config) SortedSections() []Section {
	s := append([]Section(nil), c.Sezioni...)
	sort.SliceStable(s, func(i, j int) bool {
		if s[i].Ordine != s[j].Ordine {
			return s[i].Ordine < s[j].Ordine
		}
		return s[i].ID < s[j].ID
	})
	return s
}

func (c *Config) Section(id string) (Section, bool) {
	for _, s := range c.Sezioni {
		if s.ID == id {
			return s, true
		}
	}
	return Section{}, false
}

// ---------------------------------------------------------------------------
// Validazione

var (
	uncRe  = regexp.MustCompile(`^\\\\[A-Za-z0-9._-]+\\[^\\/:*?"<>|]+(\\[^\\/:*?"<>|]+)*\\?$`)
	userRe = regexp.MustCompile(`^([A-Za-z0-9._-]+\\)?[A-Za-z0-9._@$-]+$`)
)

// ValidSharePath controlla il formato \\server\condivisione[\cartella...] ed esclude "..".
func ValidSharePath(p string) bool {
	if !uncRe.MatchString(p) {
		return false
	}
	for _, part := range strings.Split(p[2:], `\`) {
		if part == ".." || part == "." {
			return false
		}
		for _, r := range part {
			if r < 0x20 {
				return false
			}
		}
	}
	return true
}

// ValidUser accetta "DOMINIO\utente", "utente@dominio" o "utente".
func ValidUser(u string) bool { return userRe.MatchString(u) }

func validIcon(i string) bool {
	for _, x := range Icons {
		if x == i {
			return true
		}
	}
	return false
}

// ValidationError descrive un campo non valido; il campo è il nome JSON.
type ValidationError struct{ Field, Msg string }

func (e *ValidationError) Error() string { return e.Field + ": " + e.Msg }

// ValidateSection controlla i campi modificabili dall'interfaccia (la password non è controllata qui).
func ValidateSection(s Section) error {
	n := strings.TrimSpace(s.Nome)
	if n == "" {
		return &ValidationError{"nome", "obbligatorio"}
	}
	if utf8.RuneCountInString(n) > 30 {
		return &ValidationError{"nome", "massimo 30 caratteri"}
	}
	if !validIcon(s.Icona) {
		return &ValidationError{"icona", "icona non ammessa"}
	}
	if !ValidSharePath(s.Share.Percorso) {
		return &ValidationError{"percorso", "formato non valido"}
	}
	if s.Share.Utente != "" && !ValidUser(s.Share.Utente) {
		return &ValidationError{"utente", "formato non valido"}
	}
	return nil
}

// ---------------------------------------------------------------------------
// Store: accesso concorrente e persistenza

// Store custodisce la configurazione in memoria e la salva su disco a ogni modifica.
type Store struct {
	mu        sync.RWMutex
	path      string
	backupDir string
	cfg       *Config
	listeners []func()
}

// Open carica config.json (o ne crea uno vuoto) e applica le migrazioni.
// Prima di qualsiasi migrazione salva una copia in backupDir.
func Open(path, backupDir string) (*Store, error) {
	st := &Store{path: path, backupDir: backupDir}
	b, err := os.ReadFile(path)
	switch {
	case errors.Is(err, os.ErrNotExist):
		st.cfg = &Config{SchemaVersion: SchemaVersion, Sezioni: []Section{}, Globali: Globals{Dispositivi: []Device{}}}
		if err := st.persist(st.cfg); err != nil {
			return nil, err
		}
		return st, nil
	case err != nil:
		return nil, err
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil {
		return nil, fmt.Errorf("config.json non valido: %w", err)
	}
	ver := 0
	if v, ok := raw["schemaVersion"]; ok {
		_ = json.Unmarshal(v, &ver)
	}
	if ver > SchemaVersion {
		return nil, fmt.Errorf("config.json ha schemaVersion %d, più recente di questo binario (%d): usare una versione più nuova", ver, SchemaVersion)
	}
	if ver < SchemaVersion {
		if err := st.backup(b, ver); err != nil {
			return nil, fmt.Errorf("backup prima della migrazione: %w", err)
		}
		b, err = migrate(b, ver)
		if err != nil {
			return nil, err
		}
	}
	var c Config
	if err := json.Unmarshal(b, &c); err != nil {
		return nil, fmt.Errorf("config.json non valido: %w", err)
	}
	c.SchemaVersion = SchemaVersion
	st.cfg = c.Clone()
	if ver < SchemaVersion {
		if err := st.persist(st.cfg); err != nil {
			return nil, err
		}
	}
	return st, nil
}

// migrate porta il JSON grezzo da una versione alla successiva, un passo alla volta.
func migrate(b []byte, from int) ([]byte, error) {
	for v := from; v < SchemaVersion; v++ {
		switch v {
		case 0:
			// v0 = file scritto a mano senza schemaVersion: stessi campi della v1.
		case 1:
			// v2 aggiunge reparti, numeri utili, avvisi, salvaschermo e monitoraggio:
			// campi nuovi con valori di default, nessuna trasformazione.
		default:
			return nil, fmt.Errorf("migrazione da schemaVersion %d non prevista", v)
		}
	}
	return b, nil
}

func (st *Store) backup(b []byte, ver int) error {
	if err := os.MkdirAll(st.backupDir, 0o700); err != nil {
		return err
	}
	name := fmt.Sprintf("config-%s-v%d.json", time.Now().Format("20060102-150405"), ver)
	return WriteFileAtomic(filepath.Join(st.backupDir, name), b, 0o600)
}

// Get restituisce una copia della configurazione corrente.
func (st *Store) Get() *Config {
	st.mu.RLock()
	defer st.mu.RUnlock()
	return st.cfg.Clone()
}

// View chiama fn con la configurazione corrente in sola lettura, senza copiarla.
// fn non deve modificare c né conservarne riferimenti.
func (st *Store) View(fn func(c *Config)) {
	st.mu.RLock()
	defer st.mu.RUnlock()
	fn(st.cfg)
}

// Update applica fn a una copia; se fn non restituisce errore la copia viene salvata
// su disco e diventa la configurazione corrente. In caso di errore nulla cambia.
func (st *Store) Update(fn func(c *Config) error) error {
	st.mu.Lock()
	next := st.cfg.Clone()
	if err := fn(next); err != nil {
		st.mu.Unlock()
		return err
	}
	if err := st.persist(next); err != nil {
		st.mu.Unlock()
		return err
	}
	st.cfg = next
	ls := append([]func(){}, st.listeners...)
	st.mu.Unlock()
	for _, l := range ls {
		l()
	}
	return nil
}

// OnChange registra una funzione chiamata dopo ogni Update riuscito.
func (st *Store) OnChange(fn func()) {
	st.mu.Lock()
	st.listeners = append(st.listeners, fn)
	st.mu.Unlock()
}

func (st *Store) persist(c *Config) error {
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return WriteFileAtomic(st.path, append(b, '\n'), 0o600)
}

// WriteFileAtomic scrive su un file temporaneo nella stessa cartella, fa fsync e poi rename.
func WriteFileAtomic(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	ok := false
	defer func() {
		if !ok {
			_ = os.Remove(tmp)
		}
	}()
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err := f.Chmod(perm); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		return err
	}
	ok = true
	if d, err := os.Open(dir); err == nil {
		_ = d.Sync()
		d.Close()
	}
	return nil
}
