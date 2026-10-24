package api

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"
	"unicode"

	"github.com/toigoweb/totem/internal/config"
	"github.com/toigoweb/totem/internal/smbsrc"
	"github.com/toigoweb/totem/internal/sources"
)

// Caricamento diretto: per le sezioni di tipo "upload" i PDF si caricano dall'area
// manutenzione (totem o postazione) e il server li tiene in /data/uploads/<sezione>.
// Da lì passano per lo scanner come qualsiasi altra origine (stabilità, verifica,
// archivio). Ricaricare un file con lo stesso nome ne pubblica una nuova versione.

type uploadedFile struct {
	Path  string    `json:"path"`
	Size  int64     `json:"size"`
	MTime time.Time `json:"mtime"`
}

func (s *Server) uploadSection(w http.ResponseWriter, r *http.Request) (string, bool) {
	id := r.PathValue("id")
	sec, ok := s.o.Config.Get().Section(id)
	if !ok {
		jsonErr(w, http.StatusNotFound, "not_found")
		return "", false
	}
	if sec.Share.Kind() != config.SourceUpload {
		jsonErr(w, http.StatusConflict, "not_upload")
		return "", false
	}
	dir := sources.UploadDir(s.o.DataDir, id)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		jsonErr(w, http.StatusInternalServerError, "io")
		return "", false
	}
	return dir, true
}

// cleanUploadName rende sicuro il nome del file: niente percorsi, caratteri di controllo,
// file nascosti; estensione .pdf obbligatoria.
func cleanUploadName(n string) (string, bool) {
	n = strings.TrimSpace(path.Base(strings.ReplaceAll(n, `\`, "/")))
	n = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || strings.ContainsRune(`<>:"/\|?*`, r) {
			return -1
		}
		return r
	}, n)
	if n == "" || n == "." || n == ".." || strings.HasPrefix(n, ".") || strings.HasPrefix(n, "~$") || len(n) > 150 || !smbsrc.IsPDFName(n) {
		return "", false
	}
	return n, true
}

// cleanFolder accetta una sola sottocartella facoltativa (diventa un chip nell'elenco).
func cleanFolder(f string) (string, bool) {
	f = strings.TrimSpace(f)
	if f == "" {
		return "", true
	}
	if strings.ContainsAny(f, `/\<>:"|?*`) || strings.HasPrefix(f, ".") || len([]rune(f)) > 40 {
		return "", false
	}
	for _, r := range f {
		if unicode.IsControl(r) {
			return "", false
		}
	}
	return f, true
}

func (s *Server) listUploads(w http.ResponseWriter, r *http.Request) {
	dir, ok := s.uploadSection(w, r)
	if !ok {
		return
	}
	files, err := (&smbsrc.DirSource{Root: dir, Recursive: true}).List(context.Background())
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "io")
		return
	}
	out := []uploadedFile{}
	for _, f := range files {
		out = append(out, uploadedFile{Path: f.Path, Size: f.Size, MTime: f.ModTime})
	}
	writeJSON(w, http.StatusOK, map[string]any{"files": out, "maxBytes": s.o.Settings.MaxPDFBytes})
}

// putUpload riceve il PDF come corpo della richiesta (Content-Type application/pdf),
// con nome ed eventuale cartella nei parametri: ?nome=…&cartella=….
func (s *Server) putUpload(w http.ResponseWriter, r *http.Request) {
	dir, ok := s.uploadSection(w, r)
	if !ok {
		return
	}
	if ct := r.Header.Get("Content-Type"); ct != "application/pdf" && ct != "application/octet-stream" {
		jsonErr(w, http.StatusUnsupportedMediaType, "content_type")
		return
	}
	name, ok := cleanUploadName(r.URL.Query().Get("nome"))
	if !ok {
		jsonErr(w, http.StatusBadRequest, "invalid", "field", "nome")
		return
	}
	folder, ok := cleanFolder(r.URL.Query().Get("cartella"))
	if !ok {
		jsonErr(w, http.StatusBadRequest, "invalid", "field", "cartella")
		return
	}
	dest := filepath.Join(dir, folder, name)
	if err := os.MkdirAll(filepath.Dir(dest), 0o700); err != nil {
		jsonErr(w, http.StatusInternalServerError, "io")
		return
	}
	max := s.o.Settings.MaxPDFBytes
	tmp, err := os.CreateTemp(filepath.Dir(dest), ".caricamento-*")
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "io")
		return
	}
	defer os.Remove(tmp.Name())
	tw := &probe{}
	n, err := io.Copy(io.MultiWriter(tmp, tw), io.LimitReader(r.Body, max+1))
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		jsonErr(w, http.StatusBadRequest, "io")
		return
	}
	switch {
	case n > max:
		jsonErr(w, http.StatusRequestEntityTooLarge, "too_large", "maxBytes", max)
		return
	case !bytes.Contains(tw.head, []byte("%PDF-")) || !bytes.Contains(tw.tail(), []byte("%%EOF")):
		jsonErr(w, http.StatusBadRequest, "corrupt")
		return
	}
	if err := os.Chmod(tmp.Name(), 0o600); err != nil {
		jsonErr(w, http.StatusInternalServerError, "io")
		return
	}
	if err := os.Rename(tmp.Name(), dest); err != nil {
		jsonErr(w, http.StatusInternalServerError, "io")
		return
	}
	rel := name
	if folder != "" {
		rel = folder + "/" + name
	}
	s.o.Logger.Info("PDF caricato", "sezione", r.PathValue("id"), "file", rel, "byte", n)
	s.o.Scanner.Kick()
	writeJSON(w, http.StatusCreated, map[string]any{"ok": true, "path": rel, "size": n})
}

func (s *Server) deleteUpload(w http.ResponseWriter, r *http.Request) {
	dir, ok := s.uploadSection(w, r)
	if !ok {
		return
	}
	rel := r.URL.Query().Get("path")
	if !smbsrc.SafeRel(rel) || strings.Count(rel, "/") > 1 {
		jsonErr(w, http.StatusBadRequest, "invalid", "field", "path")
		return
	}
	full := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.Remove(full); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			jsonErr(w, http.StatusNotFound, "not_found")
			return
		}
		jsonErr(w, http.StatusInternalServerError, "io")
		return
	}
	if d := filepath.Dir(full); d != dir {
		_ = os.Remove(d) // cartella rimasta vuota
	}
	s.o.Logger.Info("PDF caricato eliminato", "sezione", r.PathValue("id"), "file", rel)
	s.o.Scanner.Kick()
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// probe conserva i primi e gli ultimi 1024 byte, per il controllo %PDF … %%EOF.
type probe struct{ head, buf []byte }

func (p *probe) Write(b []byte) (int, error) {
	if len(p.head) < 1024 {
		k := min(1024-len(p.head), len(b))
		p.head = append(p.head, b[:k]...)
	}
	p.buf = append(p.buf, b...)
	if len(p.buf) > 4096 {
		p.buf = append(p.buf[:0], p.buf[len(p.buf)-1024:]...)
	}
	return len(b), nil
}

func (p *probe) tail() []byte {
	if len(p.buf) > 1024 {
		return p.buf[len(p.buf)-1024:]
	}
	return p.buf
}
