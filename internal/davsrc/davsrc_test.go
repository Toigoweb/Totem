package davsrc

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"golang.org/x/net/webdav"

	"github.com/toigoweb/totem/internal/smbsrc"
)

// server WebDAV vero (x/net/webdav) con autenticazione Basic, montato sotto /dav/
func davServer(t *testing.T) (string, string) {
	root := t.TempDir()
	h := &webdav.Handler{Prefix: "/dav", FileSystem: webdav.Dir(root), LockSystem: webdav.NewMemLS()}
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, p, ok := r.BasicAuth()
		if !ok || u != "totem" || p != "giusta" {
			w.Header().Set("WWW-Authenticate", `Basic realm="x"`)
			w.WriteHeader(401)
			return
		}
		if strings.Contains(r.URL.Path, "vietata") {
			w.WriteHeader(403)
			return
		}
		h.ServeHTTP(w, r)
	}))
	t.Cleanup(ts.Close)
	write := func(p, body string) {
		full := filepath.Join(root, filepath.FromSlash(p))
		os.MkdirAll(filepath.Dir(full), 0o755)
		os.WriteFile(full, []byte(body), 0o644)
	}
	write("SDS/Acetone rev3.pdf", "%PDF-1.4\nacetone\n%%EOF\n")
	write("SDS/Solventi/Diluente è nitro.pdf", "%PDF-1.4\nd\n%%EOF\n")
	write("SDS/nota.txt", "x")
	write("SDS/.nascosto.pdf", "x")
	os.MkdirAll(filepath.Join(root, "SDS", "Vuota"), 0o755)
	return ts.URL, root
}

func TestWebDAV(t *testing.T) {
	base, _ := davServer(t)
	ctx := context.Background()
	src, err := Connect(ctx, smbsrc.Target{Type: "webdav", Path: base + "/dav/SDS", User: "totem", Password: "giusta", Recursive: true})
	if err != nil {
		t.Fatal(err)
	}
	files, err := src.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, f := range files {
		names = append(names, f.Path)
		if f.Size == 0 || f.ModTime.IsZero() {
			t.Errorf("metadati mancanti: %+v", f)
		}
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
	if !strings.HasPrefix(string(b), "%PDF-1.4\nd") {
		t.Fatal(string(b))
	}
	if _, err := src.Open(ctx, "../segreto.pdf"); smbsrc.Code(err) != smbsrc.ErrInvalidPath {
		t.Fatal(err)
	}

	// non ricorsivo
	src2, _ := Connect(ctx, smbsrc.Target{Path: base + "/dav/SDS/", User: "totem", Password: "giusta"})
	files, _ = src2.List(ctx)
	if len(files) != 1 {
		t.Fatal(files)
	}
}

func TestWebDAVErrors(t *testing.T) {
	base, _ := davServer(t)
	ctx := context.Background()
	cases := map[string]smbsrc.Target{
		smbsrc.ErrAuth:         {Path: base + "/dav/SDS", User: "totem", Password: "no"},
		smbsrc.ErrNotFound:     {Path: base + "/dav/Manca", User: "totem", Password: "giusta"},
		smbsrc.ErrAccessDenied: {Path: base + "/dav/vietata", User: "totem", Password: "giusta"},
		smbsrc.ErrUnreachable:  {Path: "http://127.0.0.1:1/dav", User: "totem", Password: "giusta"},
		smbsrc.ErrInvalidPath:  {Path: "ftp://server/x"},
	}
	for want, tg := range cases {
		_, err := Connect(ctx, tg)
		if got := smbsrc.Code(err); got != want {
			t.Errorf("%s: atteso %s, ottenuto %s (%v)", tg.Path, want, got, err)
		}
	}
}
