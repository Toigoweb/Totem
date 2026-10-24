// Package davsrc legge i PDF da una cartella WebDAV (Nextcloud, ownCloud, Synology, QNAP,
// SharePoint on-premise…), in sola lettura: PROPFIND per l'elenco, GET per il contenuto.
package davsrc

import (
	"bytes"
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/toigoweb/totem/internal/smbsrc"
)

const maxDepth = 16

type source struct {
	c         *http.Client
	base      *url.URL // cartella radice, con "/" finale
	user, pwd string
	recursive bool
}

// Connect verifica che l'URL sia una cartella raggiungibile con le credenziali date.
func Connect(ctx context.Context, t smbsrc.Target) (smbsrc.Source, error) {
	u, err := url.Parse(t.Path)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return nil, &smbsrc.Error{Code: smbsrc.ErrInvalidPath, Err: fmt.Errorf("URL non valido")}
	}
	if !strings.HasSuffix(u.Path, "/") {
		u.Path += "/"
	}
	s := &source{c: &http.Client{Timeout: 0}, base: u, user: t.User, pwd: t.Password, recursive: t.Recursive}
	if _, err := s.propfind(ctx, u); err != nil {
		return nil, err
	}
	return s, nil
}

const propBody = `<?xml version="1.0" encoding="utf-8"?>
<d:propfind xmlns:d="DAV:"><d:prop><d:resourcetype/><d:getcontentlength/><d:getlastmodified/></d:prop></d:propfind>`

type multistatus struct {
	Responses []struct {
		Href     string `xml:"href"`
		Propstat []struct {
			Status string `xml:"status"`
			Prop   struct {
				Length       string `xml:"getcontentlength"`
				LastModified string `xml:"getlastmodified"`
				Type         struct {
					Collection *struct{} `xml:"collection"`
				} `xml:"resourcetype"`
			} `xml:"prop"`
		} `xml:"propstat"`
	} `xml:"response"`
}

type entry struct {
	href  *url.URL
	dir   bool
	size  int64
	mtime time.Time
}

func (s *source) do(req *http.Request) (*http.Response, error) {
	if s.user != "" || s.pwd != "" {
		req.SetBasicAuth(s.user, s.pwd)
	}
	res, err := s.c.Do(req)
	if err != nil {
		return nil, smbsrc.Wrap(err)
	}
	return res, nil
}

// propfind elenca il contenuto diretto di una cartella (Depth: 1).
func (s *source) propfind(ctx context.Context, dir *url.URL) ([]entry, error) {
	req, _ := http.NewRequestWithContext(ctx, "PROPFIND", dir.String(), bytes.NewReader([]byte(propBody)))
	req.Header.Set("Depth", "1")
	req.Header.Set("Content-Type", "application/xml; charset=utf-8")
	res, err := s.do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusMultiStatus {
		return nil, smbsrc.HTTPError(res.StatusCode, "PROPFIND")
	}
	var ms multistatus
	if err := xml.NewDecoder(io.LimitReader(res.Body, 32<<20)).Decode(&ms); err != nil {
		return nil, &smbsrc.Error{Code: smbsrc.ErrIO, Err: fmt.Errorf("risposta WebDAV non valida: %w", err)}
	}
	var out []entry
	for _, r := range ms.Responses {
		h, err := dir.Parse(r.Href) // href relativo, assoluto o URL completo
		if err != nil {
			continue
		}
		e := entry{href: h}
		for _, ps := range r.Propstat {
			if !strings.Contains(ps.Status, " 200") {
				continue
			}
			if ps.Prop.Type.Collection != nil {
				e.dir = true
			}
			if ps.Prop.Length != "" {
				e.size, _ = strconv.ParseInt(strings.TrimSpace(ps.Prop.Length), 10, 64)
			}
			if t, err := http.ParseTime(strings.TrimSpace(ps.Prop.LastModified)); err == nil {
				e.mtime = t.UTC().Truncate(time.Second)
			}
		}
		out = append(out, e)
	}
	return out, nil
}

// rel restituisce il percorso relativo alla radice, con "/" ("" per la radice stessa).
func (s *source) rel(u *url.URL) (string, bool) {
	p := strings.TrimSuffix(u.Path, "/")
	b := strings.TrimSuffix(s.base.Path, "/")
	if p == b {
		return "", true
	}
	if !strings.HasPrefix(p, b+"/") {
		return "", false
	}
	return strings.TrimPrefix(p, b+"/"), true
}

func (s *source) List(ctx context.Context) ([]smbsrc.File, error) {
	var out []smbsrc.File
	var walk func(dir *url.URL, depth int) error
	walk = func(dir *url.URL, depth int) error {
		ents, err := s.propfind(ctx, dir)
		if err != nil {
			return err
		}
		for _, e := range ents {
			r, ok := s.rel(e.href)
			if !ok || r == "" {
				continue
			}
			dr, _ := s.rel(dir)
			if r == dr {
				continue // la cartella stessa
			}
			name := path.Base(r)
			if strings.HasPrefix(name, ".") || strings.HasPrefix(name, "~$") {
				continue
			}
			if e.dir {
				if s.recursive && depth < maxDepth {
					sub := *e.href
					if !strings.HasSuffix(sub.Path, "/") {
						sub.Path += "/"
					}
					if err := walk(&sub, depth+1); err != nil {
						return err
					}
				}
				continue
			}
			if smbsrc.IsPDFName(name) && smbsrc.SafeRel(r) {
				out = append(out, smbsrc.File{Path: r, Size: e.size, ModTime: e.mtime})
			}
		}
		return nil
	}
	if err := walk(s.base, 0); err != nil {
		return nil, err
	}
	return out, nil
}

func (s *source) Open(ctx context.Context, rel string) (io.ReadCloser, error) {
	if !smbsrc.SafeRel(rel) {
		return nil, &smbsrc.Error{Code: smbsrc.ErrInvalidPath, Err: fmt.Errorf("percorso non valido")}
	}
	u := *s.base
	u.Path = s.base.Path + rel
	u.RawPath = ""
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	res, err := s.do(req)
	if err != nil {
		return nil, err
	}
	if res.StatusCode != http.StatusOK {
		res.Body.Close()
		return nil, smbsrc.HTTPError(res.StatusCode, "GET")
	}
	return res.Body, nil
}

func (s *source) Close() error { return nil }
