package config

import (
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

// Limiti delle liste modificabili dall'interfaccia.
const (
	MaxDepartments = 20
	MaxContacts    = 8
	MaxNotices     = 10
)

// Valori di default di salvaschermo e monitoraggio quando il file non li indica.
const (
	DefaultScreensaverMinutes = 5
	DefaultNightHours         = "20:00-06:00"
	DefaultMonitorMinutes     = 30
)

var (
	phoneRe = regexp.MustCompile(`^[0-9+][0-9 ./-]{0,19}$`)
	dateRe  = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)
)

func runes(s string) int { return utf8.RuneCountInString(s) }

func (c *Config) sectionIDs() map[string]bool {
	m := map[string]bool{}
	for _, s := range c.Sezioni {
		m[s.ID] = true
	}
	return m
}

func (c *Config) departmentIDs() map[string]bool {
	m := map[string]bool{}
	for _, d := range c.Reparti {
		m[d.ID] = true
	}
	return m
}

// Department restituisce il reparto con quell'id.
func (c *Config) Department(id string) (Department, bool) {
	for _, d := range c.Reparti {
		if d.ID == id {
			return d, true
		}
	}
	return Department{}, false
}

// SectionsFor restituisce le sezioni visibili da un reparto; nil = tutte.
// Un reparto inesistente o vuoto vede tutte le sezioni.
func (c *Config) SectionsFor(reparto string) map[string]bool {
	d, ok := c.Department(reparto)
	if !ok || len(d.Sezioni) == 0 {
		return nil
	}
	m := map[string]bool{}
	for _, id := range d.Sezioni {
		m[id] = true
	}
	return m
}

func appliesTo(reparti []string, reparto string) bool {
	if len(reparti) == 0 {
		return true
	}
	for _, r := range reparti {
		if r == reparto {
			return true
		}
	}
	return false
}

// ContactsFor restituisce i numeri utili validi per un reparto ("" = solo quelli comuni,
// salvo che il dispositivo veda tutto: allora si mostrano tutti).
func (c *Config) ContactsFor(reparto string, all bool) []Contact {
	out := []Contact{}
	for _, x := range c.Globali.Contatti {
		if all || appliesTo(x.Reparti, reparto) {
			out = append(out, x)
		}
	}
	return out
}

// NoticesFor restituisce gli avvisi di un reparto non ancora scaduti alla data indicata.
// Gli avvisi futuri si includono: il totem li mostra dal giorno giusto anche offline.
func (c *Config) NoticesFor(reparto string, all bool, today string) []Notice {
	out := []Notice{}
	for _, n := range c.Globali.Avvisi {
		if n.Al != "" && n.Al < today {
			continue
		}
		if all || appliesTo(n.Reparti, reparto) {
			out = append(out, n)
		}
	}
	return out
}

// ScreensaverOrDefault completa i campi mancanti.
func (c *Config) ScreensaverOrDefault() Screensaver {
	s := c.Globali.Salvaschermo
	if s.DopoMinuti == 0 {
		s.DopoMinuti = DefaultScreensaverMinutes
	}
	if s.OrarioNero == "" {
		s.OrarioNero = DefaultNightHours
	}
	return s
}

// MonitorMinutes è la soglia oltre la quale un totem silenzioso è un errore.
func (c *Config) MonitorMinutes() int {
	if c.Globali.Monitor.SogliaMinuti <= 0 {
		return DefaultMonitorMinutes
	}
	return c.Globali.Monitor.SogliaMinuti
}

// ---------------------------------------------------------------------------
// Validazione

func checkRefs(field string, ids []string, valid map[string]bool) error {
	seen := map[string]bool{}
	for _, id := range ids {
		if !valid[id] || seen[id] {
			return &ValidationError{field, "riferimento non valido"}
		}
		seen[id] = true
	}
	return nil
}

// ValidateDepartments controlla l'elenco completo dei reparti.
func (c *Config) ValidateDepartments(ds []Department) error {
	if len(ds) > MaxDepartments {
		return &ValidationError{"reparti", "troppi reparti"}
	}
	names := map[string]bool{}
	secs := c.sectionIDs()
	for _, d := range ds {
		n := strings.TrimSpace(d.Nome)
		if n == "" || runes(n) > 30 {
			return &ValidationError{"nome", "da 1 a 30 caratteri"}
		}
		if names[strings.ToLower(n)] {
			return &ValidationError{"nome", "nome già usato"}
		}
		names[strings.ToLower(n)] = true
		if err := checkRefs("sezioni", d.Sezioni, secs); err != nil {
			return err
		}
	}
	return nil
}

// ValidateContacts controlla l'elenco completo dei numeri utili.
func (c *Config) ValidateContacts(cs []Contact) error {
	if len(cs) > MaxContacts {
		return &ValidationError{"contatti", "troppi numeri"}
	}
	deps := c.departmentIDs()
	for _, x := range cs {
		if l := runes(strings.TrimSpace(x.Etichetta)); l == 0 || l > 40 {
			return &ValidationError{"etichetta", "da 1 a 40 caratteri"}
		}
		if !phoneRe.MatchString(strings.TrimSpace(x.Numero)) {
			return &ValidationError{"numero", "solo cifre, spazi, + / . -"}
		}
		if err := checkRefs("reparti", x.Reparti, deps); err != nil {
			return err
		}
	}
	return nil
}

func validDate(d string) bool {
	if !dateRe.MatchString(d) {
		return false
	}
	_, err := time.Parse("2006-01-02", d)
	return err == nil
}

// ValidateNotices controlla l'elenco completo degli avvisi.
func (c *Config) ValidateNotices(ns []Notice) error {
	if len(ns) > MaxNotices {
		return &ValidationError{"avvisi", "troppi avvisi"}
	}
	deps := c.departmentIDs()
	for _, n := range ns {
		if l := runes(strings.TrimSpace(n.Testo)); l == 0 || l > 200 {
			return &ValidationError{"testo", "da 1 a 200 caratteri"}
		}
		if n.Livello != "info" && n.Livello != "attenzione" {
			return &ValidationError{"livello", "info o attenzione"}
		}
		if (n.Dal != "" && !validDate(n.Dal)) || (n.Al != "" && !validDate(n.Al)) {
			return &ValidationError{"date", "formato AAAA-MM-GG"}
		}
		if n.Dal != "" && n.Al != "" && n.Al < n.Dal {
			return &ValidationError{"date", "la fine precede l'inizio"}
		}
		if err := checkRefs("reparti", n.Reparti, deps); err != nil {
			return err
		}
	}
	return nil
}

// ValidateScreensaver controlla le impostazioni del salvaschermo.
func ValidateScreensaver(s Screensaver) error {
	if s.DopoMinuti < 1 || s.DopoMinuti > 120 {
		return &ValidationError{"dopoMinuti", "da 1 a 120 minuti"}
	}
	if !nightRe.MatchString(s.OrarioNero) {
		return &ValidationError{"orarioNero", "formato HH:MM-HH:MM"}
	}
	return nil
}

// RemoveSectionRefs toglie una sezione eliminata dai reparti.
func (c *Config) RemoveSectionRefs(id string) {
	for i := range c.Reparti {
		c.Reparti[i].Sezioni = without(c.Reparti[i].Sezioni, id)
	}
}

// RemoveDepartmentRefs toglie un reparto eliminato da dispositivi, numeri utili e avvisi.
func (c *Config) RemoveDepartmentRefs(id string) {
	for i := range c.Globali.Dispositivi {
		if c.Globali.Dispositivi[i].Reparto == id {
			c.Globali.Dispositivi[i].Reparto = ""
		}
	}
	for i := range c.Globali.Contatti {
		c.Globali.Contatti[i].Reparti = without(c.Globali.Contatti[i].Reparti, id)
	}
	for i := range c.Globali.Avvisi {
		c.Globali.Avvisi[i].Reparti = without(c.Globali.Avvisi[i].Reparti, id)
	}
}

func without(list []string, id string) []string {
	out := []string{}
	for _, x := range list {
		if x != id {
			out = append(out, x)
		}
	}
	return out
}
