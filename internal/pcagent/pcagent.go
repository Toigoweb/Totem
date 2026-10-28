// Package pcagent segue i PC Windows dei totem attraverso il loro agente
// (deploy/windows/totem-agente.ps1): ogni 30 secondi l'agente manda lo stato del PC e
// riceve i comandi in coda. È il PC a chiamare il server, quindi sul PC non si apre
// nessuna porta. I comandi sono un elenco chiuso: il server non può eseguire altro.
package pcagent

import (
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/toigoweb/totem/internal/auth"
	"github.com/toigoweb/totem/internal/config"
	"github.com/toigoweb/totem/internal/kiosk"
)

// Azioni che l'agente sa eseguire.
var Actions = map[string]bool{
	"riavvia-chrome": true,
	"riavvia-pc":     true,
	"schermo-on":     true,
	"schermo-off":    true,
	"screenshot":     true,
	"annuncio":       true,
}

// Soglie.
const (
	PollSeconds    = 30
	SilentAfter    = 3 * time.Minute
	UnpluggedAfter = 10 * time.Minute
	BatteryLow     = 20
	DiskLowMB      = 2048
	MaxShotBytes   = 8 << 20
)

// Report è ciò che l'agente manda a ogni giro.
type Report struct {
	Versione        string   `json:"versione"`
	Chrome          bool     `json:"chrome"`
	Rete            *bool    `json:"rete"`     // alimentazione da rete; nil se Windows non lo sa
	Batteria        *float64 `json:"batteria"` // % di batteria o UPS; nil se assente
	UptimeMin       int      `json:"uptimeMin"`
	DiscoLiberoMB   float64  `json:"discoLiberoMB"`
	RiavvioInAttesa bool     `json:"riavvioInAttesa"` // aggiornamenti di Windows da completare
	Windows         string   `json:"windows"`
	SchermoSpento   bool     `json:"schermoSpento"`
	Fatti           []Done   `json:"fatti"`
}

// Done è l'esito di un comando.
type Done struct {
	ID     string `json:"id"`
	OK     bool   `json:"ok"`
	Errore string `json:"errore,omitempty"`
}

// Command è un comando in coda per l'agente.
type Command struct {
	ID     string `json:"id"`
	Azione string `json:"azione"`
	Testo  string `json:"testo,omitempty"` // solo per "annuncio"
}

// View è lo stato del PC per la manutenzione.
type View struct {
	Configured    bool          `json:"configured"`
	OK            bool          `json:"ok"`
	LastReport    *time.Time    `json:"lastReport,omitempty"`
	Version       string        `json:"version,omitempty"`
	Chrome        bool          `json:"chrome"`
	Mains         *bool         `json:"mains,omitempty"`
	Battery       *float64      `json:"battery,omitempty"`
	UptimeMin     int           `json:"uptimeMin"`
	DiskFreeMB    float64       `json:"diskFreeMb"`
	RebootPending bool          `json:"rebootPending"`
	Windows       string        `json:"windows,omitempty"`
	ScreenOff     bool          `json:"screenOff"`
	LastResults   []Done        `json:"lastResults"`
	Pending       int           `json:"pending"`
	ScreenshotAt  *time.Time    `json:"screenshotAt,omitempty"`
	Issues        []kiosk.Issue `json:"issues"`
}

type state struct {
	last          *Report
	lastAt        time.Time
	chromeMissing int
	onBattery     time.Time
	queue         []Command
	results       []Done
	shot          []byte
	shotAt        time.Time
}

// Manager tiene in memoria lo stato degli agenti e le code dei comandi.
type Manager struct {
	mu     sync.Mutex
	st     map[string]*state
	Now    func() time.Time
	Logger *slog.Logger
}

func New(now func() time.Time, logger *slog.Logger) *Manager {
	if now == nil {
		now = time.Now
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &Manager{st: map[string]*state{}, Now: now, Logger: logger}
}

func (m *Manager) stateOf(id string) *state {
	s := m.st[id]
	if s == nil {
		s = &state{}
		m.st[id] = s
	}
	return s
}

// Report registra lo stato del PC e restituisce (togliendoli dalla coda) i comandi da eseguire.
func (m *Manager) Report(id string, r Report) []Command {
	now := m.Now()
	m.mu.Lock()
	defer m.mu.Unlock()
	s := m.stateOf(id)
	s.last, s.lastAt = &r, now
	if r.Chrome {
		s.chromeMissing = 0
	} else {
		s.chromeMissing++
	}
	if r.Rete != nil && !*r.Rete {
		if s.onBattery.IsZero() {
			s.onBattery = now
		}
	} else {
		s.onBattery = time.Time{}
	}
	if len(r.Fatti) > 0 {
		s.results = append(r.Fatti, s.results...)
		if len(s.results) > 10 {
			s.results = s.results[:10]
		}
	}
	cmds := s.queue
	s.queue = nil
	if cmds == nil {
		cmds = []Command{}
	}
	return cmds
}

// Queue mette in coda un comando e ne restituisce l'id.
func (m *Manager) Queue(id, azione, testo string) (string, error) {
	if !Actions[azione] {
		return "", fmt.Errorf("azione %q non prevista", azione)
	}
	c := Command{ID: auth.NewID("c"), Azione: azione, Testo: testo}
	m.mu.Lock()
	s := m.stateOf(id)
	// stesso comando già in coda: non si duplica (doppio clic)
	for _, q := range s.queue {
		if q.Azione == azione && q.Testo == testo {
			m.mu.Unlock()
			return q.ID, nil
		}
	}
	s.queue = append(s.queue, c)
	m.mu.Unlock()
	return c.ID, nil
}

// SetScreenshot conserva l'ultimo screenshot ricevuto (solo in memoria).
func (m *Manager) SetScreenshot(id string, png []byte) {
	m.mu.Lock()
	s := m.stateOf(id)
	s.shot, s.shotAt = png, m.Now()
	m.mu.Unlock()
}

// Screenshot restituisce l'ultimo screenshot del PC.
func (m *Manager) Screenshot(id string) ([]byte, time.Time, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s := m.st[id]
	if s == nil || s.shot == nil {
		return nil, time.Time{}, false
	}
	return s.shot, s.shotAt, true
}

// Forget dimentica un agente scollegato.
func (m *Manager) Forget(id string) {
	m.mu.Lock()
	delete(m.st, id)
	m.mu.Unlock()
}

// ViewOf restituisce lo stato del PC di un dispositivo.
func (m *Manager) ViewOf(d config.Device) View {
	v := View{Issues: []kiosk.Issue{}, LastResults: []Done{}}
	if d.Agente == nil {
		return v
	}
	v.Configured = true
	now := m.Now()
	m.mu.Lock()
	var cp state
	if s := m.st[d.ID]; s != nil {
		cp = *s
		cp.results = append([]Done(nil), s.results...)
	}
	m.mu.Unlock()
	v.Issues = issues(d, cp, now)
	v.Pending = len(cp.queue)
	if cp.results != nil {
		v.LastResults = cp.results
	}
	if !cp.shotAt.IsZero() {
		t := cp.shotAt
		v.ScreenshotAt = &t
	}
	if r := cp.last; r != nil {
		t := cp.lastAt
		v.LastReport = &t
		v.OK = now.Sub(cp.lastAt) < SilentAfter
		v.Version, v.Chrome, v.Mains, v.Battery = r.Versione, r.Chrome, r.Rete, r.Batteria
		v.UptimeMin, v.DiskFreeMB, v.RebootPending, v.Windows, v.ScreenOff = r.UptimeMin, r.DiscoLiberoMB, r.RiavvioInAttesa, r.Windows, r.SchermoSpento
	}
	return v
}

func issues(d config.Device, s state, now time.Time) []kiosk.Issue {
	out := []kiosk.Issue{}
	add := func(code, text string, isErr bool) {
		out = append(out, kiosk.Issue{DeviceID: d.ID, Device: d.Nome, Code: code, Text: text, Error: isErr})
	}
	r := s.last
	if r == nil {
		add("agent_waiting", "agente del PC non ancora collegato", false)
		return out
	}
	if now.Sub(s.lastAt) >= SilentAfter {
		add("unreachable", fmt.Sprintf("PC non raggiungibile: nessun contatto dell'agente da %d minuti", int(now.Sub(s.lastAt).Minutes())), true)
		return out
	}
	if s.chromeMissing >= 2 {
		add("chrome", "Chrome non è in esecuzione sul PC", true)
	}
	if !s.onBattery.IsZero() && now.Sub(s.onBattery) >= UnpluggedAfter {
		txt := "PC scollegato dalla corrente"
		if r.Batteria != nil {
			txt += fmt.Sprintf(" (batteria %.0f%%)", *r.Batteria)
		}
		add("unplugged", txt, true)
	}
	if r.Batteria != nil && r.Rete != nil && !*r.Rete && *r.Batteria <= BatteryLow {
		add("battery", fmt.Sprintf("batteria del PC scarica: %.0f%%", *r.Batteria), true)
	}
	if r.DiscoLiberoMB > 0 && r.DiscoLiberoMB < DiskLowMB {
		add("disk", fmt.Sprintf("spazio su disco quasi esaurito: %.1f GB", r.DiscoLiberoMB/1024), false)
	}
	if r.RiavvioInAttesa {
		add("updates", "aggiornamenti di Windows in attesa di riavvio", false)
	}
	return out
}

// Issues: problemi di tutti i PC con agente.
func (m *Manager) Issues(devs []config.Device) []kiosk.Issue {
	var out []kiosk.Issue
	for _, d := range devs {
		if d.Agente != nil {
			out = append(out, m.ViewOf(d).Issues...)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Device < out[j].Device })
	return out
}

// CleanText limita il testo di un annuncio.
func CleanText(t string) string {
	t = strings.TrimSpace(strings.Map(func(r rune) rune {
		if r < 32 {
			return ' '
		}
		return r
	}, t))
	if len([]rune(t)) > 400 {
		t = string([]rune(t)[:400])
	}
	return t
}
