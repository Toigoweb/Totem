package api

import (
	"bytes"
	"encoding/json"
	"image"
	"image/png"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/toigoweb/totem/internal/config"
)

func testPNG() []byte {
	var b bytes.Buffer
	png.Encode(&b, image.NewGray(image.Rect(0, 0, 300, 200)))
	return b.Bytes()
}

func (h *harness) raw(c *http.Client, method, path, ctype, mt string, body []byte) resp {
	h.t.Helper()
	req, _ := http.NewRequest(method, h.ts.URL+path, bytes.NewReader(body))
	req.Header.Set("Content-Type", ctype)
	if mt != "" {
		req.Header.Set(MaintHeader, mt)
	}
	r, err := c.Do(req)
	if err != nil {
		h.t.Fatal(err)
	}
	defer r.Body.Close()
	b, _ := io.ReadAll(r.Body)
	out := resp{code: r.StatusCode, raw: b, hdr: r.Header}
	_ = json.Unmarshal(b, &out.body)
	return out
}

func TestSafetyMap(t *testing.T) {
	h, c, mt := extrasHarness(t)
	M := func(method, path string, body any) resp { return h.do(c, method, path, body, MaintHeader, mt) }

	if m := manifestOf(t, h, c); m["safety"] != nil {
		t.Fatal("mappa senza planimetrie")
	}
	if r := h.raw(c, "POST", "/api/maint/safety/maps?nome=Stabilimento", "image/png", mt, []byte("no")); r.code != 400 || r.body["field"] != "immagine" {
		t.Fatal(string(r.raw))
	}
	if r := h.raw(c, "POST", "/api/maint/safety/maps?nome=Stabilimento", "image/png", "", testPNG()); r.code != 401 {
		t.Fatal("planimetria senza PIN")
	}
	r := h.raw(c, "POST", "/api/maint/safety/maps?nome=Stabilimento", "image/png", mt, testPNG())
	if r.code != 201 {
		t.Fatal(string(r.raw))
	}
	mapID, sha := r.body["id"].(string), r.body["immagine"].(string)

	ev := func(x float64, data, tipo, nota string) resp {
		return M("POST", "/api/maint/safety/events", map[string]any{"mappa": mapID, "x": x, "y": 0.5, "data": data, "tipo": tipo, "nota": nota})
	}
	if r := ev(0.2, "2026-09-01", "infortunio", "Mario Rossi, mano"); r.code != 201 {
		t.Fatal(string(r.raw))
	}
	ev(0.3, "2026-10-10", "quasi", "")
	ev(0.4, "2022-01-10", "infortunio", "") // oltre 3 anni: non va al totem
	if r := ev(0.4, "2026-12-01", "infortunio", ""); r.code != 400 || r.body["field"] != "data" {
		t.Fatal(string(r.raw))
	}
	if r := M("PUT", "/api/maint/safety/settings", map[string]any{"inizio": "2025-01-01"}); r.code != 200 {
		t.Fatal(string(r.raw))
	}

	m := manifestOf(t, h, c)
	sf := m["safety"].(map[string]any)
	if len(sf["maps"].([]any)) != 1 || len(sf["events"].([]any)) != 2 || sf["lastInjury"] != "2026-09-01" || sf["since"] != "2025-01-01" {
		t.Fatal(sf)
	}
	raw, _ := json.Marshal(m)
	if strings.Contains(string(raw), "Mario") {
		t.Fatal("nota nel manifest")
	}
	// immagine solo ai dispositivi abbinati
	if r := h.do(c, "GET", "/api/maps/"+sha, nil); r.code != 200 || r.hdr.Get("Content-Type") != "image/png" {
		t.Fatal(r.code, r.hdr)
	}
	if r := h.do(h.client(), "GET", "/api/maps/"+sha, nil); r.code != 401 {
		t.Fatal("immagine senza abbinamento")
	}
	// export CSV con le note (solo manutenzione)
	if r := M("GET", "/api/maint/safety/export", nil); r.code != 200 || !strings.Contains(string(r.raw), "Mario Rossi") || !strings.Contains(string(r.raw), "Quasi incidente") {
		t.Fatal(string(r.raw))
	}

	// planimetria riservata a un altro reparto: il totem non la vede più
	h.cfg.Update(func(cfg *config.Config) error {
		cfg.Reparti = []config.Department{{ID: "r_1", Nome: "Magazzino", Sezioni: []string{}}, {ID: "r_2", Nome: "Officina", Sezioni: []string{}}}
		for i := range cfg.Globali.Dispositivi {
			cfg.Globali.Dispositivi[i].Reparto = "r_2"
		}
		return nil
	})
	if r := M("PUT", "/api/maint/safety/maps/"+mapID, map[string]any{"nome": "Magazzino", "reparti": []string{"r_1"}}); r.code != 200 {
		t.Fatal(string(r.raw))
	}
	if m := manifestOf(t, h, c); m["safety"] != nil {
		t.Fatal("planimetria di un altro reparto")
	}
	if r := M("PUT", "/api/maint/safety/maps/"+mapID, map[string]any{"nome": "Magazzino", "reparti": []string{"r_manca"}}); r.code != 400 {
		t.Fatal(string(r.raw))
	}

	// eliminazione
	if r := M("DELETE", "/api/maint/safety/maps/"+mapID, nil); r.code != 200 {
		t.Fatal(string(r.raw))
	}
	if r := M("GET", "/api/maint/safety", nil); len(r.body["eventi"].([]any)) != 0 {
		t.Fatal(string(r.raw))
	}
	if r := h.do(c, "GET", "/api/maps/"+sha, nil); r.code != 404 {
		t.Fatal(r.code)
	}
}
