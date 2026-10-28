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
	"github.com/toigoweb/totem/internal/kiosk"
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
	role     string
	name     string
	reparto  string // solo totem
	replaces string // solo totem: id del totem da sostituire
	expires  time.Time
}

// newPairCode crea un codice monouso per il ruolo indicato.
func (s *Server) newPairCode(role, name string, ttl time.Duration) string {
	return s.newPairCodeGrant(pairGrant{role: role, name: name}, ttl)
}

func (s *Server) newPairCodeGrant(g pairGrant, ttl time.Duration) string {
	code := auth.NormalizeCode(auth.NewSetupCode())
	now := s.o.Now()
	s.mu.Lock()
	for k, g := range s.pairCodes {
		if now.After(g.expires) {
			delete(s.pairCodes, k)
		}
	}
	g.expires = now.Add(ttl)
	s.pairCodes[code] = g
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
	return s.registerGrant(w, pairGrant{role: role, name: name}, platform)
}

// registerGrant registra il dispositivo descritto da un codice; un totem di sostituzione
// prende il posto (e il reparto) di quello indicato.
func (s *Server) registerGrant(w http.ResponseWriter, g pairGrant, platform string) (config.Device, error) {
	tok := auth.NewToken()
	now := s.o.Now().UTC()
	dev := config.Device{ID: auth.NewID("d"), Nome: g.name, Ruolo: g.role, Reparto: g.reparto, Piattaforma: validPlatform(platform),
		TokenHash: auth.HashToken(tok), Creato: now, UltimoContatto: now}
	if err := s.o.Config.Update(func(c *config.Config) error {
		if _, ok := c.Department(dev.Reparto); !ok {
			dev.Reparto = "" // reparto eliminato nel frattempo
		}
		if g.replaces != "" {
			for i, d := range c.Globali.Dispositivi {
				if d.ID == g.replaces {
					c.Globali.Dispositivi = append(c.Globali.Dispositivi[:i], c.Globali.Dispositivi[i+1:]...)
					break
				}
			}
		}
		c.Globali.Dispositivi = append(c.Globali.Dispositivi, dev)
		return nil
	}); err != nil {
		return dev, err
	}
	s.setDeviceCookie(w, tok)
	s.o.Logger.Info("dispositivo abbinato", "dispositivo", dev.ID, "ruolo", dev.Ruolo, "nome", dev.Nome, "reparto", dev.Reparto,
		"sostituisce", g.replaces, "piattaforma", dev.Piattaforma)
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

// pairByCode è chiamata dalla procedura guidata quando il codice inserito è un codice di
// postazione o di un nuovo totem. Restituisce true se ha risposto.
func (s *Server) pairByCode(w http.ResponseWriter, code, platform string) bool {
	g, ok := s.takePairCode(code, RoleStation)
	if !ok {
		if g, ok = s.takePairCode(code, RoleTotem); !ok {
			return false
		}
	}
	s.codeLimiter.Success()
	dev, err := s.registerGrant(w, g, platform)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "save")
		return true
	}
	out := map[string]any{"paired": true, "role": dev.Ruolo, "deviceId": dev.ID, "name": dev.Nome}
	s.o.Config.View(func(c *config.Config) {
		if d, ok := c.Department(dev.Reparto); ok {
			out["department"] = d.Nome
		}
	})
	writeJSON(w, http.StatusOK, out)
	return true
}

// ---------------------------------------------------------------------------
// Totem aggiuntivi e sostituzioni

// totemCode genera il codice per aggiungere un totem a un reparto o per sostituirne uno.
func (s *Server) totemCode(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Reparto     string `json:"reparto"`
		Sostituisce string `json:"sostituisce"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	code, err := s.TotemCode(req.Reparto, req.Sostituisce)
	if err != nil {
		jsonErr(w, http.StatusBadRequest, "invalid")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"code": code, "expiresInSec": int(stationCodeTTL.Seconds())})
}

// TotemCode genera un codice di totem (anche dal listener interno). reparto accetta
// l'id o il nome; con sostituisce il nuovo totem eredita il reparto di quello vecchio.
func (s *Server) TotemCode(reparto, sostituisce string) (string, error) {
	g := pairGrant{role: RoleTotem, name: "totem"}
	var err error
	s.o.Config.View(func(c *config.Config) {
		if sostituisce != "" {
			for _, d := range c.Globali.Dispositivi {
				if d.ID == sostituisce && d.Ruolo == RoleTotem {
					g.replaces, g.reparto, g.name = d.ID, d.Reparto, d.Nome
					return
				}
			}
			err = errNotFound
			return
		}
		if reparto != "" {
			for _, d := range c.Reparti {
				if d.ID == reparto || strings.EqualFold(d.Nome, reparto) {
					g.reparto = d.ID
					g.name = "totem " + d.Nome
					return
				}
			}
			err = errNotFound
		}
	})
	if err != nil {
		return "", err
	}
	s.o.Logger.Info("codice di totem generato", "reparto", g.reparto, "sostituisce", g.replaces)
	return s.newPairCodeGrant(g, stationCodeTTL), nil
}

// updateDevice cambia nome o reparto di un dispositivo.
func (s *Server) updateDevice(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var req struct {
		Nome    *string `json:"nome"`
		Reparto *string `json:"reparto"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	err := s.o.Config.Update(func(c *config.Config) error {
		for i := range c.Globali.Dispositivi {
			d := &c.Globali.Dispositivi[i]
			if d.ID != id {
				continue
			}
			if req.Nome != nil {
				d.Nome = cleanName(*req.Nome, d.Nome)
			}
			if req.Reparto != nil {
				if _, ok := c.Department(*req.Reparto); !ok && *req.Reparto != "" {
					return &config.ValidationError{Field: "reparto", Msg: "reparto inesistente"}
				}
				d.Reparto = *req.Reparto
			}
			return nil
		}
		return errNotFound
	})
	if errors.Is(err, errNotFound) {
		jsonErr(w, http.StatusNotFound, "not_found")
		return
	}
	if err != nil {
		validationResponse(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
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
	Reparto        string    `json:"reparto"`
	RepartoNome    string    `json:"repartoNome"`
	Piattaforma    string    `json:"piattaforma"`
	Creato         time.Time `json:"creato"`
	UltimoContatto time.Time `json:"ultimoContatto"`
	Corrente       bool      `json:"corrente"`
	// dall'ultimo heartbeat, se c'è
	AppVersion string     `json:"appVersion,omitempty"`
	LastSync   *time.Time `json:"lastSync,omitempty"`
	Documents  *int       `json:"documents,omitempty"`
	FreeBytes  *int64     `json:"freeBytes,omitempty"`
	// tablet con app kiosk controllabile (FreeKiosk), se collegato
	Kiosk *kioskOut `json:"kiosk,omitempty"`
	// agente del PC Windows, se collegato
	Agent *agentOut `json:"agent,omitempty"`
	// dall'app (heartbeat): batteria del dispositivo, browser, cosa mostra
	Battery   *batteryHB    `json:"battery,omitempty"`
	Browser   string        `json:"browser,omitempty"`
	View      string        `json:"view,omitempty"`
	AppIssues []kiosk.Issue `json:"appIssues"`
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
			o := deviceOut{ID: d.ID, Nome: d.Nome, Ruolo: d.Ruolo, Reparto: d.Reparto, Piattaforma: d.Piattaforma, Creato: d.Creato,
				UltimoContatto: d.UltimoContatto, Corrente: d.ID == me}
			if dep, ok := c.Department(d.Reparto); ok {
				o.RepartoNome = dep.Nome
			}
			if hb, ok := hbs[d.ID]; ok {
				o.AppVersion = hb.AppVersion
				if !hb.LastSync.IsZero() {
					t := hb.LastSync
					o.LastSync = &t
				}
				n, f := hb.Documents, hb.FreeBytes
				o.Documents, o.FreeBytes = &n, &f
			}
			o.Kiosk = s.kioskOf(d)
			o.Agent = s.agentOf(d)
			o.AppIssues = []kiosk.Issue{}
			if hb, ok := hbs[d.ID]; ok {
				o.Battery, o.Browser, o.View = hb.Battery, hb.Browser, hb.View
				if d.Agente == nil {
					o.AppIssues = appIssuesOf(d, hb, s.o.Now())
				}
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

// revokeDevice revoca un dispositivo, totem compresi; il dispositivo in uso non si revoca
// da sé. Per sostituire un totem conviene il codice di sostituzione, che ne conserva il
// reparto.
func (s *Server) revokeDevice(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == deviceFrom(r).ID {
		jsonErr(w, http.StatusConflict, "self")
		return
	}
	err := s.o.Config.Update(func(c *config.Config) error {
		for i, d := range c.Globali.Dispositivi {
			if d.ID == id {
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
