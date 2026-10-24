package scanner

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
	"time"

	"github.com/toigoweb/totem/internal/config"
)

const ManifestSchema = 1

type Manifest struct {
	SchemaVersion int              `json:"schemaVersion"`
	Version       int              `json:"version"`
	Hash          string           `json:"hash"`
	GeneratedAt   time.Time        `json:"generatedAt"`
	Settings      ManifestSettings `json:"settings"`
	Sections      []MSection       `json:"sections"`
	Documents     []MDocument      `json:"documents"`
}

type ManifestSettings struct {
	SyncIntervalSec      int    `json:"syncIntervalSec"`
	HeartbeatIntervalSec int    `json:"heartbeatIntervalSec"`
	StaleDays            int    `json:"staleDays"`
	RecentDays           int    `json:"recentDays"`
	IdleMinutes          int    `json:"idleMinutes"`
	NightWindow          string `json:"nightWindow"`
	UpdateIdleMinutes    int    `json:"updateIdleMinutes"`
	SpaceMarginMB        int    `json:"spaceMarginMB"`
}

type MSection struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Icon      string `json:"icon"`
	Order     int    `json:"order"`
	Emergency bool   `json:"emergency"`
}

type MDocument struct {
	ID        string    `json:"id"`
	Section   string    `json:"section"`
	Path      string    `json:"path"`
	Folder    string    `json:"folder"`
	Title     string    `json:"title"`
	Rev       string    `json:"rev"`
	Size      int64     `json:"size"`
	MTime     time.Time `json:"mtime"`
	SHA256    string    `json:"sha256"`
	FirstSeen time.Time `json:"firstSeen"`
	Updated   time.Time `json:"updated"`
}

func settingsFor(s config.Settings) ManifestSettings {
	return ManifestSettings{
		SyncIntervalSec:      int(s.SyncInterval.Seconds()),
		HeartbeatIntervalSec: int(s.HeartbeatInterval.Seconds()),
		StaleDays:            s.StaleDays,
		RecentDays:           s.RecentDays,
		IdleMinutes:          s.IdleMinutes,
		NightWindow:          s.NightWindow,
		UpdateIdleMinutes:    s.UpdateIdleMinutes,
		SpaceMarginMB:        s.SpaceMarginMB,
	}
}

// buildContent produce sezioni e documenti ordinati in modo deterministico.
// Nel manifest non finiscono mai server, share, utenti o password: solo percorsi relativi.
func buildContent(cfg *config.Config, entries map[string]*Entry, hasBlob func(string) bool, s config.Settings) ([]MSection, []MDocument) {
	secs := []MSection{}
	known := map[string]bool{}
	for _, sec := range cfg.SortedSections() {
		secs = append(secs, MSection{ID: sec.ID, Name: sec.Nome, Icon: sec.Icona, Order: sec.Ordine, Emergency: sec.Emergenza})
		known[sec.ID] = true
	}
	docs := []MDocument{}
	for id, e := range entries {
		if !known[e.Section] || !hasBlob(e.SHA256) {
			continue
		}
		title, rev := Title(e.Path, s.RevisionRegex)
		docs = append(docs, MDocument{
			ID: id, Section: e.Section, Path: e.Path, Folder: Folder(e.Path), Title: title, Rev: rev,
			Size: e.Size, MTime: e.MTime, SHA256: e.SHA256, FirstSeen: e.FirstSeen, Updated: e.Updated,
		})
	}
	sort.Slice(docs, func(i, j int) bool { return docs[i].ID < docs[j].ID })
	return secs, docs
}

// contentHash è lo SHA-256 della serializzazione canonica di sezioni e documenti.
func contentHash(secs []MSection, docs []MDocument) string {
	b, _ := json.Marshal(struct {
		S []MSection  `json:"s"`
		D []MDocument `json:"d"`
	}{secs, docs})
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

// Refs restituisce l'insieme degli SHA referenziati dal manifest.
func (m *Manifest) Refs() map[string]bool {
	out := map[string]bool{}
	if m == nil {
		return out
	}
	for _, d := range m.Documents {
		out[d.SHA256] = true
	}
	return out
}
