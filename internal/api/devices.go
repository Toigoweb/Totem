package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/toigoweb/totem/internal/auth"
	"github.com/toigoweb/totem/internal/config"
	"github.com/toigoweb/totem/internal/scanner"
)

// Dispositivi oltre al totem.
//
//   - Postazione: un altro PC (ufficio IT, prove) che legge i documenti e può entrare in
//     manutenzione con il PIN, senza sostituire il totem. Si abbina con un codice monouso
//     valido 10 minuti, generato dall'area manutenzione o con "totem abbina-postazione".
//     Il codice si inserisce nella procedura guidata al posto del codice di setup.
//   - Telefono (fase 2): codice monouso valido 2 minuti mostrato dal totem come QR.
//     Le API rispondono 404 finché globali.fase2 è false.

const (
	stationCodeTTL = 10 * time.Minute
	phoneCodeTTL   = 2 * time.Minute
)

type pairGrant struct {
	role    string
	name    string
	expires time.Time
}

// newPairCode crea un codice monouso per il ruolo indicato.
func (s *Server) newPairCode(role, name string, ttl time.Duration) string {
	code := auth.NormalizeCode(auth.NewSetupCode())
	now := s.o.Now()
	s.mu.Lock()
	for k, g := range s.pairCodes {
		if now.After(g.expires) {
			delete(s.pairCodes, k)
		}
	}
	s.pairCodes[code] = pairGrant{role: role, name: name, expires: now.Add(ttl)}
	s.mu.Unlock()
	return code
}

// takePairCode consuma un codice (monouso) se esiste, non è scaduto ed è del ruolo atteso.
func (s *Server) takePairCode(code, role string) (pairGrant, bool) {
	code = auth.NormalizeCode(code)
	s.mu.Lock()
	defer s.mu.Unlock()
	g, ok := s.pairCodes[code]
	if !ok || g.role != role {
		return pairGrant{}, false
	}
	delete(s.pairCodes, code)
	return g, s.o.Now().Before(g.expires)
}

// registerDevice aggiunge un dispositivo (non totem) e imposta il suo cookie.
func (s *Server) registerDevice(w http.ResponseWriter, role, name, platform string) (config.Device, error) {
	tok := auth.NewToken()
	now := s.o.Now().UTC()
	dev := config.Device{ID: auth.NewID("d"), Nome: name, Ruolo: role, Piattaforma: validPlatform(platform),
		TokenHash: auth.HashToken(tok), Creato: now, UltimoContatto: now}
	if err := s.o.Config.Update(func(c *config.Config) error {
		c.Globali.Dispositivi = append(c.Globali.Dispositivi, dev)
		return nil
	}); err != nil {
		return dev, err
	}
	s.setDeviceCookie(w, tok)
	s.o.Logger.Info("dispositivo abbinato", "dispositivo", dev.ID, "ruolo", role, "nome", name, "piattaforma", dev.Piattaforma)
	return dev, nil
}

func cleanName(n, def string) string {
	n = strings.TrimSpace(n)
	if n == "" || len([]rune(n)) > 40 {
		return def
	}
	return n
}

// ---------------------------------------------------------------------------
// Postazioni

// stationCode genera dall'area manutenzione il codice per abbinare un altro PC.
func (s *Server) stationCode(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Nome string `json:"nome"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	code := s.StationCode(req.Nome)
	writeJSON(w, http.StatusOK, map[string]any{"code": code, "expiresInSec": int(stationCodeTTL.Seconds())})
}

// StationCode genera un codice di postazione (anche dal listener interno).
func (s *Server) StationCode(nome string) string {
	code := s.newPairCode(RoleStation, cleanName(nome, "Postazione"), stationCodeTTL)
	s.o.Logger.Info("codice di postazione generato", "nome", cleanName(nome, "Postazione"))
	return code
}

// pairStation è chiamata dalla procedura guidata quando il codice inserito è un codice
// di postazione. Restituisce true se ha risposto.
func (s *Server) pairStation(w http.ResponseWriter, code, platform string) bool {
	g, ok := s.takePairCode(code, RoleStation)
	if !ok {
		return false
	}
	s.codeLimiter.Success()
	dev, err := s.registerDevice(w, RoleStation, g.name, platform)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "save")
		return true
	}
	writeJSON(w, http.StatusOK, map[string]any{"paired": true, "role": RoleStation, "deviceId": dev.ID, "name": dev.Nome})
	return true
}

// ---------------------------------------------------------------------------
// Telefoni (fase 2)

func (s *Server) pairCode(w http.ResponseWriter, r *http.Request) {
	code := s.newPairCode(RolePhone, "telefono", phoneCodeTTL)
	writeJSON(w, http.StatusOK, map[string]any{"code": code, "expiresInSec": int(phoneCodeTTL.Seconds())})
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
	if _, ok := s.takePairCode(req.Code, RolePhone); !ok {
		s.codeLimiter.Fail()
		jsonErr(w, http.StatusUnauthorized, "wrong_code")
		return
	}
	dev, err := s.registerDevice(w, RolePhone, cleanName(req.Name, "telefono"), req.Platform)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "save")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "deviceId": dev.ID})
}

// ---------------------------------------------------------------------------
// Elenco e revoca

type deviceOut struct {
	ID             string    `json:"id"`
	Nome           string    `json:"nome"`
	Ruolo          string    `json:"ruolo"`
	Piattaforma    string    `json:"piattaforma"`
	Creato         time.Time `json:"creato"`
	UltimoContatto time.Time `json:"ultimoContatto"`
	Corrente       bool      `json:"corrente"`
	// dall'ultimo heartbeat, se c'è
	AppVersion string     `json:"appVersion,omitempty"`
	LastSync   *time.Time `json:"lastSync,omitempty"`
	Documents  *int       `json:"documents,omitempty"`
	FreeBytes  *int64     `json:"freeBytes,omitempty"`
}

func (s *Server) readHeartbeats() map[string]heartbeatReq {
	out := map[string]heartbeatReq{}
	s.mu.Lock()
	b, err := os.ReadFile(filepath.Join(s.o.DataDir, "state", "heartbeat.json"))
	s.mu.Unlock()
	if err == nil {
		_ = json.Unmarshal(b, &out)
	}
	return out
}

func (s *Server) listDevices(w http.ResponseWriter, r *http.Request) {
	hbs := s.readHeartbeats()
	me := deviceFrom(r).ID
	out := []deviceOut{}
	s.o.Config.View(func(c *config.Config) {
		for _, d := range c.Globali.Dispositivi {
			o := deviceOut{ID: d.ID, Nome: d.Nome, Ruolo: d.Ruolo, Piattaforma: d.Piattaforma, Creato: d.Creato,
				UltimoContatto: d.UltimoContatto, Corrente: d.ID == me}
			if hb, ok := hbs[d.ID]; ok {
				o.AppVersion = hb.AppVersion
				if !hb.LastSync.IsZero() {
					t := hb.LastSync
					o.LastSync = &t
				}
				n, f := hb.Documents, hb.FreeBytes
				o.Documents, o.FreeBytes = &n, &f
			}
			out = append(out, o)
		}
	})
	// prima il totem, poi le postazioni, poi i telefoni
	rank := map[string]int{RoleTotem: 0, RoleStation: 1, RolePhone: 2}
	sort.SliceStable(out, func(i, j int) bool { return rank[out[i].Ruolo] < rank[out[j].Ruolo] })
	var phase2 bool
	s.o.Config.View(func(c *config.Config) { phase2 = c.Globali.Fase2 })
	writeJSON(w, http.StatusOK, map[string]any{"devices": out, "fase2": phase2})
}

var errSelf = errors.New("non si può revocare il dispositivo in uso")

// revokeDevice revoca una postazione o un telefono. Il totem si sostituisce solo con
// reset-pairing; il dispositivo in uso non si revoca da sé.
func (s *Server) revokeDevice(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == deviceFrom(r).ID {
		jsonErr(w, http.StatusConflict, "self")
		return
	}
	err := s.o.Config.Update(func(c *config.Config) error {
		for i, d := range c.Globali.Dispositivi {
			if d.ID == id && d.Ruolo != RoleTotem {
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
	s.o.Logger.Info("dispositivo revocato", "dispositivo", id)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// ---------------------------------------------------------------------------
// Documenti scartati

type problemOut struct {
	scanner.Problem
	SectionName string `json:"sectionName"`
}

// problems elenca i PDF scartati dal server. Non sono i log completi: solo sezione,
// percorso relativo, motivo e date.
func (s *Server) problems(w http.ResponseWriter, r *http.Request) {
	names := map[string]string{}
	s.o.Config.View(func(c *config.Config) {
		for _, sec := range c.Sezioni {
			names[sec.ID] = sec.Nome
		}
	})
	out := []problemOut{}
	for _, p := range s.o.Scanner.Problems() {
		if n, ok := names[p.Section]; ok {
			out = append(out, problemOut{Problem: p, SectionName: n})
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"problems": out})
}
