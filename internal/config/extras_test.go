package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func cfgWith() *Config {
	return &Config{
		Sezioni: []Section{{ID: "s_a"}, {ID: "s_b"}},
		Reparti: []Department{{ID: "r_1", Nome: "Reparto 1", Sezioni: []string{"s_a"}}, {ID: "r_2", Nome: "Magazzino"}},
	}
}

func TestDepartmentsValidation(t *testing.T) {
	c := cfgWith()
	if err := c.ValidateDepartments(c.Reparti); err != nil {
		t.Fatal(err)
	}
	bad := [][]Department{
		{{ID: "x", Nome: ""}},
		{{ID: "x", Nome: strings.Repeat("a", 31)}},
		{{ID: "x", Nome: "A"}, {ID: "y", Nome: "a"}},
		{{ID: "x", Nome: "A", Sezioni: []string{"s_z"}}},
		{{ID: "x", Nome: "A", Sezioni: []string{"s_a", "s_a"}}},
	}
	for _, b := range bad {
		if c.ValidateDepartments(b) == nil {
			t.Errorf("accettato: %+v", b)
		}
	}
	if m := c.SectionsFor("r_1"); len(m) != 1 || !m["s_a"] {
		t.Fatal(m)
	}
	if c.SectionsFor("r_2") != nil || c.SectionsFor("") != nil || c.SectionsFor("manca") != nil {
		t.Fatal("reparto senza sezioni deve vedere tutto")
	}
}

func TestContactsAndNotices(t *testing.T) {
	c := cfgWith()
	if err := c.ValidateContacts([]Contact{{Etichetta: "Emergenze", Numero: "112"}, {Etichetta: "Primo soccorso", Numero: "+39 0422/123-45", Reparti: []string{"r_1"}}}); err != nil {
		t.Fatal(err)
	}
	for _, x := range []Contact{{Etichetta: "", Numero: "1"}, {Etichetta: "a", Numero: "abc"}, {Etichetta: "a", Numero: "1", Reparti: []string{"r_9"}}} {
		if c.ValidateContacts([]Contact{x}) == nil {
			t.Errorf("accettato %+v", x)
		}
	}
	c.Globali.Contatti = []Contact{{ID: "1", Etichetta: "A", Numero: "112"}, {ID: "2", Etichetta: "B", Numero: "1", Reparti: []string{"r_1"}}}
	if n := len(c.ContactsFor("r_2", false)); n != 1 {
		t.Fatal(n)
	}
	if n := len(c.ContactsFor("r_1", false)); n != 2 {
		t.Fatal(n)
	}
	if n := len(c.ContactsFor("", true)); n != 2 {
		t.Fatal(n)
	}

	if err := c.ValidateNotices([]Notice{{Testo: "Esercitazione", Livello: "attenzione", Dal: "2026-10-01", Al: "2026-10-31"}}); err != nil {
		t.Fatal(err)
	}
	for _, n := range []Notice{{Testo: "", Livello: "info"}, {Testo: "x", Livello: "boh"}, {Testo: "x", Livello: "info", Dal: "2026-13-01"},
		{Testo: "x", Livello: "info", Dal: "2026-10-10", Al: "2026-10-01"}} {
		if c.ValidateNotices([]Notice{n}) == nil {
			t.Errorf("accettato %+v", n)
		}
	}
	c.Globali.Avvisi = []Notice{{ID: "a", Testo: "x", Livello: "info", Al: "2026-10-20"}, {ID: "b", Testo: "y", Livello: "info", Dal: "2026-11-01"}}
	if ns := c.NoticesFor("", false, "2026-10-24"); len(ns) != 1 || ns[0].ID != "b" {
		t.Fatal(ns)
	}
}

func TestRemoveRefs(t *testing.T) {
	c := cfgWith()
	c.Globali.Dispositivi = []Device{{ID: "d", Reparto: "r_1"}}
	c.Globali.Contatti = []Contact{{ID: "1", Reparti: []string{"r_1", "r_2"}}}
	c.RemoveSectionRefs("s_a")
	if len(c.Reparti[0].Sezioni) != 0 {
		t.Fatal(c.Reparti[0])
	}
	c.RemoveDepartmentRefs("r_1")
	if c.Globali.Dispositivi[0].Reparto != "" || len(c.Globali.Contatti[0].Reparti) != 1 {
		t.Fatal(c.Globali)
	}
}

func TestScreensaver(t *testing.T) {
	c := &Config{}
	s := c.ScreensaverOrDefault()
	if s.DopoMinuti != 5 || s.OrarioNero != "20:00-06:00" || s.Attivo {
		t.Fatal(s)
	}
	if ValidateScreensaver(s) != nil || ValidateScreensaver(Screensaver{DopoMinuti: 0, OrarioNero: "20:00-06:00"}) == nil ||
		ValidateScreensaver(Screensaver{DopoMinuti: 5, OrarioNero: "25:00-06:00"}) == nil {
		t.Fatal("validazione salvaschermo")
	}
	if c.MonitorMinutes() != 30 {
		t.Fatal(c.MonitorMinutes())
	}
}

func TestMigrationV1ToV2(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "config.json")
	os.WriteFile(p, []byte(`{"schemaVersion":1,"sezioni":[],"globali":{"dispositivi":[]}}`), 0o600)
	st, err := Open(p, filepath.Join(dir, "backup"))
	if err != nil {
		t.Fatal(err)
	}
	c := st.Get()
	if c.SchemaVersion != 2 || c.Reparti == nil || c.Globali.Avvisi == nil {
		t.Fatal(c)
	}
	b, _ := os.ReadDir(filepath.Join(dir, "backup"))
	if len(b) != 1 || !strings.HasSuffix(b[0].Name(), "-v1.json") {
		t.Fatal(b)
	}
}
