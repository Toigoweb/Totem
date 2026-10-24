package graphsrc

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/toigoweb/totem/internal/smbsrc"
)

const link = "https://azienda.sharepoint.com/:f:/s/HSE/EabcDEF123?e=xyz"

// fakeGraph simula login.microsoftonline.com e graph.microsoft.com quanto basta:
// token, /shares, children con paginazione e 429, content con rinvio al download.
type fakeGraph struct {
	srv        *httptest.Server
	tokenCalls atomic.Int32
	throttled  atomic.Bool
}

func newFake(t *testing.T) *fakeGraph {
	f := &fakeGraph{}
	tree := map[string][]map[string]any{
		"root": {
			{"id": "f1", "name": "Acetone rev3.pdf", "size": 30, "lastModifiedDateTime": "2026-10-20T10:00:00Z", "file": map[string]any{}},
			{"id": "d1", "name": "Solventi", "folder": map[string]any{"childCount": 2}},
			{"id": "x1", "name": "nota.docx", "size": 5, "lastModifiedDateTime": "2026-10-20T10:00:00Z", "file": map[string]any{}},
		},
		"d1": {
			{"id": "f2", "name": "Diluente è nitro.pdf", "size": 25, "lastModifiedDateTime": "2026-10-21T10:00:00Z", "file": map[string]any{}},
			{"id": "f3", "name": "~$temp.pdf", "size": 1, "lastModifiedDateTime": "2026-10-21T10:00:00Z", "file": map[string]any{}},
		},
	}
	content := map[string]string{"f1": "%PDF-1.4\nacetone\n%%EOF\n", "f2": "%PDF-1.4\ndiluente\n%%EOF\n"}
	mux := http.NewServeMux()
	auth := func(w http.ResponseWriter, r *http.Request) bool {
		if r.Header.Get("Authorization") != "Bearer tok-123" {
			w.WriteHeader(401)
			return false
		}
		return true
	}
	mux.HandleFunc("POST /tenant-ok/oauth2/v2.0/token", func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		f.tokenCalls.Add(1)
		if r.Form.Get("grant_type") != "client_credentials" || r.Form.Get("client_secret") != "segreto" ||
			r.Form.Get("scope") != "https://graph.microsoft.com/.default" {
			w.WriteHeader(401)
			json.NewEncoder(w).Encode(map[string]string{"error": "invalid_client"})
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"access_token": "tok-123", "expires_in": 3600})
	})
	mux.HandleFunc("GET /v1.0/shares/{id}/driveItem", func(w http.ResponseWriter, r *http.Request) {
		if !auth(w, r) {
			return
		}
		want := "u!" + base64.RawURLEncoding.EncodeToString([]byte(link))
		switch r.PathValue("id") {
		case want:
			json.NewEncoder(w).Encode(map[string]any{"id": "root", "name": "SDS", "folder": map[string]any{}, "parentReference": map[string]any{"driveId": "drv"}})
		case "u!" + base64.RawURLEncoding.EncodeToString([]byte(link+"&file")):
			json.NewEncoder(w).Encode(map[string]any{"id": "f1", "name": "a.pdf", "file": map[string]any{}, "parentReference": map[string]any{"driveId": "drv"}})
		case "u!" + base64.RawURLEncoding.EncodeToString([]byte(link+"&denied")):
			w.WriteHeader(403)
		default:
			w.WriteHeader(404)
		}
	})
	mux.HandleFunc("GET /v1.0/drives/drv/items/{id}/children", func(w http.ResponseWriter, r *http.Request) {
		if !auth(w, r) {
			return
		}
		if f.throttled.CompareAndSwap(true, false) {
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(429)
			return
		}
		items := tree[r.PathValue("id")]
		// paginazione: un elemento per pagina
		skip := 0
		fmt.Sscan(r.URL.Query().Get("skip"), &skip)
		page := map[string]any{"value": []any{}}
		if skip < len(items) {
			page["value"] = items[skip : skip+1]
			if skip+1 < len(items) {
				page["@odata.nextLink"] = fmt.Sprintf("%s/v1.0/drives/drv/items/%s/children?skip=%d", f.srv.URL, r.PathValue("id"), skip+1)
			}
		}
		json.NewEncoder(w).Encode(page)
	})
	mux.HandleFunc("GET /v1.0/drives/drv/items/{id}/content", func(w http.ResponseWriter, r *http.Request) {
		if !auth(w, r) {
			return
		}
		http.Redirect(w, r, "/download/"+r.PathValue("id"), http.StatusFound)
	})
	mux.HandleFunc("GET /download/{id}", func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, content[r.PathValue("id")])
	})
	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close)
	GraphBase, LoginBase = f.srv.URL+"/v1.0", f.srv.URL
	tokMu.Lock()
	tokens = map[string]cachedToken{}
	tokMu.Unlock()
	return f
}

func target(l string) smbsrc.Target {
	return smbsrc.Target{Type: "onedrive", Path: l, Tenant: "tenant-ok", User: "00000000-0000-0000-0000-000000000001", Password: "segreto", Recursive: true}
}

func TestGraphListAndOpen(t *testing.T) {
	f := newFake(t)
	f.throttled.Store(true) // la prima pagina risponde 429: si riprova
	ctx := context.Background()
	src, err := Connect(ctx, target(link))
	if err != nil {
		t.Fatal(err)
	}
	files, err := src.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, x := range files {
		names = append(names, x.Path)
	}
	sort.Strings(names)
	if strings.Join(names, "|") != "Acetone rev3.pdf|Solventi/Diluente è nitro.pdf" {
		t.Fatal(names)
	}
	r, err := src.Open(ctx, "Solventi/Diluente è nitro.pdf")
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(r)
	r.Close()
	if string(b) != "%PDF-1.4\ndiluente\n%%EOF\n" {
		t.Fatal(string(b))
	}
	if _, err := src.Open(ctx, "manca.pdf"); smbsrc.Code(err) != smbsrc.ErrNotFound {
		t.Fatal(err)
	}
	// il token resta in cache tra una connessione e l'altra
	Connect(ctx, target(link))
	if n := f.tokenCalls.Load(); n != 1 {
		t.Fatalf("token richiesto %d volte", n)
	}
}

func TestGraphErrors(t *testing.T) {
	newFake(t)
	ctx := context.Background()
	bad := target(link)
	bad.Password = "sbagliato"
	cases := []struct {
		t    smbsrc.Target
		want string
	}{
		{bad, smbsrc.ErrAuth},
		{target(link + "&manca"), smbsrc.ErrNotFound},
		{target(link + "&file"), smbsrc.ErrNotFound},
		{target(link + "&denied"), smbsrc.ErrAccessDenied},
		{target("http://non-https"), smbsrc.ErrInvalidPath},
	}
	for _, c := range cases {
		_, err := Connect(ctx, c.t)
		if got := smbsrc.Code(err); got != c.want {
			t.Errorf("%s: atteso %s, ottenuto %s (%v)", c.t.Path, c.want, got, err)
		}
	}
	// tenant inesistente → credenziali non valide
	tt := target(link)
	tt.Tenant = "tenant-manca"
	if _, err := Connect(ctx, tt); smbsrc.Code(err) != smbsrc.ErrAuth {
		t.Fatal(err)
	}
	// server Microsoft irraggiungibile
	GraphBase, LoginBase = "http://127.0.0.1:1/v1.0", "http://127.0.0.1:1"
	tokMu.Lock()
	tokens = map[string]cachedToken{}
	tokMu.Unlock()
	if _, err := Connect(ctx, target(link)); smbsrc.Code(err) != smbsrc.ErrUnreachable {
		t.Fatal(err)
	}
}
