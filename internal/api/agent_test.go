package api

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/toigoweb/totem/internal/config"
)

func (h *harness) agentPost(tok, path, ctype string, body []byte) resp {
	h.t.Helper()
	req, _ := http.NewRequest("POST", h.ts.URL+path, bytes.NewReader(body))
	req.Header.Set("Content-Type", ctype)
	if tok != "" {
		req.Header.Set("Authorization", "Bearer "+tok)
	}
	r, err := h.client().Do(req) // l'agente non ha il cookie del totem
	if err != nil {
		h.t.Fatal(err)
	}
	defer r.Body.Close()
	raw, _ := io.ReadAll(r.Body)
	out := resp{code: r.StatusCode, raw: raw, hdr: r.Header}
	_ = json.Unmarshal(raw, &out.body)
	return out
}

func TestPCAgent(t *testing.T) {
	h := newHarness(t)
	c, mt := h.pair("1234")
	M := func(method, path string, body any) resp { return h.do(c, method, path, body, MaintHeader, mt) }
	id := h.cfg.Get().Globali.Dispositivi[0].ID

	r := M("POST", "/api/maint/devices/"+id+"/agent/token", nil)
	tok, _ := r.body["token"].(string)
	if len(tok) < 32 {
		t.Fatal(string(r.raw))
	}
	if strings.Contains(mustRead(t, h), tok) {
		t.Fatal("token in chiaro su disco")
	}
	rep := `{"versione":"1.0","chrome":true,"rete":false,"batteria":64,"uptimeMin":300,"discoLiberoMB":1500,"riavvioInAttesa":true,"windows":"Windows 11 Pro 23H2","schermoSpento":false}`
	if r := h.agentPost("sbagliato", "/api/agent/report", "application/json", []byte(rep)); r.code != 401 {
		t.Fatal(r.code)
	}
	r = h.agentPost(tok, "/api/agent/report", "application/json", []byte(rep))
	if r.code != 200 || r.body["schermoSpento"] != false || len(r.body["comandi"].([]any)) != 0 {
		t.Fatal(string(r.raw))
	}
	// comandi: in coda, consegnati una volta sola
	if r := M("POST", "/api/maint/devices/"+id+"/agent/cmd/riavvia-chrome", nil); r.body["ok"] != true {
		t.Fatal(string(r.raw))
	}
	M("POST", "/api/maint/devices/"+id+"/agent/cmd/riavvia-chrome", nil) // doppio clic
	if r := M("POST", "/api/maint/devices/"+id+"/agent/cmd/esegui", nil); r.code != 404 {
		t.Fatal("azione arbitraria accettata")
	}
	r = h.agentPost(tok, "/api/agent/report", "application/json", []byte(rep))
	if cm := r.body["comandi"].([]any); len(cm) != 1 || cm[0].(map[string]any)["azione"] != "riavvia-chrome" {
		t.Fatal(string(r.raw))
	}
	if r := h.agentPost(tok, "/api/agent/report", "application/json", []byte(rep)); len(r.body["comandi"].([]any)) != 0 {
		t.Fatal("comando consegnato due volte")
	}
	// screenshot
	if r := h.agentPost(tok, "/api/agent/screenshot", "image/png", []byte("non png")); r.code != 400 {
		t.Fatal(r.code)
	}
	h.agentPost(tok, "/api/agent/screenshot", "image/png", []byte("\x89PNG schermo del pc"))
	if r := M("GET", "/api/maint/devices/"+id+"/agent/screenshot", nil); r.code != 200 || !strings.HasPrefix(string(r.raw), "\x89PNG") {
		t.Fatal(r.code)
	}
	// stato e avvisi: in Dispositivi e nel monitoraggio
	h.clk.Add(11 * time.Minute)
	mt = h.do(c, "POST", "/api/maint/login", map[string]any{"pin": "1234"}).body["token"].(string)
	h.agentPost(tok, "/api/agent/report", "application/json", []byte(rep))
	d := M("GET", "/api/maint/devices", nil).body["devices"].([]any)[0].(map[string]any)
	ag := d["agent"].(map[string]any)["stato"].(map[string]any)
	if ag["ok"] != true || ag["windows"] != "Windows 11 Pro 23H2" || ag["battery"].(float64) != 64 {
		t.Fatal(ag)
	}
	all := strings.Join(h.srv.monitorReport().Problems, "|") + "|" + strings.Join(h.srv.monitorReport().Warnings, "|")
	for _, want := range []string{"scollegato dalla corrente (batteria 64%)", "spazio su disco quasi esaurito", "aggiornamenti di Windows in attesa"} {
		if !strings.Contains(all, want) {
			t.Errorf("manca %q in %s", want, all)
		}
	}
	// schermo di notte: secondo l'orario del salvaschermo
	M("PUT", "/api/maint/devices/"+id+"/agent", map[string]any{"schermoNotte": true})
	h.cfg.Update(func(c *config.Config) error {
		c.Globali.Salvaschermo = config.Screensaver{OrarioNero: h.clk.Now().Format("15:04") + "-" + h.clk.Now().Add(2*time.Hour).Format("15:04")}
		return nil
	})
	if r := h.agentPost(tok, "/api/agent/report", "application/json", []byte(rep)); r.body["schermoSpento"] != true {
		t.Fatal(string(r.raw))
	}
	// emergenza: arriva nella risposta, lo schermo resta acceso; "Ripeti annuncio" va in coda
	M("POST", "/api/maint/emergency/start", map[string]any{"tipo": "evacuazione"})
	r = h.agentPost(tok, "/api/agent/report", "application/json", []byte(rep))
	if r.body["schermoSpento"] != false || r.body["emergenza"].(map[string]any)["titolo"] != "Evacuazione" {
		t.Fatal(string(r.raw))
	}
	M("POST", "/api/maint/emergency/announce", nil)
	r = h.agentPost(tok, "/api/agent/report", "application/json", []byte(rep))
	if cm := r.body["comandi"].([]any); len(cm) != 1 || !strings.HasPrefix(cm[0].(map[string]any)["testo"].(string), "Evacuazione.") {
		t.Fatal(string(r.raw))
	}
	// silenzio: errore dopo 3 minuti
	h.clk.Add(4 * time.Minute)
	if p := strings.Join(h.srv.monitorReport().Problems, "|"); !strings.Contains(p, "PC non raggiungibile") {
		t.Fatal(p)
	}
	// scollegare l'agente: il token non vale più
	mt = h.do(c, "POST", "/api/maint/login", map[string]any{"pin": "1234"}).body["token"].(string)
	M("DELETE", "/api/maint/devices/"+id+"/agent", nil)
	if r := h.agentPost(tok, "/api/agent/report", "application/json", []byte(rep)); r.code != 401 {
		t.Fatal(r.code)
	}
}

func TestAppCommandsAndBattery(t *testing.T) {
	h := newHarness(t)
	c, mt := h.pair("1234")
	M := func(method, path string, body any) resp { return h.do(c, method, path, body, MaintHeader, mt) }
	id := h.cfg.Get().Globali.Dispositivi[0].ID
	if r := M("POST", "/api/maint/devices/"+id+"/app/ricarica", nil); r.body["ok"] != true {
		t.Fatal(string(r.raw))
	}
	if r := M("POST", "/api/maint/devices/"+id+"/app/formatta", nil); r.code != 404 {
		t.Fatal("azione non prevista accettata")
	}
	e := h.do(c, "GET", "/api/emergency", nil)
	if cm := e.body["commands"].([]any); len(cm) != 1 || cm[0].(map[string]any)["action"] != "ricarica" {
		t.Fatal(string(e.raw))
	}
	if e := h.do(c, "GET", "/api/emergency", nil); len(e.body["commands"].([]any)) != 0 {
		t.Fatal("comando consegnato due volte")
	}
	// batteria dall'app: avviso dopo 10 minuti senza ricarica
	hb := map[string]any{"appVersion": "1.2.0", "platform": "windows", "documents": 3, "battery": map[string]any{"level": 55, "charging": false},
		"browser": "Chrome 131", "view": "Documento: Acetone"}
	h.do(c, "POST", "/api/heartbeat", hb)
	if p := h.srv.monitorReport().Problems; len(p) != 0 {
		t.Fatal(p)
	}
	h.clk.Add(11 * time.Minute)
	h.do(c, "POST", "/api/heartbeat", hb)
	if p := strings.Join(h.srv.monitorReport().Problems, "|"); !strings.Contains(p, "scollegato dalla corrente (batteria 55%)") {
		t.Fatal(p)
	}
	mt = h.do(c, "POST", "/api/maint/login", map[string]any{"pin": "1234"}).body["token"].(string)
	d := M("GET", "/api/maint/devices", nil).body["devices"].([]any)[0].(map[string]any)
	if d["browser"] != "Chrome 131" || d["view"] != "Documento: Acetone" || len(d["appIssues"].([]any)) != 1 {
		t.Fatal(d)
	}
	// di nuovo in carica: niente avviso
	hb["battery"] = map[string]any{"level": 56, "charging": true}
	h.do(c, "POST", "/api/heartbeat", hb)
	if p := h.srv.monitorReport().Problems; len(p) != 0 {
		t.Fatal(p)
	}
}

func mustRead(t *testing.T, h *harness) string {
	b, err := os.ReadFile(filepath.Join(h.data, "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
