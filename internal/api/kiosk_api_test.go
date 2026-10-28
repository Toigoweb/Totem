package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// FreeKiosk simulato: registra i comandi ricevuti.
type fakeTablet struct {
	srv      *httptest.Server
	mu       sync.Mutex
	calls    []string
	charging bool
}

func newTablet(t *testing.T) *fakeTablet {
	f := &fakeTablet{charging: true}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Api-Key") != "chiave-del-tablet" {
			w.WriteHeader(401)
			return
		}
		b, _ := io.ReadAll(r.Body)
		f.mu.Lock()
		f.calls = append(f.calls, strings.TrimSpace(r.Method+" "+r.URL.Path+" "+string(b)))
		ch := f.charging
		f.mu.Unlock()
		switch r.URL.Path {
		case "/api/status":
			json.NewEncoder(w).Encode(map[string]any{"success": true, "data": map[string]any{
				"battery": map[string]any{"level": 64, "charging": ch, "temperature": 31},
				"screen":  map[string]any{"on": true}, "device": map[string]any{"model": "Tab A9+", "manufacturer": "samsung", "android": "14"},
				"wifi": map[string]any{"connected": true, "ssid": "Reparto", "rssi": -60}, "storage": map[string]any{"totalMB": 64000, "availableMB": 30000},
				"sensors": map[string]any{"accelerometer": map[string]any{"x": 0, "y": 9.8, "z": 0}}}})
		case "/api/info":
			json.NewEncoder(w).Encode(map[string]any{"success": true, "data": map[string]any{"version": "1.2.20", "isDeviceOwner": true, "kioskMode": true}})
		case "/api/screenshot":
			w.Header().Set("Content-Type", "image/png")
			w.Write([]byte("\x89PNG schermo"))
		default:
			io.WriteString(w, `{"success":true,"data":{}}`)
		}
	}))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeTablet) addr() string { return strings.TrimPrefix(f.srv.URL, "http://") }
func (f *fakeTablet) has(prefix string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, c := range f.calls {
		if strings.HasPrefix(c, prefix) {
			return true
		}
	}
	return false
}

func TestKioskTablet(t *testing.T) {
	h := newHarness(t)
	c, mt := h.pair("1234")
	M := func(method, path string, body any) resp { return h.do(c, method, path, body, MaintHeader, mt) }
	tab := newTablet(t)
	id := h.cfg.Get().Globali.Dispositivi[0].ID

	if r := M("POST", "/api/maint/kiosk/test", map[string]any{"indirizzo": tab.addr(), "chiave": "sbagliata"}); r.body["ok"] != false || r.body["code"] != "auth" {
		t.Fatal(string(r.raw))
	}
	r := M("POST", "/api/maint/kiosk/test", map[string]any{"indirizzo": tab.addr(), "chiave": "chiave-del-tablet"})
	if r.body["ok"] != true || r.body["deviceOwner"] != true || r.body["model"] != "samsung Tab A9+" {
		t.Fatal(string(r.raw))
	}
	// validazioni
	if r := M("PUT", "/api/maint/devices/"+id+"/kiosk", map[string]any{"tipo": "freekiosk", "indirizzo": "http://x"}); r.body["field"] != "indirizzo" {
		t.Fatal(string(r.raw))
	}
	if r := M("PUT", "/api/maint/devices/"+id+"/kiosk", map[string]any{"tipo": "freekiosk", "indirizzo": tab.addr()}); r.body["field"] != "chiave" {
		t.Fatal(string(r.raw))
	}
	if r := M("PUT", "/api/maint/devices/"+id+"/kiosk", map[string]any{"tipo": "freekiosk", "indirizzo": tab.addr(), "chiave": "chiave-del-tablet", "luminositaAuto": true}); r.code != 200 {
		t.Fatal(string(r.raw))
	}
	raw, _ := os.ReadFile(filepath.Join(h.data, "config.json"))
	if strings.Contains(string(raw), "chiave-del-tablet") {
		t.Fatal("chiave in chiaro su disco")
	}
	h.km.Tick(context.Background())
	list := M("GET", "/api/maint/devices", nil)
	if strings.Contains(string(list.raw), "chiave-del-tablet") || strings.Contains(string(list.raw), "v1:") {
		t.Fatal("chiave inviata al browser")
	}
	k := list.body["devices"].([]any)[0].(map[string]any)["kiosk"].(map[string]any)
	st := k["stato"].(map[string]any)
	if k["hasKey"] != true || st["ok"] != true || st["battery"].(float64) != 64 || st["wifi"] != "Reparto" {
		t.Fatal(k)
	}
	if !tab.has("POST /api/autoBrightness/enable") {
		t.Fatal("luminosità automatica non applicata")
	}
	// modifica senza chiave: resta quella salvata
	if r := M("PUT", "/api/maint/devices/"+id+"/kiosk", map[string]any{"tipo": "freekiosk", "indirizzo": tab.addr(), "schermoNotte": true}); r.code != 200 {
		t.Fatal(string(r.raw))
	}
	if r := M("POST", "/api/maint/kiosk/test", map[string]any{"deviceId": id, "indirizzo": tab.addr()}); r.body["ok"] != true {
		t.Fatal(string(r.raw))
	}
	// azioni e screenshot
	if r := M("POST", "/api/maint/devices/"+id+"/kiosk/ricarica", nil); r.body["ok"] != true || !tab.has("POST /api/reload") {
		t.Fatal(string(r.raw))
	}
	if r := M("POST", "/api/maint/devices/"+id+"/kiosk/js", nil); r.code != 404 {
		t.Fatal("azione non prevista accettata")
	}
	if r := M("GET", "/api/maint/devices/"+id+"/kiosk/screenshot", nil); r.code != 200 || !strings.HasPrefix(string(r.raw), "\x89PNG") {
		t.Fatal(r.code)
	}
	if r := h.do(c, "GET", "/api/maint/devices/"+id+"/kiosk/screenshot", nil); r.code != 401 {
		t.Fatal("screenshot senza PIN")
	}

	// tablet scollegato dalla corrente: errore nel monitoraggio dopo 10 minuti
	tab.mu.Lock()
	tab.charging = false
	tab.mu.Unlock()
	h.km.Tick(context.Background())
	h.clk.Add(11 * time.Minute)
	h.km.Tick(context.Background())
	rep := h.srv.monitorReport()
	if rep.OK || len(rep.Tablets) != 1 || !strings.Contains(strings.Join(rep.Problems, "|"), "scollegato dalla corrente") {
		t.Fatal(rep.Problems, rep.Tablets)
	}

	// scollegare il tablet (la sessione di manutenzione è scaduta con l'orologio: si rientra)
	mt = h.do(c, "POST", "/api/maint/login", map[string]any{"pin": "1234"}).body["token"].(string)
	if r := M("PUT", "/api/maint/devices/"+id+"/kiosk", map[string]any{"tipo": ""}); r.code != 200 || h.cfg.Get().Globali.Dispositivi[0].Kiosk != nil {
		t.Fatal(string(r.raw))
	}
}

func TestEmergency(t *testing.T) {
	h := newHarness(t)
	c, mt := h.pair("1234")
	M := func(method, path string, body any) resp { return h.do(c, method, path, body, MaintHeader, mt) }
	tab := newTablet(t)
	id := h.cfg.Get().Globali.Dispositivi[0].ID
	M("PUT", "/api/maint/devices/"+id+"/kiosk", map[string]any{"tipo": "freekiosk", "indirizzo": tab.addr(), "chiave": "chiave-del-tablet"})

	if r := h.do(c, "GET", "/api/emergency", nil); r.body["active"] != false {
		t.Fatal(string(r.raw))
	}
	if r := h.do(h.client(), "GET", "/api/emergency", nil); r.code != 401 {
		t.Fatal("emergenza letta senza abbinamento")
	}
	if r := M("POST", "/api/maint/emergency/start", map[string]any{"tipo": "avviso"}); r.code != 400 || r.body["field"] != "testo" {
		t.Fatal(string(r.raw))
	}
	r := M("POST", "/api/maint/emergency/start", map[string]any{"tipo": "evacuazione"})
	if r.body["ok"] != true || r.body["tablets"].(float64) != 1 {
		t.Fatal(string(r.raw))
	}
	e := h.do(c, "GET", "/api/emergency", nil)
	if e.body["active"] != true || e.body["title"] != "Evacuazione" || !strings.Contains(e.body["text"].(string), "punto di raccolta") {
		t.Fatal(string(e.raw))
	}
	// annuncio vocale sul tablet (in background)
	deadline := time.Now().Add(3 * time.Second)
	for !tab.has(`POST /api/tts {"language":"it-IT","text":"Evacuazione.`) && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if !tab.has(`POST /api/volume {"value":100}`) || !tab.has(`POST /api/tts`) {
		t.Fatal(tab.calls)
	}
	if rep := h.srv.monitorReport(); rep.Emergency == nil || !strings.Contains(strings.Join(rep.Warnings, "|"), "emergenza in corso") {
		t.Fatal(rep.Warnings)
	}
	if r := M("POST", "/api/maint/emergency/stop", nil); r.body["ok"] != true || h.em.Active() {
		t.Fatal(string(r.raw))
	}
	if r := M("POST", "/api/maint/emergency/announce", nil); r.code != 409 {
		t.Fatal("annuncio senza emergenza")
	}

	// API esterna con token
	if r := h.do(h.client(), "POST", "/api/emergency/start", nil); r.code != 404 {
		t.Fatal("API attiva senza token")
	}
	tok := M("POST", "/api/maint/emergency/token", nil).body["token"].(string)
	if r := h.do(h.client(), "POST", "/api/emergency/start", nil, "Authorization", "Bearer sbagliato"); r.code != 401 {
		t.Fatal(r.code)
	}
	if r := h.do(h.client(), "POST", "/api/emergency/start", nil, "Authorization", "Bearer "+tok); r.code != 200 || !h.em.Active() || h.em.Get().Da != "API" {
		t.Fatal(string(r.raw))
	}
	if r := h.do(h.client(), "POST", "/api/emergency/start", map[string]any{"tipo": "avviso", "testo": "Fuga di gas in magazzino"}, "Authorization", "Bearer "+tok); r.code != 200 || h.em.Get().Tipo != "avviso" {
		t.Fatal(string(r.raw))
	}
	if r := h.do(h.client(), "POST", "/api/emergency/stop", nil, "Authorization", "Bearer "+tok); r.code != 200 || h.em.Active() {
		t.Fatal(string(r.raw))
	}
	M("DELETE", "/api/maint/emergency/token", nil)
	if r := h.do(h.client(), "POST", "/api/emergency/start", nil, "Authorization", "Bearer "+tok); r.code != 404 {
		t.Fatal("token revocato ancora valido")
	}
}
