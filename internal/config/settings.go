package config

import (
	"fmt"
	"regexp"
	"time"
)

// Settings raccoglie i valori di default nel codice. Si sovrascrivono solo dal file
// (config.json → "impostazioni"), mai dall'interfaccia.
type Settings struct {
	// server
	ScanInterval    time.Duration
	StabilityDelay  time.Duration
	RevisionRegex   *regexp.Regexp
	MaxPDFBytes     int64
	SMBTimeout      time.Duration
	PinAttempts     int
	PinLockout      time.Duration
	MaintenanceIdle time.Duration
	// PWA (inviati nel manifest)
	SyncInterval      time.Duration
	HeartbeatInterval time.Duration
	StaleDays         int
	RecentDays        int
	IdleMinutes       int
	NightWindow       string
	UpdateIdleMinutes int
	SpaceMarginMB     int
}

// DefaultRevisionRegex riconosce "rev3", "Rev. 3", "_R04", "revisione 2" in fondo al nome.
const DefaultRevisionRegex = `(?i)(?:^|[\s_.-]+)(?:rev(?:isione)?|r)\.?[\s_-]*(\d{1,3})$`

func DefaultSettings() Settings {
	return Settings{
		ScanInterval:      2 * time.Minute,
		StabilityDelay:    20 * time.Second,
		RevisionRegex:     regexp.MustCompile(DefaultRevisionRegex),
		MaxPDFBytes:       200 << 20,
		SMBTimeout:        2 * time.Minute,
		PinAttempts:       5,
		PinLockout:        5 * time.Minute,
		MaintenanceIdle:   10 * time.Minute,
		SyncInterval:      5 * time.Minute,
		HeartbeatInterval: 5 * time.Minute,
		StaleDays:         3,
		RecentDays:        30,
		IdleMinutes:       3,
		NightWindow:       "02:00-05:00",
		UpdateIdleMinutes: 15,
		SpaceMarginMB:     50,
	}
}

var nightRe = regexp.MustCompile(`^([01]\d|2[0-3]):[0-5]\d-([01]\d|2[0-3]):[0-5]\d$`)

// ResolveSettings applica le sovrascritture di "impostazioni" ai default.
// Restituisce anche gli avvisi per chiavi sconosciute o valori non validi,
// che vengono ignorati senza bloccare l'avvio.
func ResolveSettings(over map[string]any) (Settings, []string) {
	s := DefaultSettings()
	var warn []string
	dur := func(k string, dst *time.Duration, min time.Duration) {
		v, ok := over[k]
		if !ok {
			return
		}
		str, _ := v.(string)
		d, err := time.ParseDuration(str)
		if err != nil || d < min {
			warn = append(warn, fmt.Sprintf("impostazione %q non valida: %v", k, v))
			return
		}
		*dst = d
	}
	num := func(k string, dst *int, min, max int) {
		v, ok := over[k]
		if !ok {
			return
		}
		f, isNum := v.(float64)
		if !isNum || f != float64(int(f)) || int(f) < min || int(f) > max {
			warn = append(warn, fmt.Sprintf("impostazione %q non valida: %v", k, v))
			return
		}
		*dst = int(f)
	}
	known := map[string]bool{}
	for _, k := range []string{"intervalloScansione", "ritardoStabilita", "regexRevisione", "maxDimensionePDFMB", "timeoutSMB",
		"tentativiPIN", "bloccoPIN", "scadenzaManutenzione", "intervalloSync", "intervalloHeartbeat", "giorniAvviso",
		"giorniRecenti", "inattivitaMinuti", "finestraNotturna", "inattivitaAggiornamentoMinuti", "margineSpazioMB"} {
		known[k] = true
	}
	for k := range over {
		if !known[k] {
			warn = append(warn, fmt.Sprintf("impostazione sconosciuta %q ignorata", k))
		}
	}
	dur("intervalloScansione", &s.ScanInterval, 10*time.Second)
	dur("ritardoStabilita", &s.StabilityDelay, time.Second)
	dur("timeoutSMB", &s.SMBTimeout, 5*time.Second)
	dur("bloccoPIN", &s.PinLockout, time.Second)
	dur("scadenzaManutenzione", &s.MaintenanceIdle, time.Minute)
	dur("intervalloSync", &s.SyncInterval, 30*time.Second)
	dur("intervalloHeartbeat", &s.HeartbeatInterval, 30*time.Second)
	if v, ok := over["regexRevisione"]; ok {
		str, _ := v.(string)
		re, err := regexp.Compile(str)
		if err != nil || re.NumSubexp() < 1 {
			warn = append(warn, fmt.Sprintf("impostazione \"regexRevisione\" non valida: %v", v))
		} else {
			s.RevisionRegex = re
		}
	}
	mb := int(s.MaxPDFBytes >> 20)
	num("maxDimensionePDFMB", &mb, 1, 4096)
	s.MaxPDFBytes = int64(mb) << 20
	num("tentativiPIN", &s.PinAttempts, 3, 20)
	num("giorniAvviso", &s.StaleDays, 1, 365)
	num("giorniRecenti", &s.RecentDays, 1, 365)
	num("inattivitaMinuti", &s.IdleMinutes, 1, 120)
	num("inattivitaAggiornamentoMinuti", &s.UpdateIdleMinutes, 1, 240)
	num("margineSpazioMB", &s.SpaceMarginMB, 0, 10000)
	if v, ok := over["finestraNotturna"]; ok {
		str, _ := v.(string)
		if !nightRe.MatchString(str) {
			warn = append(warn, fmt.Sprintf("impostazione \"finestraNotturna\" non valida: %v", v))
		} else {
			s.NightWindow = str
		}
	}
	return s, warn
}
