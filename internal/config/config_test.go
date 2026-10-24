package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestValidSharePath(t *testing.T) {
	ok := []string{`\\srv\sds`, `\\srv\sds\`, `\\fileserver.local\HSE\SDS\2026`, `\\10.0.0.5\Share con spazi\Cartella`}
	ko := []string{``, `srv\sds`, `\\srv`, `\\srv\`, `//srv/sds`, `\\srv\sds\..\x`, `\\srv\sds\a:b`, `\\srv\\sds`, "\\\\srv\\sd\x01s"}
	for _, p := range ok {
		if !ValidSharePath(p) {
			t.Errorf("atteso valido: %q", p)
		}
	}
	for _, p := range ko {
		if ValidSharePath(p) {
			t.Errorf("atteso non valido: %q", p)
		}
	}
}

func TestValidateSection(t *testing.T) {
	s := Section{Nome: "SDS", Icona: "flacone", Share: Share{Percorso: `\\srv\sds`, Utente: `AZIENDA\totem`}}
	if err := ValidateSection(s); err != nil {
		t.Fatal(err)
	}
	s.Nome = strings.Repeat("à", 31)
	if err := ValidateSection(s); err == nil {
		t.Fatal("nome troppo lungo accettato")
	}
	s.Nome = "SDS"
	s.Icona = "razzo"
	if err := ValidateSection(s); err == nil {
		t.Fatal("icona non ammessa accettata")
	}
}

func TestCipherRoundTrip(t *testing.T) {
	dir := t.TempDir()
	c, created, err := LoadOrCreateKey(filepath.Join(dir, "secrets", "key"))
	if err != nil || !created {
		t.Fatal(err, created)
	}
	st, _ := os.Stat(filepath.Join(dir, "secrets", "key"))
	if st.Mode().Perm() != 0o600 {
		t.Fatalf("permessi chiave %v", st.Mode().Perm())
	}
	enc, err := c.Encrypt("s_1", "Segreta!")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(enc, "Segreta") {
		t.Fatal("password in chiaro")
	}
	got, err := c.Decrypt("s_1", enc)
	if err != nil || got != "Segreta!" {
		t.Fatal(got, err)
	}
	if _, err := c.Decrypt("s_2", enc); err != ErrNoPassword {
		t.Fatal("decifrata con id sezione diverso")
	}
	// chiave persa: un'altra chiave non decifra, ma non va in panico
	c2, _, _ := LoadOrCreateKey(filepath.Join(dir, "other"))
	if _, err := c2.Decrypt("s_1", enc); err != ErrNoPassword {
		t.Fatal(err)
	}
	// la chiave esistente si riusa
	c3, created, _ := LoadOrCreateKey(filepath.Join(dir, "secrets", "key"))
	if created {
		t.Fatal("chiave rigenerata")
	}
	if got, _ := c3.Decrypt("s_1", enc); got != "Segreta!" {
		t.Fatal("chiave riletta diversa")
	}
}

func TestStoreCreateUpdateReload(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "config.json")
	st, err := Open(p, filepath.Join(dir, "backup"))
	if err != nil {
		t.Fatal(err)
	}
	called := 0
	st.OnChange(func() { called++ })
	err = st.Update(func(c *Config) error {
		c.Sezioni = append(c.Sezioni, Section{ID: "s_a", Nome: "A", Icona: "scudo", Share: Share{Percorso: `\\s\a`}})
		return nil
	})
	if err != nil || called != 1 {
		t.Fatal(err, called)
	}
	// un Update fallito non cambia nulla
	_ = st.Update(func(c *Config) error { c.Sezioni = nil; return os.ErrInvalid })
	if len(st.Get().Sezioni) != 1 {
		t.Fatal("update fallito applicato")
	}
	st2, err := Open(p, filepath.Join(dir, "backup"))
	if err != nil || len(st2.Get().Sezioni) != 1 {
		t.Fatal(err)
	}
	// nessun file temporaneo lasciato in giro
	ents, _ := os.ReadDir(dir)
	for _, e := range ents {
		if strings.Contains(e.Name(), ".tmp-") {
			t.Fatal("tmp rimasto", e.Name())
		}
	}
}

func TestMigrationMakesBackup(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "config.json")
	os.WriteFile(p, []byte(`{"sezioni":[{"id":"s_x","nome":"X","icona":"mappa","share":{"percorso":"\\\\a\\b"}}]}`), 0o600)
	st, err := Open(p, filepath.Join(dir, "backup"))
	if err != nil {
		t.Fatal(err)
	}
	if st.Get().SchemaVersion != SchemaVersion || len(st.Get().Sezioni) != 1 {
		t.Fatal("migrazione errata")
	}
	b, _ := os.ReadDir(filepath.Join(dir, "backup"))
	if len(b) != 1 || !strings.HasSuffix(b[0].Name(), "-v0.json") {
		t.Fatal("backup mancante", b)
	}
	// versione futura: rifiuta senza toccare il file
	os.WriteFile(p, []byte(`{"schemaVersion":99}`), 0o600)
	if _, err := Open(p, filepath.Join(dir, "backup")); err == nil {
		t.Fatal("schemaVersion futura accettata")
	}
}

func TestResolveSettings(t *testing.T) {
	s, w := ResolveSettings(map[string]any{"intervalloScansione": "30s", "giorniAvviso": float64(7), "boh": 1, "finestraNotturna": "25:00-02:00"})
	if s.ScanInterval != 30*time.Second || s.StaleDays != 7 {
		t.Fatal(s)
	}
	if s.NightWindow != "02:00-05:00" || len(w) != 2 {
		t.Fatal(w)
	}
}
