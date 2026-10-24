// Package graphsrc legge i PDF da una cartella OneDrive o SharePoint tramite Microsoft
// Graph, in sola lettura, con un'app registrata in Entra ID (flusso client credentials:
// tenant, client ID, client secret; permesso applicativo Files.Read.All o Sites.Read.All).
//
// La cartella si indica con il suo link ("Copia collegamento" in OneDrive/SharePoint):
// l'API /shares lo risolve in drive e cartella, sia per OneDrive sia per SharePoint.
package graphsrc

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/toigoweb/totem/internal/smbsrc"
)

// Indirizzi dei servizi Microsoft; i test (e TOTEM_GRAPH_URL / TOTEM_LOGIN_URL) li sostituiscono.
var (
	GraphBase = "https://graph.microsoft.com/v1.0"
	LoginBase = "https://login.microsoftonline.com"
)

const maxDepth = 16

// ---------------------------------------------------------------------------
// Token (in cache fino a poco prima della scadenza)

type cachedToken struct {
	value   string
	expires time.Time
}

var (
	tokMu  sync.Mutex
	tokens = map[string]cachedToken{}
)

func tokenKey(tenant, client, secret string) string {
	h := sha256.Sum256([]byte(tenant + "\x00" + client + "\x00" + secret))
	return hex.EncodeToString(h[:])
}

func getToken(ctx context.Context, c *http.Client, tenant, client, secret string) (string, error) {
	key := tokenKey(tenant, client, secret)
	tokMu.Lock()
	if t, ok := tokens[key]; ok && time.Now().Before(t.expires) {
		tokMu.Unlock()
		return t.value, nil
	}
	tokMu.Unlock()
	form := url.Values{
		"grant_type":    {"client_credentials"},
		"client_id":     {client},
		"client_secret": {secret},
		"scope":         {"https://graph.microsoft.com/.default"},
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, LoginBase+"/"+url.PathEscape(tenant)+"/oauth2/v2.0/token", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	res, err := c.Do(req)
	if err != nil {
		return "", smbsrc.Wrap(err)
	}
	defer res.Body.Close()
	var out struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
		Error       string `json:"error"`
	}
	_ = json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(&out)
	if res.StatusCode != http.StatusOK || out.AccessToken == "" {
		// tenant inesistente, client ID o secret errati, secret scaduto: per chi configura
		// sono tutti "credenziali dell'app non valide"
		return "", &smbsrc.Error{Code: smbsrc.ErrAuth, Err: fmt.Errorf("token Microsoft: HTTP %d %s", res.StatusCode, out.Error)}
	}
	life := time.Duration(out.ExpiresIn) * time.Second
	if life <= 0 {
		life = time.Hour
	}
	tokMu.Lock()
	tokens[key] = cachedToken{value: out.AccessToken, expires: time.Now().Add(life - 5*time.Minute)}
	tokMu.Unlock()
	return out.AccessToken, nil
}

// ---------------------------------------------------------------------------
// Sorgente

type item struct {
	ID       string    `json:"id"`
	Name     string    `json:"name"`
	Size     int64     `json:"size"`
	Modified time.Time `json:"lastModifiedDateTime"`
	Folder   *struct{} `json:"folder"`
	File     *struct{} `json:"file"`
	Parent   struct {
		DriveID string `json:"driveId"`
	} `json:"parentReference"`
}

type source struct {
	c         *http.Client
	token     string
	driveID   string
	rootID    string
	recursive bool
	ids       map[string]string // percorso relativo → id dell'elemento (dall'ultimo elenco)
}

// shareID codifica un link di condivisione per l'API /shares ("u!" + base64url senza "=").
func shareID(link string) string {
	return "u!" + base64.RawURLEncoding.EncodeToString([]byte(link))
}

// Connect ottiene il token e risolve il link della cartella.
func Connect(ctx context.Context, t smbsrc.Target) (smbsrc.Source, error) {
	if !strings.HasPrefix(t.Path, "https://") {
		return nil, &smbsrc.Error{Code: smbsrc.ErrInvalidPath, Err: fmt.Errorf("link non valido")}
	}
	c := &http.Client{}
	tok, err := getToken(ctx, c, t.Tenant, t.User, t.Password)
	if err != nil {
		return nil, err
	}
	s := &source{c: c, token: tok, recursive: t.Recursive, ids: map[string]string{}}
	var it item
	if err := s.getJSON(ctx, GraphBase+"/shares/"+shareID(t.Path)+"/driveItem?$select=id,name,folder,parentReference", &it); err != nil {
		return nil, err
	}
	if it.Folder == nil {
		return nil, &smbsrc.Error{Code: smbsrc.ErrNotFound, Err: fmt.Errorf("il link non è una cartella")}
	}
	s.driveID, s.rootID = it.Parent.DriveID, it.ID
	return s, nil
}

// getJSON esegue una GET verso Graph (solo verso GraphBase: il token non esce da lì),
// riprovando fino a 3 volte se Graph chiede di rallentare (429/503 con Retry-After).
func (s *source) getJSON(ctx context.Context, u string, out any) error {
	if !strings.HasPrefix(u, GraphBase+"/") {
		return &smbsrc.Error{Code: smbsrc.ErrIO, Err: fmt.Errorf("indirizzo inatteso nella risposta di Graph")}
	}
	for attempt := 0; ; attempt++ {
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
		req.Header.Set("Authorization", "Bearer "+s.token)
		res, err := s.c.Do(req)
		if err != nil {
			return smbsrc.Wrap(err)
		}
		if (res.StatusCode == http.StatusTooManyRequests || res.StatusCode == http.StatusServiceUnavailable) && attempt < 3 {
			wait, _ := strconv.Atoi(res.Header.Get("Retry-After"))
			res.Body.Close()
			if wait <= 0 || wait > 60 {
				wait = 2 << attempt
			}
			select {
			case <-ctx.Done():
				return smbsrc.Wrap(ctx.Err())
			case <-time.After(time.Duration(wait) * time.Second):
			}
			continue
		}
		defer res.Body.Close()
		if res.StatusCode != http.StatusOK {
			return smbsrc.HTTPError(res.StatusCode, "Graph")
		}
		if err := json.NewDecoder(io.LimitReader(res.Body, 64<<20)).Decode(out); err != nil {
			return &smbsrc.Error{Code: smbsrc.ErrIO, Err: fmt.Errorf("risposta Graph non valida: %w", err)}
		}
		return nil
	}
}

func (s *source) children(ctx context.Context, id string) ([]item, error) {
	var all []item
	next := fmt.Sprintf("%s/drives/%s/items/%s/children?$select=id,name,size,lastModifiedDateTime,folder,file&$top=999",
		GraphBase, url.PathEscape(s.driveID), url.PathEscape(id))
	for next != "" {
		var page struct {
			Value []item `json:"value"`
			Next  string `json:"@odata.nextLink"`
		}
		if err := s.getJSON(ctx, next, &page); err != nil {
			return nil, err
		}
		all = append(all, page.Value...)
		next = page.Next
	}
	return all, nil
}

func (s *source) List(ctx context.Context) ([]smbsrc.File, error) {
	var out []smbsrc.File
	ids := map[string]string{}
	var walk func(id, rel string, depth int) error
	walk = func(id, rel string, depth int) error {
		items, err := s.children(ctx, id)
		if err != nil {
			return err
		}
		for _, it := range items {
			if it.Name == "" || strings.HasPrefix(it.Name, ".") || strings.HasPrefix(it.Name, "~$") || strings.Contains(it.Name, "/") {
				continue
			}
			r := it.Name
			if rel != "" {
				r = rel + "/" + it.Name
			}
			if it.Folder != nil {
				if s.recursive && depth < maxDepth {
					if err := walk(it.ID, r, depth+1); err != nil {
						return err
					}
				}
				continue
			}
			if it.File != nil && smbsrc.IsPDFName(it.Name) && smbsrc.SafeRel(r) {
				out = append(out, smbsrc.File{Path: r, Size: it.Size, ModTime: it.Modified.UTC().Truncate(time.Second)})
				ids[r] = it.ID
			}
		}
		return nil
	}
	if err := walk(s.rootID, "", 0); err != nil {
		return nil, err
	}
	s.ids = ids
	return out, nil
}

// Open scarica il contenuto. Graph risponde con un rinvio a un URL di download già
// autorizzato: il client Go non inoltra l'header Authorization verso un altro dominio.
func (s *source) Open(ctx context.Context, rel string) (io.ReadCloser, error) {
	id, ok := s.ids[rel]
	if !ok || !smbsrc.SafeRel(rel) || path.Clean(rel) != rel {
		return nil, &smbsrc.Error{Code: smbsrc.ErrNotFound, Err: fmt.Errorf("file non presente nell'ultimo elenco")}
	}
	u := fmt.Sprintf("%s/drives/%s/items/%s/content", GraphBase, url.PathEscape(s.driveID), url.PathEscape(id))
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	req.Header.Set("Authorization", "Bearer "+s.token)
	res, err := s.c.Do(req)
	if err != nil {
		return nil, smbsrc.Wrap(err)
	}
	if res.StatusCode != http.StatusOK {
		res.Body.Close()
		return nil, smbsrc.HTTPError(res.StatusCode, "download")
	}
	return res.Body, nil
}

func (s *source) Close() error { return nil }
