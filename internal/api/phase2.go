package api

import (
	"net/http"
	"strings"
	"time"

	"github.com/toigoweb/totem/internal/auth"
	"github.com/toigoweb/totem/internal/config"
)

// Fase 2: abbinamento dei telefoni con un codice monouso valido 2 minuti, mostrato dal
// totem come QR dietro PIN. In v1 le API esistono ma rispondono 404 finché
// globali.fase2 è false, e l'interfaccia non le mostra.

const pairCodeTTL = 2 * time.Minute

func (s *Server) pairCode(w http.ResponseWriter, r *http.Request) {
	code := auth.NormalizeCode(auth.NewSetupCode())
	now := s.o.Now()
	s.mu.Lock()
	for k, exp := range s.pairCodes {
		if now.After(exp) {
			delete(s.pairCodes, k)
		}
	}
	s.pairCodes[code] = now.Add(pairCodeTTL)
	s.mu.Unlock()
	writeJSON(w, http.StatusOK, map[string]any{"code": code, "expiresInSec": int(pairCodeTTL.Seconds())})
}

func (s *Server) pair(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Code     string `json:"code"`
		Name     string `json:"name"`
		Platform string `json:"platform"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	if lockedResponse(w, s.codeLimiter.Check(), s.o.Now()) {
		return
	}
	code := auth.NormalizeCode(req.Code)
	now := s.o.Now()
	s.mu.Lock()
	exp, ok := s.pairCodes[code]
	if ok {
		delete(s.pairCodes, code) // monouso
	}
	s.mu.Unlock()
	if !ok || now.After(exp) {
		s.codeLimiter.Fail()
		jsonErr(w, http.StatusUnauthorized, "wrong_code")
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" || len(name) > 40 {
		name = "telefono"
	}
	tok := auth.NewToken()
	dev := config.Device{ID: auth.NewID("d"), Nome: name, Ruolo: RolePhone, Piattaforma: validPlatform(req.Platform),
		TokenHash: auth.HashToken(tok), Creato: now.UTC(), UltimoContatto: now.UTC()}
	if err := s.o.Config.Update(func(c *config.Config) error {
		c.Globali.Dispositivi = append(c.Globali.Dispositivi, dev)
		return nil
	}); err != nil {
		jsonErr(w, http.StatusInternalServerError, "save")
		return
	}
	s.setDeviceCookie(w, tok)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "deviceId": dev.ID})
}

func (s *Server) listDevices(w http.ResponseWriter, r *http.Request) {
	type devOut struct {
		ID             string    `json:"id"`
		Nome           string    `json:"nome"`
		Ruolo          string    `json:"ruolo"`
		Piattaforma    string    `json:"piattaforma"`
		Creato         time.Time `json:"creato"`
		UltimoContatto time.Time `json:"ultimoContatto"`
	}
	out := []devOut{}
	s.o.Config.View(func(c *config.Config) {
		for _, d := range c.Globali.Dispositivi {
			out = append(out, devOut{d.ID, d.Nome, d.Ruolo, d.Piattaforma, d.Creato, d.UltimoContatto})
		}
	})
	writeJSON(w, http.StatusOK, map[string]any{"devices": out})
}

// revokeDevice revoca un telefono. Il totem si sostituisce solo con reset-pairing.
func (s *Server) revokeDevice(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	err := s.o.Config.Update(func(c *config.Config) error {
		for i, d := range c.Globali.Dispositivi {
			if d.ID == id && d.Ruolo == RolePhone {
				c.Globali.Dispositivi = append(c.Globali.Dispositivi[:i], c.Globali.Dispositivi[i+1:]...)
				return nil
			}
		}
		return errNotFound
	})
	if err != nil {
		jsonErr(w, http.StatusNotFound, "not_found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}
