package api

import (
	"net/http"
	"strings"

	"github.com/toigoweb/totem/internal/auth"
	"github.com/toigoweb/totem/internal/config"
)

// Reparti, numeri utili, avvisi, salvaschermo e monitoraggio: si modificano dall'area
// manutenzione. Le liste si salvano intere: gli elementi senza id ne ricevono uno nuovo.

func (s *Server) getExtras(w http.ResponseWriter, r *http.Request) {
	c := s.o.Config.Get()
	writeJSON(w, http.StatusOK, map[string]any{
		"reparti":      c.Reparti,
		"contatti":     c.Globali.Contatti,
		"avvisi":       c.Globali.Avvisi,
		"salvaschermo": c.ScreensaverOrDefault(),
		"monitor":      map[string]any{"attivo": c.Globali.Monitor.TokenHash != "", "sogliaMinuti": c.MonitorMinutes()},
		"limiti":       map[string]int{"reparti": config.MaxDepartments, "contatti": config.MaxContacts, "avvisi": config.MaxNotices},
	})
}

func saveResult(w http.ResponseWriter, err error) {
	if err != nil {
		validationResponse(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) putDepartments(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Reparti []config.Department `json:"reparti"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	saveResult(w, s.o.Config.Update(func(c *config.Config) error {
		for i := range req.Reparti {
			req.Reparti[i].Nome = strings.TrimSpace(req.Reparti[i].Nome)
			if req.Reparti[i].ID == "" {
				req.Reparti[i].ID = auth.NewID("r")
			}
			if req.Reparti[i].Sezioni == nil {
				req.Reparti[i].Sezioni = []string{}
			}
		}
		if err := c.ValidateDepartments(req.Reparti); err != nil {
			return err
		}
		keep := map[string]bool{}
		for _, d := range req.Reparti {
			keep[d.ID] = true
		}
		for _, d := range c.Reparti {
			if !keep[d.ID] {
				c.RemoveDepartmentRefs(d.ID) // i suoi totem tornano a vedere tutto
			}
		}
		c.Reparti = req.Reparti
		return nil
	}))
}

func (s *Server) putContacts(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Contatti []config.Contact `json:"contatti"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	saveResult(w, s.o.Config.Update(func(c *config.Config) error {
		for i := range req.Contatti {
			x := &req.Contatti[i]
			x.Etichetta, x.Numero = strings.TrimSpace(x.Etichetta), strings.TrimSpace(x.Numero)
			if x.ID == "" {
				x.ID = auth.NewID("c")
			}
			if x.Reparti == nil {
				x.Reparti = []string{}
			}
		}
		if err := c.ValidateContacts(req.Contatti); err != nil {
			return err
		}
		c.Globali.Contatti = req.Contatti
		return nil
	}))
}

func (s *Server) putNotices(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Avvisi []config.Notice `json:"avvisi"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	saveResult(w, s.o.Config.Update(func(c *config.Config) error {
		for i := range req.Avvisi {
			x := &req.Avvisi[i]
			x.Testo = strings.TrimSpace(x.Testo)
			if x.ID == "" {
				x.ID = auth.NewID("a")
			}
			if x.Reparti == nil {
				x.Reparti = []string{}
			}
		}
		if err := c.ValidateNotices(req.Avvisi); err != nil {
			return err
		}
		c.Globali.Avvisi = req.Avvisi
		return nil
	}))
}

func (s *Server) putScreensaver(w http.ResponseWriter, r *http.Request) {
	var req config.Screensaver
	if !readJSON(w, r, &req) {
		return
	}
	if err := config.ValidateScreensaver(req); err != nil {
		validationResponse(w, err)
		return
	}
	saveResult(w, s.o.Config.Update(func(c *config.Config) error {
		c.Globali.Salvaschermo = req
		return nil
	}))
}

// newMonitorToken genera (o rigenera) il token della pagina di stato; si mostra una volta sola.
func (s *Server) newMonitorToken(w http.ResponseWriter, r *http.Request) {
	tok := auth.NewToken()
	if err := s.o.Config.Update(func(c *config.Config) error {
		c.Globali.Monitor.TokenHash = auth.HashToken(tok)
		return nil
	}); err != nil {
		jsonErr(w, http.StatusInternalServerError, "save")
		return
	}
	s.o.Logger.Info("token di monitoraggio generato")
	writeJSON(w, http.StatusOK, map[string]any{"token": tok})
}

func (s *Server) deleteMonitorToken(w http.ResponseWriter, r *http.Request) {
	saveResult(w, s.o.Config.Update(func(c *config.Config) error {
		c.Globali.Monitor.TokenHash = ""
		return nil
	}))
}

func (s *Server) putMonitor(w http.ResponseWriter, r *http.Request) {
	var req struct {
		SogliaMinuti int `json:"sogliaMinuti"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	if req.SogliaMinuti < 10 || req.SogliaMinuti > 7*24*60 {
		validationResponse(w, &config.ValidationError{Field: "sogliaMinuti", Msg: "da 10 minuti a 7 giorni"})
		return
	}
	saveResult(w, s.o.Config.Update(func(c *config.Config) error {
		c.Globali.Monitor.SogliaMinuti = req.SogliaMinuti
		return nil
	}))
}
