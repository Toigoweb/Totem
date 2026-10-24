package api

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/toigoweb/totem/internal/auth"
	"github.com/toigoweb/totem/internal/config"
	"github.com/toigoweb/totem/internal/smbsrc"
)

// ---------------------------------------------------------------------------
// Login e PIN

func (s *Server) maintLogin(w http.ResponseWriter, r *http.Request) {
	var req struct {
		PIN string `json:"pin"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	if lockedResponse(w, s.pinLimiter.Check(), s.o.Now()) {
		return
	}
	hash := ""
	s.o.Config.View(func(c *config.Config) { hash = c.Globali.PinHash })
	if hash == "" || !auth.ValidPIN(req.PIN) || !auth.VerifyPIN(req.PIN, hash) {
		rem := s.pinLimiter.Fail()
		if rem == 0 {
			lockedResponse(w, s.pinLimiter.Check(), s.o.Now())
			return
		}
		jsonErr(w, http.StatusUnauthorized, "wrong_pin", "remaining", rem)
		return
	}
	s.pinLimiter.Success()
	tok := s.maint.New(deviceFrom(r).ID)
	writeJSON(w, http.StatusOK, map[string]any{"token": tok, "idleSec": int(s.o.Settings.MaintenanceIdle.Seconds())})
}

func (s *Server) maintLogout(w http.ResponseWriter, r *http.Request) {
	s.maint.Delete(r.Header.Get(MaintHeader))
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) changePin(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Current string `json:"current"`
		New     string `json:"new"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	if lockedResponse(w, s.pinLimiter.Check(), s.o.Now()) {
		return
	}
	hash := ""
	s.o.Config.View(func(c *config.Config) { hash = c.Globali.PinHash })
	if !auth.VerifyPIN(req.Current, hash) {
		rem := s.pinLimiter.Fail()
		if rem == 0 {
			lockedResponse(w, s.pinLimiter.Check(), s.o.Now())
			return
		}
		jsonErr(w, http.StatusUnauthorized, "wrong_pin", "remaining", rem)
		return
	}
	s.pinLimiter.Success()
	if !auth.ValidPIN(req.New) {
		jsonErr(w, http.StatusBadRequest, "invalid_pin")
		return
	}
	h, err := auth.HashPIN(req.New)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "internal")
		return
	}
	if err := s.o.Config.Update(func(c *config.Config) error { c.Globali.PinHash = h; return nil }); err != nil {
		jsonErr(w, http.StatusInternalServerError, "save")
		return
	}
	s.o.Logger.Info("PIN di manutenzione cambiato")
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// ---------------------------------------------------------------------------
// Sezioni

type sectionOut struct {
	ID            string `json:"id"`
	Nome          string `json:"nome"`
	Icona         string `json:"icona"`
	Ordine        int    `json:"ordine"`
	Emergenza     bool   `json:"emergenza"`
	Percorso      string `json:"percorso"`
	Utente        string `json:"utente"`
	HasPassword   bool   `json:"hasPassword"`
	Sottocartelle bool   `json:"sottocartelle"`
	Documents     int    `json:"documents"`
}

// listSections non restituisce mai le password, solo se ce n'è una.
func (s *Server) listSections(w http.ResponseWriter, r *http.Request) {
	counts := map[string]int{}
	if m := s.o.Scanner.Current(); m != nil {
		for _, d := range m.Documents {
			counts[d.Section]++
		}
	}
	out := []sectionOut{}
	cfg := s.o.Config.Get()
	for _, sec := range cfg.SortedSections() {
		out = append(out, sectionOut{ID: sec.ID, Nome: sec.Nome, Icona: sec.Icona, Ordine: sec.Ordine, Emergenza: sec.Emergenza,
			Percorso: sec.Share.Percorso, Utente: sec.Share.Utente, HasPassword: sec.Share.Password != "",
			Sottocartelle: sec.Share.Sottocartelle, Documents: counts[sec.ID]})
	}
	maxN := config.MaxSections
	writeJSON(w, http.StatusOK, map[string]any{"sections": out, "max": maxN, "icons": config.Icons})
}

type sectionIn struct {
	Nome          string `json:"nome"`
	Icona         string `json:"icona"`
	Emergenza     bool   `json:"emergenza"`
	Percorso      string `json:"percorso"`
	Utente        string `json:"utente"`
	Password      string `json:"password"`
	Sottocartelle bool   `json:"sottocartelle"`
	TestToken     string `json:"testToken"`
}

// testGrant ricorda una prova di connessione riuscita: il salvataggio di percorso,
// utente o password è ammesso solo con una prova riuscita sugli stessi valori.
type testGrant struct {
	fingerprint string
	expires     time.Time
}

func fingerprint(path, user, pwd string) string {
	h := sha256.Sum256([]byte(strings.ToLower(path) + "\x00" + user + "\x00" + pwd))
	return hex.EncodeToString(h[:])
}

func (s *Server) grantTest(fp string) string {
	tok := auth.NewToken()
	now := s.o.Now()
	s.mu.Lock()
	for k, g := range s.tests {
		if now.After(g.expires) {
			delete(s.tests, k)
		}
	}
	s.tests[tok] = testGrant{fingerprint: fp, expires: now.Add(10 * time.Minute)}
	s.mu.Unlock()
	return tok
}

func (s *Server) checkTest(tok, fp string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	g, ok := s.tests[tok]
	return ok && g.fingerprint == fp && s.o.Now().Before(g.expires)
}

func (s *Server) storedPassword(sectionID string) (string, error) {
	sec, ok := s.o.Config.Get().Section(sectionID)
	if !ok {
		return "", errors.New("sezione inesistente")
	}
	if sec.Share.Password == "" {
		return "", nil
	}
	return s.o.Cipher.Decrypt(sec.ID, sec.Share.Password)
}

func (s *Server) testConnection(w http.ResponseWriter, r *http.Request) {
	var req struct {
		SectionID     string `json:"sectionId"`
		Percorso      string `json:"percorso"`
		Utente        string `json:"utente"`
		Password      string `json:"password"`
		Sottocartelle bool   `json:"sottocartelle"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	if !config.ValidSharePath(req.Percorso) {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "code": smbsrc.ErrInvalidPath})
		return
	}
	if req.Utente != "" && !config.ValidUser(req.Utente) {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "code": "invalid_user"})
		return
	}
	pwd := req.Password
	if pwd == "" && req.SectionID != "" {
		p, err := s.storedPassword(req.SectionID)
		if err != nil {
			writeJSON(w, http.StatusOK, map[string]any{"ok": false, "code": "no_password"})
			return
		}
		pwd = p
	}
	n, err := s.o.Scanner.TestConnection(r.Context(), smbsrc.Target{Path: req.Percorso, User: req.Utente, Password: pwd, Recursive: req.Sottocartelle})
	if err != nil {
		code := smbsrc.Code(err)
		s.o.Logger.Info("prova di connessione fallita", "codice", code)
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "code": code})
		return
	}
	tok := s.grantTest(fingerprint(req.Percorso, req.Utente, pwd))
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "pdfCount": n, "testToken": tok})
}

func validationResponse(w http.ResponseWriter, err error) {
	var ve *config.ValidationError
	if errors.As(err, &ve) {
		jsonErr(w, http.StatusBadRequest, "invalid", "field", ve.Field)
		return
	}
	jsonErr(w, http.StatusInternalServerError, "save")
}

func (s *Server) createSection(w http.ResponseWriter, r *http.Request) {
	var in sectionIn
	if !readJSON(w, r, &in) {
		return
	}
	in.Nome = strings.TrimSpace(in.Nome)
	sec := config.Section{ID: auth.NewID("s"), Nome: in.Nome, Icona: in.Icona, Emergenza: in.Emergenza,
		Share: config.Share{Percorso: in.Percorso, Utente: in.Utente, Sottocartelle: in.Sottocartelle}}
	if err := config.ValidateSection(sec); err != nil {
		validationResponse(w, err)
		return
	}
	if !s.checkTest(in.TestToken, fingerprint(in.Percorso, in.Utente, in.Password)) {
		jsonErr(w, http.StatusConflict, "test_required")
		return
	}
	if in.Password != "" {
		enc, err := s.o.Cipher.Encrypt(sec.ID, in.Password)
		if err != nil {
			jsonErr(w, http.StatusInternalServerError, "internal")
			return
		}
		sec.Share.Password = enc
	}
	err := s.o.Config.Update(func(c *config.Config) error {
		if len(c.Sezioni) >= config.MaxSections {
			return errTooMany
		}
		maxOrd := 0
		for _, x := range c.Sezioni {
			if x.Ordine > maxOrd {
				maxOrd = x.Ordine
			}
		}
		sec.Ordine = maxOrd + 1
		c.Sezioni = append(c.Sezioni, sec)
		return nil
	})
	if errors.Is(err, errTooMany) {
		jsonErr(w, http.StatusConflict, "too_many")
		return
	}
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "save")
		return
	}
	s.o.Logger.Info("sezione creata", "sezione", sec.ID, "nome", sec.Nome)
	writeJSON(w, http.StatusCreated, map[string]any{"id": sec.ID})
}

var (
	errTooMany  = errors.New("troppe sezioni")
	errNotFound = errors.New("non trovata")
)

func (s *Server) updateSection(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var in sectionIn
	if !readJSON(w, r, &in) {
		return
	}
	old, ok := s.o.Config.Get().Section(id)
	if !ok {
		jsonErr(w, http.StatusNotFound, "not_found")
		return
	}
	next := old
	next.Nome, next.Icona, next.Emergenza = strings.TrimSpace(in.Nome), in.Icona, in.Emergenza
	next.Share.Percorso, next.Share.Utente, next.Share.Sottocartelle = in.Percorso, in.Utente, in.Sottocartelle
	if err := config.ValidateSection(next); err != nil {
		validationResponse(w, err)
		return
	}
	shareChanged := in.Password != "" || old.Share.Percorso != in.Percorso || old.Share.Utente != in.Utente
	if shareChanged {
		pwd := in.Password
		if pwd == "" {
			p, err := s.storedPassword(id)
			if err != nil {
				jsonErr(w, http.StatusConflict, "test_required")
				return
			}
			pwd = p
		}
		if !s.checkTest(in.TestToken, fingerprint(in.Percorso, in.Utente, pwd)) {
			jsonErr(w, http.StatusConflict, "test_required")
			return
		}
		if in.Password != "" {
			enc, err := s.o.Cipher.Encrypt(id, in.Password)
			if err != nil {
				jsonErr(w, http.StatusInternalServerError, "internal")
				return
			}
			next.Share.Password = enc
		}
	}
	err := s.o.Config.Update(func(c *config.Config) error {
		for i := range c.Sezioni {
			if c.Sezioni[i].ID == id {
				next.Ordine = c.Sezioni[i].Ordine
				c.Sezioni[i] = next
				return nil
			}
		}
		return errNotFound
	})
	if err != nil {
		jsonErr(w, http.StatusNotFound, "not_found")
		return
	}
	s.o.Logger.Info("sezione modificata", "sezione", id)
	writeJSON(w, http.StatusOK, map[string]any{"id": id})
}

func (s *Server) deleteSection(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	err := s.o.Config.Update(func(c *config.Config) error {
		for i := range c.Sezioni {
			if c.Sezioni[i].ID == id {
				c.Sezioni = append(c.Sezioni[:i], c.Sezioni[i+1:]...)
				return nil
			}
		}
		return errNotFound
	})
	if err != nil {
		jsonErr(w, http.StatusNotFound, "not_found")
		return
	}
	s.o.Logger.Info("sezione eliminata", "sezione", id)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) orderSections(w http.ResponseWriter, r *http.Request) {
	var req struct {
		IDs []string `json:"ids"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	err := s.o.Config.Update(func(c *config.Config) error {
		if len(req.IDs) != len(c.Sezioni) {
			return errNotFound
		}
		pos := map[string]int{}
		for i, id := range req.IDs {
			if _, dup := pos[id]; dup {
				return errNotFound
			}
			pos[id] = i + 1
		}
		for i := range c.Sezioni {
			p, ok := pos[c.Sezioni[i].ID]
			if !ok {
				return errNotFound
			}
			c.Sezioni[i].Ordine = p
		}
		return nil
	})
	if err != nil {
		jsonErr(w, http.StatusBadRequest, "invalid")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}
