// Package auth contiene le primitive di sicurezza: hash del PIN con argon2id, blocco
// dopo troppi tentativi, codice di setup monouso, token di dispositivo e sessioni brevi
// (manutenzione, setup) tenute solo in memoria.
package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/argon2"
)

// ---------------------------------------------------------------------------
// PIN

const (
	argonTime    = 3
	argonMemory  = 64 * 1024
	argonThreads = 2
	argonKeyLen  = 32
)

// ValidPIN: da 4 a 6 cifre.
func ValidPIN(p string) bool {
	if len(p) < 4 || len(p) > 6 {
		return false
	}
	for _, c := range p {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

// HashPIN restituisce la stringa PHC "$argon2id$v=19$m=…,t=…,p=…$salt$hash".
func HashPIN(pin string) (string, error) {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key := argon2.IDKey([]byte(pin), salt, argonTime, argonMemory, argonThreads, argonKeyLen)
	b64 := base64.RawStdEncoding
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s", argon2.Version, argonMemory, argonTime, argonThreads,
		b64.EncodeToString(salt), b64.EncodeToString(key)), nil
}

// VerifyPIN confronta in tempo costante; i parametri si leggono dall'hash salvato.
func VerifyPIN(pin, encoded string) bool {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return false
	}
	var v int
	var m uint32
	var t uint32
	var p uint8
	if _, err := fmt.Sscanf(parts[2], "v=%d", &v); err != nil || v != argon2.Version {
		return false
	}
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &m, &t, &p); err != nil || m == 0 || m > 1<<20 || t == 0 || t > 10 || p == 0 {
		return false
	}
	b64 := base64.RawStdEncoding
	salt, err1 := b64.DecodeString(parts[4])
	want, err2 := b64.DecodeString(parts[5])
	if err1 != nil || err2 != nil || len(want) == 0 {
		return false
	}
	got := argon2.IDKey([]byte(pin), salt, t, m, p, uint32(len(want)))
	return subtle.ConstantTimeCompare(got, want) == 1
}

// ---------------------------------------------------------------------------
// Blocco tentativi

// ErrLocked indica il blocco in corso; Until dice fino a quando.
type ErrLocked struct{ Until time.Time }

func (e *ErrLocked) Error() string { return "troppi tentativi" }

// Limiter conta gli errori: dopo Max errori consecutivi blocca per Lockout.
// È globale (non per IP): in LAN gli indirizzi si cambiano facilmente.
type Limiter struct {
	mu       sync.Mutex
	Max      int
	Lockout  time.Duration
	Now      func() time.Time
	failures int
	until    time.Time
}

func NewLimiter(max int, lockout time.Duration) *Limiter {
	return &Limiter{Max: max, Lockout: lockout, Now: time.Now}
}

// Check restituisce *ErrLocked se il blocco è attivo.
func (l *Limiter) Check() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if now := l.Now(); now.Before(l.until) {
		return &ErrLocked{Until: l.until}
	}
	return nil
}

// Fail registra un errore e restituisce i tentativi rimasti (0 = appena bloccato).
func (l *Limiter) Fail() (remaining int) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.failures++
	if l.failures >= l.Max {
		l.failures = 0
		l.until = l.Now().Add(l.Lockout)
		return 0
	}
	return l.Max - l.failures
}

func (l *Limiter) Success() {
	l.mu.Lock()
	l.failures = 0
	l.mu.Unlock()
}

// ---------------------------------------------------------------------------
// Token

// NewToken genera 32 byte casuali in base64url.
func NewToken() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

// HashToken: SHA-256 esadecimale. Basta un hash veloce perché il token ha 256 bit casuali.
func HashToken(tok string) string {
	h := sha256.Sum256([]byte(tok))
	return hex.EncodeToString(h[:])
}

// NewID genera un id breve con prefisso (es. "s_k3v9q2").
func NewID(prefix string) string {
	const alpha = "abcdefghijkmnpqrstuvwxyz23456789"
	b := make([]byte, 6)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	for i := range b {
		b[i] = alpha[int(b[i])%len(alpha)]
	}
	return prefix + "_" + string(b)
}

// ---------------------------------------------------------------------------
// Codice di setup

// alfabeto senza caratteri ambigui (0/O, 1/I/L)
const codeAlphabet = "ABCDEFGHJKMNPQRSTUVWXYZ23456789"

// NewSetupCode genera un codice "XXXX-XXXX".
func NewSetupCode() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	out := make([]byte, 0, 9)
	for i, c := range b {
		if i == 4 {
			out = append(out, '-')
		}
		out = append(out, codeAlphabet[int(c)%len(codeAlphabet)])
	}
	return string(out)
}

// NormalizeCode toglie spazi e trattini e porta in maiuscolo, così "k7qm 4xpa" vale.
func NormalizeCode(c string) string {
	c = strings.ToUpper(c)
	return strings.Map(func(r rune) rune {
		if r == ' ' || r == '-' {
			return -1
		}
		return r
	}, c)
}

// CodesEqual confronta due codici normalizzati in tempo costante.
func CodesEqual(a, b string) bool {
	a, b = NormalizeCode(a), NormalizeCode(b)
	return a != "" && subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

// ---------------------------------------------------------------------------
// Sessioni brevi in memoria (manutenzione, setup, abbinamento)

// ErrExpired indica una sessione assente o scaduta.
var ErrExpired = errors.New("sessione scaduta")

type session struct {
	value    string
	lastUsed time.Time
	created  time.Time
}

// Sessions custodisce token brevi con scadenza per inattività (Idle) e, se MaxAge > 0,
// anche assoluta.
type Sessions struct {
	mu     sync.Mutex
	Idle   time.Duration
	MaxAge time.Duration
	Now    func() time.Time
	m      map[string]*session
}

func NewSessions(idle, maxAge time.Duration) *Sessions {
	return &Sessions{Idle: idle, MaxAge: maxAge, Now: time.Now, m: map[string]*session{}}
}

// New crea una sessione legata a value (es. id del dispositivo) e restituisce il token.
func (s *Sessions) New(value string) string {
	tok := NewToken()
	now := s.Now()
	s.mu.Lock()
	s.gc(now)
	s.m[HashToken(tok)] = &session{value: value, lastUsed: now, created: now}
	s.mu.Unlock()
	return tok
}

// Use verifica il token, ne rinnova la scadenza e restituisce il valore associato.
func (s *Sessions) Use(tok string) (string, error) {
	if tok == "" {
		return "", ErrExpired
	}
	now := s.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	k := HashToken(tok)
	ss, ok := s.m[k]
	if !ok || s.expired(ss, now) {
		delete(s.m, k)
		return "", ErrExpired
	}
	ss.lastUsed = now
	return ss.value, nil
}

// Delete chiude una sessione.
func (s *Sessions) Delete(tok string) {
	s.mu.Lock()
	delete(s.m, HashToken(tok))
	s.mu.Unlock()
}

// Clear chiude tutte le sessioni (es. dopo reset-pairing o cambio PIN).
func (s *Sessions) Clear() {
	s.mu.Lock()
	s.m = map[string]*session{}
	s.mu.Unlock()
}

func (s *Sessions) expired(ss *session, now time.Time) bool {
	if now.Sub(ss.lastUsed) > s.Idle {
		return true
	}
	return s.MaxAge > 0 && now.Sub(ss.created) > s.MaxAge
}

func (s *Sessions) gc(now time.Time) {
	for k, ss := range s.m {
		if s.expired(ss, now) {
			delete(s.m, k)
		}
	}
}
