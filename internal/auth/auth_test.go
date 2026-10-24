package auth

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestPIN(t *testing.T) {
	for _, ok := range []string{"1234", "123456", "0000"} {
		if !ValidPIN(ok) {
			t.Error(ok)
		}
	}
	for _, ko := range []string{"123", "1234567", "12a4", "", "١٢٣٤"} {
		if ValidPIN(ko) {
			t.Error(ko)
		}
	}
	h, err := HashPIN("4321")
	if err != nil || !strings.HasPrefix(h, "$argon2id$v=19$m=65536,t=3,p=2$") {
		t.Fatal(h, err)
	}
	if !VerifyPIN("4321", h) || VerifyPIN("1234", h) || VerifyPIN("4321", "") || VerifyPIN("4321", "$argon2id$x") {
		t.Fatal("verifica PIN errata")
	}
	h2, _ := HashPIN("4321")
	if h == h2 {
		t.Fatal("salt non casuale")
	}
}

func TestLimiter(t *testing.T) {
	now := time.Unix(0, 0)
	l := NewLimiter(5, 5*time.Minute)
	l.Now = func() time.Time { return now }
	for i := 4; i >= 1; i-- {
		if r := l.Fail(); r != i {
			t.Fatal(r, i)
		}
	}
	if l.Fail() != 0 {
		t.Fatal("blocco non attivato al quinto errore")
	}
	var le *ErrLocked
	if err := l.Check(); !errors.As(err, &le) || !le.Until.Equal(now.Add(5*time.Minute)) {
		t.Fatal(err)
	}
	now = now.Add(5*time.Minute + time.Second)
	if l.Check() != nil {
		t.Fatal("blocco non scaduto")
	}
	l.Fail()
	l.Success()
	if l.Fail() != 4 {
		t.Fatal("successo non azzera i tentativi")
	}
}

func TestSetupCode(t *testing.T) {
	c := NewSetupCode()
	if len(c) != 9 || c[4] != '-' || strings.ContainsAny(c, "01OIL") {
		t.Fatal(c)
	}
	if !CodesEqual(c, strings.ToLower(strings.ReplaceAll(c, "-", " "))) {
		t.Fatal("normalizzazione")
	}
	if CodesEqual("", "") || CodesEqual(c, "AAAA-AAAA") {
		t.Fatal("confronto errato")
	}
}

func TestSessions(t *testing.T) {
	now := time.Unix(0, 0)
	s := NewSessions(10*time.Minute, 0)
	s.Now = func() time.Time { return now }
	tok := s.New("d_1")
	now = now.Add(9 * time.Minute)
	if v, err := s.Use(tok); err != nil || v != "d_1" {
		t.Fatal(v, err)
	}
	now = now.Add(9 * time.Minute) // l'uso ha rinnovato la scadenza
	if _, err := s.Use(tok); err != nil {
		t.Fatal(err)
	}
	now = now.Add(11 * time.Minute)
	if _, err := s.Use(tok); err != ErrExpired {
		t.Fatal("sessione non scaduta dopo inattività")
	}
	tok = s.New("x")
	s.Clear()
	if _, err := s.Use(tok); err != ErrExpired {
		t.Fatal("Clear non ha chiuso la sessione")
	}
	a := NewSessions(time.Hour, 2*time.Minute)
	a.Now = s.Now
	tok = a.New("p")
	now = now.Add(3 * time.Minute)
	if _, err := a.Use(tok); err != ErrExpired {
		t.Fatal("durata massima ignorata")
	}
}
