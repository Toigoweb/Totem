package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/toigoweb/totem/internal/config"
	"github.com/toigoweb/totem/internal/scanner"
	"github.com/toigoweb/totem/internal/smbsrc"
	"github.com/toigoweb/totem/internal/sources"
	"github.com/toigoweb/totem/internal/synclog"
)

type clock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *clock) Now() time.Time { c.mu.Lock(); defer c.mu.Unlock(); return c.t }
func (c *clock) Add(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

type harness struct {
	t     *testing.T
	clk   *clock
	cfg   *config.Store
	ci    *config.Cipher
	sc    *scanner.Scanner
	srv   *Server
	ts    *httptest.Server
	share string
	data  string
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	h := &harness{t: t, clk: &clock{t: time.Date(2026, 10, 24, 8, 0, 0, 0, time.UTC)}, data: t.TempDir(), share: t.TempDir()}
	h.cfg, _ = config.Open(filepath.Join(h.data, "config.json"), filepath.Join(h.data, "backup"))
	h.ci, _, _ = config.LoadOrCreateKey(filepath.Join(h.data, "key"))
	lg := slog.New(slog.NewTextHandler(io.Discard, nil))
	sl, _ := synclog.Open(filepath.Join(h.data, "logs", "sync.log"), 1<<20, 2)
	var err error
	h.sc, err = scanner.New(scanner.Options{
		DataDir: h.data, Config: h.cfg, Cipher: h.ci, Settings: config.DefaultSettings(), Log: sl, Logger: lg, Now: h.clk.Now,
		Connect: func(ctx context.Context, t smbsrc.Target) (smbsrc.Source, error) {
			if t.Type != config.SourceSMB {
				return sources.Connect(ctx, t)
			}
			if t.Path != `\\srv\sds` && !strings.HasPrefix(t.Path, `\\srv\sds\`) {
				return nil, &smbsrc.Error{Code: smbsrc.ErrNotFound, Err: errors.New("bad network name")}
			}
			if t.Password != "giusta" {
				return nil, &smbsrc.Error{Code: smbsrc.ErrAuth, Err: errors.New("logon failure")}
			}
			sub := strings.TrimPrefix(strings.TrimPrefix(t.Path, `\\srv\sds`), `\`)
			return &smbsrc.DirSource{Root: filepath.Join(h.share, sub), Recursive: t.Recursive}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	h.srv = New(Options{Config: h.cfg, Cipher: h.ci, Scanner: h.sc, Log: sl, Settings: config.DefaultSettings(), DataDir: h.data,
		Version: "test", Logger: lg, Now: h.clk.Now})
	h.srv.EnsureSetupCode()
	h.ts = httptest.NewTLSServer(h.srv.Handler())
	t.Cleanup(h.ts.Close)
	return h
}

// client restituisce un client con cookie jar (il cookie è Secure: serve TLS).
func (h *harness) client() *http.Client {
	jar, _ := cookiejar.New(nil)
	c := *h.ts.Client() // copia: il client di httptest è condiviso
	c.Jar = jar
	return &c
}

type resp struct {
	code int
	body map[string]any
	raw  []byte
	hdr  http.Header
}

func (h *harness) do(c *http.Client, method, path string, body any, hdr ...string) resp {
	h.t.Helper()
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, h.ts.URL+path, rd)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for i := 0; i+1 < len(hdr); i += 2 {
		req.Header.Set(hdr[i], hdr[i+1])
	}
	r, err := c.Do(req)
	if err != nil {
		h.t.Fatal(err)
	}
	defer r.Body.Close()
	raw, _ := io.ReadAll(r.Body)
	out := resp{code: r.StatusCode, raw: raw, hdr: r.Header}
	_ = json.Unmarshal(raw, &out.body)
	return out
}

func (h *harness) code() string {
	h.srv.mu.Lock()
	defer h.srv.mu.Unlock()
	return h.srv.setupCode
}

// pair esegue la procedura guidata e restituisce il client abbinato e il token di manutenzione.
func (h *harness) pair(pin string) (*http.Client, string) {
	h.t.Helper()
	c := h.client()
	r := h.do(c, "POST", "/api/setup/code", map[string]any{"code": strings.ToLower(h.code())})
	if r.code != 200 {
		h.t.Fatalf("codice: %d %s", r.code, r.raw)
	}
	r = h.do(c, "POST", "/api/setup/pin", map[string]any{"setupToken": r.body["setupToken"], "pin": pin, "platform": "windows"})
	if r.code != 200 {
		h.t.Fatalf("pin: %d %s", r.code, r.raw)
	}
	return c, r.body["maintToken"].(string)
}

func pdf(s string) []byte { return []byte("%PDF-1.4\n" + s + "\n%%EOF\n") }

func TestSetupFlowAndDeviceAuth(t *testing.T) {
	h := newHarness(t)
	anon := h.client()
	if r := h.do(anon, "GET", "/manifest.json", nil); r.code != 401 {
		t.Fatal("manifest senza token", r.code)
	}
	r := h.do(anon, "GET", "/api/setup/state", nil)
	if r.body["paired"] != false || r.body["totemRegistered"] != false || r.body["pinSet"] != false {
		t.Fatal(string(r.raw))
	}
	r = h.do(anon, "POST", "/api/setup/code", map[string]any{"code": "AAAA-AAAA"})
	if r.code != 401 || r.body["remaining"].(float64) != 4 {
		t.Fatal(string(r.raw))
	}
	c, maint := h.pair("2468")
	if maint == "" {
		t.Fatal("token di manutenzione assente")
	}
	// il cookie è HttpOnly, Secure, SameSite=Strict
	u, _ := url.Parse(h.ts.URL)
	cookies := c.Jar.Cookies(u)
	if len(cookies) != 1 || cookies[0].Name != CookieName {
		t.Fatal(cookies)
	}
	cfgB, _ := os.ReadFile(filepath.Join(h.data, "config.json"))
	if strings.Contains(string(cfgB), cookies[0].Value) {
		t.Fatal("token in chiaro in config.json")
	}
	// codice monouso
	if r := h.do(h.client(), "POST", "/api/setup/code", map[string]any{"code": "X"}); r.code != 401 || r.body["totemPaired"] != true {
		t.Fatal("codice riutilizzabile", r.code, string(r.raw))
	}
	r = h.do(c, "GET", "/api/setup/state", nil)
	if r.body["paired"] != true || r.body["role"] != "totem" || r.body["pinSet"] != true {
		t.Fatal(string(r.raw))
	}
	h.sc.ScanOnce(context.Background())
	r = h.do(c, "GET", "/manifest.json", nil)
	if r.code != 200 || r.hdr.Get("ETag") == "" {
		t.Fatal(r.code)
	}
	if r2 := h.do(c, "GET", "/manifest.json", nil, "If-None-Match", r.hdr.Get("ETag")); r2.code != 304 {
		t.Fatal("ETag non rispettato", r2.code)
	}
}

func TestPinLockout(t *testing.T) {
	h := newHarness(t)
	c, _ := h.pair("1357")
	for i := 4; i >= 1; i-- {
		r := h.do(c, "POST", "/api/maint/login", map[string]any{"pin": "0000"})
		if r.code != 401 || int(r.body["remaining"].(float64)) != i {
			t.Fatal(string(r.raw))
		}
	}
	r := h.do(c, "POST", "/api/maint/login", map[string]any{"pin": "0000"})
	if r.code != 429 || r.body["retryAfterSec"].(float64) != 300 {
		t.Fatal(string(r.raw))
	}
	// durante il blocco anche il PIN giusto è rifiutato
	if r := h.do(c, "POST", "/api/maint/login", map[string]any{"pin": "1357"}); r.code != 429 {
		t.Fatal(r.code)
	}
	h.clk.Add(5*time.Minute + time.Second)
	r = h.do(c, "POST", "/api/maint/login", map[string]any{"pin": "1357"})
	if r.code != 200 || r.body["token"] == "" {
		t.Fatal(string(r.raw))
	}
	tok := r.body["token"].(string)
	if r := h.do(c, "GET", "/api/maint/sections", nil, MaintHeader, tok); r.code != 200 {
		t.Fatal(r.code)
	}
	h.clk.Add(10*time.Minute + time.Second)
	if r := h.do(c, "GET", "/api/maint/sections", nil, MaintHeader, tok); r.code != 401 {
		t.Fatal("token di manutenzione non scaduto dopo 10 minuti", r.code)
	}
	// senza cookie il token di manutenzione non basta
	if r := h.do(h.client(), "GET", "/api/maint/sections", nil, MaintHeader, tok); r.code != 401 {
		t.Fatal(r.code)
	}
}

func TestSectionsLifecycle(t *testing.T) {
	h := newHarness(t)
	c, mt := h.pair("1234")
	M := func(method, path string, body any) resp { return h.do(c, method, path, body, MaintHeader, mt) }
	os.WriteFile(filepath.Join(h.share, "Acetone_rev2.pdf"), pdf("a"), 0o644)

	in := map[string]any{"nome": "Schede di sicurezza", "icona": "flacone", "percorso": `\\srv\sds`, "utente": `AZ\totem`, "password": "giusta", "sottocartelle": true}
	if r := M("POST", "/api/maint/sections", in); r.code != 409 || r.body["error"] != "test_required" {
		t.Fatal("salvataggio senza prova", string(r.raw))
	}
	// prove fallite, distinte per causa
	bad := M("POST", "/api/maint/test", map[string]any{"percorso": `\\srv\sds`, "utente": `AZ\totem`, "password": "no"})
	if bad.body["ok"] != false || bad.body["code"] != "auth" {
		t.Fatal(string(bad.raw))
	}
	bad = M("POST", "/api/maint/test", map[string]any{"percorso": `\\srv\altro`, "utente": `AZ\totem`, "password": "giusta"})
	if bad.body["code"] != "not_found" {
		t.Fatal(string(bad.raw))
	}
	bad = M("POST", "/api/maint/test", map[string]any{"percorso": `srv\sds`})
	if bad.body["code"] != "invalid_path" {
		t.Fatal(string(bad.raw))
	}
	ok := M("POST", "/api/maint/test", map[string]any{"percorso": `\\srv\sds`, "utente": `AZ\totem`, "password": "giusta", "sottocartelle": true})
	if ok.body["ok"] != true || ok.body["pdfCount"].(float64) != 1 {
		t.Fatal(string(ok.raw))
	}
	// il token della prova vale solo per gli stessi valori
	in["testToken"] = ok.body["testToken"]
	in["password"] = "diversa"
	if r := M("POST", "/api/maint/sections", in); r.code != 409 {
		t.Fatal("token di prova accettato per un'altra password")
	}
	in["password"] = "giusta"
	r := M("POST", "/api/maint/sections", in)
	if r.code != 201 {
		t.Fatal(string(r.raw))
	}
	id := r.body["id"].(string)
	cfgB, _ := os.ReadFile(filepath.Join(h.data, "config.json"))
	if strings.Contains(string(cfgB), "giusta") {
		t.Fatal("password in chiaro su disco")
	}
	list := M("GET", "/api/maint/sections", nil)
	if strings.Contains(string(list.raw), "giusta") || strings.Contains(string(list.raw), "v1:") {
		t.Fatal("password inviata alla PWA")
	}
	if list.body["sections"].([]any)[0].(map[string]any)["hasPassword"] != true {
		t.Fatal(string(list.raw))
	}
	// cambiare solo nome e icona non richiede una prova
	upd := map[string]any{"nome": "SDS", "icona": "scudo", "emergenza": true, "percorso": `\\srv\sds`, "utente": `AZ\totem`, "sottocartelle": true}
	if r := M("PUT", "/api/maint/sections/"+id, upd); r.code != 200 {
		t.Fatal(string(r.raw))
	}
	// cambiare il percorso sì; con password vuota si usa quella salvata
	upd["percorso"] = `\\srv\sds\sub`
	if r := M("PUT", "/api/maint/sections/"+id, upd); r.code != 409 {
		t.Fatal("percorso cambiato senza prova")
	}
	upd["percorso"] = `\\srv\sds`
	upd["utente"] = `AZ\altro`
	ok = M("POST", "/api/maint/test", map[string]any{"sectionId": id, "percorso": `\\srv\sds`, "utente": `AZ\altro`, "sottocartelle": true})
	if ok.body["ok"] != true {
		t.Fatal(string(ok.raw))
	}
	upd["testToken"] = ok.body["testToken"]
	if r := M("PUT", "/api/maint/sections/"+id, upd); r.code != 200 {
		t.Fatal(string(r.raw))
	}
	sec, _ := h.cfg.Get().Section(id)
	if sec.Nome != "SDS" || !sec.Emergenza || sec.Share.Utente != `AZ\altro` {
		t.Fatal(sec)
	}
	if p, _ := h.ci.Decrypt(id, sec.Share.Password); p != "giusta" {
		t.Fatal("password persa nella modifica")
	}
	// validazioni
	if r := M("PUT", "/api/maint/sections/"+id, map[string]any{"nome": strings.Repeat("x", 31), "icona": "scudo", "percorso": `\\srv\sds`}); r.code != 400 || r.body["field"] != "nome" {
		t.Fatal(string(r.raw))
	}
	// ordine e eliminazione
	h.cfg.Update(func(c *config.Config) error {
		c.Sezioni = append(c.Sezioni, config.Section{ID: "s_b", Nome: "B", Icona: "mappa", Ordine: 2, Share: config.Share{Percorso: `\\srv\b`}})
		return nil
	})
	if r := M("POST", "/api/maint/sections-order", map[string]any{"ids": []string{"s_b", id}}); r.code != 200 {
		t.Fatal(string(r.raw))
	}
	if h.cfg.Get().SortedSections()[0].ID != "s_b" {
		t.Fatal("ordine non applicato")
	}
	if r := M("POST", "/api/maint/sections-order", map[string]any{"ids": []string{"s_b", "s_b"}}); r.code != 400 {
		t.Fatal("ordine non valido accettato")
	}
	if r := M("DELETE", "/api/maint/sections/"+id, nil); r.code != 200 || len(h.cfg.Get().Sezioni) != 1 {
		t.Fatal(string(r.raw))
	}
}

func TestFilesRangeAndTraversal(t *testing.T) {
	h := newHarness(t)
	c, _ := h.pair("1234")
	content := pdf(strings.Repeat("x", 5000))
	os.WriteFile(filepath.Join(h.share, "a.pdf"), content, 0o644)
	enc, _ := h.ci.Encrypt("s_a", "giusta")
	h.cfg.Update(func(cfg *config.Config) error {
		cfg.Sezioni = []config.Section{{ID: "s_a", Nome: "A", Icona: "flacone", Share: config.Share{Percorso: `\\srv\sds`, Password: enc}}}
		return nil
	})
	h.sc.ScanOnce(context.Background())
	h.sc.ScanOnce(context.Background())
	m := h.sc.Current()
	if len(m.Documents) != 1 {
		t.Fatal(m.Documents)
	}
	sha := m.Documents[0].SHA256
	r := h.do(c, "GET", "/api/files/"+sha, nil)
	if r.code != 200 || !bytes.Equal(r.raw, content) || r.hdr.Get("ETag") != `"`+sha+`"` || r.hdr.Get("Content-Type") != "application/pdf" {
		t.Fatal(r.code, r.hdr)
	}
	r = h.do(c, "GET", "/api/files/"+sha, nil, "Range", "bytes=0-7")
	if r.code != 206 || string(r.raw) != "%PDF-1.4" {
		t.Fatal(r.code, string(r.raw))
	}
	if r := h.do(c, "GET", "/api/files/"+sha, nil, "If-None-Match", `"`+sha+`"`); r.code != 304 {
		t.Fatal(r.code)
	}
	for _, bad := range []string{"..%2Fconfig.json", "%2E%2E%2Fconfig.json", strings.Repeat("0", 64), "tmp"} {
		if r := h.do(c, "GET", "/api/files/"+bad, nil); r.code != 404 {
			t.Fatal(bad, r.code)
		}
	}
	if r := h.do(h.client(), "GET", "/api/files/"+sha, nil); r.code != 401 {
		t.Fatal("PDF senza token", r.code)
	}
}

func TestResetPairingKeepsPinAndSections(t *testing.T) {
	h := newHarness(t)
	old, _ := h.pair("1234")
	h.cfg.Update(func(c *config.Config) error {
		c.Sezioni = []config.Section{{ID: "s_a", Nome: "A", Icona: "flacone", Share: config.Share{Percorso: `\\srv\sds`}}}
		return nil
	})
	in := httptest.NewServer(h.srv.InternalHandler())
	defer in.Close()
	rr, err := http.Post(in.URL+"/internal/reset-pairing", "application/json", nil)
	if err != nil || rr.StatusCode != 200 {
		t.Fatal(err)
	}
	if r := h.do(old, "GET", "/api/status", nil); r.code != 401 {
		t.Fatal("token del vecchio totem ancora valido", r.code)
	}
	if h.cfg.Get().Globali.PinHash == "" || len(h.cfg.Get().Sezioni) != 1 {
		t.Fatal("PIN o sezioni persi")
	}
	// nuovo totem: codice + PIN attuale
	c := h.client()
	r := h.do(c, "POST", "/api/setup/code", map[string]any{"code": h.code()})
	if r.body["pinSet"] != true || r.body["sections"].(float64) != 1 {
		t.Fatal(string(r.raw))
	}
	tok := r.body["setupToken"]
	if r := h.do(c, "POST", "/api/setup/pin", map[string]any{"setupToken": tok, "pin": "9999", "platform": "android"}); r.code != 401 || r.body["error"] != "wrong_pin" {
		t.Fatal(string(r.raw))
	}
	if r := h.do(c, "POST", "/api/setup/pin", map[string]any{"setupToken": tok, "pin": "1234", "platform": "android"}); r.code != 200 {
		t.Fatal(string(r.raw))
	}
	devs := h.cfg.Get().Globali.Dispositivi
	if len(devs) != 1 || devs[0].Piattaforma != "android" {
		t.Fatal(devs)
	}
	// --reset-pin
	if _, err := h.srv.ResetPairing(true); err != nil || h.cfg.Get().Globali.PinHash != "" {
		t.Fatal("PIN non azzerato")
	}
}

func TestHeartbeatAndStatus(t *testing.T) {
	h := newHarness(t)
	c, _ := h.pair("1234")
	h.clk.Add(2 * time.Minute)
	r := h.do(c, "POST", "/api/heartbeat", map[string]any{"appVersion": "1.0.0", "platform": "windows", "documents": 12, "freeBytes": 1 << 30})
	if r.code != 200 {
		t.Fatal(string(r.raw))
	}
	d := h.cfg.Get().Globali.Dispositivi[0]
	if !d.UltimoContatto.Equal(h.clk.Now()) {
		t.Fatal(d.UltimoContatto)
	}
	b, _ := os.ReadFile(filepath.Join(h.data, "state", "heartbeat.json"))
	if !strings.Contains(string(b), `"freeBytes": 1073741824`) {
		t.Fatal(string(b))
	}
	if r := h.do(c, "POST", "/api/heartbeat", "x"); r.code != 400 {
		t.Fatal(r.code)
	}
	if r := h.do(c, "POST", "/api/heartbeat", nil); r.code != 415 {
		t.Fatal("POST senza JSON accettato", r.code)
	}
	r = h.do(c, "GET", "/api/status", nil)
	if r.code != 200 || r.body["serverVersion"] != "test" {
		t.Fatal(string(r.raw))
	}
	if r := h.do(c, "POST", "/api/scan", map[string]any{}); r.code != 202 || r.body["queued"] != true {
		t.Fatal(string(r.raw))
	}
	if r := h.do(c, "POST", "/api/scan", map[string]any{}); r.body["queued"] != false {
		t.Fatal("scansione manuale non limitata")
	}
}

func TestPhase2(t *testing.T) {
	h := newHarness(t)
	c, mt := h.pair("1234")
	if r := h.do(c, "POST", "/api/maint/devices/pair-code", map[string]any{}, MaintHeader, mt); r.code != 404 {
		t.Fatal("fase 2 attiva senza flag", r.code)
	}
	h.cfg.Update(func(cfg *config.Config) error { cfg.Globali.Fase2 = true; return nil })
	r := h.do(c, "POST", "/api/maint/devices/pair-code", map[string]any{}, MaintHeader, mt)
	if r.code != 200 || r.body["expiresInSec"].(float64) != 120 {
		t.Fatal(string(r.raw))
	}
	code := r.body["code"].(string)
	phone := h.client()
	h.clk.Add(time.Minute)
	if r := h.do(phone, "POST", "/api/pair", map[string]any{"code": code, "name": "Telefono Mario", "platform": "android"}); r.code != 200 {
		t.Fatal(string(r.raw))
	}
	if r := h.do(phone, "POST", "/api/pair", map[string]any{"code": code}); r.code != 401 {
		t.Fatal("codice di abbinamento riutilizzabile")
	}
	if r := h.do(phone, "GET", "/api/status", nil); r.code != 200 {
		t.Fatal(r.code)
	}
	// il telefono non può entrare in manutenzione
	if r := h.do(phone, "POST", "/api/maint/login", map[string]any{"pin": "1234"}); r.code != 403 {
		t.Fatal(r.code)
	}
	r = h.do(c, "GET", "/api/maint/devices", nil, MaintHeader, mt)
	if r.code != 200 {
		t.Fatal(r.code, string(r.raw))
	}
	var phoneID string
	for _, d := range r.body["devices"].([]any) {
		if d.(map[string]any)["ruolo"] == RolePhone {
			phoneID = d.(map[string]any)["id"].(string)
		}
	}
	if r := h.do(c, "DELETE", "/api/maint/devices/"+phoneID, nil, MaintHeader, mt); r.code != 200 {
		t.Fatal(r.code)
	}
	if r := h.do(phone, "GET", "/api/status", nil); r.code != 401 {
		t.Fatal("telefono revocato ancora valido")
	}
	// codice scaduto
	r = h.do(c, "POST", "/api/maint/devices/pair-code", map[string]any{}, MaintHeader, mt)
	h.clk.Add(2*time.Minute + time.Second)
	if r := h.do(h.client(), "POST", "/api/pair", map[string]any{"code": r.body["code"]}); r.code != 401 {
		t.Fatal("codice scaduto accettato")
	}
}

func TestStaticFallback(t *testing.T) {
	h := newHarness(t)
	r := h.do(h.client(), "GET", "/qualsiasi/percorso", nil)
	if r.code != 200 || !strings.Contains(string(r.raw), "PWA non compilata") {
		t.Fatal(r.code)
	}
	if r := h.do(h.client(), "GET", "/api/inesistente", nil); r.code != 404 {
		t.Fatal(r.code)
	}
	if !strings.Contains(r.hdr.Get("Content-Security-Policy"), "default-src 'self'") {
		t.Fatal("CSP assente")
	}
}

func TestStations(t *testing.T) {
	h := newHarness(t)
	totem, mt := h.pair("1234")
	M := func(c *http.Client, tok, method, path string, body any) resp {
		return h.do(c, method, path, body, MaintHeader, tok)
	}

	r := M(totem, mt, "POST", "/api/maint/stations/pair-code", map[string]any{"nome": "PC ufficio IT"})
	if r.code != 200 || r.body["expiresInSec"].(float64) != 600 {
		t.Fatal(string(r.raw))
	}
	code := r.body["code"].(string)

	// sull'altro PC il codice di postazione va nella procedura guidata
	pc := h.client()
	r = h.do(pc, "POST", "/api/setup/code", map[string]any{"code": strings.ToLower(code), "platform": "windows"})
	if r.code != 200 || r.body["role"] != "postazione" || r.body["name"] != "PC ufficio IT" {
		t.Fatal(string(r.raw))
	}
	if r := h.do(h.client(), "POST", "/api/setup/code", map[string]any{"code": code}); r.code != 401 {
		t.Fatal("codice di postazione riutilizzabile")
	}
	// il totem resta abbinato, la postazione legge e fa manutenzione con il PIN
	if r := h.do(totem, "GET", "/api/status", nil); r.code != 200 {
		t.Fatal("totem scollegato dalla postazione")
	}
	r = h.do(pc, "GET", "/api/setup/state", nil)
	if r.body["role"] != "postazione" || r.body["paired"] != true {
		t.Fatal(string(r.raw))
	}
	r = h.do(pc, "POST", "/api/maint/login", map[string]any{"pin": "1234"})
	if r.code != 200 {
		t.Fatal(string(r.raw))
	}
	pt := r.body["token"].(string)
	if r := M(pc, pt, "GET", "/api/maint/sections", nil); r.code != 200 {
		t.Fatal(r.code)
	}
	// il token di manutenzione è legato al dispositivo
	if r := M(totem, pt, "GET", "/api/maint/sections", nil); r.code != 401 {
		t.Fatal("token della postazione accettato dal totem")
	}

	// elenco dei dispositivi con dati dell'ultimo contatto
	h.clk.Add(2 * time.Minute)
	h.do(totem, "POST", "/api/heartbeat", map[string]any{"appVersion": "1.0.0", "platform": "windows", "documents": 7, "freeBytes": 5000})
	r = M(pc, pt, "GET", "/api/maint/devices", nil)
	devs := r.body["devices"].([]any)
	if len(devs) != 2 {
		t.Fatal(string(r.raw))
	}
	tot, sta := devs[0].(map[string]any), devs[1].(map[string]any)
	if tot["ruolo"] != "totem" || tot["documents"].(float64) != 7 || tot["appVersion"] != "1.0.0" || tot["corrente"] != false {
		t.Fatal(tot)
	}
	if sta["ruolo"] != "postazione" || sta["corrente"] != true {
		t.Fatal(sta)
	}
	// non si revoca sé stessi; un id inesistente dà 404
	if r := M(pc, pt, "DELETE", "/api/maint/devices/d_manca", nil); r.code != 404 {
		t.Fatal(r.code)
	}
	if r := M(pc, pt, "DELETE", "/api/maint/devices/"+sta["id"].(string), nil); r.code != 409 {
		t.Fatal("auto-revoca", r.code)
	}
	if r := M(totem, mt, "DELETE", "/api/maint/devices/"+sta["id"].(string), nil); r.code != 200 {
		t.Fatal(r.code)
	}
	if r := h.do(pc, "GET", "/api/status", nil); r.code != 401 {
		t.Fatal("postazione revocata ancora valida")
	}

	// codice scaduto dopo 10 minuti; codice dal listener interno
	r = M(totem, mt, "POST", "/api/maint/stations/pair-code", map[string]any{})
	h.clk.Add(10*time.Minute + time.Second)
	if r := h.do(h.client(), "POST", "/api/setup/code", map[string]any{"code": r.body["code"]}); r.code != 401 {
		t.Fatal("codice scaduto accettato")
	}
	h.srv.codeLimiter.Success()
	in := httptest.NewServer(h.srv.InternalHandler())
	defer in.Close()
	rr, _ := http.Post(in.URL+"/internal/pair-station?nome=Prove", "application/json", nil)
	var out struct{ Code string }
	json.NewDecoder(rr.Body).Decode(&out)
	r = h.do(h.client(), "POST", "/api/setup/code", map[string]any{"code": out.Code})
	if r.code != 200 || r.body["name"] != "Prove" {
		t.Fatal(string(r.raw))
	}
}

func TestProblems(t *testing.T) {
	h := newHarness(t)
	c, mt := h.pair("1234")
	os.WriteFile(filepath.Join(h.share, "rotto.pdf"), []byte("%PDF-1.4 senza fine"), 0o644)
	enc, _ := h.ci.Encrypt("s_a", "giusta")
	h.cfg.Update(func(cfg *config.Config) error {
		cfg.Sezioni = []config.Section{{ID: "s_a", Nome: "Schede", Icona: "flacone", Share: config.Share{Percorso: `\\srv\sds`, Password: enc}}}
		return nil
	})
	h.sc.ScanOnce(context.Background())
	h.sc.ScanOnce(context.Background())
	r := h.do(c, "GET", "/api/maint/problems", nil, MaintHeader, mt)
	ps := r.body["problems"].([]any)
	if len(ps) != 1 {
		t.Fatal(string(r.raw))
	}
	p := ps[0].(map[string]any)
	if p["path"] != "rotto.pdf" || p["code"] != "corrupt" || p["sectionName"] != "Schede" || p["hasPrevious"] != false {
		t.Fatal(p)
	}
	if r := h.do(c, "GET", "/api/maint/problems", nil); r.code != 401 {
		t.Fatal("problemi senza PIN")
	}
}

// extrasHarness: totem abbinato con due sezioni e tre PDF.
func extrasHarness(t *testing.T) (*harness, *http.Client, string) {
	h := newHarness(t)
	c, mt := h.pair("1234")
	os.MkdirAll(filepath.Join(h.share, "A"), 0o755)
	os.MkdirAll(filepath.Join(h.share, "B"), 0o755)
	os.WriteFile(filepath.Join(h.share, "A", "a1.pdf"), pdf("a1"), 0o644)
	os.WriteFile(filepath.Join(h.share, "A", "a2.pdf"), pdf("a2"), 0o644)
	os.WriteFile(filepath.Join(h.share, "B", "b1.pdf"), pdf("b1"), 0o644)
	enc := func(id string) string { e, _ := h.ci.Encrypt(id, "giusta"); return e }
	h.cfg.Update(func(cfg *config.Config) error {
		cfg.Sezioni = []config.Section{
			{ID: "s_a", Nome: "A", Icona: "flacone", Ordine: 1, Share: config.Share{Percorso: `\\srv\sds\A`, Password: enc("s_a")}},
			{ID: "s_b", Nome: "B", Icona: "uscita", Ordine: 2, Share: config.Share{Percorso: `\\srv\sds\B`, Password: enc("s_b")}},
		}
		return nil
	})
	h.sc.ScanOnce(context.Background())
	h.sc.ScanOnce(context.Background())
	return h, c, mt
}

func manifestOf(t *testing.T, h *harness, c *http.Client) map[string]any {
	t.Helper()
	r := h.do(c, "GET", "/manifest.json", nil)
	if r.code != 200 {
		t.Fatal(r.code, string(r.raw))
	}
	return r.body
}

func TestDepartmentsAndExtras(t *testing.T) {
	h, c, mt := extrasHarness(t)
	// la connessione di prova usa il percorso \\srv\sds: le sottocartelle A e B sono sezioni
	M := func(method, path string, body any) resp { return h.do(c, method, path, body, MaintHeader, mt) }
	m := manifestOf(t, h, c)
	if len(m["documents"].([]any)) != 3 || m["role"] != "totem" || m["screensaver"] == nil {
		t.Fatal(m)
	}
	h1 := m["hash"].(string)

	// reparti
	if r := M("PUT", "/api/maint/departments", map[string]any{"reparti": []map[string]any{{"nome": "Reparto A", "sezioni": []string{"s_a"}}, {"nome": "reparto a"}}}); r.code != 400 {
		t.Fatal("nomi duplicati accettati", string(r.raw))
	}
	if r := M("PUT", "/api/maint/departments", map[string]any{"reparti": []map[string]any{{"nome": "Reparto A", "sezioni": []string{"s_a"}}, {"nome": "Magazzino"}}}); r.code != 200 {
		t.Fatal(string(r.raw))
	}
	ex := M("GET", "/api/maint/extras", nil).body
	reps := ex["reparti"].([]any)
	repA := reps[0].(map[string]any)["id"].(string)
	repM := reps[1].(map[string]any)["id"].(string)

	// numeri utili e avvisi, comuni e di reparto
	r := M("PUT", "/api/maint/contacts", map[string]any{"contatti": []map[string]any{
		{"etichetta": "Emergenze", "numero": "112"},
		{"etichetta": "Primo soccorso reparto A", "numero": "3210", "reparti": []string{repA}},
	}})
	if r.code != 200 {
		t.Fatal(string(r.raw))
	}
	if r := M("PUT", "/api/maint/contacts", map[string]any{"contatti": []map[string]any{{"etichetta": "x", "numero": "abc"}}}); r.code != 400 || r.body["field"] != "numero" {
		t.Fatal(string(r.raw))
	}
	r = M("PUT", "/api/maint/notices", map[string]any{"avvisi": []map[string]any{
		{"testo": "Esercitazione antincendio giovedì", "livello": "attenzione", "dal": "2026-10-20", "al": "2026-10-30"},
		{"testo": "Scaduto", "livello": "info", "al": "2026-10-01"},
		{"testo": "Solo magazzino", "livello": "info", "reparti": []string{repM}},
	}})
	if r.code != 200 {
		t.Fatal(string(r.raw))
	}
	if r := M("PUT", "/api/maint/screensaver", map[string]any{"attivo": true, "dopoMinuti": 4, "notte": true, "orarioNero": "21:00-05:30"}); r.code != 200 {
		t.Fatal(string(r.raw))
	}

	// totem senza reparto: tutte le sezioni, solo numeri e avvisi comuni non scaduti
	m = manifestOf(t, h, c)
	if m["hash"] == h1 {
		t.Fatal("hash invariato dopo avvisi e numeri")
	}
	if len(m["contacts"].([]any)) != 1 || len(m["notices"].([]any)) != 1 || m["department"] != nil {
		t.Fatal(m["contacts"], m["notices"])
	}
	ss := m["screensaver"].(map[string]any)
	if ss["enabled"] != true || ss["afterMinutes"].(float64) != 4 || ss["nightHours"] != "21:00-05:30" {
		t.Fatal(ss)
	}

	// totem assegnato al reparto A: solo la sezione A e il numero del reparto
	devID := h.cfg.Get().Globali.Dispositivi[0].ID
	if r := M("PUT", "/api/maint/devices/"+devID, map[string]any{"reparto": repA}); r.code != 200 {
		t.Fatal(string(r.raw))
	}
	m = manifestOf(t, h, c)
	if len(m["sections"].([]any)) != 1 || len(m["documents"].([]any)) != 2 || m["department"].(map[string]any)["name"] != "Reparto A" {
		t.Fatal(m["sections"], m["department"])
	}
	if len(m["contacts"].([]any)) != 2 {
		t.Fatal(m["contacts"])
	}
	st := h.do(c, "GET", "/api/status", nil).body
	if len(st["sections"].([]any)) != 1 {
		t.Fatal("stato non filtrato per reparto")
	}
	if r := M("PUT", "/api/maint/devices/"+devID, map[string]any{"reparto": "r_manca"}); r.code != 400 {
		t.Fatal(r.code)
	}

	// eliminare la sezione o il reparto ripulisce i riferimenti
	M("DELETE", "/api/maint/sections/s_a", nil)
	if dep, _ := h.cfg.Get().Department(repA); len(dep.Sezioni) != 0 {
		t.Fatal(dep)
	}
	M("PUT", "/api/maint/departments", map[string]any{"reparti": []map[string]any{{"id": repM, "nome": "Magazzino"}}})
	cfg := h.cfg.Get()
	if cfg.Globali.Dispositivi[0].Reparto != "" || len(cfg.Globali.Contatti[1].Reparti) != 0 {
		t.Fatal("riferimenti al reparto eliminato rimasti")
	}
}

func TestTotemCodesAndReplacement(t *testing.T) {
	h, c, mt := extrasHarness(t)
	M := func(method, path string, body any) resp { return h.do(c, method, path, body, MaintHeader, mt) }
	M("PUT", "/api/maint/departments", map[string]any{"reparti": []map[string]any{{"nome": "Magazzino", "sezioni": []string{"s_b"}}}})
	repM := h.cfg.Get().Reparti[0].ID

	// secondo totem per il magazzino
	r := M("POST", "/api/maint/totems/pair-code", map[string]any{"reparto": repM})
	if r.code != 200 {
		t.Fatal(string(r.raw))
	}
	t2 := h.client()
	r = h.do(t2, "POST", "/api/setup/code", map[string]any{"code": r.body["code"], "platform": "android"})
	if r.code != 200 || r.body["role"] != "totem" || r.body["department"] != "Magazzino" {
		t.Fatal(string(r.raw))
	}
	if m := manifestOf(t, h, t2); len(m["documents"].([]any)) != 1 {
		t.Fatal("il totem del magazzino vede tutto")
	}
	if m := manifestOf(t, h, c); len(m["documents"].([]any)) != 3 {
		t.Fatal("il primo totem ha perso documenti")
	}
	// il secondo totem entra in manutenzione con lo stesso PIN
	if r := h.do(t2, "POST", "/api/maint/login", map[string]any{"pin": "1234"}); r.code != 200 {
		t.Fatal(r.code)
	}

	// sostituzione del totem del magazzino: il nuovo eredita il reparto, il vecchio è fuori
	var oldID string
	for _, d := range h.cfg.Get().Globali.Dispositivi {
		if d.Reparto == repM {
			oldID = d.ID
		}
	}
	r = M("POST", "/api/maint/totems/pair-code", map[string]any{"sostituisce": oldID})
	t3 := h.client()
	r = h.do(t3, "POST", "/api/setup/code", map[string]any{"code": r.body["code"]})
	if r.code != 200 || r.body["department"] != "Magazzino" {
		t.Fatal(string(r.raw))
	}
	if r := h.do(t2, "GET", "/api/status", nil); r.code != 401 {
		t.Fatal("totem sostituito ancora valido")
	}
	if n := len(h.cfg.Get().Globali.Dispositivi); n != 2 {
		t.Fatal(n)
	}
	if r := M("POST", "/api/maint/totems/pair-code", map[string]any{"reparto": "inesistente"}); r.code != 400 {
		t.Fatal(r.code)
	}
	// codice dal listener interno, reparto indicato per nome
	in := httptest.NewServer(h.srv.InternalHandler())
	defer in.Close()
	rr, _ := http.Post(in.URL+"/internal/pair-totem?reparto=magazzino", "application/json", nil)
	if rr.StatusCode != 200 {
		t.Fatal(rr.StatusCode)
	}
}

func TestMonitor(t *testing.T) {
	h, c, mt := extrasHarness(t)
	M := func(method, path string, body any) resp { return h.do(c, method, path, body, MaintHeader, mt) }
	anon := h.client()
	if r := h.do(anon, "GET", "/api/monitor", nil); r.code != 404 {
		t.Fatal("monitoraggio attivo senza token", r.code)
	}
	tok := M("POST", "/api/maint/monitor/token", map[string]any{}).body["token"].(string)
	if r := h.do(anon, "GET", "/api/monitor", nil, "Authorization", "Bearer sbagliato"); r.code != 401 {
		t.Fatal(r.code)
	}
	r := h.do(anon, "GET", "/api/monitor", nil, "Authorization", "Bearer "+tok)
	if r.code != 200 || r.body["ok"] != true || len(r.body["sections"].([]any)) != 2 || len(r.body["totems"].([]any)) != 1 {
		t.Fatal(string(r.raw))
	}
	r = h.do(anon, "GET", "/api/monitor?formato=testo&token="+tok, nil)
	if r.code != 200 || !strings.HasPrefix(string(r.raw), "TOTEM OK\n") {
		t.Fatal(string(r.raw))
	}
	// totem silenzioso oltre la soglia → 503
	if r := M("PUT", "/api/maint/monitor", map[string]any{"sogliaMinuti": 5}); r.code != 400 {
		t.Fatal("soglia troppo bassa accettata")
	}
	M("PUT", "/api/maint/monitor", map[string]any{"sogliaMinuti": 15})
	h.clk.Add(20 * time.Minute)
	r = h.do(anon, "GET", "/api/monitor?formato=testo", nil, "Authorization", "Bearer "+tok)
	if r.code != 503 || !strings.Contains(string(r.raw), "silenzioso da 20 minuti") {
		t.Fatal(r.code, string(r.raw))
	}
	// un heartbeat lo rimette a posto
	h.do(c, "POST", "/api/heartbeat", map[string]any{"appVersion": "1", "platform": "windows"})
	if r := h.do(anon, "GET", "/api/monitor", nil, "Authorization", "Bearer "+tok); r.code != 200 {
		t.Fatal(r.code, string(r.raw))
	}
	// token revocato (la manutenzione è scaduta nei 20 minuti simulati: nuovo login)
	mt2 := h.do(c, "POST", "/api/maint/login", map[string]any{"pin": "1234"}).body["token"].(string)
	if r := h.do(c, "DELETE", "/api/maint/monitor/token", nil, MaintHeader, mt2); r.code != 200 {
		t.Fatal(r.code)
	}
	if r := h.do(anon, "GET", "/api/monitor", nil, "Authorization", "Bearer "+tok); r.code != 404 {
		t.Fatal(r.code)
	}
	// il listener interno risponde senza token
	in := httptest.NewServer(h.srv.InternalHandler())
	defer in.Close()
	rr, _ := http.Get(in.URL + "/internal/monitor?formato=testo")
	b, _ := io.ReadAll(rr.Body)
	if !strings.HasPrefix(string(b), "TOTEM OK") {
		t.Fatal(string(b))
	}
}
