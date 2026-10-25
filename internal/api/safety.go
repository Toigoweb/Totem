package api

import (
	"bytes"
	"encoding/csv"
	"errors"
	"io"
	"net/http"
	"os"
	"strconv"

	"github.com/toigoweb/totem/internal/config"
	"github.com/toigoweb/totem/internal/safety"
)

// Mappa degli incidenti: planimetrie ed eventi si gestiscono in manutenzione; il totem
// riceve nel manifest le planimetrie del suo reparto e gli eventi senza note.

type viewSafetyMap struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	SHA256 string `json:"sha256"`
	Type   string `json:"type"`
	Size   int64  `json:"size"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
}

type viewEvent struct {
	Map  string  `json:"map"`
	X    float64 `json:"x"`
	Y    float64 `json:"y"`
	Date string  `json:"date"`
	Type string  `json:"type"`
}

type viewSafety struct {
	Maps   []viewSafetyMap `json:"maps"`
	Events []viewEvent     `json:"events"`
	// LastInjury: data dell'ultimo infortunio sulle planimetrie visibili (anche oltre
	// i 3 anni inviati); Since: inizio della registrazione. Il totem calcola i giorni.
	LastInjury string `json:"lastInjury,omitempty"`
	Since      string `json:"since,omitempty"`
}

// eventi più vecchi di così non vanno al totem (il filtro più ampio è "Tutto" = 3 anni)
const safetyYears = 3

func knownDepartments(c *config.Config) map[string]bool {
	m := map[string]bool{}
	for _, d := range c.Reparti {
		m[d.ID] = true
	}
	return m
}

func (s *Server) safetyView(d config.Device, cfg *config.Config) *viewSafety {
	if s.o.Safety == nil {
		return nil
	}
	data := s.o.Safety.Get()
	maps := data.MapsFor(d.Reparto, d.Ruolo != RoleTotem, knownDepartments(cfg))
	if len(maps) == 0 {
		return nil
	}
	v := &viewSafety{Maps: []viewSafetyMap{}, Events: []viewEvent{}, Since: data.Inizio}
	vis := map[string]bool{}
	for _, m := range maps {
		vis[m.ID] = true
		v.Maps = append(v.Maps, viewSafetyMap{ID: m.ID, Name: m.Nome, SHA256: m.Immagine, Type: m.Formato, Size: m.Byte, Width: m.Larghezza, Height: m.Altezza})
	}
	from := s.o.Now().AddDate(-safetyYears, 0, 0).Format("2006-01-02")
	for _, e := range data.SortedEvents() {
		if !vis[e.Mappa] {
			continue
		}
		if e.Tipo == safety.TypeInjury && e.Data > v.LastInjury {
			v.LastInjury = e.Data
		}
		if e.Data >= from {
			v.Events = append(v.Events, viewEvent{Map: e.Mappa, X: e.X, Y: e.Y, Date: e.Data, Type: e.Tipo})
		}
	}
	return v
}

// mapImage serve l'immagine di una planimetria ai dispositivi abbinati.
func (s *Server) mapImage(w http.ResponseWriter, r *http.Request) {
	sha := r.PathValue("sha")
	if s.o.Safety == nil {
		jsonErr(w, http.StatusNotFound, "not_found")
		return
	}
	p, ok := s.o.Safety.ImagePath(sha)
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
	h.Set("Content-Type", s.o.Safety.MapFormat(sha))
	h.Set("ETag", `"`+sha+`"`)
	h.Set("Cache-Control", "private, max-age=31536000, immutable")
	http.ServeContent(w, r, "", st.ModTime(), f)
}

func safetyErr(w http.ResponseWriter, err error) {
	switch {
	case err == nil:
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	case errors.Is(err, safety.ErrNotFound):
		jsonErr(w, http.StatusNotFound, "not_found")
	default:
		validationResponse(w, err)
	}
}

func (s *Server) getSafety(w http.ResponseWriter, r *http.Request) {
	d := s.o.Safety.Get()
	writeJSON(w, http.StatusOK, map[string]any{
		"mappe":  d.Mappe,
		"eventi": d.SortedEvents(),
		"inizio": d.Inizio,
		"limiti": map[string]any{"mappe": safety.MaxMaps, "eventi": safety.MaxEvents, "byte": safety.MaxImageBytes, "nota": safety.MaxNote},
	})
}

// addSafetyMap riceve l'immagine come corpo (image/png o image/jpeg), il nome in ?nome=.
func (s *Server) addSafetyMap(w http.ResponseWriter, r *http.Request) {
	b, err := io.ReadAll(io.LimitReader(r.Body, safety.MaxImageBytes+1))
	if err != nil {
		jsonErr(w, http.StatusBadRequest, "io")
		return
	}
	if len(b) > safety.MaxImageBytes {
		jsonErr(w, http.StatusRequestEntityTooLarge, "too_large", "maxBytes", safety.MaxImageBytes)
		return
	}
	m, err := s.o.Safety.AddMap(r.URL.Query().Get("nome"), b)
	if err != nil {
		validationResponse(w, err)
		return
	}
	s.o.Logger.Info("planimetria aggiunta", "id", m.ID, "nome", m.Nome)
	writeJSON(w, http.StatusCreated, m)
}

func (s *Server) updateSafetyMap(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Nome    string   `json:"nome"`
		Reparti []string `json:"reparti"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	safetyErr(w, s.o.Safety.UpdateMap(r.PathValue("id"), req.Nome, req.Reparti, knownDepartments(s.o.Config.Get())))
}

func (s *Server) deleteSafetyMap(w http.ResponseWriter, r *http.Request) {
	err := s.o.Safety.DeleteMap(r.PathValue("id"))
	if err == nil {
		s.o.Logger.Info("planimetria eliminata", "id", r.PathValue("id"))
	}
	safetyErr(w, err)
}

func (s *Server) putSafetySettings(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Inizio string `json:"inizio"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	safetyErr(w, s.o.Safety.SetStart(req.Inizio))
}

func (s *Server) putSafetyEvent(w http.ResponseWriter, r *http.Request) {
	var e safety.Event
	if !readJSON(w, r, &e) {
		return
	}
	e.ID = r.PathValue("id") // vuoto in creazione
	out, err := s.o.Safety.PutEvent(e)
	if err != nil {
		safetyErr(w, err)
		return
	}
	code := http.StatusOK
	if r.Method == http.MethodPost {
		code = http.StatusCreated
	}
	writeJSON(w, code, out)
}

func (s *Server) deleteSafetyEvent(w http.ResponseWriter, r *http.Request) {
	safetyErr(w, s.o.Safety.DeleteEvent(r.PathValue("id")))
}

// exportSafety: CSV degli eventi (separatore ";" per Excel in italiano, BOM UTF-8).
func (s *Server) exportSafety(w http.ResponseWriter, r *http.Request) {
	d := s.o.Safety.Get()
	names := map[string]string{}
	for _, m := range d.Mappe {
		names[m.ID] = m.Nome
	}
	label := map[string]string{safety.TypeInjury: "Infortunio", safety.TypeFirstAid: "Medicazione", safety.TypeNearMiss: "Quasi incidente", safety.TypeDamage: "Danno a cose"}
	var buf bytes.Buffer
	buf.WriteString("\xef\xbb\xbf") // BOM: Excel riconosce l'UTF-8
	cw := csv.NewWriter(&buf)
	cw.Comma = ';'
	cw.Write([]string{"Data", "Tipo", "Planimetria", "X %", "Y %", "Nota"})
	pct := func(f float64) string { return strconv.FormatFloat(f*100, 'f', 1, 64) }
	for _, e := range d.SortedEvents() {
		cw.Write([]string{e.Data, label[e.Tipo], names[e.Mappa], pct(e.X), pct(e.Y), e.Nota})
	}
	cw.Flush()
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="eventi-sicurezza-`+s.o.Now().Format("2006-01-02")+`.csv"`)
	w.Write(buf.Bytes())
}
