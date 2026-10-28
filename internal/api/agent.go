package api

import (
	"bytes"
	"crypto/subtle"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/toigoweb/totem/internal/auth"
	"github.com/toigoweb/totem/internal/config"
	"github.com/toigoweb/totem/internal/kiosk"
	"github.com/toigoweb/totem/internal/pcagent"
)

// Agente dei PC Windows: l'agente chiama il server con un token suo (non il cookie del
// totem) ogni 30 secondi, riferisce lo stato e riceve i comandi. Il token si genera in
// manutenzione (o con "totem token-agente") e si mostra una volta sola.

type agentOut struct {
	SchermoNotte bool         `json:"schermoNotte"`
	Stato        pcagent.View `json:"stato"`
}

func (s *Server) agentOf(d config.Device) *agentOut {
	if d.Agente == nil || s.o.Agents == nil {
		return nil
	}
	return &agentOut{SchermoNotte: d.Agente.SchermoNotte, Stato: s.o.Agents.ViewOf(d)}
}

// agentDevice trova il dispositivo dal token dell'agente.
func (s *Server) agentDevice(w http.ResponseWriter, r *http.Request) (config.Device, bool) {
	if s.o.Agents == nil {
		jsonErr(w, http.StatusNotFound, "not_found")
		return config.Device{}, false
	}
	if lockedResponse(w, s.agentLimiter.Check(), s.o.Now()) {
		return config.Device{}, false
	}
	tok := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	if tok != "" {
		h := auth.HashToken(tok)
		for _, d := range s.o.Config.Get().Globali.Dispositivi {
			if d.Agente != nil && subtle.ConstantTimeCompare([]byte(d.Agente.TokenHash), []byte(h)) == 1 {
				s.agentLimiter.Success()
				return d, true
			}
		}
	}
	s.agentLimiter.Fail()
	jsonErr(w, http.StatusUnauthorized, "token")
	return config.Device{}, false
}

// agentReport: POST /api/agent/report.
func (s *Server) agentReport(w http.ResponseWriter, r *http.Request) {
	d, ok := s.agentDevice(w, r)
	if !ok {
		return
	}
	var rep pcagent.Report
	if !readJSON(w, r, &rep) {
		return
	}
	if len(rep.Windows) > 120 {
		rep.Windows = rep.Windows[:120]
	}
	if len(rep.Versione) > 20 {
		rep.Versione = rep.Versione[:20]
	}
	cmds := s.o.Agents.Report(d.ID, rep)
	for _, c := range cmds {
		s.o.Logger.Info("comando consegnato all'agente del PC", "dispositivo", d.Nome, "azione", c.Azione)
	}
	cfg := s.o.Config.Get()
	em := emergencyOut{}
	if s.o.Emergency != nil {
		em = emergencyView(s.o.Emergency.Get())
	}
	night := d.Agente.SchermoNotte && kiosk.InWindow(s.o.Now(), cfg.ScreensaverOrDefault().OrarioNero) && !em.Active
	resp := map[string]any{"comandi": cmds, "schermoSpento": night, "intervalloSec": pcagent.PollSeconds}
	if em.Active {
		resp["emergenza"] = map[string]any{"id": em.ID, "titolo": em.Title, "testo": em.Text}
	}
	writeJSON(w, http.StatusOK, resp)
}

// agentScreenshot: POST /api/agent/screenshot con il PNG dello schermo come corpo.
func (s *Server) agentScreenshot(w http.ResponseWriter, r *http.Request) {
	d, ok := s.agentDevice(w, r)
	if !ok {
		return
	}
	b, err := io.ReadAll(io.LimitReader(r.Body, pcagent.MaxShotBytes+1))
	if err != nil || len(b) > pcagent.MaxShotBytes || !bytes.HasPrefix(b, []byte("\x89PNG")) {
		jsonErr(w, http.StatusBadRequest, "png")
		return
	}
	s.o.Agents.SetScreenshot(d.ID, b)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// NewAgentToken genera il token dell'agente di un dispositivo (id o nome).
func (s *Server) NewAgentToken(device string) (string, config.Device, error) {
	tok := auth.NewToken()
	var dev config.Device
	err := s.o.Config.Update(func(c *config.Config) error {
		for i := range c.Globali.Dispositivi {
			d := &c.Globali.Dispositivi[i]
			if d.ID != device && !strings.EqualFold(d.Nome, device) {
				continue
			}
			if d.Ruolo == RolePhone {
				return &config.ValidationError{Field: "ruolo", Msg: "solo totem e postazioni"}
			}
			if d.Agente == nil {
				d.Agente = &config.Agent{}
			}
			d.Agente.TokenHash = auth.HashToken(tok)
			dev = *d
			return nil
		}
		return errNotFound
	})
	if err == nil {
		s.o.Logger.Info("token dell'agente del PC generato", "dispositivo", dev.Nome)
	}
	return tok, dev, err
}

func (s *Server) maintAgentToken(w http.ResponseWriter, r *http.Request) {
	tok, _, err := s.NewAgentToken(r.PathValue("id"))
	if errors.Is(err, errNotFound) {
		jsonErr(w, http.StatusNotFound, "not_found")
		return
	}
	if err != nil {
		validationResponse(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"token": tok})
}

func (s *Server) updateAgentDevice(id string, fn func(d *config.Device)) error {
	return s.o.Config.Update(func(c *config.Config) error {
		for i := range c.Globali.Dispositivi {
			if c.Globali.Dispositivi[i].ID == id {
				fn(&c.Globali.Dispositivi[i])
				return nil
			}
		}
		return errNotFound
	})
}

func (s *Server) maintAgentPut(w http.ResponseWriter, r *http.Request) {
	var in struct {
		SchermoNotte bool `json:"schermoNotte"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	err := s.updateAgentDevice(r.PathValue("id"), func(d *config.Device) {
		if d.Agente != nil {
			d.Agente.SchermoNotte = in.SchermoNotte
		}
	})
	if err != nil {
		jsonErr(w, http.StatusNotFound, "not_found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) maintAgentDelete(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := s.updateAgentDevice(id, func(d *config.Device) { d.Agente = nil }); err != nil {
		jsonErr(w, http.StatusNotFound, "not_found")
		return
	}
	s.o.Agents.Forget(id)
	s.o.Logger.Info("agente del PC scollegato", "dispositivo", id)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// maintAgentCmd mette in coda un comando per l'agente; arriva al giro successivo (≤ 30 s).
func (s *Server) maintAgentCmd(w http.ResponseWriter, r *http.Request) {
	id, azione := r.PathValue("id"), r.PathValue("azione")
	if !pcagent.Actions[azione] || azione == "annuncio" {
		jsonErr(w, http.StatusNotFound, "not_found")
		return
	}
	found := false
	for _, d := range s.o.Config.Get().Globali.Dispositivi {
		found = found || (d.ID == id && d.Agente != nil)
	}
	if !found {
		jsonErr(w, http.StatusNotFound, "not_found")
		return
	}
	cid, _ := s.o.Agents.Queue(id, azione, "")
	s.o.Logger.Info("comando per l'agente del PC in coda", "dispositivo", id, "azione", azione)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "id": cid, "withinSec": pcagent.PollSeconds})
}

func (s *Server) maintAgentScreenshot(w http.ResponseWriter, r *http.Request) {
	b, at, ok := s.o.Agents.Screenshot(r.PathValue("id"))
	if !ok {
		jsonErr(w, http.StatusNotFound, "not_found")
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Scattato", at.UTC().Format(time.RFC3339))
	w.Write(b)
}

// announceAgents mette in coda l'annuncio vocale per tutti i PC con agente.
func (s *Server) announceAgents(text string) int {
	if s.o.Agents == nil {
		return 0
	}
	n := 0
	for _, d := range s.o.Config.Get().Globali.Dispositivi {
		if d.Agente != nil {
			s.o.Agents.Queue(d.ID, "annuncio", text)
			n++
		}
	}
	return n
}

// ---------------------------------------------------------------------------
// Comandi all'app del totem (PWA): consegnati con /api/emergency, che ogni dispositivo
// legge ogni 10 secondi. Consegna una volta sola.

var appActions = map[string]bool{"ricarica": true, "home": true, "sincronizza": true}

type appCommand struct {
	ID     string `json:"id"`
	Action string `json:"action"`
}

func (s *Server) maintAppCmd(w http.ResponseWriter, r *http.Request) {
	id, a := r.PathValue("id"), r.PathValue("azione")
	if !appActions[a] {
		jsonErr(w, http.StatusNotFound, "not_found")
		return
	}
	c := appCommand{ID: auth.NewID("c"), Action: a}
	s.mu.Lock()
	q := s.appCmds[id]
	dup := false
	for _, x := range q {
		dup = dup || x.Action == a
	}
	if !dup {
		s.appCmds[id] = append(q, c)
	}
	s.mu.Unlock()
	s.o.Logger.Info("comando per l'app del dispositivo in coda", "dispositivo", id, "azione", a)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "withinSec": 10})
}

func (s *Server) takeAppCommands(id string) []appCommand {
	s.mu.Lock()
	defer s.mu.Unlock()
	q := s.appCmds[id]
	delete(s.appCmds, id)
	if q == nil {
		q = []appCommand{}
	}
	return q
}

// appIssuesOf: avvisi dalla batteria segnalata dall'app (dispositivi senza agente). Dati
// più vecchi di 15 minuti non contano: il silenzio del totem è già un errore a parte.
func appIssuesOf(d config.Device, hb heartbeatReq, now time.Time) []kiosk.Issue {
	out := []kiosk.Issue{}
	b := hb.Battery
	if b == nil || b.Charging || now.Sub(hb.ReceivedAt) > 15*time.Minute {
		return out
	}
	if !hb.NotChargingSince.IsZero() && now.Sub(hb.NotChargingSince) >= pcagent.UnpluggedAfter {
		out = append(out, kiosk.Issue{DeviceID: d.ID, Device: d.Nome, Code: "unplugged", Error: true,
			Text: fmt.Sprintf("scollegato dalla corrente (batteria %.0f%%)", b.Level)})
	}
	if b.Level <= pcagent.BatteryLow {
		out = append(out, kiosk.Issue{DeviceID: d.ID, Device: d.Nome, Code: "battery", Error: true,
			Text: fmt.Sprintf("batteria scarica: %.0f%%", b.Level)})
	}
	return out
}

// appIssues: avvisi dall'app per i dispositivi senza agente né tablet collegato.
func (s *Server) appIssues(cfg *config.Config) []kiosk.Issue {
	hbs := s.readHeartbeats()
	out := []kiosk.Issue{}
	for _, d := range cfg.Globali.Dispositivi {
		if d.Agente != nil || (d.Kiosk != nil && d.Kiosk.Tipo != "") {
			continue
		}
		if hb, ok := hbs[d.ID]; ok {
			out = append(out, appIssuesOf(d, hb, s.o.Now())...)
		}
	}
	return out
}

// pcIssues: avvisi degli agenti dei PC.
func (s *Server) pcIssues(cfg *config.Config) []kiosk.Issue {
	if s.o.Agents == nil {
		return []kiosk.Issue{}
	}
	out := s.o.Agents.Issues(cfg.Globali.Dispositivi)
	if out == nil {
		out = []kiosk.Issue{}
	}
	return out
}
