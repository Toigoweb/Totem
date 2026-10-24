package scanner

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
)

// Errori di validazione di un PDF.
var (
	ErrNotPDF   = errors.New("intestazione %PDF assente")
	ErrNoEOF    = errors.New("marcatore %%EOF assente: file troncato o in scrittura")
	ErrTooLarge = errors.New("file oltre la dimensione massima")
	ErrChanged  = errors.New("il file è cambiato durante la lettura")
)

var shaRe = regexp.MustCompile(`^[0-9a-f]{64}$`)

// ValidSHA protegge dal path traversal: un blob si indica solo con 64 cifre esadecimali.
func ValidSHA(s string) bool { return shaRe.MatchString(s) }

// BlobStore è l'archivio /data/blobs/<sha256>. Un blob, una volta scritto, non cambia più.
type BlobStore struct{ dir string }

func NewBlobStore(dir string) (*BlobStore, error) {
	if err := os.MkdirAll(filepath.Join(dir, "tmp"), 0o700); err != nil {
		return nil, err
	}
	b := &BlobStore{dir: dir}
	b.CleanTmp()
	return b, nil
}

// CleanTmp elimina i download rimasti a metà (per esempio dopo un riavvio).
func (b *BlobStore) CleanTmp() {
	ents, _ := os.ReadDir(filepath.Join(b.dir, "tmp"))
	for _, e := range ents {
		_ = os.Remove(filepath.Join(b.dir, "tmp", e.Name()))
	}
}

func (b *BlobStore) Path(sha string) string { return filepath.Join(b.dir, sha) }

func (b *BlobStore) Has(sha string) bool {
	if !ValidSHA(sha) {
		return false
	}
	st, err := os.Stat(b.Path(sha))
	return err == nil && st.Mode().IsRegular()
}

const probe = 1024

// Ingest copia r in un file temporaneo calcolando SHA-256 e controllando intestazione
// e marcatore finale; se il PDF è valido lo sposta in blobs/<sha>. Il file sorgente si
// legge una sola volta. expectSize è la dimensione vista nell'elenco: se differisce
// il file è ancora in scrittura (ErrChanged).
func (b *BlobStore) Ingest(r io.Reader, expectSize, maxBytes int64) (sha string, err error) {
	if expectSize > maxBytes {
		return "", ErrTooLarge
	}
	f, err := os.CreateTemp(filepath.Join(b.dir, "tmp"), "in-*")
	if err != nil {
		return "", err
	}
	tmp := f.Name()
	defer func() {
		if err != nil {
			_ = os.Remove(tmp)
		}
	}()
	h := sha256.New()
	tw := &tailWriter{}
	n, err := io.Copy(io.MultiWriter(f, h, tw), io.LimitReader(r, maxBytes+1))
	if err == nil {
		err = f.Sync()
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return "", err
	}
	switch {
	case n > maxBytes:
		return "", ErrTooLarge
	case n != expectSize:
		return "", ErrChanged
	case !bytes.Contains(tw.head, []byte("%PDF-")):
		return "", ErrNotPDF
	case !bytes.Contains(tw.tail(), []byte("%%EOF")):
		return "", ErrNoEOF
	}
	sha = hex.EncodeToString(h.Sum(nil))
	dst := b.Path(sha)
	if b.Has(sha) {
		_ = os.Remove(tmp)
		return sha, nil
	}
	if err := os.Chmod(tmp, 0o600); err != nil {
		return "", err
	}
	if err := os.Rename(tmp, dst); err != nil {
		return "", fmt.Errorf("rename blob: %w", err)
	}
	return sha, nil
}

// GC elimina i blob non contenuti in keep. Si chiama solo dopo aver pubblicato il manifest.
func (b *BlobStore) GC(keep map[string]bool) (removed int) {
	ents, err := os.ReadDir(b.dir)
	if err != nil {
		return 0
	}
	for _, e := range ents {
		if e.IsDir() || !ValidSHA(e.Name()) || keep[e.Name()] {
			continue
		}
		if os.Remove(filepath.Join(b.dir, e.Name())) == nil {
			removed++
		}
	}
	return removed
}

// tailWriter conserva i primi e gli ultimi 1024 byte scritti.
type tailWriter struct {
	head []byte
	buf  []byte
}

func (t *tailWriter) Write(p []byte) (int, error) {
	if len(t.head) < probe {
		k := probe - len(t.head)
		if k > len(p) {
			k = len(p)
		}
		t.head = append(t.head, p[:k]...)
	}
	t.buf = append(t.buf, p...)
	if len(t.buf) > 4*probe {
		t.buf = append(t.buf[:0], t.buf[len(t.buf)-probe:]...)
	}
	return len(p), nil
}

func (t *tailWriter) tail() []byte {
	if len(t.buf) > probe {
		return t.buf[len(t.buf)-probe:]
	}
	return t.buf
}
