package kiosk

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/toigoweb/totem/internal/config"
)

// fakeKiosk simula l'API REST di FreeKiosk quanto basta.
type fakeKiosk struct {
	srv   *httptest.Server
	mu    sync.Mutex
	calls []string
	st    map[string]any
	key   string
}

func newFake(t *testing.T) *fakeKiosk {
	f := &fakeKiosk{key: "chiave-giusta"}
	f.st = map[string]any{
		"battery":        map[string]any{"level": 85, "charging": true, "temperature": 30.5},
		"screen":         map[string]any{"on": true, "brightness": 75},
		"device":         map[string]any{"model": "SM-X210", "manufacturer": "samsung", "android": "14"},
		"wifi":           map[string]any{"connected": true, "ssid": "Reparto", "rssi": -55},
		"sensors":        map[string]any{"light": 300, "accelerometer": map[string]any{"x": 0.0, "y": 9.8, "z": 0.3}},
		"autoBrightness": map[string]any{"enabled": false},
		"storage":        map[string]any{"totalMB": 64000, "availableMB": 40000},
	}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Api-Key") != f.key {
			w.WriteHeader(401)
			io.WriteString(w, `{"success":false,"error":"Unauthorized"}`)
			return
		}
		b, _ := io.ReadAll(r.Body)
		f.mu.Lock()
		f.calls = append(f.calls, strings.TrimSpace(r.Method+" "+r.URL.Path+" "+string(b)))
		st := f.st
		f.mu.Unlock()
		switch r.URL.Path {
		case "/api/status":
			json.NewEncoder(w).Encode(map[string]any{"success": true, "data": st})
		case "/api/info":
			json.NewEncoder(w).Encode(map[string]any{"success": true, "data": map[string]any{"version": "1.2.20", "isDeviceOwner": true, "kioskMode": true}})
		case "/api/screenshot":
			w.Header().Set("Content-Type", "image/png")
			w.Write([]byte("\x89PNG finto"))
		case "/api/reboot":
			w.WriteHeader(500)
			io.WriteString(w, `{"success":false,"error":"Device Owner required"}`)
		default:
			io.WriteString(w, `{"success":true,"data":{}}`)
		}
	}))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeKiosk) addr() string { return strings.TrimPrefix(f.srv.URL, "http://") }

func (f *fakeKiosk) set(path string, v any) {
	f.mu.Lock()
	defer f.mu.Unlock()
	parts := strings.Split(path, ".")
	m := f.st
	for _, p := range parts[:len(parts)-1] {
		m = m[p].(map[string]any)
	}
	m[parts[len(parts)-1]] = v
}

func (f *fakeKiosk) took(prefix string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, c := range f.calls {
		if strings.HasPrefix(c, prefix) {
			n++
		}
	}
	return n
}

type clock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *clock) Now() time.Time      { c.mu.Lock(); defer c.mu.Unlock(); return c.t }
func (c *clock) Add(d time.Duration) { c.mu.Lock(); c.t = c.t.Add(d); c.mu.Unlock() }

func setup(t *testing.T, f *fakeKiosk, k config.Kiosk) (*Manager, *config.Store, *clock, *bool) {
	dir := t.TempDir()
	cfg, _ := config.Open(filepath.Join(dir, "config.json"), filepath.Join(dir, "backup"))
	ci, _, _ := config.LoadOrCreateKey(filepath.Join(dir, "key"))
	enc, _ := ci.Encrypt(config.KioskKeyID("d_tab"), "chiave-giusta")
	k.Tipo, k.Indirizzo, k.Chiave = "freekiosk", f.addr(), enc
	cfg.Update(func(c *config.Config) error {
		c.Globali.Dispositivi = []config.Device{{ID: "d_tab", Nome: "totem magazzino", Ruolo: "totem", Kiosk: &k}}
		c.Globali.Salvaschermo = config.Screensaver{OrarioNero: "20:00-06:00"}
		return nil
	})
	clk := &clock{t: time.Date(2026, 10, 28, 12, 0, 0, 0, time.Local)}
	emerg := false
	m := New(Options{Config: cfg, Cipher: ci, Logger: slog.New(slog.NewTextHandler(io.Discard, nil)), Now: clk.Now,
		EmergencyActive: func() bool { return emerg }})
	return m, cfg, clk, &emerg
}

func dev(cfg *config.Store) config.Device { return cfg.Get().Globali.Dispositivi[0] }

func codes(is []Issue) string {
	var s []string
	for _, i := range is {
		s = append(s, i.Code)
	}
	return strings.Join(s, ",")
}

func TestStatusAndIssues(t *testing.T) {
	f := newFake(t)
	m, cfg, clk, _ := setup(t, f, config.Kiosk{LuminositaAuto: true})
	ctx := context.Background()
	m.Tick(ctx)
	v := m.ViewOf(dev(cfg))
	if !v.OK || *v.Battery != 85 || v.Wifi != "Reparto" || v.Model != "samsung SM-X210" || len(v.Issues) != 0 {
		t.Fatalf("%+v", v)
	}
	if dev(cfg).Kiosk.Posizione == nil {
		t.Fatal("posizione di riferimento non salvata")
	}
	if f.took("POST /api/autoBrightness/enable") != 1 {
		t.Fatal("luminosità automatica non attivata")
	}

	// scollegato: avviso solo dopo 10 minuti senza ricarica
	f.set("battery.charging", false)
	m.Tick(ctx)
	if codes(m.Issues()) != "" {
		t.Fatal(m.Issues())
	}
	clk.Add(11 * time.Minute)
	m.Tick(ctx)
	if c := codes(m.Issues()); c != "unplugged" {
		t.Fatal(c)
	}
	// batteria scarica, surriscaldamento, spostato
	f.set("battery.level", 15)
	f.set("battery.temperature", 47)
	f.set("sensors.accelerometer", map[string]any{"x": 9.8, "y": 0.0, "z": 0.3})
	m.Tick(ctx)
	if c := codes(m.Issues()); c != "unplugged,battery,temperature,moved" {
		t.Fatal(c)
	}
	// nuova posizione di riferimento
	if err := m.ResetPosition("d_tab"); err != nil {
		t.Fatal(err)
	}
	if c := codes(m.Issues()); strings.Contains(c, "moved") {
		t.Fatal(c)
	}
	// irraggiungibile dopo 3 controlli falliti
	f.srv.Close()
	for i := 0; i < UnreachableAfter; i++ {
		m.Tick(ctx)
	}
	if c := codes(m.Issues()); c != "unreachable" {
		t.Fatal(c)
	}
}

func TestWrongKey(t *testing.T) {
	f := newFake(t)
	f.key = "altra"
	m, cfg, _, _ := setup(t, f, config.Kiosk{})
	m.Tick(context.Background())
	if v := m.ViewOf(dev(cfg)); v.OK || v.Error != ErrAuth || codes(v.Issues) != "auth" {
		t.Fatalf("%+v", v)
	}
}

func TestNightScreen(t *testing.T) {
	f := newFake(t)
	m, _, clk, emerg := setup(t, f, config.Kiosk{SchermoNotte: true})
	ctx := context.Background()
	m.Tick(ctx) // mezzogiorno: niente
	if f.took("POST /api/screen/") != 0 {
		t.Fatal("comando di giorno")
	}
	clk.Add(9 * time.Hour) // 21:00
	m.Tick(ctx)
	m.Tick(ctx)
	if f.took("POST /api/screen/off") != 1 {
		t.Fatal("schermo non spento una sola volta")
	}
	// emergenza di notte: lo schermo torna acceso
	*emerg = true
	m.Tick(ctx)
	if f.took("POST /api/screen/on") != 1 {
		t.Fatal("schermo non riacceso per l'emergenza")
	}
	*emerg = false
	m.Tick(ctx)
	if f.took("POST /api/screen/off") != 2 {
		t.Fatal("schermo non rispento dopo l'emergenza")
	}
	clk.Add(10 * time.Hour) // 07:00
	m.Tick(ctx)
	if f.took("POST /api/screen/on") != 2 {
		t.Fatal("schermo non riacceso al mattino")
	}
}

func TestAnnounceAndActions(t *testing.T) {
	f := newFake(t)
	m, _, _, _ := setup(t, f, config.Kiosk{})
	ctx := context.Background()
	if ok, n := m.Announce(ctx, "Evacuazione"); ok != 1 || n != 1 {
		t.Fatal(ok, n)
	}
	for _, c := range []string{"POST /api/wake", `POST /api/volume {"value":100}`, "POST /api/audio/beep", `POST /api/tts {"language":"it-IT","text":"Evacuazione"}`} {
		if f.took(c) != 1 {
			t.Errorf("manca %s", c)
		}
	}
	c, _, err := m.ClientFor("d_tab")
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Post(ctx, Actions["riavvia-tablet"], nil); Code(err) != ErrDevice || !strings.Contains(err.Error(), "Device Owner") {
		t.Fatal(err)
	}
	if b, err := c.Screenshot(ctx); err != nil || !strings.HasPrefix(string(b), "\x89PNG") {
		t.Fatal(err)
	}
	if i, err := c.Info(ctx); err != nil || !i.IsDeviceOwner {
		t.Fatal(i, err)
	}
	if err := m.NewClient("127.0.0.1:1", "x").Post(ctx, "/api/reload", nil); Code(err) != ErrUnreachable {
		t.Fatal(err)
	}
}

func TestHelpers(t *testing.T) {
	for in, want := range map[string]string{"192.168.2.60": "192.168.2.60:8080", "tablet-1.local:9000": "tablet-1.local:9000", " 10.0.0.5:8080 ": "10.0.0.5:8080",
		"http://10.0.0.5": "", "10.0.0.5:0": "", "a b": "", "10.0.0.5/x": "", "": ""} {
		got, ok := NormalizeAddress(in)
		if got != want || ok != (want != "") {
			t.Errorf("%q: %q %v", in, got, ok)
		}
	}
	at := func(h, m int) time.Time { return time.Date(2026, 1, 1, h, m, 0, 0, time.UTC) }
	if !InWindow(at(21, 0), "20:00-06:00") || !InWindow(at(5, 59), "20:00-06:00") || InWindow(at(6, 0), "20:00-06:00") || InWindow(at(12, 0), "20:00-06:00") {
		t.Fatal("fascia notturna")
	}
	if !InWindow(at(13, 0), "12:30-14:00") || InWindow(at(14, 0), "12:30-14:00") || InWindow(at(1, 0), "x") {
		t.Fatal("fascia diurna")
	}
	if d := Tilt([3]float64{0, 9.8, 0}, [3]float64{9.8, 0, 0}); d < 89 || d > 91 {
		t.Fatal(d)
	}
}
