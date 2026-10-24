package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/toigoweb/totem/internal/config"
	"github.com/toigoweb/totem/internal/sources"
)

func (h *harness) put(c *http.Client, path, mt string, body []byte) resp {
	h.t.Helper()
	req, _ := http.NewRequest("PUT", h.ts.URL+path, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/pdf")
	req.Header.Set(MaintHeader, mt)
	r, err := c.Do(req)
	if err != nil {
		h.t.Fatal(err)
	}
	defer r.Body.Close()
	var buf bytes.Buffer
	buf.ReadFrom(r.Body)
	out := resp{code: r.StatusCode, raw: buf.Bytes(), hdr: r.Header}
	_ = json.Unmarshal(buf.Bytes(), &out.body)
	return out
}

func TestUploadSection(t *testing.T) {
	h := newHarness(t)
	c, mt := h.pair("1234")
	M := func(method, path string, body any) resp { return h.do(c, method, path, body, MaintHeader, mt) }

	// una sezione a caricamento diretto non richiede prova di connessione
	r := M("POST", "/api/maint/sections", map[string]any{"nome": "Piani di emergenza", "icona": "mappa", "tipo": "upload", "percorso": `\\ignorato`})
	if r.code != 201 {
		t.Fatal(string(r.raw))
	}
	id := r.body["id"].(string)
	sec, _ := h.cfg.Get().Section(id)
	if sec.Share.Kind() != config.SourceUpload || sec.Share.Percorso != "" {
		t.Fatal(sec.Share)
	}
	base := "/api/maint/sections/" + id + "/files"
	q := func(nome, cartella string) string {
		v := url.Values{"nome": {nome}}
		if cartella != "" {
			v.Set("cartella", cartella)
		}
		return base + "?" + v.Encode()
	}
	if r := h.put(c, q("Evacuazione piano 1.pdf", "Edificio A"), mt, pdf("piano1")); r.code != 201 || r.body["path"] != "Edificio A/Evacuazione piano 1.pdf" {
		t.Fatal(r.code, string(r.raw))
	}
	if r := h.put(c, q("Generale.PDF", ""), mt, pdf("gen")); r.code != 201 {
		t.Fatal(string(r.raw))
	}
	// rifiuti
	for _, c2 := range []struct {
		path string
		body []byte
		code int
	}{
		{q("nota.docx", ""), pdf("x"), 400},
		{q(".nascosto.pdf", ""), pdf("x"), 400},
		{q("ok.pdf", "../fuori"), pdf("x"), 400},
		{q("rotto.pdf", ""), []byte("%PDF-1.4 senza fine"), 400},
		{q("finto.pdf", ""), []byte("ciao\n%%EOF"), 400},
	} {
		if r := h.put(c, c2.path, mt, c2.body); r.code != c2.code {
			t.Errorf("%s: %d %s", c2.path, r.code, r.raw)
		}
	}
	// percorsi con separatori: resta solo il nome
	if r := h.put(c, q(`..\..\etc\x.pdf`, ""), mt, pdf("x")); r.code != 201 || r.body["path"] != "x.pdf" {
		t.Fatal(string(r.raw))
	}
	dir := sources.UploadDir(h.data, id)
	if _, err := os.Stat(filepath.Join(dir, "x.pdf")); err != nil {
		t.Fatal(err)
	}
	// troppo grande
	big := append(pdf(strings.Repeat("x", 100)), make([]byte, config.DefaultSettings().MaxPDFBytes)...)
	if r := h.put(c, q("grande.pdf", ""), mt, big); r.code != 413 {
		t.Fatal(r.code)
	}
	// senza PIN no
	if r := h.do(c, "GET", base, nil); r.code != 401 {
		t.Fatal("elenco senza PIN")
	}
	list := M("GET", base, nil)
	if n := len(list.body["files"].([]any)); n != 3 {
		t.Fatal(string(list.raw))
	}

	// lo scanner pubblica i file caricati come le altre origini
	h.sc.ScanOnce(context.Background())
	h.sc.ScanOnce(context.Background())
	m := manifestOf(t, h, c)
	if n := len(m["documents"].([]any)); n != 3 {
		t.Fatal(m["documents"])
	}

	if r := M("DELETE", base+"?path="+url.QueryEscape("Edificio A/Evacuazione piano 1.pdf"), nil); r.code != 200 {
		t.Fatal(string(r.raw))
	}
	if _, err := os.Stat(filepath.Join(dir, "Edificio A")); !os.IsNotExist(err) {
		t.Fatal("cartella vuota non rimossa")
	}
	if r := M("DELETE", base+"?path="+url.QueryEscape("../config.json"), nil); r.code != 400 {
		t.Fatal(string(r.raw))
	}
	if r := M("DELETE", base+"?path=manca.pdf", nil); r.code != 404 {
		t.Fatal(string(r.raw))
	}
	h.sc.ScanOnce(context.Background())
	h.sc.ScanOnce(context.Background())
	if n := len(manifestOf(t, h, c)["documents"].([]any)); n != 2 {
		t.Fatal(n)
	}

	// passare a SMB richiede la prova; poi la cartella dei caricamenti si elimina
	upd := map[string]any{"nome": "Piani", "icona": "mappa", "tipo": "smb", "percorso": `\\srv\sds`, "password": "giusta"}
	if r := M("PUT", "/api/maint/sections/"+id, upd); r.code != 409 {
		t.Fatal(string(r.raw))
	}
	ok := M("POST", "/api/maint/test", map[string]any{"sectionId": id, "tipo": "smb", "percorso": `\\srv\sds`, "password": "giusta"})
	upd["testToken"] = ok.body["testToken"]
	if r := M("PUT", "/api/maint/sections/"+id, upd); r.code != 200 {
		t.Fatal(string(r.raw))
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatal("caricamenti rimasti dopo il cambio di tipo")
	}
	if r := M("GET", base, nil); r.code != 409 {
		t.Fatal("caricamento su sezione SMB")
	}
}

func TestTestConnectionValidatesByType(t *testing.T) {
	h := newHarness(t)
	c, mt := h.pair("1234")
	M := func(body any) resp { return h.do(c, "POST", "/api/maint/test", body, MaintHeader, mt) }
	cases := []struct {
		in   map[string]any
		code string
	}{
		{map[string]any{"tipo": "onedrive", "percorso": "https://example.com/x", "tenant": "azienda.it", "utente": "00000000-0000-0000-0000-000000000001", "password": "s"}, "invalid_path"},
		{map[string]any{"tipo": "onedrive", "percorso": "https://azienda.sharepoint.com/:f:/s/HSE/E1", "tenant": "", "utente": "00000000-0000-0000-0000-000000000001", "password": "s"}, "invalid_tenant"},
		{map[string]any{"tipo": "onedrive", "percorso": "https://azienda.sharepoint.com/:f:/s/HSE/E1", "tenant": "azienda.it", "utente": "mario", "password": "s"}, "invalid_utente"},
		{map[string]any{"tipo": "webdav", "percorso": "ftp://nas/dav"}, "invalid_path"},
		{map[string]any{"tipo": "boh", "percorso": "x"}, "invalid_tipo"},
	}
	for _, x := range cases {
		if r := M(x.in); r.body["ok"] != false || r.body["code"] != x.code {
			t.Errorf("%v: %s", x.in, r.raw)
		}
	}
	if r := M(map[string]any{"tipo": "upload"}); r.body["ok"] != true {
		t.Fatal(string(r.raw))
	}
}
