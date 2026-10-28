package api

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/toigoweb/totem/internal/config"
	"github.com/toigoweb/totem/internal/kiosk"
)

// Tablet con app kiosk (FreeKiosk): l'area manutenzione associa a un dispositivo
// l'indirizzo e la chiave dell'API REST del tablet; il server ne legge lo stato ogni
// minuto e invia i comandi. La chiave è cifrata e non torna mai al browser.

type kioskOut struct {
	Tipo           string     `json:"tipo"`
	Indirizzo      string     `json:"indirizzo"`
	HasKey         bool       `json:"hasKey"`
	SchermoNotte   bool       `json:"schermoNotte"`
	LuminositaAuto bool       `json:"luminositaAuto"`
	Stato          kiosk.View `json:"stato"`
}

func (s *Server) kioskOf(d config.Device) *kioskOut {
	if d.Kiosk == nil || d.Kiosk.Tipo == "" || s.o.Kiosk == nil {
		return nil
	}
	return &kioskOut{Tipo: d.Kiosk.Tipo, Indirizzo: d.Kiosk.Indirizzo, HasKey: d.Kiosk.Chiave != "", SchermoNotte: d.Kiosk.SchermoNotte,
		LuminositaAuto: d.Kiosk.LuminositaAuto, Stato: s.o.Kiosk.ViewOf(d)}
}

type kioskIn struct {
	Tipo           string `json:"tipo"` // "freekiosk"; vuoto = scollega il tablet
	Indirizzo      string `json:"indirizzo"`
	Chiave         string `json:"chiave"` // vuota = invariata
	SchermoNotte   bool   `json:"schermoNotte"`
	LuminositaAuto bool   `json:"luminositaAuto"`
}

func kioskErr(w http.ResponseWriter, err error) {
	writeJSON(w, http.StatusOK, map[string]any{"ok": false, "code": kiosk.Code(err), "message": err.Error()})
}

// putKiosk salva (o toglie) il tablet associato a un dispositivo.
func (s *Server) putKiosk(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var in kioskIn
	if !readJSON(w, r, &in) {
		return
	}
	var k *config.Kiosk
	if in.Tipo != "" {
		if in.Tipo != "freekiosk" {
			validationResponse(w, &config.ValidationError{Field: "tipo", Msg: "tipo non supportato"})
			return
		}
		addr, ok := kiosk.NormalizeAddress(in.Indirizzo)
		if !ok {
			validationResponse(w, &config.ValidationError{Field: "indirizzo", Msg: "formato host:porta"})
			return
		}
		k = &config.Kiosk{Tipo: in.Tipo, Indirizzo: addr, SchermoNotte: in.SchermoNotte, LuminositaAuto: in.LuminositaAuto}
		if in.Chiave = strings.TrimSpace(in.Chiave); in.Chiave != "" {
			if len(in.Chiave) < 8 || len(in.Chiave) > 200 {
				validationResponse(w, &config.ValidationError{Field: "chiave", Msg: "da 8 a 200 caratteri"})
				return
			}
			enc, err := s.o.Cipher.Encrypt(config.KioskKeyID(id), in.Chiave)
			if err != nil {
				jsonErr(w, http.StatusInternalServerError, "save")
				return
			}
			k.Chiave = enc
		}
	}
	err := s.o.Config.Update(func(c *config.Config) error {
		for i := range c.Globali.Dispositivi {
			d := &c.Globali.Dispositivi[i]
			if d.ID != id {
				continue
			}
			if k != nil {
				if k.Chiave == "" && d.Kiosk != nil {
					k.Chiave = d.Kiosk.Chiave // chiave invariata
				}
				if k.Chiave == "" {
					return &config.ValidationError{Field: "chiave", Msg: "obbligatoria"}
				}
				if d.Kiosk != nil && d.Kiosk.Indirizzo == k.Indirizzo {
					k.Posizione = d.Kiosk.Posizione // stesso tablet: stessa posizione di riferimento
				}
			}
			d.Kiosk = k
			return nil
		}
		return errNotFound
	})
	if err == errNotFound {
		jsonErr(w, http.StatusNotFound, "not_found")
		return
	}
	if err != nil {
		validationResponse(w, err)
		return
	}
	s.o.Logger.Info("tablet del dispositivo aggiornato", "dispositivo", id, "collegato", k != nil)
	if s.o.Kiosk != nil {
		go s.o.Kiosk.Tick(context.Background()) // stato subito, senza aspettare il minuto
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// testKiosk prova indirizzo e chiave (quella salvata se la chiave è vuota).
func (s *Server) testKiosk(w http.ResponseWriter, r *http.Request) {
	var in struct {
		DeviceID  string `json:"deviceId"`
		Indirizzo string `json:"indirizzo"`
		Chiave    string `json:"chiave"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	addr, ok := kiosk.NormalizeAddress(in.Indirizzo)
	if !ok {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "code": "indirizzo"})
		return
	}
	key := strings.TrimSpace(in.Chiave)
	if key == "" && in.DeviceID != "" {
		if c, _, err := s.o.Kiosk.ClientFor(in.DeviceID); err == nil {
			key = c.Key
		}
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	c := s.o.Kiosk.NewClient(addr, key)
	info, err := c.Info(ctx)
	if err != nil {
		kioskErr(w, err)
		return
	}
	st, err := c.Status(ctx)
	if err != nil {
		kioskErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "indirizzo": addr, "version": info.Version, "deviceOwner": info.IsDeviceOwner,
		"kioskMode": info.KioskMode, "model": strings.TrimSpace(st.Device.Manufacturer + " " + st.Device.Model), "battery": st.Battery.Level})
}

// kioskAction esegue un comando sul tablet di un dispositivo.
func (s *Server) kioskAction(w http.ResponseWriter, r *http.Request) {
	id, name := r.PathValue("id"), r.PathValue("azione")
	if name == "posizione" {
		if err := s.o.Kiosk.ResetPosition(id); err != nil {
			kioskErr(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
		return
	}
	path, ok := kiosk.Actions[name]
	if !ok {
		jsonErr(w, http.StatusNotFound, "not_found")
		return
	}
	c, d, err := s.o.Kiosk.ClientFor(id)
	if err != nil {
		kioskErr(w, err)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	if err := c.Post(ctx, path, nil); err != nil {
		kioskErr(w, err)
		return
	}
	s.o.Logger.Info("comando inviato al tablet", "dispositivo", d.Nome, "azione", name)
	go s.o.Kiosk.Tick(context.Background())
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// kioskScreenshot restituisce l'immagine dello schermo del tablet (solo manutenzione).
func (s *Server) kioskScreenshot(w http.ResponseWriter, r *http.Request) {
	c, _, err := s.o.Kiosk.ClientFor(r.PathValue("id"))
	if err != nil {
		jsonErr(w, http.StatusNotFound, "not_found")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	b, err := c.Screenshot(ctx)
	if err != nil {
		jsonErr(w, http.StatusBadGateway, kiosk.Code(err))
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "no-store")
	w.Write(b)
}
