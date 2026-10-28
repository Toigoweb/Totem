package api

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/toigoweb/totem/internal/webpush"
)

// sse apre il flusso di eventi dell'interfono e restituisce gli eventi ricevuti.
func (h *harness) sse(c *http.Client) (<-chan map[string]any, func()) {
	h.t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	req, _ := http.NewRequestWithContext(ctx, "GET", h.ts.URL+"/api/intercom/events", nil)
	r, err := c.Do(req)
	if err != nil || r.StatusCode != 200 {
		h.t.Fatal("flusso di eventi", err, r.StatusCode)
	}
	if r.Header.Get("X-Accel-Buffering") != "no" || !strings.HasPrefix(r.Header.Get("Content-Type"), "text/event-stream") {
		h.t.Fatal(r.Header)
	}
	ch := make(chan map[string]any, 16)
	go func() {
		defer r.Body.Close()
		sc := bufio.NewScanner(r.Body)
		for sc.Scan() {
			if line, ok := strings.CutPrefix(sc.Text(), "data: "); ok {
				var e map[string]any
				json.Unmarshal([]byte(line), &e)
				ch <- e
			}
		}
	}()
	return ch, cancel
}

func waitEvent(t *testing.T, ch <-chan map[string]any, typ string) map[string]any {
	t.Helper()
	select {
	case e := <-ch:
		if e["type"] != typ {
			t.Fatalf("atteso %s, ricevuto %v", typ, e)
		}
		return e
	case <-time.After(3 * time.Second):
		t.Fatal("nessun evento", typ)
	}
	return nil
}

// servizio di notifica simulato
type fakePush struct {
	srv *httptest.Server
	mu  sync.Mutex
	n   int
}

func (f *fakePush) count() int { f.mu.Lock(); defer f.mu.Unlock(); return f.n }

func TestIntercom(t *testing.T) {
	h, totem, mt := extrasHarness(t)
	M := func(c *http.Client, tok, method, path string, body any) resp {
		return h.do(c, method, path, body, MaintHeader, tok)
	}
	totemID := h.cfg.Get().Globali.Dispositivi[0].ID

	fp := &fakePush{}
	fp.srv = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.Copy(io.Discard, r.Body)
		if r.Header.Get("Content-Encoding") == "aes128gcm" && strings.HasPrefix(r.Header.Get("Authorization"), "vapid ") {
			fp.mu.Lock()
			fp.n++
			fp.mu.Unlock()
		}
		w.WriteHeader(201)
	}))
	defer fp.srv.Close()
	keys, _ := webpush.LoadOrCreateKeys(filepath.Join(h.data, "vapid.key"))
	h.srv.o.Push = &webpush.Sender{Keys: keys, HTTP: fp.srv.Client()}
	h.srv.IntercomHooks()

	// spento: niente API, niente campo nel manifest, niente codici per i telefoni
	if r := h.do(totem, "GET", "/api/intercom/directory", nil); r.code != 404 {
		t.Fatal("interfono spento ma raggiungibile", r.code)
	}
	if r := h.do(totem, "GET", "/api/intercom/events", nil); r.code != 404 {
		t.Fatal(r.code)
	}
	if _, ok := manifestOf(t, h, totem)["intercom"]; ok {
		t.Fatal("manifest con l'interfono spento")
	}
	if r := M(totem, mt, "POST", "/api/maint/intercom/phone-code", map[string]any{"nome": "x"}); r.code != 409 {
		t.Fatal(r.code)
	}

	// postazione
	r := M(totem, mt, "POST", "/api/maint/stations/pair-code", map[string]any{"nome": "Portineria"})
	station := h.client()
	r = h.do(station, "POST", "/api/setup/code", map[string]any{"code": r.body["code"]})
	stationID := r.body["deviceId"].(string)

	// acceso globalmente; il totem senza interruttore suo resta escluso
	if r := M(totem, mt, "PUT", "/api/maint/intercom", map[string]any{"attivo": true, "gruppi": []any{}}); r.code != 200 || r.body["attivo"] != true {
		t.Fatal(string(r.raw))
	}
	if r := h.do(totem, "GET", "/api/intercom/directory", nil); r.code != 404 {
		t.Fatal("interfono attivo su un dispositivo non abilitato")
	}
	for _, id := range []string{totemID, stationID} {
		if r := M(totem, mt, "PUT", "/api/maint/devices/"+id+"/intercom", map[string]any{"attivo": true, "numero": " 201 "}); r.code != 200 {
			t.Fatal(string(r.raw))
		}
	}
	if m := manifestOf(t, h, totem); m["intercom"] == nil {
		t.Fatal("manca intercom nel manifest")
	}

	// telefono abbinato con il codice dell'interfono (nella procedura guidata)
	r = M(totem, mt, "POST", "/api/maint/intercom/phone-code", map[string]any{"nome": "Telefono Marco"})
	if r.code != 200 || !strings.HasPrefix(r.body["path"].(string), "/?abbina=") {
		t.Fatal(string(r.raw))
	}
	phone := h.client()
	r = h.do(phone, "POST", "/api/setup/code", map[string]any{"code": r.body["code"], "platform": "android"})
	if r.code != 200 || r.body["role"] != "telefono" {
		t.Fatal(string(r.raw))
	}
	phoneID := r.body["deviceId"].(string)
	if r := h.do(phone, "POST", "/api/maint/login", map[string]any{"pin": "1234"}); r.code != 403 {
		t.Fatal("telefono in manutenzione", r.code)
	}
	sub := map[string]any{"endpoint": fp.srv.URL + "/push/1", "keys": map[string]any{
		"p256dh": "BCVxsr7N_eNgVRqvHtD0zTZsEc6-VV-JvLexhqUzORcxaOzi6-AYWXvTBHm4bjyPjs7Vd8pZGH6SRpkNtoIAiw4", "auth": "BTBZMqHH6r4Tts7J_aSIgg"}}
	if r := h.do(phone, "PUT", "/api/intercom/me", map[string]any{"push": sub}); r.code != 200 {
		t.Fatal(string(r.raw))
	}
	if r := h.do(phone, "PUT", "/api/intercom/me", map[string]any{"push": map[string]any{"endpoint": "http://x"}}); r.code != 400 {
		t.Fatal("sottoscrizione non valida accettata")
	}

	// gruppo con postazione e telefono
	r = M(totem, mt, "PUT", "/api/maint/intercom", map[string]any{"attivo": true, "gruppi": []any{
		map[string]any{"nome": "Primo soccorso", "numero": "333", "membri": []string{stationID, phoneID, "inesistente"}}}})
	groups := r.body["gruppi"].([]any)
	g := groups[0].(map[string]any)
	if len(g["membri"].([]any)) != 2 || g["id"] == "" {
		t.Fatal(string(r.raw))
	}

	stEv, stop := h.sse(station)
	defer stop()
	tEv, stopT := h.sse(totem)
	defer stopT()
	time.Sleep(50 * time.Millisecond)
	r = h.do(totem, "GET", "/api/intercom/directory", nil)
	dg := r.body["groups"].([]any)[0].(map[string]any)
	if dg["available"].(float64) != 2 || r.body["vapidKey"] == "" || len(r.body["devices"].([]any)) != 2 {
		t.Fatal(string(r.raw))
	}

	// chiamata al gruppo: squilla sulla postazione (evento) e sul telefono (notifica)
	r = h.do(totem, "POST", "/api/intercom/calls", map[string]any{"target": "group:" + g["id"].(string)})
	if r.code != 200 {
		t.Fatal(string(r.raw))
	}
	callID := r.body["call"].(map[string]any)["id"].(string)
	in := waitEvent(t, stEv, "incoming")
	if in["call"].(map[string]any)["label"] != "Primo soccorso" {
		t.Fatal(in)
	}
	deadline := time.Now().Add(3 * time.Second)
	for fp.count() < 1 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if fp.count() != 1 {
		t.Fatal("notifica di chiamata non inviata")
	}
	if r := h.do(totem, "POST", "/api/intercom/calls", map[string]any{"target": "device:" + stationID}); r.code != 409 {
		t.Fatal("seconda chiamata in uscita", r.code)
	}

	// risponde la postazione: al telefono arriva la notifica di fine squillo
	if r := h.do(station, "POST", "/api/intercom/calls/"+callID+"/answer", map[string]any{}); r.code != 200 {
		t.Fatal(string(r.raw))
	}
	waitEvent(t, tEv, "accepted")
	for fp.count() < 2 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if fp.count() != 2 {
		t.Fatal("notifica di fine non inviata")
	}
	if r := h.do(phone, "POST", "/api/intercom/calls/"+callID+"/answer", map[string]any{}); r.code != 409 {
		t.Fatal("risposta doppia", r.code)
	}
	// segnalazione WebRTC tra le due parti, non da terzi
	if r := h.do(totem, "POST", "/api/intercom/calls/"+callID+"/signal", map[string]any{"sdp": "offerta"}); r.code != 200 {
		t.Fatal(string(r.raw))
	}
	if e := waitEvent(t, stEv, "signal"); e["data"].(map[string]any)["sdp"] != "offerta" {
		t.Fatal(e)
	}
	if r := h.do(phone, "POST", "/api/intercom/calls/"+callID+"/signal", map[string]any{"sdp": "x"}); r.code != 409 {
		t.Fatal("segnale da un estraneo", r.code)
	}
	if r := h.do(station, "POST", "/api/intercom/calls/"+callID+"/hangup", map[string]any{}); r.code != 200 {
		t.Fatal(r.code)
	}
	if e := waitEvent(t, tEv, "ended"); e["reason"] != "hangup" {
		t.Fatal(e)
	}

	// telefono non disponibile e senza flusso aperto: chiamata subito chiusa con il numero
	h.do(phone, "PUT", "/api/intercom/me", map[string]any{"disponibile": false})
	r = h.do(totem, "POST", "/api/intercom/calls", map[string]any{"target": "device:" + phoneID})
	if r.body["reason"] != "offline" || r.body["call"].(map[string]any)["state"] != "ended" {
		t.Fatal(string(r.raw))
	}

	// spento il dispositivo: 404 e nessun campo nel manifest
	M(totem, mt, "PUT", "/api/maint/devices/"+totemID+"/intercom", map[string]any{"attivo": false})
	if r := h.do(totem, "GET", "/api/intercom/directory", nil); r.code != 404 {
		t.Fatal(r.code)
	}
	if m := manifestOf(t, h, totem); m["intercom"] != nil {
		t.Fatal("intercom nel manifest dopo lo spegnimento")
	}
	r = M(totem, mt, "GET", "/api/maint/intercom", nil)
	if r.body["push"] != true || len(r.body["devices"].([]any)) != 3 {
		t.Fatal(string(r.raw))
	}
}
