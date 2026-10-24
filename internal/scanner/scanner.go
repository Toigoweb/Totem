// Package scanner legge periodicamente le share, copia i PDF validi nell'archivio
// /data/blobs e pubblica il manifest. È l'unico componente che scrive blob e manifest.
package scanner

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/toigoweb/totem/internal/config"
	"github.com/toigoweb/totem/internal/smbsrc"
	"github.com/toigoweb/totem/internal/synclog"
)

// Codici di errore propri dello scanner, oltre a quelli di smbsrc.
const (
	ErrCodeCorrupt    = "corrupt"
	ErrCodeTooLarge   = "too_large"
	ErrCodeNoPassword = "no_password"
)

// Entry è un documento accettato: l'ultima versione valida vista per quel percorso.
type Entry struct {
	Section   string    `json:"section"`
	Path      string    `json:"path"`
	Size      int64     `json:"size"`
	MTime     time.Time `json:"mtime"`
	SHA256    string    `json:"sha256"`
	FirstSeen time.Time `json:"firstSeen"`
	Updated   time.Time `json:"updated"`
}

type stamp struct {
	Size  int64     `json:"size"`
	MTime time.Time `json:"mtime"`
}

func (a stamp) eq(b stamp) bool { return a.Size == b.Size && a.MTime.Equal(b.MTime) }

type rejected struct {
	stamp
	Code string    `json:"code"`
	At   time.Time `json:"at"` // primo scarto di questa versione del file
}

// SectionStatus è l'esito dell'ultima scansione di una sezione.
type SectionStatus struct {
	OK      bool       `json:"ok"`
	At      time.Time  `json:"at"`
	Error   string     `json:"error"`
	Since   *time.Time `json:"since"`
	Pending int        `json:"pending"`
}

// state è persistito in /data/state/index.json. Fa anche da cache degli hash:
// lo SHA-256 si ricalcola solo se cambiano dimensione o data di modifica.
type state struct {
	Version    int                       `json:"version"`
	Hash       string                    `json:"hash"`
	Entries    map[string]*Entry         `json:"entries"`
	Candidates map[string]stamp          `json:"candidates"`
	Rejected   map[string]rejected       `json:"rejected"`
	Status     map[string]*SectionStatus `json:"status"`
}

func newState() *state {
	return &state{Entries: map[string]*Entry{}, Candidates: map[string]stamp{}, Rejected: map[string]rejected{}, Status: map[string]*SectionStatus{}}
}

type Options struct {
	DataDir  string
	Config   *config.Store
	Cipher   *config.Cipher
	Settings config.Settings
	Log      *synclog.Log
	Logger   *slog.Logger
	// Connect apre una share; di default smbsrc.Connect.
	Connect func(context.Context, smbsrc.Target) (smbsrc.Source, error)
	Now     func() time.Time
	// Port sostituisce la 445 (solo test di integrazione).
	Port int
}

type Scanner struct {
	o     Options
	blobs *BlobStore
	kick  chan struct{}

	scanMu sync.Mutex // una scansione alla volta
	st     *state     // di proprietà della scansione in corso

	mu       sync.RWMutex // protegge i campi pubblicati qui sotto
	current  *Manifest
	prev     *Manifest
	body     []byte
	etag     string
	status   map[string]SectionStatus
	problems []Problem
	lastScan time.Time
}

func New(o Options) (*Scanner, error) {
	if o.Connect == nil {
		o.Connect = smbsrc.Connect
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	if o.Logger == nil {
		o.Logger = slog.Default()
	}
	blobs, err := NewBlobStore(filepath.Join(o.DataDir, "blobs"))
	if err != nil {
		return nil, err
	}
	s := &Scanner{o: o, blobs: blobs, kick: make(chan struct{}, 1), status: map[string]SectionStatus{}}
	s.current = readManifest(s.manifestPath())
	s.prev = readManifest(s.manifestPath() + ".prev")
	s.st = newState()
	if b, err := os.ReadFile(s.statePath()); err == nil {
		if err := json.Unmarshal(b, s.st); err != nil {
			o.Logger.Warn("index.json illeggibile, ricostruito dal manifest", "err", err)
			s.st = newState()
		}
	}
	s.st.fill()
	if len(s.st.Entries) == 0 && s.current != nil && len(s.current.Documents) > 0 {
		// index.json perso: si riparte dal manifest pubblicato, così nessuna sezione
		// si svuota mentre i file attendono le due scansioni di stabilità.
		for _, d := range s.current.Documents {
			s.st.Entries[d.ID] = &Entry{Section: d.Section, Path: d.Path, Size: d.Size, MTime: d.MTime, SHA256: d.SHA256, FirstSeen: d.FirstSeen, Updated: d.Updated}
		}
		s.st.Version, s.st.Hash = s.current.Version, s.current.Hash
	}
	if s.current == nil {
		s.st.Hash = "" // forza la pubblicazione alla prima scansione
	} else {
		// le impostazioni possono essere cambiate da file: si rigenera il corpo
		s.current.Settings = settingsFor(o.Settings)
		if err := s.writeManifest(s.current); err != nil {
			return nil, err
		}
	}
	for id, st := range s.st.Status {
		s.status[id] = *st
	}
	s.problems = s.buildProblems()
	return s, nil
}

func (st *state) fill() {
	if st.Entries == nil {
		st.Entries = map[string]*Entry{}
	}
	if st.Candidates == nil {
		st.Candidates = map[string]stamp{}
	}
	if st.Rejected == nil {
		st.Rejected = map[string]rejected{}
	}
	if st.Status == nil {
		st.Status = map[string]*SectionStatus{}
	}
}

func (s *Scanner) manifestPath() string { return filepath.Join(s.o.DataDir, "manifest.json") }
func (s *Scanner) statePath() string    { return filepath.Join(s.o.DataDir, "state", "index.json") }

func readManifest(p string) *Manifest {
	b, err := os.ReadFile(p)
	if err != nil {
		return nil
	}
	var m Manifest
	if json.Unmarshal(b, &m) != nil || m.SchemaVersion != ManifestSchema {
		return nil
	}
	return &m
}

// Kick chiede una scansione appena possibile (per esempio dopo una modifica della config).
func (s *Scanner) Kick() {
	select {
	case s.kick <- struct{}{}:
	default:
	}
}

// Run esegue le scansioni finché ctx non viene annullato.
func (s *Scanner) Run(ctx context.Context) {
	for {
		pending := s.ScanOnce(ctx)
		wait := s.o.Settings.ScanInterval
		if pending {
			wait = s.o.Settings.StabilityDelay
		}
		t := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			t.Stop()
			return
		case <-t.C:
		case <-s.kick:
			t.Stop()
			// piccola attesa per raccogliere più modifiche ravvicinate
			select {
			case <-ctx.Done():
				return
			case <-time.After(time.Second):
			}
		}
	}
}

// ScanOnce esegue una scansione completa e pubblica il manifest se è cambiato.
// Restituisce true se restano file in attesa di stabilità.
func (s *Scanner) ScanOnce(ctx context.Context) (pending bool) {
	s.scanMu.Lock()
	defer s.scanMu.Unlock()
	now := s.o.Now().UTC()
	cfg := s.o.Config.Get()
	res := synclog.Entry{At: now}
	inCfg := map[string]bool{}
	for _, sec := range cfg.Sezioni {
		inCfg[sec.ID] = true
		if s.scanSection(ctx, sec, now, &res) {
			pending = true
		}
	}
	// sezioni eliminate: i loro documenti escono dal manifest
	for id, e := range s.st.Entries {
		if !inCfg[e.Section] {
			delete(s.st.Entries, id)
			res.Removed++
		}
	}
	for id := range s.st.Status {
		if !inCfg[id] {
			delete(s.st.Status, id)
		}
	}
	for id := range s.st.Candidates {
		if !inCfg[sectionOf(id)] {
			delete(s.st.Candidates, id)
		}
	}
	for id := range s.st.Rejected {
		if !inCfg[sectionOf(id)] {
			delete(s.st.Rejected, id)
		}
	}
	s.publish(cfg, now)
	if err := s.saveState(); err != nil {
		s.o.Logger.Error("salvataggio index.json", "err", err)
	}
	if res.Added+res.Updated+res.Removed+res.Errors > 0 {
		if s.o.Log != nil {
			_ = s.o.Log.Write(res)
		}
		s.o.Logger.Info("scansione", "nuovi", res.Added, "aggiornati", res.Updated, "rimossi", res.Removed, "errori", res.Errors)
	}
	probs := s.buildProblems()
	s.mu.Lock()
	s.status = map[string]SectionStatus{}
	for id, st := range s.st.Status {
		s.status[id] = *st
	}
	s.problems = probs
	s.lastScan = now
	s.mu.Unlock()
	return pending
}

func (s *Scanner) buildProblems() []Problem {
	out := []Problem{}
	for id, r := range s.st.Rejected {
		sec := sectionOf(id)
		_, prev := s.st.Entries[id]
		out = append(out, Problem{Section: sec, Path: id[len(sec)+1:], Code: r.Code, Size: r.Size, MTime: r.MTime, At: r.At, HasPrev: prev})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Section != out[j].Section {
			return out[i].Section < out[j].Section
		}
		return out[i].Path < out[j].Path
	})
	return out
}

func sectionOf(docID string) string {
	for i := 0; i < len(docID); i++ {
		if docID[i] == '/' {
			return docID[:i]
		}
	}
	return docID
}

func (s *Scanner) target(sec config.Section) (smbsrc.Target, error) {
	pwd := ""
	if sec.Share.Password != "" {
		p, err := s.o.Cipher.Decrypt(sec.ID, sec.Share.Password)
		if err != nil {
			return smbsrc.Target{}, err
		}
		pwd = p
	}
	return smbsrc.Target{Path: sec.Share.Percorso, User: sec.Share.Utente, Password: pwd, Recursive: sec.Share.Sottocartelle, Port: s.o.Port}, nil
}

func connLevel(code string) bool {
	return code == smbsrc.ErrUnreachable || code == smbsrc.ErrTimeout || code == smbsrc.ErrAuth
}

func (s *Scanner) scanSection(ctx context.Context, sec config.Section, now time.Time, res *synclog.Entry) (pending bool) {
	stt := s.st.Status[sec.ID]
	if stt == nil {
		stt = &SectionStatus{}
		s.st.Status[sec.ID] = stt
	}
	fail := func(code string, err error) {
		// un errore che si ripete uguale non si conta a ogni scansione
		if stt.OK || stt.Error != code || stt.At.IsZero() {
			res.Errors++
			res.Details = append(res.Details, synclog.Detail{Section: sec.ID, Code: code})
			s.o.Logger.Warn("sezione non leggibile", "sezione", sec.ID, "nome", sec.Nome, "codice", code, "err", errString(err))
		}
		if stt.OK || stt.Since == nil {
			t := now
			stt.Since = &t
		}
		stt.OK, stt.Error, stt.At = false, code, now
	}
	t, err := s.target(sec)
	if err != nil {
		fail(ErrCodeNoPassword, err)
		return false
	}
	lctx, cancel := context.WithTimeout(ctx, s.o.Settings.SMBTimeout)
	src, err := s.o.Connect(lctx, t)
	if err != nil {
		cancel()
		fail(smbsrc.Code(err), err)
		return false
	}
	defer src.Close()
	files, err := src.List(lctx)
	cancel()
	if err != nil {
		fail(smbsrc.Code(err), err)
		return false
	}
	listed := map[string]bool{}
	nPending := 0
	for _, f := range files {
		id := sec.ID + "/" + f.Path
		listed[id] = true
		cur := stamp{f.Size, f.ModTime}
		e := s.st.Entries[id]
		same := e != nil && cur.eq(stamp{e.Size, e.MTime})
		if same && s.blobs.Has(e.SHA256) {
			delete(s.st.Candidates, id)
			delete(s.st.Rejected, id)
			continue
		}
		if r, ok := s.st.Rejected[id]; ok && r.stamp.eq(cur) {
			continue // già scartato: si tiene la versione precedente
		}
		if !same {
			// PDF in scrittura: si accetta solo se è invariato per due scansioni consecutive
			if c, ok := s.st.Candidates[id]; !ok || !c.eq(cur) {
				s.st.Candidates[id] = cur
				nPending++
				continue
			}
		}
		sha, err := s.fetch(ctx, src, f)
		delete(s.st.Candidates, id)
		if err != nil {
			code := ingestCode(err)
			switch {
			case errors.Is(err, ErrChanged):
				nPending++
				continue
			case code == ErrCodeCorrupt || code == ErrCodeTooLarge:
				s.st.Rejected[id] = rejected{cur, code, now}
				res.Errors++
				res.Details = append(res.Details, synclog.Detail{Section: sec.ID, Path: f.Path, Code: code, Msg: err.Error()})
				s.o.Logger.Warn("PDF scartato, resta la versione precedente", "sezione", sec.ID, "file", f.Path, "codice", code, "err", err.Error())
				continue
			case connLevel(code):
				// la share è caduta durante la lettura: niente rimozioni in questo giro
				s.st.Candidates[id] = cur
				fail(code, err)
				return true
			default:
				s.st.Candidates[id] = cur // si riprova alla prossima scansione
				nPending++
				res.Errors++
				res.Details = append(res.Details, synclog.Detail{Section: sec.ID, Path: f.Path, Code: code})
				continue
			}
		}
		delete(s.st.Rejected, id)
		if e == nil {
			s.st.Entries[id] = &Entry{Section: sec.ID, Path: f.Path, Size: f.Size, MTime: f.ModTime, SHA256: sha, FirstSeen: now, Updated: now}
			res.Added++
			continue
		}
		if e.SHA256 != sha {
			e.Updated = now
			res.Updated++
		}
		e.SHA256, e.Size, e.MTime = sha, f.Size, f.ModTime
	}
	prefix := sec.ID + "/"
	for id, e := range s.st.Entries {
		if e.Section == sec.ID && !listed[id] {
			delete(s.st.Entries, id)
			res.Removed++
		}
	}
	for id := range s.st.Candidates {
		if len(id) > len(prefix) && id[:len(prefix)] == prefix && !listed[id] {
			delete(s.st.Candidates, id)
		}
	}
	for id := range s.st.Rejected {
		if len(id) > len(prefix) && id[:len(prefix)] == prefix && !listed[id] {
			delete(s.st.Rejected, id)
		}
	}
	stt.OK, stt.Error, stt.Since, stt.At, stt.Pending = true, "", nil, now, nPending
	return nPending > 0
}

func (s *Scanner) fetch(ctx context.Context, src smbsrc.Source, f smbsrc.File) (string, error) {
	// tempo proporzionato alla dimensione: almeno il timeout SMB, poi 256 KB/s
	d := s.o.Settings.SMBTimeout
	if byRate := time.Duration(f.Size/(256<<10)) * time.Second; byRate > d {
		d = byRate
	}
	fctx, cancel := context.WithTimeout(ctx, d)
	defer cancel()
	r, err := src.Open(fctx, f.Path)
	if err != nil {
		return "", err
	}
	defer r.Close()
	return s.blobs.Ingest(r, f.Size, s.o.Settings.MaxPDFBytes)
}

func ingestCode(err error) string {
	switch {
	case errors.Is(err, ErrNotPDF), errors.Is(err, ErrNoEOF):
		return ErrCodeCorrupt
	case errors.Is(err, ErrTooLarge):
		return ErrCodeTooLarge
	}
	return smbsrc.Code(err)
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// publish ricostruisce il manifest; se l'hash è cambiato lo scrive (tmp + rename),
// poi elimina i blob non più referenziati né dal nuovo né dal precedente manifest.
func (s *Scanner) publish(cfg *config.Config, now time.Time) {
	secs, docs := buildContent(cfg, s.st.Entries, s.blobs.Has, s.o.Settings)
	h := contentHash(secs, docs)
	if h == s.st.Hash && s.current != nil {
		return
	}
	m := &Manifest{
		SchemaVersion: ManifestSchema, Version: s.st.Version + 1, Hash: h, GeneratedAt: now,
		Settings: settingsFor(s.o.Settings), Sections: secs, Documents: docs,
	}
	old := s.current
	if old != nil {
		if b, err := json.Marshal(old); err == nil {
			_ = config.WriteFileAtomic(s.manifestPath()+".prev", b, 0o600)
		}
	}
	if err := s.writeManifest(m); err != nil {
		s.o.Logger.Error("scrittura manifest", "err", err)
		return
	}
	s.st.Version, s.st.Hash = m.Version, h
	s.mu.Lock()
	s.prev = old
	keep := m.Refs()
	for k := range s.prev.Refs() {
		keep[k] = true
	}
	s.mu.Unlock()
	if n := s.blobs.GC(keep); n > 0 {
		s.o.Logger.Info("blob eliminati", "n", n)
	}
	s.o.Logger.Info("manifest pubblicato", "versione", m.Version, "documenti", len(docs))
}

func (s *Scanner) writeManifest(m *Manifest) error {
	b, err := json.Marshal(m)
	if err != nil {
		return err
	}
	if err := config.WriteFileAtomic(s.manifestPath(), b, 0o600); err != nil {
		return err
	}
	s.mu.Lock()
	s.current = m
	s.body = b
	s.etag = `"` + m.Hash[:16] + "-" + hashBytes(b)[:16] + `"`
	s.mu.Unlock()
	return nil
}

func hashBytes(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

func (s *Scanner) saveState() error {
	b, err := json.Marshal(s.st)
	if err != nil {
		return err
	}
	return config.WriteFileAtomic(s.statePath(), b, 0o600)
}

// ManifestBody restituisce il manifest pubblicato (nil se non ancora pubblicato) e il suo ETag.
func (s *Scanner) ManifestBody() ([]byte, string) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.body, s.etag
}

// Current restituisce il manifest pubblicato (da non modificare).
func (s *Scanner) Current() *Manifest {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.current
}

// Problem è un PDF presente sulla share ma scartato (danneggiato, troppo grande).
// Sul totem resta l'eventuale versione precedente valida.
type Problem struct {
	Section string    `json:"section"`
	Path    string    `json:"path"`
	Code    string    `json:"code"`
	Size    int64     `json:"size"`
	MTime   time.Time `json:"mtime"`
	At      time.Time `json:"at"`
	HasPrev bool      `json:"hasPrevious"`
}

// Problems restituisce i PDF scartati, ordinati per sezione e percorso.
func (s *Scanner) Problems() []Problem {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append([]Problem(nil), s.problems...)
}

// Status restituisce lo stato delle sezioni e l'ora dell'ultima scansione.
func (s *Scanner) Status() (map[string]SectionStatus, time.Time) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make(map[string]SectionStatus, len(s.status))
	for k, v := range s.status {
		out[k] = v
	}
	return out, s.lastScan
}

// BlobPath restituisce il percorso di un blob referenziato dal manifest corrente o dal
// precedente. Uno SHA qualsiasi, anche se presente su disco, non basta.
func (s *Scanner) BlobPath(sha string) (string, bool) {
	if !ValidSHA(sha) {
		return "", false
	}
	s.mu.RLock()
	ok := s.current.Refs()[sha] || s.prev.Refs()[sha]
	s.mu.RUnlock()
	if !ok || !s.blobs.Has(sha) {
		return "", false
	}
	return s.blobs.Path(sha), true
}

// TestConnection prova una share e conta i PDF, senza toccare stato o archivio.
func (s *Scanner) TestConnection(ctx context.Context, t smbsrc.Target) (int, error) {
	if t.Port == 0 {
		t.Port = s.o.Port
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	src, err := s.o.Connect(ctx, t)
	if err != nil {
		return 0, err
	}
	defer src.Close()
	files, err := src.List(ctx)
	if err != nil {
		return 0, err
	}
	return len(files), nil
}
