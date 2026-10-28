// Package emergency custodisce lo stato della modalità emergenza: quando è attiva tutti
// i totem mostrano a tutto schermo il messaggio (evacuazione o avviso urgente) finché
// qualcuno non la termina. Lo stato sta in /data/emergency.json, così sopravvive a un
// riavvio del server.
package emergency

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/toigoweb/totem/internal/auth"
	"github.com/toigoweb/totem/internal/config"
)

// Tipi di emergenza.
const (
	TypeEvacuation = "evacuazione"
	TypeAlert      = "avviso"
	MaxText        = 200
)

// DefaultEvacuationText è il messaggio di evacuazione se non se ne indica un altro.
const DefaultEvacuationText = "Lasciare l'edificio con calma seguendo le vie di fuga e raggiungere il punto di raccolta. Non usare gli ascensori."

// State è lo stato corrente (o l'ultima emergenza terminata).
type State struct {
	Attiva      bool      `json:"attiva"`
	ID          string    `json:"id,omitempty"`
	Tipo        string    `json:"tipo,omitempty"`
	Testo       string    `json:"testo,omitempty"`
	Avviata     time.Time `json:"avviata,omitempty"`
	Da          string    `json:"da,omitempty"` // chi l'ha avviata: dispositivo, "API", "server"
	Terminata   time.Time `json:"terminata,omitempty"`
	TerminataDa string    `json:"terminataDa,omitempty"`
}

// Title è il titolo mostrato e letto ad alta voce.
func (s State) Title() string {
	if s.Tipo == TypeEvacuation {
		return "Evacuazione"
	}
	return "Attenzione"
}

// Announcement è il testo dell'annuncio vocale sui tablet.
func (s State) Announcement() string { return s.Title() + ". " + s.Testo }

// Store salva lo stato in modo atomico.
type Store struct {
	mu   sync.Mutex
	path string
	s    State
	Now  func() time.Time
}

func Open(dataDir string) (*Store, error) {
	st := &Store{path: filepath.Join(dataDir, "emergency.json"), Now: time.Now}
	b, err := os.ReadFile(st.path)
	switch {
	case errors.Is(err, os.ErrNotExist):
		return st, nil
	case err != nil:
		return nil, err
	}
	if err := json.Unmarshal(b, &st.s); err != nil {
		return nil, fmt.Errorf("emergency.json non valido: %w", err)
	}
	return st, nil
}

func (st *Store) Get() State {
	st.mu.Lock()
	defer st.mu.Unlock()
	return st.s
}

// Active è comodo per chi deve solo sapere se c'è un'emergenza in corso.
func (st *Store) Active() bool { return st.Get().Attiva }

func (st *Store) save(s State) error {
	b, _ := json.MarshalIndent(s, "", "  ")
	if err := config.WriteFileAtomic(st.path, append(b, '\n'), 0o600); err != nil {
		return err
	}
	st.s = s
	return nil
}

// Start avvia (o aggiorna, se già attiva) l'emergenza.
func (st *Store) Start(tipo, testo, da string) (State, error) {
	testo = strings.TrimSpace(testo)
	switch tipo {
	case TypeEvacuation:
		if testo == "" {
			testo = DefaultEvacuationText
		}
	case TypeAlert:
		if testo == "" {
			return State{}, &config.ValidationError{Field: "testo", Msg: "obbligatorio"}
		}
	default:
		return State{}, &config.ValidationError{Field: "tipo", Msg: "tipo non valido"}
	}
	if utf8.RuneCountInString(testo) > MaxText {
		return State{}, &config.ValidationError{Field: "testo", Msg: fmt.Sprintf("massimo %d caratteri", MaxText)}
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	s := State{Attiva: true, ID: auth.NewID("em"), Tipo: tipo, Testo: testo, Avviata: st.Now().UTC(), Da: da}
	if st.s.Attiva {
		s.Avviata = st.s.Avviata // un aggiornamento non azzera l'ora di inizio
	}
	return s, st.save(s)
}

// Stop termina l'emergenza; false se non era attiva.
func (st *Store) Stop(da string) (bool, error) {
	st.mu.Lock()
	defer st.mu.Unlock()
	if !st.s.Attiva {
		return false, nil
	}
	s := st.s
	s.Attiva, s.Terminata, s.TerminataDa = false, st.Now().UTC(), da
	return true, st.save(s)
}
