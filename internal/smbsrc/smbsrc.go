// Package smbsrc legge i PDF dalle share SMB in sola lettura, in user space con go-smb2
// (niente mount CIFS, niente privilegi). Espone anche una sorgente su cartella locale
// usata dai test.
package smbsrc

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path"
	"strings"
	"time"

	smb2 "github.com/hirochachacha/go-smb2"
)

// File descrive un PDF trovato nella share. Path è relativo alla cartella della sezione,
// con "/" come separatore.
type File struct {
	Path    string
	Size    int64
	ModTime time.Time
}

// Source è una share montata e pronta per la lettura.
type Source interface {
	// List restituisce tutti i PDF (ricorsivamente se richiesto).
	List(ctx context.Context) ([]File, error)
	// Open apre un file in sola lettura.
	Open(ctx context.Context, rel string) (io.ReadCloser, error)
	Close() error
}

// Target sono i parametri di connessione di una sezione, con password in chiaro
// (solo in memoria, mai su disco o nei log).
type Target struct {
	// Type è il tipo di origine: "smb" (o vuoto), "webdav", "onedrive", "upload".
	// La connessione per tipo la sceglie il pacchetto sources.
	Type      string
	Path      string // SMB: \\server\condivisione[\cartella...]; WebDAV/OneDrive: URL; upload: cartella locale
	User      string // SMB/WebDAV: utente; OneDrive: client ID
	Password  string // SMB/WebDAV: password; OneDrive: client secret
	Tenant    string // solo OneDrive
	Recursive bool
	// Port vale 445 se zero. Serve solo ai test di integrazione.
	Port int
}

// Codici di errore stabili, usati da API, stato e interfaccia.
const (
	ErrAuth         = "auth"
	ErrNotFound     = "not_found"
	ErrAccessDenied = "access_denied"
	ErrUnreachable  = "unreachable"
	ErrTimeout      = "timeout"
	ErrInvalidPath  = "invalid_path"
	ErrIO           = "io"
)

// Error porta un codice classificato e l'errore originale (che non contiene mai la password).
type Error struct {
	Code string
	Err  error
}

func (e *Error) Error() string { return e.Code + ": " + e.Err.Error() }
func (e *Error) Unwrap() error { return e.Err }

// Code estrae il codice da un errore; ErrIO se non classificato.
func Code(err error) string {
	var e *Error
	if errors.As(err, &e) {
		return e.Code
	}
	return Classify(err)
}

// NTSTATUS rilevanti (MS-ERREF).
const (
	statusAccessDenied       = 0xC0000022
	statusObjectNameNotFound = 0xC0000034
	statusObjectPathNotFound = 0xC000003A
	statusLogonFailure       = 0xC000006D
	statusAccountRestrict    = 0xC000006E
	statusPasswordExpired    = 0xC0000071
	statusAccountDisabled    = 0xC0000072
	statusBadNetworkName     = 0xC00000CC
	statusNotADirectory      = 0xC0000103
	statusAccountLockedOut   = 0xC0000234
	statusPasswordMustChange = 0xC0000224
)

// Classify traduce un errore di rete o SMB in uno dei codici stabili.
func Classify(err error) string {
	if err == nil {
		return ""
	}
	var re *smb2.ResponseError
	if errors.As(err, &re) {
		switch re.Code {
		case statusLogonFailure, statusAccountRestrict, statusPasswordExpired, statusAccountDisabled,
			statusAccountLockedOut, statusPasswordMustChange:
			return ErrAuth
		case statusBadNetworkName, statusObjectNameNotFound, statusObjectPathNotFound, statusNotADirectory:
			return ErrNotFound
		case statusAccessDenied:
			return ErrAccessDenied
		}
		return ErrIO
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, os.ErrDeadlineExceeded) {
		return ErrTimeout
	}
	var ce *smb2.ContextError
	if errors.As(err, &ce) && ce.Timeout() {
		return ErrTimeout
	}
	var te *smb2.TransportError
	if errors.As(err, &te) {
		// TransportError non implementa Unwrap: si guarda dentro a mano
		if inner, ok := te.Err.(net.Error); ok && inner.Timeout() || errors.Is(te.Err, os.ErrDeadlineExceeded) {
			return ErrTimeout
		}
		return ErrUnreachable
	}
	var ne net.Error
	if errors.As(err, &ne) && ne.Timeout() {
		return ErrTimeout
	}
	var oe *net.OpError
	var dnsErr *net.DNSError
	if errors.As(err, &oe) || errors.As(err, &dnsErr) {
		return ErrUnreachable
	}
	if errors.Is(err, os.ErrNotExist) {
		return ErrNotFound
	}
	if errors.Is(err, os.ErrPermission) {
		return ErrAccessDenied
	}
	return ErrIO
}

func wrap(err error) error {
	if err == nil {
		return nil
	}
	var e *Error
	if errors.As(err, &e) {
		return err
	}
	return &Error{Code: Classify(err), Err: err}
}

// ParsePath scompone \\server\condivisione\cartella in server, condivisione e cartella
// interna (con "\" come separatore, eventualmente vuota).
func ParsePath(p string) (host, share, dir string, err error) {
	if !strings.HasPrefix(p, `\\`) {
		return "", "", "", &Error{ErrInvalidPath, fmt.Errorf("il percorso deve iniziare con \\\\")}
	}
	parts := strings.Split(strings.Trim(p[2:], `\`), `\`)
	if len(parts) < 2 || parts[0] == "" || parts[1] == "" {
		return "", "", "", &Error{ErrInvalidPath, fmt.Errorf("servono almeno server e condivisione")}
	}
	for _, x := range parts {
		if x == "" || x == "." || x == ".." {
			return "", "", "", &Error{ErrInvalidPath, fmt.Errorf("percorso non valido")}
		}
	}
	return parts[0], parts[1], strings.Join(parts[2:], `\`), nil
}

// SplitUser separa "DOMINIO\utente". "utente@dominio" resta intero (NTLM lo accetta così).
func SplitUser(u string) (domain, user string) {
	if i := strings.IndexByte(u, '\\'); i >= 0 {
		return u[:i], u[i+1:]
	}
	return "", u
}

// ---------------------------------------------------------------------------
// Sorgente SMB

type smbSource struct {
	conn      net.Conn
	sess      *smb2.Session
	share     *smb2.Share
	dir       string
	recursive bool
}

// Connect apre connessione, sessione NTLM e tree connect. La firma e la cifratura
// SMB 3.x sono negoziate da go-smb2 in base a quanto richiede il server.
func Connect(ctx context.Context, t Target) (Source, error) {
	host, shareName, dir, err := ParsePath(t.Path)
	if err != nil {
		return nil, err
	}
	port := t.Port
	if port == 0 {
		port = 445
	}
	var d net.Dialer
	dctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	conn, err := d.DialContext(dctx, "tcp", net.JoinHostPort(host, fmt.Sprint(port)))
	if err != nil {
		if errors.Is(dctx.Err(), context.DeadlineExceeded) && ctx.Err() == nil {
			// il server non risponde affatto: per l'utente è "non raggiungibile"
			return nil, &Error{ErrUnreachable, err}
		}
		return nil, wrap(err)
	}
	if dl, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(dl)
	}
	domain, user := SplitUser(t.User)
	dialer := &smb2.Dialer{Initiator: &smb2.NTLMInitiator{User: user, Password: t.Password, Domain: domain}}
	sess, err := dialer.DialContext(ctx, conn)
	if err != nil {
		conn.Close()
		return nil, wrap(err)
	}
	sh, err := sess.WithContext(ctx).Mount(`\\` + host + `\` + shareName)
	if err != nil {
		_ = sess.Logoff()
		conn.Close()
		return nil, wrap(err)
	}
	s := &smbSource{conn: conn, sess: sess, share: sh, dir: dir, recursive: t.Recursive}
	// verifica subito che la cartella esista ed è una directory
	if dir != "" {
		st, err := sh.WithContext(ctx).Stat(dir)
		if err != nil {
			s.Close()
			return nil, wrap(err)
		}
		if !st.IsDir() {
			s.Close()
			return nil, &Error{ErrNotFound, fmt.Errorf("non è una cartella")}
		}
	}
	return s, nil
}

const maxDepth = 16

func (s *smbSource) List(ctx context.Context) ([]File, error) {
	fs := s.share.WithContext(ctx)
	if dl, ok := ctx.Deadline(); ok {
		_ = s.conn.SetDeadline(dl)
	}
	var out []File
	var walk func(rel string, depth int) error
	walk = func(rel string, depth int) error {
		full := s.dir
		if rel != "" {
			if full != "" {
				full += `\`
			}
			full += strings.ReplaceAll(rel, "/", `\`)
		}
		ents, err := fs.ReadDir(full)
		if err != nil {
			return wrap(err)
		}
		for _, e := range ents {
			name := e.Name()
			if skipName(name) || e.Mode()&os.ModeSymlink != 0 {
				continue
			}
			r := name
			if rel != "" {
				r = rel + "/" + name
			}
			if e.IsDir() {
				if s.recursive && depth < maxDepth {
					if err := walk(r, depth+1); err != nil {
						return err
					}
				}
				continue
			}
			if IsPDFName(name) {
				out = append(out, File{Path: r, Size: e.Size(), ModTime: e.ModTime().UTC().Truncate(time.Second)})
			}
		}
		return nil
	}
	if err := walk("", 0); err != nil {
		return nil, err
	}
	return out, nil
}

func (s *smbSource) Open(ctx context.Context, rel string) (io.ReadCloser, error) {
	if !SafeRel(rel) {
		return nil, &Error{ErrInvalidPath, fmt.Errorf("percorso relativo non valido")}
	}
	if dl, ok := ctx.Deadline(); ok {
		_ = s.conn.SetDeadline(dl)
	}
	full := strings.ReplaceAll(rel, "/", `\`)
	if s.dir != "" {
		full = s.dir + `\` + full
	}
	f, err := s.share.WithContext(ctx).Open(full)
	if err != nil {
		return nil, wrap(err)
	}
	return f, nil
}

func (s *smbSource) Close() error {
	if s.share != nil {
		_ = s.share.Umount()
	}
	if s.sess != nil {
		_ = s.sess.Logoff()
	}
	return s.conn.Close()
}

// IsPDFName accetta solo l'estensione .pdf (senza distinzione maiuscole/minuscole).
func IsPDFName(name string) bool {
	return strings.EqualFold(path.Ext(name), ".pdf") && len(name) > 4
}

// skipName esclude file nascosti, temporanei di Office (~$) e cartelle di sistema.
func skipName(name string) bool {
	if name == "" || name[0] == '.' || strings.HasPrefix(name, "~$") {
		return true
	}
	switch strings.ToLower(name) {
	case "$recycle.bin", "system volume information", "thumbs.db", "desktop.ini":
		return true
	}
	return false
}

// SafeRel controlla un percorso relativo con "/": niente assoluti, "..", backslash o NUL.
func SafeRel(rel string) bool {
	if rel == "" || strings.HasPrefix(rel, "/") || strings.ContainsAny(rel, "\\\x00:") {
		return false
	}
	for _, p := range strings.Split(rel, "/") {
		if p == "" || p == "." || p == ".." {
			return false
		}
	}
	return true
}

// HTTPError traduce lo stato HTTP di WebDAV o Microsoft Graph in un codice stabile.
func HTTPError(status int, what string) error {
	code := ErrIO
	switch {
	case status == 401:
		code = ErrAuth
	case status == 403:
		code = ErrAccessDenied
	case status == 404 || status == 410:
		code = ErrNotFound
	case status == 408 || status == 504:
		code = ErrTimeout
	case status == 502 || status == 503:
		code = ErrUnreachable
	}
	return &Error{Code: code, Err: fmt.Errorf("%s: HTTP %d", what, status)}
}

// Wrap classifica un errore qualsiasi (rete, contesto) mantenendo i codici già assegnati.
func Wrap(err error) error { return wrap(err) }
