// Package webpush manda notifiche Web Push ai telefoni (chiamate dell'interfono) senza
// librerie esterne: cifratura del contenuto "aes128gcm" (RFC 8291, RFC 8188) e
// identificazione del server con VAPID (RFC 8292).
//
// Il messaggio passa dal servizio di notifica del browser (per Chrome su Android quello
// di Google), che vede solo l'indirizzo del telefono e un contenuto cifrato.
package webpush

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/hkdf"
)

var b64 = base64.RawURLEncoding

// ErrGone: la sottoscrizione non esiste più (app disinstallata, permesso revocato).
var ErrGone = errors.New("sottoscrizione non più valida")

// Keys è la coppia di chiavi VAPID del server.
type Keys struct {
	priv *ecdsa.PrivateKey
}

// LoadOrCreateKeys legge la chiave VAPID da path (32 byte, base64url) o la crea.
func LoadOrCreateKeys(path string) (*Keys, error) {
	if b, err := os.ReadFile(path); err == nil {
		d, err := b64.DecodeString(strings.TrimSpace(string(b)))
		if err != nil || len(d) != 32 {
			return nil, fmt.Errorf("chiave VAPID non valida in %s", path)
		}
		return keysFromScalar(d)
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}
	d := make([]byte, 32)
	k.D.FillBytes(d)
	if err := os.WriteFile(path, []byte(b64.EncodeToString(d)), 0o600); err != nil {
		return nil, err
	}
	return &Keys{priv: k}, nil
}

func keysFromScalar(d []byte) (*Keys, error) {
	k := new(ecdsa.PrivateKey)
	k.Curve = elliptic.P256()
	k.D = new(big.Int).SetBytes(d)
	k.X, k.Y = k.Curve.ScalarBaseMult(d)
	return &Keys{priv: k}, nil
}

// PublicKey è la chiave pubblica (punto non compresso, base64url) per applicationServerKey.
func (k *Keys) PublicKey() string {
	return b64.EncodeToString(elliptic.Marshal(elliptic.P256(), k.priv.X, k.priv.Y)) //nolint:staticcheck // formato richiesto da Web Push
}

// vapidHeader firma il JWT ES256 per l'origine del servizio di notifica.
func (k *Keys) vapidHeader(endpoint string, now time.Time) (string, error) {
	u, err := url.Parse(endpoint)
	if err != nil {
		return "", err
	}
	head := b64.EncodeToString([]byte(`{"typ":"JWT","alg":"ES256"}`))
	claims, _ := json.Marshal(map[string]any{"aud": u.Scheme + "://" + u.Host, "exp": now.Add(12 * time.Hour).Unix()})
	unsigned := head + "." + b64.EncodeToString(claims)
	h := sha256.Sum256([]byte(unsigned))
	r, s, err := ecdsa.Sign(rand.Reader, k.priv, h[:])
	if err != nil {
		return "", err
	}
	sig := make([]byte, 64)
	r.FillBytes(sig[:32])
	s.FillBytes(sig[32:])
	return "vapid t=" + unsigned + "." + b64.EncodeToString(sig) + ", k=" + k.PublicKey(), nil
}

// encrypt cifra il contenuto per il browser (RFC 8291). asPriv e salt sono parametri per
// i vettori di prova; in uso normale sono casuali.
func encrypt(payload []byte, uaPublic, authSecret []byte, asPriv *ecdh.PrivateKey, salt []byte) ([]byte, error) {
	uaPub, err := ecdh.P256().NewPublicKey(uaPublic)
	if err != nil {
		return nil, fmt.Errorf("chiave del browser non valida: %w", err)
	}
	secret, err := asPriv.ECDH(uaPub)
	if err != nil {
		return nil, err
	}
	asPublic := asPriv.PublicKey().Bytes()
	keyInfo := append(append([]byte("WebPush: info\x00"), uaPublic...), asPublic...)
	ikm := make([]byte, 32)
	if _, err := io.ReadFull(hkdf.New(sha256.New, secret, authSecret, keyInfo), ikm); err != nil {
		return nil, err
	}
	prk := hkdf.Extract(sha256.New, ikm, salt)
	cek := make([]byte, 16)
	nonce := make([]byte, 12)
	if _, err := io.ReadFull(hkdf.Expand(sha256.New, prk, []byte("Content-Encoding: aes128gcm\x00")), cek); err != nil {
		return nil, err
	}
	if _, err := io.ReadFull(hkdf.Expand(sha256.New, prk, []byte("Content-Encoding: nonce\x00")), nonce); err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(cek)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	// un solo record: contenuto + delimitatore 0x02 (ultimo record), senza riempimento
	ct := gcm.Seal(nil, nonce, append(append([]byte{}, payload...), 2), nil)
	var out bytes.Buffer
	out.Write(salt)
	binary.Write(&out, binary.BigEndian, uint32(4096))
	out.WriteByte(byte(len(asPublic)))
	out.Write(asPublic)
	out.Write(ct)
	return out.Bytes(), nil
}

// Subscription è quella ricevuta dal browser (pushManager.subscribe).
type Subscription struct {
	Endpoint string
	P256dh   string
	Auth     string
}

// Sender manda le notifiche.
type Sender struct {
	Keys *Keys
	HTTP *http.Client // con il proxy aziendale se c'è (HTTPS_PROXY)
	Now  func() time.Time
}

// Send cifra e invia il messaggio. ttl: per quanto il servizio lo tiene se il telefono è
// spento (una chiamata dopo 30 s non serve più).
func (s *Sender) Send(ctx context.Context, sub Subscription, payload []byte, ttl time.Duration) error {
	if !strings.HasPrefix(sub.Endpoint, "https://") {
		return errors.New("endpoint non https")
	}
	ua, err := b64.DecodeString(strings.TrimRight(sub.P256dh, "="))
	if err != nil {
		return fmt.Errorf("p256dh: %w", err)
	}
	au, err := b64.DecodeString(strings.TrimRight(sub.Auth, "="))
	if err != nil || len(au) != 16 {
		return errors.New("auth non valido")
	}
	asPriv, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		return err
	}
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return err
	}
	body, err := encrypt(payload, ua, au, asPriv, salt)
	if err != nil {
		return err
	}
	now := time.Now
	if s.Now != nil {
		now = s.Now
	}
	vap, err := s.Keys.vapidHeader(sub.Endpoint, now())
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, sub.Endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/octet-stream")
	req.Header.Set("Content-Encoding", "aes128gcm")
	req.Header.Set("TTL", strconv.Itoa(int(ttl.Seconds())))
	req.Header.Set("Urgency", "high")
	req.Header.Set("Authorization", vap)
	hc := s.HTTP
	if hc == nil {
		hc = &http.Client{Timeout: 10 * time.Second}
	}
	res, err := hc.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	io.Copy(io.Discard, io.LimitReader(res.Body, 4096))
	switch {
	case res.StatusCode == http.StatusNotFound || res.StatusCode == http.StatusGone:
		return ErrGone
	case res.StatusCode >= 300:
		return fmt.Errorf("servizio di notifica: HTTP %d", res.StatusCode)
	}
	return nil
}
