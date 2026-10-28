// Package api espone l'HTTP del totem: la PWA, il manifest e i PDF per i dispositivi
// abbinati, la manutenzione dietro PIN e un listener interno su 127.0.0.1 per
// healthcheck e reset-pairing.
package api

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/toigoweb/totem/internal/auth"
	"github.com/toigoweb/totem/internal/config"
	"github.com/toigoweb/totem/internal/emergency"
	"github.com/toigoweb/totem/internal/intercom"
	"github.com/toigoweb/totem/internal/kiosk"
	"github.com/toigoweb/totem/internal/pcagent"
	"github.com/toigoweb/totem/internal/safety"
	"github.com/toigoweb/totem/internal/scanner"
	"github.com/toigoweb/totem/internal/smbsrc"
	"github.com/toigoweb/totem/internal/synclog"
	"github.com/toigoweb/totem/internal/webpush"
)

const (
	CookieName  = "totem_device"
	MaintHeader = "X-Maint-Token"
	RoleTotem   = "totem"
	RoleStation = "postazione"
	RolePhone   = "telefono"
)

// Scanner è la parte dello scanner usata dalle API (interfaccia per i test).
type Scanner interface {
	ManifestBody() ([]byte, string)
	Current() *scanner.Manifest
	Status() (map[string]scanner.SectionStatus, time.Time)
	BlobPath(sha string) (string, bool)
	TestConnection(ctx context.Context, t smbsrc.Target) (int, error)
	Problems() []scanner.Problem
	Kick()
}

type Options struct {
	Config   *config.Store
	Cipher   *config.Cipher
	Scanner  Scanner
	Log      *synclog.Log
	Settings config.Settings
	DataDir  string
	Safety   *safety.Store // planimetrie ed eventi (mappa incidenti)
	// Kiosk: tablet con app kiosk controllabile (facoltativo); Emergency: modalità emergenza
	Kiosk     *kiosk.Manager
	Emergency *emergency.Store
	Agents    *pcagent.Manager // agenti dei PC Windows (facoltativo)
	Intercom  *intercom.Hub    // interfono (facoltativo)
	Push      *webpush.Sender  // notifiche push ai telefoni (facoltativo)
	Static    fs.FS            // contenuto di web/dist
	Version   string
	Logger    *slog.Logger
	Now       func() time.Time
}

type Server struct {
	o Options

	pinLimiter     *auth.Limiter
	codeLimiter    *auth.Limiter
	monitorLimiter *auth.Limiter
	// token di emergenza sbagliati: blocco come il PIN
	emergencyLimiter *auth.Limiter
	agentLimiter     *auth.Limiter
	maint            *auth.Sessions // valore: id del dispositivo
	setup            *auth.Sessions // valore: "setup"

	mu        sync.Mutex
	setupCode string               // vuoto se il totem è già abbinato
	tests     map[string]testGrant // prove di connessione riuscite
	pairCodes map[string]pairGrant // codici di abbinamento di postazioni e telefoni
	lastKick  time.Time
	lastSeen  map[string]time.Time    // ultimo salvataggio di ultimoContatto per dispositivo
	appCmds   map[string][]appCommand // comandi per l'app dei dispositivi, consegnati una volta
}

func New(o Options) *Server {
	if o.Now == nil {
		o.Now = time.Now
	}
	if o.Logger == nil {
		o.Logger = slog.Default()
	}
	s := &Server{
		o:           o,
		pinLimiter:  auth.NewLimiter(o.Settings.PinAttempts, o.Settings.PinLockout),
		codeLimiter: auth.NewLimiter(o.Settings.PinAttempts, o.Settings.PinLockout),
		// il monitoraggio interroga spesso: blocco solo dopo molti token sbagliati
		monitorLimiter:   auth.NewLimiter(20, o.Settings.PinLockout),
		emergencyLimiter: auth.NewLimiter(o.Settings.PinAttempts, o.Settings.PinLockout),
		// gli agenti chiamano ogni 30 s: blocco solo dopo molti token sbagliati
		agentLimiter: auth.NewLimiter(20, o.Settings.PinLockout),
		maint:        auth.NewSessions(o.Settings.MaintenanceIdle, 0),
		setup:        auth.NewSessions(15*time.Minute, 30*time.Minute),
		tests:        map[string]testGrant{},
		pairCodes:    map[string]pairGrant{},
		lastSeen:     map[string]time.Time{},
		appCmds:      map[string][]appCommand{},
	}
	s.pinLimiter.Now, s.codeLimiter.Now, s.monitorLimiter.Now, s.emergencyLimiter.Now, s.agentLimiter.Now = o.Now, o.Now, o.Now, o.Now, o.Now
	s.maint.Now, s.setup.Now = o.Now, o.Now
	return s
}

// Handler restituisce il router pubblico (porta 8080).
func (s *Server) Handler() http.Handler {
	m := http.NewServeMux()
	m.HandleFunc("GET /api/health", s.health)
	m.HandleFunc("GET /brand/logo", s.logo)

	// primo avvio
	m.HandleFunc("GET /api/setup/state", s.setupState)
	m.HandleFunc("POST /api/setup/code", s.setupCodeCheck)
	m.HandleFunc("POST /api/setup/pin", s.setupPin)

	// dispositivo abbinato
	m.Handle("GET /manifest.json", s.device(http.HandlerFunc(s.manifest)))
	m.Handle("GET /api/files/{sha}", s.device(http.HandlerFunc(s.file)))
	m.Handle("GET /api/maps/{sha}", s.device(http.HandlerFunc(s.mapImage)))
	m.Handle("POST /api/heartbeat", s.device(http.HandlerFunc(s.heartbeat)))
	m.Handle("GET /api/status", s.device(http.HandlerFunc(s.status)))
	m.Handle("POST /api/scan", s.device(http.HandlerFunc(s.scanNow)))
	m.Handle("GET /api/emergency", s.device(http.HandlerFunc(s.getEmergency)))
	m.HandleFunc("POST /api/emergency/start", s.apiEmergency(true))
	m.HandleFunc("POST /api/emergency/stop", s.apiEmergency(false))
	m.HandleFunc("POST /api/agent/report", s.agentReport)
	m.HandleFunc("POST /api/agent/screenshot", s.agentScreenshot)
	m.Handle("POST /api/maint/login", s.staffOnly(http.HandlerFunc(s.maintLogin)))

	// manutenzione (PIN)
	m.Handle("POST /api/maint/logout", s.maintOnly(http.HandlerFunc(s.maintLogout)))
	m.Handle("GET /api/maint/sections", s.maintOnly(http.HandlerFunc(s.listSections)))
	m.Handle("POST /api/maint/sections", s.maintOnly(http.HandlerFunc(s.createSection)))
	m.Handle("PUT /api/maint/sections/{id}", s.maintOnly(http.HandlerFunc(s.updateSection)))
	m.Handle("DELETE /api/maint/sections/{id}", s.maintOnly(http.HandlerFunc(s.deleteSection)))
	m.Handle("POST /api/maint/sections-order", s.maintOnly(http.HandlerFunc(s.orderSections)))
	m.Handle("POST /api/maint/test", s.maintOnly(http.HandlerFunc(s.testConnection)))
	m.Handle("GET /api/maint/sections/{id}/files", s.maintOnly(http.HandlerFunc(s.listUploads)))
	m.Handle("PUT /api/maint/sections/{id}/files", s.maintOnly(http.HandlerFunc(s.putUpload)))
	m.Handle("DELETE /api/maint/sections/{id}/files", s.maintOnly(http.HandlerFunc(s.deleteUpload)))
	m.Handle("POST /api/maint/pin", s.maintOnly(http.HandlerFunc(s.changePin)))

	// dispositivi: postazioni, elenco e revoca, documenti scartati
	m.Handle("POST /api/maint/stations/pair-code", s.maintOnly(http.HandlerFunc(s.stationCode)))
	m.Handle("GET /api/maint/devices", s.maintOnly(http.HandlerFunc(s.listDevices)))
	m.Handle("DELETE /api/maint/devices/{id}", s.maintOnly(http.HandlerFunc(s.revokeDevice)))
	m.Handle("GET /api/maint/problems", s.maintOnly(http.HandlerFunc(s.problems)))
	m.Handle("POST /api/maint/totems/pair-code", s.maintOnly(http.HandlerFunc(s.totemCode)))
	m.Handle("PUT /api/maint/devices/{id}", s.maintOnly(http.HandlerFunc(s.updateDevice)))

	// tablet con app kiosk (FreeKiosk) ed emergenza
	m.Handle("PUT /api/maint/devices/{id}/kiosk", s.maintOnly(http.HandlerFunc(s.putKiosk)))
	m.Handle("POST /api/maint/devices/{id}/kiosk/{azione}", s.maintOnly(http.HandlerFunc(s.kioskAction)))
	m.Handle("GET /api/maint/devices/{id}/kiosk/screenshot", s.maintOnly(http.HandlerFunc(s.kioskScreenshot)))
	m.Handle("POST /api/maint/kiosk/test", s.maintOnly(http.HandlerFunc(s.testKiosk)))
	m.Handle("POST /api/maint/devices/{id}/agent/token", s.maintOnly(http.HandlerFunc(s.maintAgentToken)))
	m.Handle("PUT /api/maint/devices/{id}/agent", s.maintOnly(http.HandlerFunc(s.maintAgentPut)))
	m.Handle("DELETE /api/maint/devices/{id}/agent", s.maintOnly(http.HandlerFunc(s.maintAgentDelete)))
	m.Handle("POST /api/maint/devices/{id}/agent/cmd/{azione}", s.maintOnly(http.HandlerFunc(s.maintAgentCmd)))
	m.Handle("GET /api/maint/devices/{id}/agent/screenshot", s.maintOnly(http.HandlerFunc(s.maintAgentScreenshot)))
	m.Handle("POST /api/maint/devices/{id}/app/{azione}", s.maintOnly(http.HandlerFunc(s.maintAppCmd)))
	m.Handle("GET /api/maint/emergency", s.maintOnly(http.HandlerFunc(s.maintEmergency)))
	m.Handle("POST /api/maint/emergency/start", s.maintOnly(http.HandlerFunc(s.maintEmergencyStart)))
	m.Handle("POST /api/maint/emergency/stop", s.maintOnly(http.HandlerFunc(s.maintEmergencyStop)))
	m.Handle("POST /api/maint/emergency/announce", s.maintOnly(http.HandlerFunc(s.maintEmergencyAnnounce)))
	m.Handle("POST /api/maint/emergency/token", s.maintOnly(http.HandlerFunc(s.newEmergencyToken)))
	m.Handle("DELETE /api/maint/emergency/token", s.maintOnly(http.HandlerFunc(s.deleteEmergencyToken)))

	// reparti, numeri utili, avvisi, salvaschermo, monitoraggio
	m.Handle("GET /api/maint/extras", s.maintOnly(http.HandlerFunc(s.getExtras)))
	m.Handle("PUT /api/maint/departments", s.maintOnly(http.HandlerFunc(s.putDepartments)))
	m.Handle("PUT /api/maint/contacts", s.maintOnly(http.HandlerFunc(s.putContacts)))
	m.Handle("PUT /api/maint/notices", s.maintOnly(http.HandlerFunc(s.putNotices)))
	m.Handle("PUT /api/maint/screensaver", s.maintOnly(http.HandlerFunc(s.putScreensaver)))
	m.Handle("POST /api/maint/monitor/token", s.maintOnly(http.HandlerFunc(s.newMonitorToken)))
	m.Handle("DELETE /api/maint/monitor/token", s.maintOnly(http.HandlerFunc(s.deleteMonitorToken)))
	m.Handle("PUT /api/maint/monitor", s.maintOnly(http.HandlerFunc(s.putMonitor)))
	m.HandleFunc("GET /api/monitor", s.monitor)

	// mappa incidenti
	m.Handle("GET /api/maint/safety", s.maintOnly(http.HandlerFunc(s.getSafety)))
	m.Handle("GET /api/maint/safety/export", s.maintOnly(http.HandlerFunc(s.exportSafety)))
	m.Handle("PUT /api/maint/safety/settings", s.maintOnly(http.HandlerFunc(s.putSafetySettings)))
	m.Handle("POST /api/maint/safety/maps", s.maintOnly(http.HandlerFunc(s.addSafetyMap)))
	m.Handle("PUT /api/maint/safety/maps/{id}", s.maintOnly(http.HandlerFunc(s.updateSafetyMap)))
	m.Handle("DELETE /api/maint/safety/maps/{id}", s.maintOnly(http.HandlerFunc(s.deleteSafetyMap)))
	m.Handle("POST /api/maint/safety/events", s.maintOnly(http.HandlerFunc(s.putSafetyEvent)))
	m.Handle("PUT /api/maint/safety/events/{id}", s.maintOnly(http.HandlerFunc(s.putSafetyEvent)))
	m.Handle("DELETE /api/maint/safety/events/{id}", s.maintOnly(http.HandlerFunc(s.deleteSafetyEvent)))

	// interfono (404 finché è spento, globalmente o per il dispositivo)
	m.Handle("GET /api/intercom/events", s.intercomDevice(s.intercomEvents))
	m.Handle("GET /api/intercom/directory", s.intercomDevice(s.intercomDirectory))
	m.Handle("POST /api/intercom/calls", s.intercomDevice(s.intercomCall))
	m.Handle("POST /api/intercom/calls/{id}/{azione}", s.intercomDevice(s.intercomAction))
	m.Handle("PUT /api/intercom/me", s.intercomDevice(s.intercomMe))
	m.Handle("GET /api/maint/intercom", s.maintOnly(http.HandlerFunc(s.maintIntercom)))
	m.Handle("PUT /api/maint/intercom", s.maintOnly(http.HandlerFunc(s.putMaintIntercom)))
	m.Handle("PUT /api/maint/devices/{id}/intercom", s.maintOnly(http.HandlerFunc(s.putDeviceIntercom)))
	m.Handle("POST /api/maint/intercom/phone-code", s.maintOnly(http.HandlerFunc(s.phoneIntercomCode)))

	// fase 2: telefoni (404 finché globali.fase2 è false)
	m.Handle("POST /api/maint/devices/pair-code", s.phase2(s.maintOnly(http.HandlerFunc(s.pairCode))))
	m.Handle("POST /api/pair", s.phase2(http.HandlerFunc(s.pair)))

	m.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) { jsonErr(w, http.StatusNotFound, "not_found") })
	m.Handle("/", s.static())
	return securityHeaders(m)
}

func securityHeaders(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hd := w.Header()
		hd.Set("X-Content-Type-Options", "nosniff")
		hd.Set("Referrer-Policy", "no-referrer")
		hd.Set("X-Frame-Options", "DENY")
		// 'wasm-unsafe-eval' serve ai decoder wasm di pdf.js (JPEG 2000, JBIG2): niente eval JS
		hd.Set("Content-Security-Policy", "default-src 'self'; script-src 'self' 'wasm-unsafe-eval'; img-src 'self' data: blob:; "+
			"style-src 'self' 'unsafe-inline'; font-src 'self' data:; worker-src 'self' blob:; connect-src 'self'; "+
			"object-src 'none'; frame-ancestors 'none'; base-uri 'self'")
		if strings.HasPrefix(r.URL.Path, "/api/") {
			hd.Set("Cache-Control", "no-store")
		}
		h.ServeHTTP(w, r)
	})
}

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "version": s.o.Version})
}

// ---------------------------------------------------------------------------
// Autenticazione dispositivo e manutenzione

type ctxKey int

const deviceKey ctxKey = 1

func deviceFrom(r *http.Request) config.Device {
	d, _ := r.Context().Value(deviceKey).(config.Device)
	return d
}

func (s *Server) lookupDevice(r *http.Request) (config.Device, bool) {
	c, err := r.Cookie(CookieName)
	if err != nil || c.Value == "" || len(c.Value) > 200 {
		return config.Device{}, false
	}
	h := auth.HashToken(c.Value)
	var dev config.Device
	found := false
	s.o.Config.View(func(cfg *config.Config) {
		for _, d := range cfg.Globali.Dispositivi {
			if d.TokenHash == h {
				dev, found = d, true
				return
			}
		}
	})
	return dev, found
}

// device richiede il cookie di un dispositivo registrato.
func (s *Server) device(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		d, ok := s.lookupDevice(r)
		if !ok {
			jsonErr(w, http.StatusUnauthorized, "device")
			return
		}
		h.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), deviceKey, d)))
	})
}

// staffOnly: totem o postazione (i telefoni non fanno manutenzione).
func (s *Server) staffOnly(h http.Handler) http.Handler {
	return s.device(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if role := deviceFrom(r).Ruolo; role != RoleTotem && role != RoleStation {
			jsonErr(w, http.StatusForbidden, "forbidden")
			return
		}
		h.ServeHTTP(w, r)
	}))
}

// maintOnly: totem o postazione, più token di manutenzione valido (scade dopo 10 minuti
// di inattività).
func (s *Server) maintOnly(h http.Handler) http.Handler {
	return s.staffOnly(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		v, err := s.maint.Use(r.Header.Get(MaintHeader))
		if err != nil || v != deviceFrom(r).ID {
			jsonErr(w, http.StatusUnauthorized, "maintenance")
			return
		}
		h.ServeHTTP(w, r)
	}))
}

func (s *Server) phase2(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		on := false
		s.o.Config.View(func(c *config.Config) { on = c.Globali.Fase2 })
		if !on {
			jsonErr(w, http.StatusNotFound, "not_found")
			return
		}
		h.ServeHTTP(w, r)
	})
}

func (s *Server) setDeviceCookie(w http.ResponseWriter, tok string) {
	http.SetCookie(w, &http.Cookie{
		Name: CookieName, Value: tok, Path: "/", MaxAge: 10 * 365 * 24 * 3600,
		HttpOnly: true, Secure: true, SameSite: http.SameSiteStrictMode,
	})
}

// ---------------------------------------------------------------------------
// Utilità JSON

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func jsonErr(w http.ResponseWriter, code int, errCode string, extra ...any) {
	m := map[string]any{"error": errCode}
	for i := 0; i+1 < len(extra); i += 2 {
		if k, ok := extra[i].(string); ok {
			m[k] = extra[i+1]
		}
	}
	writeJSON(w, code, m)
}

// readJSON accetta solo application/json (insieme a SameSite=Strict blocca le richieste
// da altri siti) e al massimo 64 KB.
func readJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		jsonErr(w, http.StatusUnsupportedMediaType, "content_type")
		return false
	}
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		jsonErr(w, http.StatusBadRequest, "bad_request")
		return false
	}
	return true
}

func lockedResponse(w http.ResponseWriter, err error, now time.Time) bool {
	var le *auth.ErrLocked
	if errors.As(err, &le) {
		sec := int(le.Until.Sub(now).Seconds() + 0.999)
		w.Header().Set("Retry-After", itoa(sec))
		jsonErr(w, http.StatusTooManyRequests, "locked", "retryAfterSec", sec)
		return true
	}
	return false
}

func itoa(n int) string {
	b, _ := json.Marshal(n)
	return string(b)
}
