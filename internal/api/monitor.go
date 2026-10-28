package api

import (
	"crypto/subtle"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/toigoweb/totem/internal/auth"
	"github.com/toigoweb/totem/internal/config"
	"github.com/toigoweb/totem/internal/kiosk"
)

// Pagina di stato per gli strumenti di monitoraggio (Uptime Kuma, Zabbix, Nagios…).
//
//	GET /api/monitor            JSON
//	GET /api/monitor?formato=testo   una riga per voce, prima riga "TOTEM OK" o "TOTEM ERRORE"
//
// Token: header "Authorization: Bearer <token>" oppure ?token=<token>. HTTP 200 se tutto va
// bene, 503 se una share è in errore o un totem non si fa vivo da più della soglia.
// I documenti scartati sono avvisi: compaiono ma non rendono lo stato un errore.

type monitorSection struct {
	ID        string     `json:"id"`
	Name      string     `json:"name"`
	OK        bool       `json:"ok"`
	Error     string     `json:"error,omitempty"`
	Since     *time.Time `json:"since,omitempty"`
	Documents int        `json:"documents"`
}

type monitorDevice struct {
	Name          string    `json:"name"`
	Role          string    `json:"role"`
	Department    string    `json:"department,omitempty"`
	LastContact   time.Time `json:"lastContact"`
	MinutesSilent int       `json:"minutesSilent"`
	OK            bool      `json:"ok"`
}

type monitorReport struct {
	OK              bool             `json:"ok"`
	Time            time.Time        `json:"time"`
	ServerVersion   string           `json:"serverVersion"`
	ManifestVersion int              `json:"manifestVersion"`
	LastScan        time.Time        `json:"lastScan"`
	ThresholdMin    int              `json:"thresholdMinutes"`
	Sections        []monitorSection `json:"sections"`
	Totems          []monitorDevice  `json:"totems"`
	Discarded       int              `json:"discardedDocuments"`
	Tablets         []kiosk.Issue    `json:"tablets"`
	Emergency       *emergencyOut    `json:"emergency,omitempty"`
	Problems        []string         `json:"problems"`
	Warnings        []string         `json:"warnings"`
}

func (s *Server) monitorReport() monitorReport {
	now := s.o.Now()
	cfg := s.o.Config.Get()
	st, last := s.o.Scanner.Status()
	counts := map[string]int{}
	ver := 0
	if m := s.o.Scanner.Current(); m != nil {
		ver = m.Version
		for _, d := range m.Documents {
			counts[d.Section]++
		}
	}
	rep := monitorReport{OK: true, Time: now, ServerVersion: s.o.Version, ManifestVersion: ver, LastScan: last,
		ThresholdMin: cfg.MonitorMinutes(), Sections: []monitorSection{}, Totems: []monitorDevice{}, Problems: []string{}, Warnings: []string{}}
	for _, sec := range cfg.SortedSections() {
		ss := st[sec.ID]
		ms := monitorSection{ID: sec.ID, Name: sec.Nome, OK: ss.OK, Error: ss.Error, Since: ss.Since, Documents: counts[sec.ID]}
		rep.Sections = append(rep.Sections, ms)
		if !ss.OK && ss.Error != "" {
			rep.OK = false
			rep.Problems = append(rep.Problems, fmt.Sprintf("sezione %q: %s", sec.Nome, ss.Error))
		}
	}
	limit := time.Duration(cfg.MonitorMinutes()) * time.Minute
	for _, d := range cfg.Globali.Dispositivi {
		if d.Ruolo != RoleTotem {
			continue
		}
		silent := now.Sub(d.UltimoContatto)
		md := monitorDevice{Name: d.Nome, Role: d.Ruolo, LastContact: d.UltimoContatto, MinutesSilent: int(silent.Minutes()), OK: silent <= limit}
		if dep, ok := cfg.Department(d.Reparto); ok {
			md.Department = dep.Nome
		}
		rep.Totems = append(rep.Totems, md)
		if !md.OK {
			rep.OK = false
			rep.Problems = append(rep.Problems, fmt.Sprintf("totem %q silenzioso da %d minuti", d.Nome, md.MinutesSilent))
		}
	}
	sort.Slice(rep.Totems, func(i, j int) bool { return rep.Totems[i].Name < rep.Totems[j].Name })
	if len(cfg.Globali.Dispositivi) == 0 || len(rep.Totems) == 0 {
		rep.Warnings = append(rep.Warnings, "nessun totem abbinato")
	}
	// tablet (FreeKiosk): corrente, batteria, temperatura, posizione, raggiungibilità
	rep.Tablets = []kiosk.Issue{}
	if s.o.Kiosk != nil {
		for _, is := range s.o.Kiosk.Issues() {
			rep.Tablets = append(rep.Tablets, is)
			msg := fmt.Sprintf("tablet %q: %s", is.Device, is.Text)
			if is.Error {
				rep.OK = false
				rep.Problems = append(rep.Problems, msg)
			} else {
				rep.Warnings = append(rep.Warnings, msg)
			}
		}
	}
	if s.o.Emergency != nil {
		if em := s.o.Emergency.Get(); em.Attiva {
			v := emergencyView(em)
			rep.Emergency = &v
			rep.Warnings = append(rep.Warnings, fmt.Sprintf("emergenza in corso dalle %s: %s", em.Avviata.Local().Format("15:04"), em.Title()))
		}
	}
	rep.Discarded = len(s.o.Scanner.Problems())
	if rep.Discarded > 0 {
		rep.Warnings = append(rep.Warnings, fmt.Sprintf("%d documenti scartati", rep.Discarded))
	}
	return rep
}

func (r monitorReport) text() string {
	var b strings.Builder
	if r.OK {
		b.WriteString("TOTEM OK\n")
	} else {
		b.WriteString("TOTEM ERRORE\n")
	}
	for _, p := range r.Problems {
		b.WriteString("ERRORE " + p + "\n")
	}
	for _, w := range r.Warnings {
		b.WriteString("AVVISO " + w + "\n")
	}
	for _, s := range r.Sections {
		state := "ok"
		if !s.OK {
			state = s.Error
			if state == "" {
				state = "in attesa"
			}
		}
		fmt.Fprintf(&b, "sezione %s: %s, %d documenti\n", s.Name, state, s.Documents)
	}
	for _, t := range r.Totems {
		fmt.Fprintf(&b, "totem %s: ultimo contatto %d minuti fa\n", t.Name, t.MinutesSilent)
	}
	fmt.Fprintf(&b, "server %s, manifest %d\n", r.ServerVersion, r.ManifestVersion)
	return b.String()
}

func writeMonitor(w http.ResponseWriter, r *http.Request, rep monitorReport) {
	code := http.StatusOK
	if !rep.OK {
		code = http.StatusServiceUnavailable
	}
	if f := r.URL.Query().Get("formato"); f == "testo" || f == "text" {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(code)
		_, _ = w.Write([]byte(rep.text()))
		return
	}
	writeJSON(w, code, rep)
}

func (s *Server) monitor(w http.ResponseWriter, r *http.Request) {
	if lockedResponse(w, s.monitorLimiter.Check(), s.o.Now()) {
		return
	}
	tok := ""
	if a := r.Header.Get("Authorization"); strings.HasPrefix(a, "Bearer ") {
		tok = strings.TrimPrefix(a, "Bearer ")
	}
	if tok == "" {
		tok = r.URL.Query().Get("token") // comodo, ma finisce nei log del proxy: meglio l'header
	}
	var want string
	s.o.Config.View(func(c *config.Config) { want = c.Globali.Monitor.TokenHash })
	if want == "" {
		jsonErr(w, http.StatusNotFound, "monitor_disabled")
		return
	}
	if tok == "" || subtle.ConstantTimeCompare([]byte(auth.HashToken(tok)), []byte(want)) != 1 {
		s.monitorLimiter.Fail()
		jsonErr(w, http.StatusUnauthorized, "token")
		return
	}
	s.monitorLimiter.Success()
	writeMonitor(w, r, s.monitorReport())
}
