package api

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"

	"github.com/toigoweb/totem/internal/config"
	"github.com/toigoweb/totem/internal/scanner"
)

// Vista del manifest per un dispositivo: le sezioni del suo reparto con i loro documenti,
// più numeri utili, avvisi e salvaschermo. Tutto viaggia nel manifest, quindi sul totem
// funziona anche offline. L'hash copre anche questi dati: se cambiano, il totem li riprende.
//
// Nota: i reparti servono a mostrare a ciascun totem ciò che gli serve, non sono un
// confine di sicurezza (un dispositivo abbinato può scaricare qualsiasi PDF pubblicato).

type viewDepartment struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type viewContact struct {
	Label  string `json:"label"`
	Number string `json:"number"`
}

type viewNotice struct {
	ID    string `json:"id"`
	Text  string `json:"text"`
	Level string `json:"level"`
	From  string `json:"from,omitempty"`
	To    string `json:"to,omitempty"`
}

type viewScreensaver struct {
	Enabled      bool   `json:"enabled"`
	AfterMinutes int    `json:"afterMinutes"`
	Night        bool   `json:"night"`
	NightHours   string `json:"nightHours"`
}

type viewManifest struct {
	scanner.Manifest
	Role        string           `json:"role"`
	Department  *viewDepartment  `json:"department,omitempty"`
	Contacts    []viewContact    `json:"contacts"`
	Notices     []viewNotice     `json:"notices"`
	Screensaver *viewScreensaver `json:"screensaver,omitempty"`
	Safety      *viewSafety      `json:"safety,omitempty"`
	// presente solo con l'interfono attivo per il dispositivo
	Intercom *viewIntercom `json:"intercom,omitempty"`
}

type viewIntercom struct {
	Enabled bool `json:"enabled"`
}

// deviceView costruisce la vista per il dispositivo d a partire dal manifest pubblicato.
func (s *Server) deviceView(d config.Device, m *scanner.Manifest) viewManifest {
	cfg := s.o.Config.Get()
	all := d.Ruolo != RoleTotem // postazioni e telefoni vedono tutto
	var visible map[string]bool
	if !all {
		visible = cfg.SectionsFor(d.Reparto)
	}
	v := viewManifest{Manifest: *m, Role: d.Ruolo, Contacts: []viewContact{}, Notices: []viewNotice{}}
	v.Sections = []scanner.MSection{}
	for _, sec := range m.Sections {
		if visible == nil || visible[sec.ID] {
			v.Sections = append(v.Sections, sec)
		}
	}
	v.Documents = []scanner.MDocument{}
	for _, doc := range m.Documents {
		if visible == nil || visible[doc.Section] {
			v.Documents = append(v.Documents, doc)
		}
	}
	if dep, ok := cfg.Department(d.Reparto); ok && !all {
		v.Department = &viewDepartment{ID: dep.ID, Name: dep.Nome}
	}
	for _, c := range cfg.ContactsFor(d.Reparto, all) {
		v.Contacts = append(v.Contacts, viewContact{Label: c.Etichetta, Number: c.Numero})
	}
	today := s.o.Now().Format("2006-01-02")
	for _, n := range cfg.NoticesFor(d.Reparto, all, today) {
		v.Notices = append(v.Notices, viewNotice{ID: n.ID, Text: n.Testo, Level: n.Livello, From: n.Dal, To: n.Al})
	}
	if d.Ruolo == RoleTotem {
		ss := cfg.ScreensaverOrDefault()
		v.Screensaver = &viewScreensaver{Enabled: ss.Attivo, AfterMinutes: ss.DopoMinuti, Night: ss.Notte, NightHours: ss.OrarioNero}
	}
	v.Safety = s.safetyView(d, cfg)
	if s.o.Intercom != nil && cfg.IntercomOn(d) {
		v.Intercom = &viewIntercom{Enabled: true}
	}
	// hash della vista: contenuto filtrato più dati del reparto (esclusi settings e data)
	b, _ := json.Marshal(struct {
		S  []scanner.MSection
		D  []scanner.MDocument
		Dp *viewDepartment
		C  []viewContact
		N  []viewNotice
		Ss *viewScreensaver
		Sf *viewSafety
		In *viewIntercom `json:",omitempty"` // assente: hash uguale a prima dell'interfono
	}{v.Sections, v.Documents, v.Department, v.Contacts, v.Notices, v.Screensaver, v.Safety, v.Intercom})
	h := sha256.Sum256(b)
	v.Hash = hex.EncodeToString(h[:])
	return v
}

func (s *Server) manifest(w http.ResponseWriter, r *http.Request) {
	m := s.o.Scanner.Current()
	if m == nil {
		jsonErr(w, http.StatusServiceUnavailable, "not_ready")
		return
	}
	v := s.deviceView(deviceFrom(r), m)
	etag := `"` + v.Hash[:32] + `"`
	w.Header().Set("Cache-Control", "no-cache, private")
	w.Header().Set("ETag", etag)
	if r.Header.Get("If-None-Match") == etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	writeJSON(w, http.StatusOK, v)
}

// visibleSections restituisce le sezioni visibili dal dispositivo della richiesta (nil = tutte).
func (s *Server) visibleSections(r *http.Request) map[string]bool {
	d := deviceFrom(r)
	if d.Ruolo != RoleTotem {
		return nil
	}
	var vis map[string]bool
	s.o.Config.View(func(c *config.Config) { vis = c.SectionsFor(d.Reparto) })
	return vis
}
