package config

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ErrNoPassword indica una password assente o non decifrabile (per esempio chiave persa):
// basta reinserirla dal totem.
var ErrNoPassword = errors.New("password non disponibile")

// Cipher cifra le password SMB con AES-256-GCM. La chiave sta in /secrets/key.
type Cipher struct{ aead cipher.AEAD }

// LoadOrCreateKey legge la chiave di 32 byte; se manca la genera con permessi 0600.
func LoadOrCreateKey(path string) (*Cipher, bool, error) {
	created := false
	key, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		key = make([]byte, 32)
		if _, err := rand.Read(key); err != nil {
			return nil, false, err
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			return nil, false, err
		}
		if err := WriteFileAtomic(path, key, 0o600); err != nil {
			return nil, false, err
		}
		created = true
	} else if err != nil {
		return nil, false, err
	}
	if len(key) != 32 {
		return nil, false, fmt.Errorf("%s: la chiave deve essere di 32 byte, trovati %d", path, len(key))
	}
	c, err := NewCipher(key)
	return c, created, err
}

func NewCipher(key []byte) (*Cipher, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Cipher{aead: aead}, nil
}

// Encrypt restituisce "v1:" + base64(nonce || ciphertext). L'id della sezione è usato
// come dato associato: una password copiata su un'altra sezione non si decifra.
func (c *Cipher) Encrypt(sectionID, plain string) (string, error) {
	nonce := make([]byte, c.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	out := c.aead.Seal(nonce, nonce, []byte(plain), []byte(sectionID))
	return "v1:" + base64.StdEncoding.EncodeToString(out), nil
}

func (c *Cipher) Decrypt(sectionID, enc string) (string, error) {
	if enc == "" || !strings.HasPrefix(enc, "v1:") {
		return "", ErrNoPassword
	}
	raw, err := base64.StdEncoding.DecodeString(enc[3:])
	if err != nil || len(raw) < c.aead.NonceSize() {
		return "", ErrNoPassword
	}
	n := c.aead.NonceSize()
	plain, err := c.aead.Open(nil, raw[:n], raw[n:], []byte(sectionID))
	if err != nil {
		return "", ErrNoPassword
	}
	return string(plain), nil
}
