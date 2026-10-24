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
	m.HandleFunc("POST /internal/scan", func(w http.ResponseWriter, r *http.Request) {
		s.o.Scanner.Kick()
		writeJSON(w, http.StatusAccepted, map[string]any{"ok": true})
	})
	m.HandleFunc("POST /internal/pair-station", func(w http.ResponseWriter, r *http.Request) {
		code := s.StationCode(r.URL.Query().Get("nome"))
		writeJSON(w, http.StatusOK, map[string]any{"code": code, "expiresInSec": int(stationCodeTTL.Seconds())})
	})
	m.HandleFunc("POST /internal/pair-totem", func(w http.ResponseWriter, r *http.Request) {
		code, err := s.TotemCode(r.URL.Query().Get("reparto"), "")
		if err != nil {
			jsonErr(w, http.StatusNotFound, "reparto")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"code": code, "expiresInSec": int(stationCodeTTL.Seconds())})
	})
	m.HandleFunc("GET /internal/monitor", func(w http.ResponseWriter, r *http.Request) {
		writeMonitor(w, r, s.monitorReport())
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
