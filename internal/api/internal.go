package api

import (
	"net/http"
)

// InternalHandler è il router del listener su 127.0.0.1:8081, raggiungibile solo da
// dentro il container (docker compose exec, HEALTHCHECK). Non è esposto all'esterno.
func (s *Server) InternalHandler() http.Handler {
	m := http.NewServeMux()
	m.HandleFunc("GET /internal/health", func(w http.ResponseWriter, r *http.Request) {
		body, _ := s.o.Scanner.ManifestBody()
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "version": s.o.Version, "manifest": body != nil})
	})
	m.HandleFunc("POST /internal/reset-pairing", func(w http.ResponseWriter, r *http.Request) {
		code, err := s.ResetPairing(r.URL.Query().Get("reset-pin") == "1")
		if err != nil {
			jsonErr(w, http.StatusInternalServerError, "save")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"setupCode": code})
	})
	return m
}
