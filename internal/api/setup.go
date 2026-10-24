package api

import (
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/toigoweb/totem/internal/auth"
	"github.com/toigoweb/totem/internal/config"
)

func hasTotem(c *config.Config) bool {
	for _, d := range c.Globali.Dispositivi {
		if d.Ruolo == RoleTotem {
			return true
		}
	}
	return false
}

// EnsureSetupCode genera e stampa nei log un codice di setup se nessun totem è abbinato.
// Si chiama all'avvio; il codice vive solo in memoria.
func (s *Server) EnsureSetupCode() {
	need := false
	s.o.Config.View(func(c *config.Config) { need = !hasTotem(c) })
	if !need {
		return
	}
	s.mu.Lock()
	if s.setupCode == "" {
		s.setupCode = auth.NewSetupCode()
	}
	code := s.setupCode
	s.mu.Unlock()
	announceCode(code)
	s.o.Logger.Info("codice di setup generato", "codice", code)
}

func announceCode(code string) {
	line := strings.Repeat("=", 52)
	fmt.Fprintf(os.Stdout, "\n%s\n  CODICE DI SETUP DEL TOTEM:  %s\n  (monouso, da inserire nella procedura guidata)\n%s\n\n", line, code, line)
}

// ResetPairing invalida tutti i token dei dispositivi e genera un nuovo codice di setup.
// Con resetPIN azzera anche il PIN. Sezioni e share restano invariate.
func (s *Server) ResetPairing(resetPIN bool) (string, error) {
	err := s.o.Config.Update(func(c *config.Config) error {
		c.Globali.Dispositivi = []config.Device{}
		if resetPIN {
			c.Globali.PinHash = ""
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	s.maint.Clear()
	s.setup.Clear()
	code := auth.NewSetupCode()
	s.mu.Lock()
	s.setupCode = code
	s.mu.Unlock()
	s.codeLimiter.Success()
	s.o.Logger.Info("reset-pairing eseguito", "resetPin", resetPIN, "codice", code)
	announceCode(code)
	return code, nil
}

func (s *Server) setupState(w http.ResponseWriter, r *http.Request) {
	var pinSet, totem bool
	var sections int
	s.o.Config.View(func(c *config.Config) {
		pinSet, totem, sections = c.Globali.PinHash != "", hasTotem(c), len(c.Sezioni)
	})
	d, paired := s.lookupDevice(r)
	depName := ""
	s.o.Config.View(func(c *config.Config) {
		if dep, ok := c.Department(d.Reparto); ok {
			depName = dep.Nome
		}
	})
	writeJSON(w, http.StatusOK, map[string]any{
		"paired": paired, "role": d.Ruolo, "department": depName, "totemRegistered": totem, "pinSet": pinSet, "sections": sections,
		"serverVersion": s.o.Version,
	})
}

func (s *Server) setupCodeCheck(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Code     string `json:"code"`
		Platform string `json:"platform"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	if lockedResponse(w, s.codeLimiter.Check(), s.o.Now()) {
		return
	}
	// codice di una postazione o di un totem aggiuntivo: abbina questo browser senza
	// toccare gli altri dispositivi
	if s.pairByCode(w, req.Code, req.Platform) {
		return
	}
	s.mu.Lock()
	code := s.setupCode
	s.mu.Unlock()
	if code == "" {
		// il totem è già abbinato: qui valgono solo i codici di postazione
		rem := s.codeLimiter.Fail()
		if rem == 0 {
			lockedResponse(w, s.codeLimiter.Check(), s.o.Now())
			return
		}
		jsonErr(w, http.StatusUnauthorized, "wrong_code", "remaining", rem, "totemPaired", true)
		return
	}
	if !auth.CodesEqual(req.Code, code) {
		rem := s.codeLimiter.Fail()
		if rem == 0 {
			lockedResponse(w, s.codeLimiter.Check(), s.o.Now())
			return
		}
		jsonErr(w, http.StatusUnauthorized, "wrong_code", "remaining", rem)
		return
	}
	s.codeLimiter.Success()
	var pinSet bool
	var sections int
	s.o.Config.View(func(c *config.Config) { pinSet, sections = c.Globali.PinHash != "", len(c.Sezioni) })
	writeJSON(w, http.StatusOK, map[string]any{"setupToken": s.setup.New("setup"), "pinSet": pinSet, "sections": sections})
}

// setupPin conclude l'abbinamento: crea il PIN (o verifica quello esistente in caso di
// sostituzione del totem), registra il dispositivo come "totem" e consuma il codice.
func (s *Server) setupPin(w http.ResponseWriter, r *http.Request) {
	var req struct {
		SetupToken string `json:"setupToken"`
		PIN        string `json:"pin"`
		Platform   string `json:"platform"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	if _, err := s.setup.Use(req.SetupToken); err != nil {
		jsonErr(w, http.StatusUnauthorized, "setup_expired")
		return
	}
	if !auth.ValidPIN(req.PIN) {
		jsonErr(w, http.StatusBadRequest, "invalid_pin")
		return
	}
	hash := ""
	s.o.Config.View(func(c *config.Config) { hash = c.Globali.PinHash })
	if hash != "" {
		if lockedResponse(w, s.pinLimiter.Check(), s.o.Now()) {
			return
		}
		if !auth.VerifyPIN(req.PIN, hash) {
			rem := s.pinLimiter.Fail()
			if rem == 0 {
				lockedResponse(w, s.pinLimiter.Check(), s.o.Now())
				return
			}
			jsonErr(w, http.StatusUnauthorized, "wrong_pin", "remaining", rem)
			return
		}
		s.pinLimiter.Success()
	}
	newHash := ""
	if hash == "" {
		h, err := auth.HashPIN(req.PIN)
		if err != nil {
			jsonErr(w, http.StatusInternalServerError, "internal")
			return
		}
		newHash = h
	}
	tok := auth.NewToken()
	dev := config.Device{ID: auth.NewID("d"), Nome: "totem", Ruolo: RoleTotem, Piattaforma: validPlatform(req.Platform),
		TokenHash: auth.HashToken(tok), Creato: s.o.Now().UTC(), UltimoContatto: s.o.Now().UTC()}
	err := s.o.Config.Update(func(c *config.Config) error {
		if newHash != "" {
			c.Globali.PinHash = newHash
		}
		// un solo totem: un eventuale totem precedente viene sostituito
		kept := []config.Device{}
		for _, d := range c.Globali.Dispositivi {
			if d.Ruolo != RoleTotem {
				kept = append(kept, d)
			}
		}
		c.Globali.Dispositivi = append(kept, dev)
		return nil
	})
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "save")
		return
	}
	s.setup.Clear()
	s.mu.Lock()
	s.setupCode = ""
	s.mu.Unlock()
	s.setDeviceCookie(w, tok)
	s.o.Logger.Info("totem abbinato", "dispositivo", dev.ID, "piattaforma", dev.Piattaforma)
	writeJSON(w, http.StatusOK, map[string]any{
		"ok": true, "deviceId": dev.ID, "maintToken": s.maint.New(dev.ID),
		"idleSec": int(s.o.Settings.MaintenanceIdle.Seconds()), "at": time.Now().UTC(),
	})
}
