package api

import (
	"context"
	"crypto/subtle"
	"net/http"
	"strings"
	"time"

	"github.com/toigoweb/totem/internal/auth"
	"github.com/toigoweb/totem/internal/config"
	"github.com/toigoweb/totem/internal/emergency"
)

// Modalità emergenza. Si avvia dall'area manutenzione, da un sistema esterno con un
// token dedicato (centrale antincendio, pulsante, automazione) o dal server con
// "totem emergenza avvia". I totem la leggono da GET /api/emergency ogni pochi secondi e
// la conservano anche se poi perdono la rete. I tablet con FreeKiosk ricevono anche
// l'annuncio vocale.

type emergencyOut struct {
	Active bool      `json:"active"`
	ID     string    `json:"id,omitempty"`
	Type   string    `json:"type,omitempty"`
	Title  string    `json:"title,omitempty"`
	Text   string    `json:"text,omitempty"`
	Since  time.Time `json:"since,omitempty"`
}

func emergencyView(s emergency.State) emergencyOut {
	if !s.Attiva {
		return emergencyOut{}
	}
	return emergencyOut{Active: true, ID: s.ID, Type: s.Tipo, Title: s.Title(), Text: s.Testo, Since: s.Avviata}
}

// getEmergency: per i dispositivi abbinati.
func (s *Server) getEmergency(w http.ResponseWriter, r *http.Request) {
	if s.o.Emergency == nil {
		writeJSON(w, http.StatusOK, emergencyOut{})
		return
	}
	writeJSON(w, http.StatusOK, emergencyView(s.o.Emergency.Get()))
}

// StartEmergency avvia l'emergenza e manda l'annuncio ai tablet (in background).
func (s *Server) StartEmergency(tipo, testo, da string) (emergency.State, error) {
	st, err := s.o.Emergency.Start(tipo, testo, da)
	if err != nil {
		return st, err
	}
	s.o.Logger.Warn("EMERGENZA AVVIATA", "tipo", st.Tipo, "testo", st.Testo, "da", da)
	s.announce(st)
	return st, nil
}

func (s *Server) announce(st emergency.State) {
	if s.o.Kiosk == nil {
		return
	}
	go func() {
		ok, n := s.o.Kiosk.Announce(context.Background(), st.Announcement())
		if n > 0 {
			s.o.Logger.Info("annuncio di emergenza sui tablet", "riusciti", ok, "tablet", n)
		}
	}()
}

// StopEmergency termina l'emergenza.
func (s *Server) StopEmergency(da string) (bool, error) {
	ok, err := s.o.Emergency.Stop(da)
	if ok && err == nil {
		s.o.Logger.Warn("emergenza terminata", "da", da)
	}
	return ok, err
}

func (s *Server) tabletCount() int {
	n := 0
	s.o.Config.View(func(c *config.Config) {
		for _, d := range c.Globali.Dispositivi {
			if d.Kiosk != nil && d.Kiosk.Tipo != "" {
				n++
			}
		}
	})
	return n
}

func (s *Server) maintEmergency(w http.ResponseWriter, r *http.Request) {
	st := s.o.Emergency.Get()
	var tok bool
	s.o.Config.View(func(c *config.Config) { tok = c.Globali.Emergenza.TokenHash != "" })
	writeJSON(w, http.StatusOK, map[string]any{"state": st, "view": emergencyView(st), "tokenActive": tok, "tablets": s.tabletCount(),
		"defaultEvacuation": emergency.DefaultEvacuationText, "maxText": emergency.MaxText})
}

type emergencyIn struct {
	Tipo  string `json:"tipo"`
	Testo string `json:"testo"`
}

func (s *Server) maintEmergencyStart(w http.ResponseWriter, r *http.Request) {
	var in emergencyIn
	if !readJSON(w, r, &in) {
		return
	}
	if _, err := s.StartEmergency(in.Tipo, in.Testo, deviceFrom(r).Nome); err != nil {
		validationResponse(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "tablets": s.tabletCount()})
}

func (s *Server) maintEmergencyStop(w http.ResponseWriter, r *http.Request) {
	if _, err := s.StopEmergency(deviceFrom(r).Nome); err != nil {
		jsonErr(w, http.StatusInternalServerError, "save")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// maintEmergencyAnnounce ripete l'annuncio vocale sui tablet.
func (s *Server) maintEmergencyAnnounce(w http.ResponseWriter, r *http.Request) {
	st := s.o.Emergency.Get()
	if !st.Attiva {
		jsonErr(w, http.StatusConflict, "not_active")
		return
	}
	s.announce(st)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "tablets": s.tabletCount()})
}

func (s *Server) newEmergencyToken(w http.ResponseWriter, r *http.Request) {
	tok := auth.NewToken()
	if err := s.o.Config.Update(func(c *config.Config) error {
		c.Globali.Emergenza.TokenHash = auth.HashToken(tok)
		return nil
	}); err != nil {
		jsonErr(w, http.StatusInternalServerError, "save")
		return
	}
	s.o.Logger.Info("token di emergenza generato")
	writeJSON(w, http.StatusOK, map[string]any{"token": tok})
}

func (s *Server) deleteEmergencyToken(w http.ResponseWriter, r *http.Request) {
	saveResult(w, s.o.Config.Update(func(c *config.Config) error {
		c.Globali.Emergenza.TokenHash = ""
		return nil
	}))
}

// apiEmergency: POST /api/emergency/start e /api/emergency/stop per i sistemi esterni,
// con "Authorization: Bearer <token>". Il corpo JSON è facoltativo ({"tipo","testo"});
// senza corpo parte un'evacuazione con il testo predefinito.
func (s *Server) apiEmergency(start bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if lockedResponse(w, s.emergencyLimiter.Check(), s.o.Now()) {
			return
		}
		var want string
		s.o.Config.View(func(c *config.Config) { want = c.Globali.Emergenza.TokenHash })
		if want == "" {
			jsonErr(w, http.StatusNotFound, "emergency_api_disabled")
			return
		}
		tok := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if tok == "" || subtle.ConstantTimeCompare([]byte(auth.HashToken(tok)), []byte(want)) != 1 {
			s.emergencyLimiter.Fail()
			jsonErr(w, http.StatusUnauthorized, "token")
			return
		}
		s.emergencyLimiter.Success()
		if !start {
			if _, err := s.StopEmergency("API"); err != nil {
				jsonErr(w, http.StatusInternalServerError, "save")
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{"ok": true})
			return
		}
		in := emergencyIn{Tipo: emergency.TypeEvacuation}
		if r.ContentLength != 0 && strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") && !readJSON(w, r, &in) {
			return
		}
		if in.Tipo == "" {
			in.Tipo = emergency.TypeEvacuation
		}
		st, err := s.StartEmergency(in.Tipo, in.Testo, "API")
		if err != nil {
			validationResponse(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "emergency": emergencyView(st)})
	}
}
