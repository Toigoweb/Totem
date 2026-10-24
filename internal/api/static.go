package api

import (
	"io"
	"io/fs"
	"net/http"
	"path"
	"strings"
	"time"
)

const placeholderPage = `<!doctype html><html lang="it"><meta charset="utf-8"><title>Totem</title>
<body style="font-family:sans-serif;padding:2em"><h1>PWA non compilata</h1>
<p>Questo binario è stato compilato senza la PWA (web/dist vuota). Usare l'immagine Docker o eseguire <code>npm run build</code> in <code>web/</code>.</p></body></html>`

// static serve la PWA incorporata. I file con hash nel nome (assets/) sono immutabili;
// index.html, sw.js e app.webmanifest no, così gli aggiornamenti arrivano.
// Ogni percorso sconosciuto restituisce index.html (routing lato client).
func (s *Server) static() http.Handler {
	start := time.Now()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			jsonErr(w, http.StatusMethodNotAllowed, "method")
			return
		}
		if s.o.Static == nil {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = io.WriteString(w, placeholderPage)
			return
		}
		p := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")
		if p == "" {
			p = "index.html"
		}
		f, err := s.o.Static.Open(p)
		if err == nil {
			if st, _ := f.Stat(); st != nil && st.IsDir() {
				f.Close()
				err = fs.ErrNotExist
			}
		}
		if err != nil {
			// file statico mancante con estensione: 404 vero; altrimenti index.html
			if path.Ext(p) != "" {
				http.NotFound(w, r)
				return
			}
			p = "index.html"
			f, err = s.o.Static.Open(p)
			if err != nil {
				w.Header().Set("Content-Type", "text/html; charset=utf-8")
				_, _ = io.WriteString(w, placeholderPage)
				return
			}
		}
		defer f.Close()
		if strings.HasPrefix(p, "assets/") {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		} else {
			w.Header().Set("Cache-Control", "no-cache")
		}
		if p == "app.webmanifest" {
			w.Header().Set("Content-Type", "application/manifest+json")
		}
		rs, ok := f.(io.ReadSeeker)
		if !ok {
			b, _ := io.ReadAll(f)
			rs = strings.NewReader(string(b))
		}
		http.ServeContent(w, r, p, start, rs)
	})
}
