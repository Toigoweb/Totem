// Package kiosk parla con l'app kiosk dei tablet (oggi FreeKiosk, API REST in LAN) per
// leggerne lo stato e inviare pochi comandi: ricarica, riavvio, schermo, luminosità,
// annuncio di emergenza. Le chiamate partono solo dal server: la chiave API non arriva mai
// alla PWA. Fotocamera, posizione ed esecuzione di JavaScript non si usano.
package kiosk

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Codici di errore stabili per interfaccia e log.
const (
	ErrAuth        = "auth"        // chiave API errata
	ErrUnreachable = "unreachable" // tablet spento, fuori rete, API disattivata
	ErrTimeout     = "timeout"
	ErrUnsupported = "unsupported" // comando non disponibile (versione o permessi)
	ErrDevice      = "device"      // il tablet ha rifiutato (es. serve Device Owner)
	ErrResponse    = "response"    // risposta non valida
)

// Error porta un codice e il messaggio (mai la chiave).
type Error struct {
	Code string
	Msg  string
}

func (e *Error) Error() string { return e.Code + ": " + e.Msg }

// Code restituisce il codice di un errore del pacchetto, "" se nil.
func Code(err error) string {
	var e *Error
	if errors.As(err, &e) {
		return e.Code
	}
	if err != nil {
		return ErrUnreachable
	}
	return ""
}

var hostRe = regexp.MustCompile(`^[A-Za-z0-9]([A-Za-z0-9.-]{0,251}[A-Za-z0-9])?$`)

// NormalizeAddress accetta host o host:porta (porta predefinita 8080) e restituisce
// host:porta, oppure false se non valido. Niente schema né percorso.
func NormalizeAddress(a string) (string, bool) {
	a = strings.TrimSpace(a)
	if a == "" || strings.ContainsAny(a, "/@?# ") {
		return "", false
	}
	host, port := a, "8080"
	if h, p, err := net.SplitHostPort(a); err == nil {
		host, port = h, p
	}
	n, err := strconv.Atoi(port)
	if err != nil || n < 1 || n > 65535 || !hostRe.MatchString(host) {
		return "", false
	}
	return net.JoinHostPort(host, strconv.Itoa(n)), true
}

// Client chiama l'API di un tablet.
type Client struct {
	Addr string // host:porta
	Key  string
	HTTP *http.Client
}

// DefaultHTTP: chiamate brevi e sempre dirette (mai attraverso un proxy: sono in LAN).
func DefaultHTTP() *http.Client {
	return &http.Client{Timeout: 8 * time.Second, Transport: &http.Transport{Proxy: nil, MaxIdleConnsPerHost: 2, IdleConnTimeout: 90 * time.Second}}
}

type envelope struct {
	Success bool            `json:"success"`
	Data    json.RawMessage `json:"data"`
	Error   string          `json:"error"`
}

// do esegue la richiesta; restituisce il corpo grezzo (fino a max byte).
func (c *Client) do(ctx context.Context, method, path string, body any, max int64) ([]byte, string, error) {
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, "http://"+c.Addr+path, rd)
	if err != nil {
		return nil, "", &Error{ErrResponse, "richiesta non valida"}
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.Key != "" {
		req.Header.Set("X-Api-Key", c.Key)
	}
	hc := c.HTTP
	if hc == nil {
		hc = DefaultHTTP()
	}
	res, err := hc.Do(req)
	if err != nil {
		var ne net.Error
		if errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &ne) && ne.Timeout()) {
			return nil, "", &Error{ErrTimeout, "il tablet non risponde"}
		}
		return nil, "", &Error{ErrUnreachable, "tablet non raggiungibile"}
	}
	defer res.Body.Close()
	b, err := io.ReadAll(io.LimitReader(res.Body, max))
	if err != nil {
		return nil, "", &Error{ErrUnreachable, "risposta interrotta"}
	}
	switch {
	case res.StatusCode == http.StatusUnauthorized || res.StatusCode == http.StatusForbidden:
		return nil, "", &Error{ErrAuth, "chiave API errata"}
	case res.StatusCode == http.StatusNotFound || res.StatusCode == http.StatusMethodNotAllowed:
		return nil, "", &Error{ErrUnsupported, path + " non disponibile"}
	case res.StatusCode >= 300:
		var env envelope
		_ = json.Unmarshal(b, &env)
		msg := env.Error
		if msg == "" {
			msg = fmt.Sprintf("HTTP %d", res.StatusCode)
		}
		return nil, "", &Error{ErrDevice, msg}
	}
	return b, res.Header.Get("Content-Type"), nil
}

func (c *Client) getData(ctx context.Context, path string, out any) error {
	b, _, err := c.do(ctx, http.MethodGet, path, nil, 1<<20)
	if err != nil {
		return err
	}
	var env envelope
	if err := json.Unmarshal(b, &env); err != nil || !env.Success {
		return &Error{ErrResponse, "risposta non valida"}
	}
	if err := json.Unmarshal(env.Data, out); err != nil {
		return &Error{ErrResponse, "dati non validi"}
	}
	return nil
}

// Status è la parte di GET /api/status che serve al totem.
type Status struct {
	Battery struct {
		Level       float64 `json:"level"`
		Charging    bool    `json:"charging"`
		Temperature float64 `json:"temperature"`
	} `json:"battery"`
	Screen struct {
		On         bool    `json:"on"`
		Brightness float64 `json:"brightness"`
	} `json:"screen"`
	Device struct {
		Model        string `json:"model"`
		Manufacturer string `json:"manufacturer"`
		Android      string `json:"android"`
	} `json:"device"`
	Wifi struct {
		Connected bool    `json:"connected"`
		SSID      string  `json:"ssid"`
		RSSI      float64 `json:"rssi"`
	} `json:"wifi"`
	Sensors struct {
		Light         float64 `json:"light"`
		Accelerometer *struct {
			X float64 `json:"x"`
			Y float64 `json:"y"`
			Z float64 `json:"z"`
		} `json:"accelerometer"`
	} `json:"sensors"`
	AutoBrightness struct {
		Enabled bool `json:"enabled"`
	} `json:"autoBrightness"`
	Kiosk struct {
		Enabled bool `json:"enabled"`
	} `json:"kiosk"`
	Storage struct {
		TotalMB     float64 `json:"totalMB"`
		AvailableMB float64 `json:"availableMB"`
	} `json:"storage"`
}

// Info è GET /api/info.
type Info struct {
	Version       string `json:"version"`
	IsDeviceOwner bool   `json:"isDeviceOwner"`
	KioskMode     bool   `json:"kioskMode"`
}

func (c *Client) Status(ctx context.Context) (Status, error) {
	var s Status
	return s, c.getData(ctx, "/api/status", &s)
}

func (c *Client) Info(ctx context.Context) (Info, error) {
	var i Info
	return i, c.getData(ctx, "/api/info", &i)
}

// Post invia un comando (POST con corpo JSON facoltativo).
func (c *Client) Post(ctx context.Context, path string, body any) error {
	_, _, err := c.do(ctx, http.MethodPost, path, body, 64<<10)
	return err
}

// Screenshot restituisce l'immagine PNG dello schermo.
func (c *Client) Screenshot(ctx context.Context) ([]byte, error) {
	b, ct, err := c.do(ctx, http.MethodGet, "/api/screenshot", nil, 20<<20)
	if err != nil {
		return nil, err
	}
	if !strings.HasPrefix(ct, "image/") && !bytes.HasPrefix(b, []byte("\x89PNG")) {
		return nil, &Error{ErrResponse, "lo screenshot non è un'immagine"}
	}
	return b, nil
}
