package api

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/toigoweb/totem/internal/config"
	"github.com/toigoweb/totem/internal/scanner"
	"github.com/toigoweb/totem/internal/synclog"
)

// file serve un PDF dall'archivio. Lo SHA viene validato e deve essere nel manifest:
// nessun percorso arriva mai dal client.
func (s *Server) file(w http.ResponseWriter, r *http.Request) {
	sha := r.PathValue("sha")
	p, ok := s.o.Scanner.BlobPath(sha)
	if !ok {
		jsonErr(w, http.StatusNotFound, "not_found")
		return
	}
	f, err := os.Open(p)
	if err != nil {
		jsonErr(w, http.StatusNotFound, "not_found")
		return
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "io")
		return
	}
	h := w.Header()
	h.Set("Content-Type", "application/pdf")
	h.Set("ETag", `"`+sha+`"`)
	h.Set("Cache-Control", "private, max-age=31536000, immutable")
	http.ServeContent(w, r, "", st.ModTime(), f)
}

type heartbeatReq struct {
	AppVersion string    `json:"appVersion"`
	Platform   string    `json:"platform"`
	LastSync   time.Time `json:"lastSync"`
	Documents  int       `json:"documents"`
	FreeBytes  int64     `json:"freeBytes"`
	UsedBytes  int64     `json:"usedBytes"`
}

func validPlatform(p string) string {
	switch p {
	case "windows", "android", "ios", "altro":
		return p
	}
	return "altro"
}

func (s *Server) heartbeat(w http.ResponseWriter, r *http.Request) {
	var hb heartbeatReq
	if !readJSON(w, r, &hb) {
		return
	}
	if len(hb.AppVersion) > 40 {
		hb.AppVersion = hb.AppVersion[:40]
	}
	hb.Platform = validPlatform(hb.Platform)
	d := deviceFrom(r)
	now := s.o.Now()
	// ultimoContatto in config.json al massimo una volta al minuto
	s.mu.Lock()
	due := now.Sub(s.lastSeen[d.ID]) >= time.Minute
	if due {
		s.lastSeen[d.ID] = now
	}
	s.mu.Unlock()
	if due {
		_ = s.o.Config.Update(func(c *config.Config) error {
			for i := range c.Globali.Dispositivi {
				if c.Globali.Dispositivi[i].ID == d.ID {
					c.Globali.Dispositivi[i].UltimoContatto = now
					c.Globali.Dispositivi[i].Piattaforma = hb.Platform
				}
			}
			return nil
		})
	}
	s.saveHeartbeat(d.ID, hb, now)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "serverTime": now})
}

func (s *Server) saveHeartbeat(id string, hb heartbeatReq, now time.Time) {
	p := filepath.Join(s.o.DataDir, "state", "heartbeat.json")
	s.mu.Lock()
	defer s.mu.Unlock()
	all := map[string]any{}
	if b, err := os.ReadFile(p); err == nil {
		_ = json.Unmarshal(b, &all)
	}
	all[id] = map[string]any{"receivedAt": now, "appVersion": hb.AppVersion, "platform": hb.Platform,
		"lastSync": hb.LastSync, "documents": hb.Documents, "freeBytes": hb.FreeBytes, "usedBytes": hb.UsedBytes}
	b, _ := json.MarshalIndent(all, "", "  ")
	_ = config.WriteFileAtomic(p, b, 0o600)
}

type statusSection struct {
	ID        string                `json:"id"`
	Name      string                `json:"name"`
	Documents int                   `json:"documents"`
	Scan      scanner.SectionStatus `json:"scan"`
}

func (s *Server) status(w http.ResponseWriter, r *http.Request) {
	st, last := s.o.Scanner.Status()
	m := s.o.Scanner.Current()
	counts := map[string]int{}
	ver := 0
	if m != nil {
		ver = m.Version
		for _, d := range m.Documents {
			counts[d.Section]++
		}
	}
	secs := []statusSection{}
	cfg := s.o.Config.Get()
	vis := s.visibleSections(r)
	for _, sec := range cfg.SortedSections() {
		if vis != nil && !vis[sec.ID] {
			continue
		}
		secs = append(secs, statusSection{ID: sec.ID, Name: sec.Nome, Documents: counts[sec.ID], Scan: st[sec.ID]})
	}
	var recent []synclog.Entry
	if s.o.Log != nil {
		recent = s.o.Log.Recent()
	}
	if recent == nil {
		recent = []synclog.Entry{}
	}
	for i := range recent {
		recent[i].Details = nil // il dettaglio completo resta nel log su disco
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"serverVersion": s.o.Version, "now": s.o.Now(), "manifestVersion": ver, "lastScan": last,
		"sections": secs, "syncLog": recent,
	})
}

// scanNow chiede una scansione al server ("Aggiorna ora"); al massimo una ogni 10 s.
func (s *Server) scanNow(w http.ResponseWriter, r *http.Request) {
	now := s.o.Now()
	s.mu.Lock()
	ok := now.Sub(s.lastKick) >= 10*time.Second
	if ok {
		s.lastKick = now
	}
	s.mu.Unlock()
	if ok {
		s.o.Scanner.Kick()
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"ok": true, "queued": ok})
}
