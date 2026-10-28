package kiosk

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/toigoweb/totem/internal/config"
)

// Soglie degli avvisi.
const (
	BatteryLow        = 20               // %
	TemperatureHigh   = 45.0             // °C della batteria
	UnpluggedAfter    = 10 * time.Minute // senza ricarica da almeno
	UnreachableAfter  = 3                // controlli falliti di fila
	MovedDegrees      = 25.0             // inclinazione rispetto alla posizione di riferimento
	DefaultPollPeriod = time.Minute
)

// Azioni consentite dall'area manutenzione (percorso dell'API FreeKiosk).
var Actions = map[string]string{
	"ricarica":       "/api/reload",
	"riavvia-app":    "/api/restart-ui",
	"riavvia-tablet": "/api/reboot",
	"schermo-on":     "/api/screen/on",
	"schermo-off":    "/api/screen/off",
}

// Issue è un problema di un tablet, per la pagina Dispositivi e il monitoraggio.
type Issue struct {
	DeviceID string `json:"deviceId"`
	Device   string `json:"device"`
	Code     string `json:"code"` // unreachable | unplugged | battery | temperature | moved | auth
	Text     string `json:"text"`
	Error    bool   `json:"error"` // false = avviso
}

// View è lo stato di un tablet mostrato in manutenzione.
type View struct {
	Configured  bool       `json:"configured"`
	OK          bool       `json:"ok"`
	Error       string     `json:"error,omitempty"`
	LastOK      *time.Time `json:"lastOk,omitempty"`
	Battery     *float64   `json:"battery,omitempty"`
	Charging    bool       `json:"charging"`
	Temperature *float64   `json:"temperature,omitempty"`
	ScreenOn    bool       `json:"screenOn"`
	Wifi        string     `json:"wifi,omitempty"`
	RSSI        *float64   `json:"rssi,omitempty"`
	FreeMB      *float64   `json:"freeMb,omitempty"`
	Model       string     `json:"model,omitempty"`
	Android     string     `json:"android,omitempty"`
	AutoBright  bool       `json:"autoBrightness"`
	TiltDeg     *float64   `json:"tiltDeg,omitempty"`
	NightOff    bool       `json:"nightOff"` // schermo spento dal server per la notte
	Issues      []Issue    `json:"issues"`
}

type state struct {
	status           *Status
	lastOK, lastTry  time.Time
	err              string
	fails            int
	notChargingSince time.Time
	nightOff         bool // lo schermo l'ha spento il server per la notte
}

// Options del gestore.
type Options struct {
	Config *config.Store
	Cipher *config.Cipher
	Logger *slog.Logger
	Now    func() time.Time
	// EmergencyActive: durante un'emergenza gli schermi restano accesi.
	EmergencyActive func() bool
}

// Manager interroga periodicamente i tablet configurati.
type Manager struct {
	o  Options
	mu sync.Mutex
	st map[string]*state
	hc func(addr, key string) *Client
}

func New(o Options) *Manager {
	if o.Now == nil {
		o.Now = time.Now
	}
	if o.Logger == nil {
		o.Logger = slog.Default()
	}
	if o.EmergencyActive == nil {
		o.EmergencyActive = func() bool { return false }
	}
	hc := DefaultHTTP()
	return &Manager{o: o, st: map[string]*state{}, hc: func(addr, key string) *Client { return &Client{Addr: addr, Key: key, HTTP: hc} }}
}

// Run controlla i tablet ogni minuto finché ctx non termina.
func (m *Manager) Run(ctx context.Context, every time.Duration) {
	if every <= 0 {
		every = DefaultPollPeriod
	}
	t := time.NewTicker(every)
	defer t.Stop()
	m.Tick(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			m.Tick(ctx)
		}
	}
}

type target struct {
	dev config.Device
	c   *Client
}

func (m *Manager) targets() []target {
	var out []target
	cfg := m.o.Config.Get()
	for _, d := range cfg.Globali.Dispositivi {
		if d.Kiosk == nil || d.Kiosk.Tipo == "" {
			continue
		}
		c, err := m.client(d)
		if err != nil {
			continue
		}
		out = append(out, target{d, c})
	}
	return out
}

func (m *Manager) client(d config.Device) (*Client, error) {
	if d.Kiosk == nil {
		return nil, &Error{ErrUnsupported, "tablet non configurato"}
	}
	key := ""
	if d.Kiosk.Chiave != "" {
		k, err := m.o.Cipher.Decrypt(config.KioskKeyID(d.ID), d.Kiosk.Chiave)
		if err != nil {
			return nil, &Error{ErrAuth, "chiave non leggibile: reinserirla"}
		}
		key = k
	}
	return m.hc(d.Kiosk.Indirizzo, key), nil
}

// ClientFor restituisce il client di un dispositivo, per test di connessione e azioni.
func (m *Manager) ClientFor(id string) (*Client, config.Device, error) {
	for _, d := range m.o.Config.Get().Globali.Dispositivi {
		if d.ID == id {
			c, err := m.client(d)
			return c, d, err
		}
	}
	return nil, config.Device{}, &Error{ErrUnsupported, "dispositivo inesistente"}
}

// NewClient crea un client per indirizzo e chiave in chiaro (prova prima del salvataggio).
func (m *Manager) NewClient(addr, key string) *Client { return m.hc(addr, key) }

// Tick: un giro di controlli su tutti i tablet, in parallelo.
func (m *Manager) Tick(ctx context.Context) {
	ts := m.targets()
	var wg sync.WaitGroup
	for _, t := range ts {
		wg.Add(1)
		go func(t target) {
			defer wg.Done()
			cctx, cancel := context.WithTimeout(ctx, 20*time.Second)
			defer cancel()
			m.poll(cctx, t)
		}(t)
	}
	wg.Wait()
	m.prune(ts)
}

// prune dimentica lo stato dei tablet non più configurati.
func (m *Manager) prune(ts []target) {
	keep := map[string]bool{}
	for _, t := range ts {
		keep[t.dev.ID] = true
	}
	m.mu.Lock()
	for id := range m.st {
		if !keep[id] {
			delete(m.st, id)
		}
	}
	m.mu.Unlock()
}

func (m *Manager) stateOf(id string) *state {
	s := m.st[id]
	if s == nil {
		s = &state{}
		m.st[id] = s
	}
	return s
}

func (m *Manager) poll(ctx context.Context, t target) {
	now := m.o.Now()
	st, err := t.c.Status(ctx)
	m.mu.Lock()
	s := m.stateOf(t.dev.ID)
	s.lastTry = now
	if err != nil {
		s.fails++
		s.err = Code(err)
		if s.fails == UnreachableAfter {
			m.o.Logger.Warn("tablet non raggiungibile", "dispositivo", t.dev.Nome, "errore", err.Error())
		}
		m.mu.Unlock()
		return
	}
	if s.fails >= UnreachableAfter {
		m.o.Logger.Info("tablet di nuovo raggiungibile", "dispositivo", t.dev.Nome)
	}
	s.fails, s.err, s.lastOK, s.status = 0, "", now, &st
	if st.Battery.Charging {
		s.notChargingSince = time.Time{}
	} else if s.notChargingSince.IsZero() {
		s.notChargingSince = now
	}
	nightOff := s.nightOff
	m.mu.Unlock()

	k := t.dev.Kiosk
	// posizione di riferimento: la prima lettura dell'accelerometro
	if k.Posizione == nil && st.Sensors.Accelerometer != nil {
		a := st.Sensors.Accelerometer
		m.savePosition(t.dev.ID, &[3]float64{a.X, a.Y, a.Z})
	}
	// luminosità automatica come da configurazione
	if st.AutoBrightness.Enabled != k.LuminositaAuto {
		var err error
		if k.LuminositaAuto {
			err = t.c.Post(ctx, "/api/autoBrightness/enable", map[string]int{"min": 10, "max": 100})
		} else {
			err = t.c.Post(ctx, "/api/autoBrightness/disable", nil)
		}
		if err != nil {
			m.o.Logger.Warn("luminosità automatica non applicata", "dispositivo", t.dev.Nome, "errore", err.Error())
		}
	}
	// schermo spento di notte: si agisce solo nei passaggi, così un'accensione a mano
	// (o un tocco, se il tablet lo permette) non viene annullata ogni minuto
	want := k.SchermoNotte && InWindow(now, m.o.Config.Get().ScreensaverOrDefault().OrarioNero) && !m.o.EmergencyActive()
	switch {
	case want && !nightOff:
		if err := t.c.Post(ctx, "/api/screen/off", nil); err == nil {
			m.setNight(t.dev.ID, true)
			m.o.Logger.Info("schermo del tablet spento per la notte", "dispositivo", t.dev.Nome)
		}
	case !want && nightOff:
		if err := t.c.Post(ctx, "/api/screen/on", nil); err == nil {
			m.setNight(t.dev.ID, false)
			m.o.Logger.Info("schermo del tablet riacceso", "dispositivo", t.dev.Nome)
		}
	}
}

func (m *Manager) setNight(id string, off bool) {
	m.mu.Lock()
	m.stateOf(id).nightOff = off
	m.mu.Unlock()
}

func (m *Manager) savePosition(id string, p *[3]float64) {
	_ = m.o.Config.Update(func(c *config.Config) error {
		for i := range c.Globali.Dispositivi {
			if d := &c.Globali.Dispositivi[i]; d.ID == id && d.Kiosk != nil {
				d.Kiosk.Posizione = p
			}
		}
		return nil
	})
}

// ResetPosition prende come riferimento la posizione attuale del tablet.
func (m *Manager) ResetPosition(id string) error {
	m.mu.Lock()
	s := m.st[id]
	var a *[3]float64
	if s != nil && s.status != nil && s.status.Sensors.Accelerometer != nil {
		x := s.status.Sensors.Accelerometer
		a = &[3]float64{x.X, x.Y, x.Z}
	}
	m.mu.Unlock()
	if a == nil {
		return &Error{ErrUnsupported, "nessuna lettura dell'accelerometro"}
	}
	m.savePosition(id, a)
	return nil
}

// Tilt: angolo in gradi tra due vettori di gravità.
func Tilt(a, b [3]float64) float64 {
	na := math.Sqrt(a[0]*a[0] + a[1]*a[1] + a[2]*a[2])
	nb := math.Sqrt(b[0]*b[0] + b[1]*b[1] + b[2]*b[2])
	if na == 0 || nb == 0 {
		return 0
	}
	c := (a[0]*b[0] + a[1]*b[1] + a[2]*b[2]) / (na * nb)
	c = math.Max(-1, math.Min(1, c))
	return math.Acos(c) * 180 / math.Pi
}

// InWindow dice se now cade nella fascia "HH:MM-HH:MM" (anche a cavallo della mezzanotte).
func InWindow(now time.Time, win string) bool {
	parts := strings.Split(win, "-")
	if len(parts) != 2 {
		return false
	}
	mins := func(s string) int {
		hm := strings.Split(strings.TrimSpace(s), ":")
		if len(hm) != 2 {
			return -1
		}
		h, e1 := strconv.Atoi(hm[0])
		mm, e2 := strconv.Atoi(hm[1])
		if e1 != nil || e2 != nil {
			return -1
		}
		return h*60 + mm
	}
	a, b := mins(parts[0]), mins(parts[1])
	if a < 0 || b < 0 || a == b {
		return false
	}
	t := now.Hour()*60 + now.Minute()
	if a < b {
		return t >= a && t < b
	}
	return t >= a || t < b
}

// ViewOf restituisce lo stato di un dispositivo per la manutenzione.
func (m *Manager) ViewOf(d config.Device) View {
	v := View{Issues: []Issue{}}
	if d.Kiosk == nil || d.Kiosk.Tipo == "" {
		return v
	}
	v.Configured = true
	m.mu.Lock()
	s := m.st[d.ID]
	var cp state
	if s != nil {
		cp = *s
	}
	m.mu.Unlock()
	now := m.o.Now()
	v.Issues = issues(d, cp, now)
	v.NightOff = cp.nightOff
	if !cp.lastOK.IsZero() {
		t := cp.lastOK
		v.LastOK = &t
	}
	v.Error = cp.err
	v.OK = cp.status != nil && cp.fails == 0
	if st := cp.status; st != nil {
		b, tmp, rssi, free := st.Battery.Level, st.Battery.Temperature, st.Wifi.RSSI, st.Storage.AvailableMB
		v.Battery, v.Temperature, v.Charging, v.ScreenOn = &b, &tmp, st.Battery.Charging, st.Screen.On
		if st.Wifi.Connected {
			v.Wifi, v.RSSI = st.Wifi.SSID, &rssi
		}
		if st.Storage.TotalMB > 0 {
			v.FreeMB = &free
		}
		v.Model = strings.TrimSpace(st.Device.Manufacturer + " " + st.Device.Model)
		v.Android = st.Device.Android
		v.AutoBright = st.AutoBrightness.Enabled
		if a := st.Sensors.Accelerometer; a != nil && d.Kiosk.Posizione != nil {
			t := math.Round(Tilt(*d.Kiosk.Posizione, [3]float64{a.X, a.Y, a.Z})*10) / 10
			v.TiltDeg = &t
		}
	}
	return v
}

func issues(d config.Device, s state, now time.Time) []Issue {
	out := []Issue{}
	add := func(code, text string, isErr bool) {
		out = append(out, Issue{DeviceID: d.ID, Device: d.Nome, Code: code, Text: text, Error: isErr})
	}
	if s.err == ErrAuth {
		add("auth", "chiave API del tablet errata", true)
	}
	if s.fails >= UnreachableAfter {
		add("unreachable", "tablet non raggiungibile (app kiosk o rete)", true)
		return out // i dati che seguono sarebbero vecchi
	}
	st := s.status
	if st == nil {
		return out
	}
	if !s.notChargingSince.IsZero() && now.Sub(s.notChargingSince) >= UnpluggedAfter {
		add("unplugged", fmt.Sprintf("tablet scollegato dalla corrente (batteria %.0f%%)", st.Battery.Level), true)
	}
	if st.Battery.Level > 0 && st.Battery.Level <= BatteryLow {
		add("battery", fmt.Sprintf("batteria scarica: %.0f%%", st.Battery.Level), true)
	}
	if st.Battery.Temperature >= TemperatureHigh {
		add("temperature", fmt.Sprintf("tablet surriscaldato: batteria a %.0f °C", st.Battery.Temperature), true)
	}
	if a := st.Sensors.Accelerometer; a != nil && d.Kiosk.Posizione != nil {
		if t := Tilt(*d.Kiosk.Posizione, [3]float64{a.X, a.Y, a.Z}); t >= MovedDegrees {
			add("moved", fmt.Sprintf("tablet spostato o inclinato di %.0f°", t), false)
		}
	}
	return out
}

// Issues: problemi di tutti i tablet configurati, ordinati per nome.
func (m *Manager) Issues() []Issue {
	var out []Issue
	for _, d := range m.o.Config.Get().Globali.Dispositivi {
		if d.Kiosk != nil && d.Kiosk.Tipo != "" {
			out = append(out, m.ViewOf(d).Issues...)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Device < out[j].Device })
	return out
}

// Announce sveglia tutti i tablet, alza il volume e legge il messaggio (emergenza).
// Restituisce quanti tablet hanno ricevuto l'annuncio.
func (m *Manager) Announce(ctx context.Context, text string) (ok, total int) {
	ts := m.targets()
	var mu sync.Mutex
	var wg sync.WaitGroup
	for _, t := range ts {
		wg.Add(1)
		go func(t target) {
			defer wg.Done()
			cctx, cancel := context.WithTimeout(ctx, 30*time.Second)
			defer cancel()
			_ = t.c.Post(cctx, "/api/screen/on", nil)
			_ = t.c.Post(cctx, "/api/wake", nil)
			_ = t.c.Post(cctx, "/api/volume", map[string]int{"value": 100})
			_ = t.c.Post(cctx, "/api/audio/beep", nil)
			err := t.c.Post(cctx, "/api/tts", map[string]string{"text": text, "language": "it-IT"})
			if err != nil {
				m.o.Logger.Warn("annuncio di emergenza non riuscito", "dispositivo", t.dev.Nome, "errore", err.Error())
				return
			}
			m.setNight(t.dev.ID, false)
			mu.Lock()
			ok++
			mu.Unlock()
		}(t)
	}
	wg.Wait()
	return ok, len(ts)
}
