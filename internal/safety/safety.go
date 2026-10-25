// Package safety custodisce le planimetrie e gli eventi (infortuni, medicazioni, quasi
// incidenti, danni) per la mappa di calore del totem.
//
// I dati stanno in /data/safety.json, separati da config.json perché crescono nel tempo;
// le immagini delle planimetrie in /data/maps/<sha256>. Nessun dato personale: la nota
// facoltativa resta in manutenzione e non va mai nel manifest.
package safety

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg" // decodifica delle dimensioni
	_ "image/png"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/toigoweb/totem/internal/auth"
	"github.com/toigoweb/totem/internal/config"
)

// Limiti.
const (
	MaxMaps       = 10
	MaxEvents     = 5000
	MaxImageBytes = 15 << 20
	MaxImageSide  = 10000
	MaxNote       = 200
	MaxMapName    = 40
)

// Tipi di evento, dal più grave al meno grave.
const (
	TypeInjury   = "infortunio"
	TypeFirstAid = "medicazione"
	TypeNearMiss = "quasi"
	TypeDamage   = "danno"
)

var Types = []string{TypeInjury, TypeFirstAid, TypeNearMiss, TypeDamage}

// Map è una planimetria.
type Map struct {
	ID        string   `json:"id"`
	Nome      string   `json:"nome"`
	Immagine  string   `json:"immagine"` // sha256 del file
	Formato   string   `json:"formato"`  // image/png | image/jpeg
	Byte      int64    `json:"byte"`
	Larghezza int      `json:"larghezza"`
	Altezza   int      `json:"altezza"`
	Reparti   []string `json:"reparti"`
}

// Event è un evento segnato sulla planimetria. X e Y vanno da 0 a 1 (frazione di
// larghezza e altezza), così non dipendono dalla risoluzione dell'immagine.
type Event struct {
	ID    string  `json:"id"`
	Mappa string  `json:"mappa"`
	X     float64 `json:"x"`
	Y     float64 `json:"y"`
	Data  string  `json:"data"` // AAAA-MM-GG
	Tipo  string  `json:"tipo"`
	Nota  string  `json:"nota,omitempty"`
}

// Data è il contenuto di safety.json.
type Data struct {
	SchemaVersion int     `json:"schemaVersion"`
	Mappe         []Map   `json:"mappe"`
	Eventi        []Event `json:"eventi"`
	// Inizio: data da cui la registrazione degli eventi è completa. Serve al contatore
	// dei giorni senza infortuni (senza, il contatore non compare).
	Inizio string `json:"inizio,omitempty"`
}

// Store: accesso concorrente e salvataggio atomico.
type Store struct {
	mu   sync.RWMutex
	path string
	dir  string
	d    *Data
	Now  func() time.Time
}

var dateRe = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)

// Open carica (o crea) safety.json nella cartella dei dati.
func Open(dataDir string) (*Store, error) {
	st := &Store{path: filepath.Join(dataDir, "safety.json"), dir: filepath.Join(dataDir, "maps"), Now: time.Now}
	if err := os.MkdirAll(st.dir, 0o700); err != nil {
		return nil, err
	}
	b, err := os.ReadFile(st.path)
	switch {
	case errors.Is(err, os.ErrNotExist):
		st.d = &Data{SchemaVersion: 1, Mappe: []Map{}, Eventi: []Event{}}
		return st, nil
	case err != nil:
		return nil, err
	}
	var d Data
	if err := json.Unmarshal(b, &d); err != nil {
		return nil, fmt.Errorf("safety.json non valido: %w", err)
	}
	if d.Mappe == nil {
		d.Mappe = []Map{}
	}
	if d.Eventi == nil {
		d.Eventi = []Event{}
	}
	st.d = &d
	return st, nil
}

// Get restituisce una copia dei dati.
func (st *Store) Get() Data {
	st.mu.RLock()
	defer st.mu.RUnlock()
	return clone(st.d)
}

func clone(d *Data) Data {
	b, _ := json.Marshal(d)
	var c Data
	_ = json.Unmarshal(b, &c)
	return c
}

// update applica fn a una copia e salva; se fn o il salvataggio falliscono non cambia nulla.
func (st *Store) update(fn func(d *Data) error) error {
	st.mu.Lock()
	defer st.mu.Unlock()
	c := clone(st.d)
	if err := fn(&c); err != nil {
		return err
	}
	b, err := json.MarshalIndent(&c, "", "  ")
	if err != nil {
		return err
	}
	if err := config.WriteFileAtomic(st.path, append(b, '\n'), 0o600); err != nil {
		return err
	}
	st.d = &c
	return nil
}

func verr(field, msg string) error { return &config.ValidationError{Field: field, Msg: msg} }

func cleanName(n string) (string, error) {
	n = strings.TrimSpace(n)
	if n == "" {
		return "", verr("nome", "obbligatorio")
	}
	if utf8.RuneCountInString(n) > MaxMapName {
		return "", verr("nome", fmt.Sprintf("massimo %d caratteri", MaxMapName))
	}
	return n, nil
}

// AddMap salva una planimetria PNG o JPEG.
func (st *Store) AddMap(nome string, img []byte) (Map, error) {
	nome, err := cleanName(nome)
	if err != nil {
		return Map{}, err
	}
	if len(img) > MaxImageBytes {
		return Map{}, verr("immagine", "troppo grande")
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(img))
	if err != nil || (format != "png" && format != "jpeg") {
		return Map{}, verr("immagine", "serve un'immagine PNG o JPEG")
	}
	if cfg.Width < 100 || cfg.Height < 100 || cfg.Width > MaxImageSide || cfg.Height > MaxImageSide {
		return Map{}, verr("immagine", "dimensioni non adatte")
	}
	sum := sha256.Sum256(img)
	sha := hex.EncodeToString(sum[:])
	m := Map{ID: auth.NewID("m"), Nome: nome, Immagine: sha, Formato: "image/" + format, Byte: int64(len(img)),
		Larghezza: cfg.Width, Altezza: cfg.Height, Reparti: []string{}}
	err = st.update(func(d *Data) error {
		if len(d.Mappe) >= MaxMaps {
			return verr("mappe", fmt.Sprintf("massimo %d planimetrie", MaxMaps))
		}
		if err := config.WriteFileAtomic(filepath.Join(st.dir, sha), img, 0o600); err != nil {
			return err
		}
		d.Mappe = append(d.Mappe, m)
		return nil
	})
	return m, err
}

// UpdateMap cambia nome e reparti (known: reparti esistenti).
func (st *Store) UpdateMap(id, nome string, reparti []string, known map[string]bool) error {
	nome, err := cleanName(nome)
	if err != nil {
		return err
	}
	if reparti == nil {
		reparti = []string{}
	}
	for _, r := range reparti {
		if !known[r] {
			return verr("reparti", "reparto inesistente")
		}
	}
	return st.update(func(d *Data) error {
		for i := range d.Mappe {
			if d.Mappe[i].ID == id {
				d.Mappe[i].Nome, d.Mappe[i].Reparti = nome, reparti
				return nil
			}
		}
		return ErrNotFound
	})
}

// ErrNotFound: planimetria o evento inesistente.
var ErrNotFound = errors.New("non trovato")

// DeleteMap elimina la planimetria e i suoi eventi.
func (st *Store) DeleteMap(id string) error {
	var sha string
	err := st.update(func(d *Data) error {
		k := -1
		for i, m := range d.Mappe {
			if m.ID == id {
				k, sha = i, m.Immagine
			}
		}
		if k < 0 {
			return ErrNotFound
		}
		d.Mappe = append(d.Mappe[:k], d.Mappe[k+1:]...)
		ev := d.Eventi[:0]
		for _, e := range d.Eventi {
			if e.Mappa != id {
				ev = append(ev, e)
			}
		}
		d.Eventi = ev
		return nil
	})
	if err == nil {
		if _, used := st.ImagePath(sha); !used {
			_ = os.Remove(filepath.Join(st.dir, sha))
		}
	}
	return err
}

// SetStart imposta la data di inizio della registrazione ("" = contatore spento).
func (st *Store) SetStart(inizio string) error {
	if inizio != "" {
		if err := st.checkDate("inizio", inizio); err != nil {
			return err
		}
	}
	return st.update(func(d *Data) error {
		d.Inizio = inizio
		return nil
	})
}

func (st *Store) checkDate(field, s string) error {
	t, err := time.Parse("2006-01-02", s)
	if !dateRe.MatchString(s) || err != nil {
		return verr(field, "data non valida")
	}
	if t.After(st.Now().AddDate(0, 0, 1)) {
		return verr(field, "data nel futuro")
	}
	if t.Year() < 2000 {
		return verr(field, "data non valida")
	}
	return nil
}

// PutEvent crea (ID vuoto) o modifica un evento.
func (st *Store) PutEvent(e Event) (Event, error) {
	e.Nota = strings.TrimSpace(e.Nota)
	switch {
	case e.X < 0 || e.X > 1 || e.Y < 0 || e.Y > 1 || e.X != e.X || e.Y != e.Y:
		return e, verr("posizione", "fuori dalla planimetria")
	case !validType(e.Tipo):
		return e, verr("tipo", "tipo non valido")
	case utf8.RuneCountInString(e.Nota) > MaxNote:
		return e, verr("nota", fmt.Sprintf("massimo %d caratteri", MaxNote))
	}
	if err := st.checkDate("data", e.Data); err != nil {
		return e, err
	}
	err := st.update(func(d *Data) error {
		found := false
		for _, m := range d.Mappe {
			found = found || m.ID == e.Mappa
		}
		if !found {
			return verr("mappa", "planimetria inesistente")
		}
		if e.ID == "" {
			if len(d.Eventi) >= MaxEvents {
				return verr("eventi", fmt.Sprintf("massimo %d eventi", MaxEvents))
			}
			e.ID = auth.NewID("e")
			d.Eventi = append(d.Eventi, e)
			return nil
		}
		for i := range d.Eventi {
			if d.Eventi[i].ID == e.ID {
				d.Eventi[i] = e
				return nil
			}
		}
		return ErrNotFound
	})
	return e, err
}

// DeleteEvent elimina un evento.
func (st *Store) DeleteEvent(id string) error {
	return st.update(func(d *Data) error {
		for i, e := range d.Eventi {
			if e.ID == id {
				d.Eventi = append(d.Eventi[:i], d.Eventi[i+1:]...)
				return nil
			}
		}
		return ErrNotFound
	})
}

func validType(t string) bool {
	for _, x := range Types {
		if x == t {
			return true
		}
	}
	return false
}

var shaRe = regexp.MustCompile(`^[0-9a-f]{64}$`)

// ImagePath restituisce il file di una planimetria in uso.
func (st *Store) ImagePath(sha string) (string, bool) {
	if !shaRe.MatchString(sha) {
		return "", false
	}
	st.mu.RLock()
	defer st.mu.RUnlock()
	for _, m := range st.d.Mappe {
		if m.Immagine == sha {
			return filepath.Join(st.dir, sha), true
		}
	}
	return "", false
}

// MapFormat restituisce il Content-Type di un'immagine in uso.
func (st *Store) MapFormat(sha string) string {
	st.mu.RLock()
	defer st.mu.RUnlock()
	for _, m := range st.d.Mappe {
		if m.Immagine == sha {
			return m.Formato
		}
	}
	return "application/octet-stream"
}

// MapsFor: planimetrie visibili da un reparto. Un riferimento a un reparto eliminato
// non conta; una planimetria senza reparti (validi) è per tutti.
func (d Data) MapsFor(reparto string, all bool, known map[string]bool) []Map {
	out := []Map{}
	for _, m := range d.Mappe {
		if all {
			out = append(out, m)
			continue
		}
		valid, match := 0, false
		for _, r := range m.Reparti {
			if known[r] {
				valid++
				match = match || r == reparto
			}
		}
		if valid == 0 || match {
			out = append(out, m)
		}
	}
	return out
}

// SortedEvents: eventi in ordine di data decrescente.
func (d Data) SortedEvents() []Event {
	ev := append([]Event{}, d.Eventi...)
	sort.SliceStable(ev, func(i, j int) bool { return ev[i].Data > ev[j].Data })
	return ev
}
