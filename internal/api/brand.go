package api

import (
	"net/http"
	"os"
	"path/filepath"
)

// logo serve il logo aziendale, se presente in /data (logo.svg, logo.png o logo.jpg).
// È pubblico: compare anche sul totem non ancora abbinato.
func (s *Server) logo(w http.ResponseWriter, r *http.Request) {
	for _, name := range []string{"logo.svg", "logo.png", "logo.jpg"} {
		p := filepath.Join(s.o.DataDir, name)
		f, err := os.Open(p)
		if err != nil {
			continue
		}
		defer f.Close()
		st, err := f.Stat()
		if err != nil || !st.Mode().IsRegular() || st.Size() > 2<<20 {
			continue
		}
		ct := map[string]string{"logo.svg": "image/svg+xml", "logo.png": "image/png", "logo.jpg": "image/jpeg"}[name]
		w.Header().Set("Content-Type", ct)
		w.Header().Set("Cache-Control", "no-cache")
		// un SVG aperto direttamente non deve poter eseguire script con l'origine del totem
		w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; sandbox")
		http.ServeContent(w, r, "", st.ModTime(), f)
		return
	}
	http.NotFound(w, r)
}
