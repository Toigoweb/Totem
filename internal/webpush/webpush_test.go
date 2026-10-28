package webpush

import (
	"context"
	"crypto/ecdh"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func mustB64(t *testing.T, s string) []byte {
	b, err := b64.DecodeString(s)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// Vettore di prova di RFC 8291, appendice A.
func TestRFC8291Vector(t *testing.T) {
	asPriv, err := ecdh.P256().NewPrivateKey(mustB64(t, "yfWPiYE-n46HLnH0KqZOF1fJJU3MYrct3AELtAQ-oRw"))
	if err != nil {
		t.Fatal(err)
	}
	if b64.EncodeToString(asPriv.PublicKey().Bytes()) != "BP4z9KsN6nGRTbVYI_c7VJSPQTBtkgcy27mlmlMoZIIgDll6e3vCYLocInmYWAmS6TlzAC8wEqKK6PBru3jl7A8" {
		t.Fatal("chiave pubblica del server diversa dal vettore")
	}
	ua := mustB64(t, "BCVxsr7N_eNgVRqvHtD0zTZsEc6-VV-JvLexhqUzORcxaOzi6-AYWXvTBHm4bjyPjs7Vd8pZGH6SRpkNtoIAiw4")
	auth := mustB64(t, "BTBZMqHH6r4Tts7J_aSIgg")
	salt := mustB64(t, "DGv6ra1nlYgDCS1FRnbzlw")
	body, err := encrypt([]byte("When I grow up, I want to be a watermelon"), ua, auth, asPriv, salt)
	if err != nil {
		t.Fatal(err)
	}
	want := "DGv6ra1nlYgDCS1FRnbzlwAAEABBBP4z9KsN6nGRTbVYI_c7VJSPQTBtkgcy27mlmlMoZIIgDll6e3vCYLocInmYWAmS6TlzAC8wEqKK6PBru3jl7A_yl95bQpu6cVPTpK4Mqgkf1CXztLVBSt2Ks3oZwbuwXPXLWyouBWLVWGNWQexSgSxsj_Qulcy4a-fN"
	if got := b64.EncodeToString(body); got != want {
		t.Fatalf("cifrato diverso dal vettore RFC 8291\n got %s\nwant %s", got, want)
	}
}

func TestSendAndVapid(t *testing.T) {
	keys, err := LoadOrCreateKeys(filepath.Join(t.TempDir(), "vapid.key"))
	if err != nil {
		t.Fatal(err)
	}
	ua, _ := ecdh.P256().GenerateKey(rand.Reader)
	var gotAuth, gotEnc, gotTTL string
	var body []byte
	status := 201
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth, gotEnc, gotTTL = r.Header.Get("Authorization"), r.Header.Get("Content-Encoding"), r.Header.Get("TTL")
		body, _ = io.ReadAll(r.Body)
		w.WriteHeader(status)
	}))
	defer srv.Close()
	s := &Sender{Keys: keys, HTTP: srv.Client()}
	sub := Subscription{Endpoint: srv.URL + "/fcm/send/abc", P256dh: b64.EncodeToString(ua.PublicKey().Bytes()), Auth: b64.EncodeToString(make([]byte, 16))}
	if err := s.Send(context.Background(), sub, []byte(`{"tipo":"chiamata"}`), 30*time.Second); err != nil {
		t.Fatal(err)
	}
	if gotEnc != "aes128gcm" || gotTTL != "30" || len(body) < 16+4+1+65+16 {
		t.Fatal(gotEnc, gotTTL, len(body))
	}
	// firma VAPID verificabile con la chiave pubblica dichiarata
	if !strings.HasPrefix(gotAuth, "vapid t=") || !strings.Contains(gotAuth, ", k="+keys.PublicKey()) {
		t.Fatal(gotAuth)
	}
	jwt := strings.TrimPrefix(strings.Split(gotAuth, ",")[0], "vapid t=")
	parts := strings.Split(jwt, ".")
	sig := mustB64(t, parts[2])
	h := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	x, y := elliptic.Unmarshal(elliptic.P256(), mustB64(t, keys.PublicKey()))
	pub := &ecdsa.PublicKey{Curve: elliptic.P256(), X: x, Y: y}
	if !ecdsa.Verify(pub, h[:], new(big.Int).SetBytes(sig[:32]), new(big.Int).SetBytes(sig[32:])) {
		t.Fatal("firma VAPID non valida")
	}
	if claims := string(mustB64(t, parts[1])); !strings.Contains(claims, `"aud":"`+srv.URL+`"`) {
		t.Fatal(claims)
	}
	// sottoscrizione scaduta
	status = 410
	if err := s.Send(context.Background(), sub, []byte("x"), time.Second); err != ErrGone {
		t.Fatal(err)
	}
}

func TestKeysPersist(t *testing.T) {
	p := filepath.Join(t.TempDir(), "vapid.key")
	a, _ := LoadOrCreateKeys(p)
	b, err := LoadOrCreateKeys(p)
	if err != nil || a.PublicKey() != b.PublicKey() || len(mustB64(t, a.PublicKey())) != 65 {
		t.Fatal(err)
	}
}
