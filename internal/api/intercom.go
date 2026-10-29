package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/toigoweb/totem/internal/auth"
	"github.com/toigoweb/totem/internal/config"
	"github.com/toigoweb/totem/internal/intercom"
	"github.com/toigoweb/totem/internal/webpush"
)

// Interfono: chiamate vocali tra totem, postazioni e telefoni Android in rete interna.
//
// Spento di default (globali.interfono.attivo=false): le API rispondono 404, il manifest
// non lo nomina e l'app non mostra nulla né chiede il microfono. Si accende in
// manutenzione e poi dispositivo per dispositivo.
//
// Il server fa solo da centralino: tiene le chiamate in memoria e inoltra i messaggi
// WebRTC sul flusso di eventi (SSE); l'audio va direttamente tra i due browser. Ai
// telefoni la chiamata arriva anche come notifica push, così squilla a schermo spento.

const (
	phoneIntercomTTL = 10 * time.Minute
	sseHeartbeat     = 20 * time.Second
	maxIntercomGroup = 20
)

// intercomDevice: dispositivo abbinato con l'interfono attivo, altrimenti 404 (come se
// le API non esistessero).
func (s *Server) intercomDevice(h http.HandlerFunc) http.Handler {
	return s.device(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		on := false
		if s.o.Intercom != nil {
			s.o.Config.View(func(c *config.Config) {
				if d, ok := deviceByID(c, deviceFrom(r).ID); ok {
					on = c.IntercomOn(d)
				}
			})
		}
		if !on {
			jsonErr(w, http.StatusNotFound, "not_found")
			return
		}
		h(w, r)
	}))
}

func deviceByID(c *config.Config, id string) (config.Device, bool) {
	for _, d := range c.Globali.Dispositivi {
		if d.ID == id {
			return d, true
		}
	}
	return config.Device{}, false
}

func party(d config.Device) intercom.Party { return intercom.Party{ID: d.ID, Name: d.Nome} }

// reachable: il dispositivo può squillare adesso. Un telefono serve disponibile, e
// raggiungibile dal flusso di eventi aperto o da una notifica push.
func (s *Server) reachable(c *config.Config, d config.Device) bool {
	if !c.IntercomOn(d) {
		return false
	}
	if d.Ruolo == RolePhone {
		return d.Interfono.Disponibile && (s.o.Intercom.Online(d.ID) || (d.Interfono.Push != nil && s.o.Push != nil))
	}
	return s.o.Intercom.Online(d.ID)
}

// ---------------------------------------------------------------------------
// Flusso di eventi

func (s *Server) intercomEvents(w http.ResponseWriter, r *http.Request) {
	rc := http.NewResponseController(w)
	_ = rc.SetWriteDeadline(time.Time{})
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-store")
	h.Set("X-Accel-Buffering", "no") // Nginx (anche NPM) altrimenti trattiene gli eventi
	w.WriteHeader(http.StatusOK)
	dev := deviceFrom(r)
	ch, cancel := s.o.Intercom.Subscribe(dev.ID)
	defer cancel()
	if _, err := w.Write([]byte("retry: 3000\n: pronto\n\n")); err != nil {
		return
	}
	_ = rc.Flush()
	tick := time.NewTicker(sseHeartbeat)
	defer tick.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case e := <-ch:
			b, _ := json.Marshal(e)
			if _, err := w.Write(append(append([]byte("data: "), b...), '\n', '\n')); err != nil {
				return
			}
		case <-tick.C:
			// spento nel frattempo (o dispositivo revocato): chiude il flusso
			on := false
			s.o.Config.View(func(c *config.Config) {
				if d, ok := deviceByID(c, dev.ID); ok {
					on = c.IntercomOn(d)
				}
			})
			if !on {
				return
			}
			if _, err := w.Write([]byte(": ping\n\n")); err != nil {
				return
			}
		}
		if err := rc.Flush(); err != nil {
			return
		}
	}
}

// ---------------------------------------------------------------------------
// Rubrica

type dirGroup struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Available int    `json:"available"`
	Total     int    `json:"total"`
}

type dirChannel struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Online   int    `json:"online"`             // altri membri che sentono adesso
	Speaking string `json:"speaking,omitempty"` // chi sta parlando
	TalkID   string `json:"talkId,omitempty"`
}

type dirDevice struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Role       string `json:"role"`
	Department string `json:"department,omitempty"`
	Reachable  bool   `json:"reachable"`
	Busy       bool   `json:"busy"`
}

func (s *Server) intercomDirectory(w http.ResponseWriter, r *http.Request) {
	me := deviceFrom(r).ID
	out := struct {
		Groups    []dirGroup   `json:"groups"`
		Channels  []dirChannel `json:"channels"`
		Devices   []dirDevice  `json:"devices"`
		Available bool         `json:"available"`
		Push      bool         `json:"push"`
		VapidKey  string       `json:"vapidKey,omitempty"`
	}{Groups: []dirGroup{}, Channels: []dirChannel{}, Devices: []dirDevice{}}
	if s.o.Push != nil {
		out.VapidKey = s.o.Push.Keys.PublicKey()
	}
	s.o.Config.View(func(c *config.Config) {
		for _, g := range c.Globali.Interfono.Gruppi {
			dg := dirGroup{ID: g.ID, Name: g.Nome}
			for _, id := range g.Membri {
				if d, ok := deviceByID(c, id); ok && c.IntercomOn(d) && id != me {
					dg.Total++
					if s.reachable(c, d) && !s.o.Intercom.Busy(id) {
						dg.Available++
					}
				}
			}
			out.Groups = append(out.Groups, dg)
		}
		// canali: solo quelli di cui il dispositivo è membro
		for _, ch := range c.Globali.Interfono.Canali {
			if !contains(ch.Membri, me) {
				continue
			}
			dc := dirChannel{ID: ch.ID, Name: ch.Nome, Online: len(s.pttListeners(c, ch, me))}
			if t, ok := s.o.Intercom.Talking(ch.ID); ok {
				dc.Speaking, dc.TalkID = t.Speaker.Name, t.ID
			}
			out.Channels = append(out.Channels, dc)
		}
		for _, d := range c.Globali.Dispositivi {
			if d.ID == me {
				out.Available = d.Interfono != nil && d.Interfono.Disponibile
				out.Push = d.Interfono != nil && d.Interfono.Push != nil
				continue
			}
			if !c.IntercomOn(d) {
				continue
			}
			dd := dirDevice{ID: d.ID, Name: d.Nome, Role: d.Ruolo, Reachable: s.reachable(c, d), Busy: s.o.Intercom.Busy(d.ID)}
			if dep, ok := c.Department(d.Reparto); ok {
				dd.Department = dep.Nome
			}
			out.Devices = append(out.Devices, dd)
		}
	})
	// prima i totem, poi postazioni e telefoni; per nome
	rank := map[string]int{RoleTotem: 0, RoleStation: 1, RolePhone: 2}
	sort.SliceStable(out.Devices, func(i, j int) bool {
		a, b := out.Devices[i], out.Devices[j]
		if rank[a.Role] != rank[b.Role] {
			return rank[a.Role] < rank[b.Role]
		}
		return strings.ToLower(a.Name) < strings.ToLower(b.Name)
	})
	writeJSON(w, http.StatusOK, out)
}

// ---------------------------------------------------------------------------
// Chiamate

func (s *Server) intercomCall(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Target string `json:"target"` // "group:<id>" | "device:<id>"
	}
	if !readJSON(w, r, &req) {
		return
	}
	me := deviceFrom(r)
	var label, number string
	var targets []string
	found := false
	s.o.Config.View(func(c *config.Config) {
		me, _ = deviceByID(c, me.ID)
		kind, id, _ := strings.Cut(req.Target, ":")
		switch kind {
		case "group":
			for _, g := range c.Globali.Interfono.Gruppi {
				if g.ID != id {
					continue
				}
				found, label, number = true, g.Nome, g.Numero
				for _, m := range g.Membri {
					if d, ok := deviceByID(c, m); ok && s.reachable(c, d) {
						targets = append(targets, d.ID)
					}
				}
			}
		case "device":
			if d, ok := deviceByID(c, id); ok && c.IntercomOn(d) && d.ID != me.ID {
				found, label, number = true, d.Nome, d.Interfono.Numero
				if s.reachable(c, d) {
					targets = []string{d.ID}
				}
			}
		}
	})
	if !found {
		jsonErr(w, http.StatusNotFound, "not_found")
		return
	}
	call, err := s.o.Intercom.Start(party(me), label, number, targets)
	if errors.Is(err, intercom.ErrBusy) {
		jsonErr(w, http.StatusConflict, "busy")
		return
	}
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "call")
		return
	}
	out := map[string]any{"call": call}
	if call.State == intercom.StateEnded {
		out["reason"] = intercom.EndOffline // nessuno raggiungibile: subito il numero di ripiego
	}
	s.o.Logger.Info("interfono: chiamata", "chiamata", call.ID, "da", me.ID, "verso", req.Target, "squilla", len(targets))
	writeJSON(w, http.StatusOK, out)
}

func callErr(w http.ResponseWriter, err error) {
	switch {
	case err == nil:
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	case errors.Is(err, intercom.ErrNotFound):
		jsonErr(w, http.StatusNotFound, "not_found")
	default:
		jsonErr(w, http.StatusConflict, "state")
	}
}

func (s *Server) intercomAction(w http.ResponseWriter, r *http.Request) {
	id, me := r.PathValue("id"), deviceFrom(r)
	switch r.PathValue("azione") {
	case "answer":
		c, err := s.o.Intercom.Answer(id, party(me))
		if err != nil {
			callErr(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"call": c})
	case "reject":
		callErr(w, s.o.Intercom.Reject(id, me.ID))
	case "hangup":
		callErr(w, s.o.Intercom.Hangup(id, me.ID))
	case "signal":
		var data json.RawMessage
		if !readJSON(w, r, &data) {
			return
		}
		callErr(w, s.o.Intercom.Signal(id, me.ID, data))
	default:
		jsonErr(w, http.StatusNotFound, "not_found")
	}
}

// intercomMe: disponibilità del telefono e sottoscrizione alle notifiche push.
func (s *Server) intercomMe(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Disponibile *bool           `json:"disponibile"`
		Push        json.RawMessage `json:"push"` // oggetto, oppure null per toglierla
	}
	if !readJSON(w, r, &req) {
		return
	}
	var sub *config.PushSubscription
	if len(req.Push) > 0 && string(req.Push) != "null" {
		var p struct {
			Endpoint string `json:"endpoint"`
			Keys     struct {
				P256dh string `json:"p256dh"`
				Auth   string `json:"auth"`
			} `json:"keys"`
		}
		if err := json.Unmarshal(req.Push, &p); err != nil || !strings.HasPrefix(p.Endpoint, "https://") || len(p.Endpoint) > 1024 ||
			p.Keys.P256dh == "" || len(p.Keys.P256dh) > 200 || p.Keys.Auth == "" || len(p.Keys.Auth) > 100 {
			jsonErr(w, http.StatusBadRequest, "push")
			return
		}
		sub = &config.PushSubscription{Endpoint: p.Endpoint, P256dh: p.Keys.P256dh, Auth: p.Keys.Auth}
	}
	id := deviceFrom(r).ID
	err := s.o.Config.Update(func(c *config.Config) error {
		for i := range c.Globali.Dispositivi {
			d := &c.Globali.Dispositivi[i]
			if d.ID != id || d.Interfono == nil {
				continue
			}
			if req.Disponibile != nil {
				d.Interfono.Disponibile = *req.Disponibile
			}
			if len(req.Push) > 0 {
				d.Interfono.Push = sub
			}
			return nil
		}
		return errNotFound
	})
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "save")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// ---------------------------------------------------------------------------
// Notifiche push ai telefoni

// pushTargets: sottoscrizioni dei telefoni tra i dispositivi indicati.
func (s *Server) pushTargets(devs []string) map[string]config.PushSubscription {
	out := map[string]config.PushSubscription{}
	s.o.Config.View(func(c *config.Config) {
		for _, id := range devs {
			if d, ok := deviceByID(c, id); ok && d.Ruolo == RolePhone && d.Interfono != nil && d.Interfono.Push != nil {
				out[id] = *d.Interfono.Push
			}
		}
	})
	return out
}

func (s *Server) sendPush(devs []string, payload any, ttl time.Duration) {
	if s.o.Push == nil {
		return
	}
	b, _ := json.Marshal(payload)
	for id, sub := range s.pushTargets(devs) {
		go func(id string, sub config.PushSubscription) {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			err := s.o.Push.Send(ctx, webpush.Subscription{Endpoint: sub.Endpoint, P256dh: sub.P256dh, Auth: sub.Auth}, b, ttl)
			switch {
			case errors.Is(err, webpush.ErrGone):
				s.o.Logger.Info("interfono: notifiche del telefono non più valide, tolte", "dispositivo", id)
				_ = s.o.Config.Update(func(c *config.Config) error {
					for i := range c.Globali.Dispositivi {
						if d := &c.Globali.Dispositivi[i]; d.ID == id && d.Interfono != nil && d.Interfono.Push != nil &&
							d.Interfono.Push.Endpoint == sub.Endpoint {
							d.Interfono.Push = nil
						}
					}
					return nil
				})
			case err != nil:
				// l'indirizzo del servizio di notifica non va nei log (identifica il telefono)
				var ue *url.Error
				if errors.As(err, &ue) {
					err = ue.Err
				}
				s.o.Logger.Warn("interfono: notifica non inviata", "dispositivo", id, "err", err)
			}
		}(id, sub)
	}
}

// IntercomHooks collega l'hub alle notifiche push (da chiamare una volta all'avvio).
func (s *Server) IntercomHooks() {
	if s.o.Intercom == nil {
		return
	}
	s.o.Intercom.OnRing = func(c intercom.Call, devs []string) {
		s.sendPush(devs, map[string]any{"tipo": "chiamata", "id": c.ID, "da": c.From.Name, "verso": c.Label}, intercom.RingTimeout)
	}
	s.o.Intercom.OnStop = func(id string, devs []string) {
		s.sendPush(devs, map[string]any{"tipo": "fine", "id": id}, intercom.RingTimeout)
	}
}

// ---------------------------------------------------------------------------
// Manutenzione

type intercomDevOut struct {
	ID          string `json:"id"`
	Nome        string `json:"nome"`
	Ruolo       string `json:"ruolo"`
	RepartoNome string `json:"repartoNome,omitempty"`
	Attivo      bool   `json:"attivo"`
	Disponibile bool   `json:"disponibile"`
	Numero      string `json:"numero"`
	Push        bool   `json:"push"`
	Online      bool   `json:"online"`
}

func (s *Server) maintIntercom(w http.ResponseWriter, r *http.Request) {
	out := struct {
		Attivo    bool                     `json:"attivo"`
		Gruppi    []config.IntercomGroup   `json:"gruppi"`
		Canali    []config.IntercomChannel `json:"canali"`
		Devices   []intercomDevOut         `json:"devices"`
		Available bool                     `json:"available"` // funzione presente sul server
		Push      bool                     `json:"push"`
	}{Gruppi: []config.IntercomGroup{}, Canali: []config.IntercomChannel{}, Devices: []intercomDevOut{}, Available: s.o.Intercom != nil, Push: s.o.Push != nil}
	s.o.Config.View(func(c *config.Config) {
		out.Attivo = c.Globali.Interfono.Attivo
		out.Gruppi = append(out.Gruppi, c.Globali.Interfono.Gruppi...)
		out.Canali = append(out.Canali, c.Globali.Interfono.Canali...)
		for _, d := range c.Globali.Dispositivi {
			o := intercomDevOut{ID: d.ID, Nome: d.Nome, Ruolo: d.Ruolo}
			if dep, ok := c.Department(d.Reparto); ok {
				o.RepartoNome = dep.Nome
			}
			if in := d.Interfono; in != nil {
				o.Attivo, o.Disponibile, o.Numero, o.Push = in.Attivo, in.Disponibile, in.Numero, in.Push != nil
			}
			if s.o.Intercom != nil {
				o.Online = s.o.Intercom.Online(d.ID)
			}
			out.Devices = append(out.Devices, o)
		}
	})
	for i := range out.Gruppi {
		if out.Gruppi[i].Membri == nil {
			out.Gruppi[i].Membri = []string{}
		}
	}
	for i := range out.Canali {
		if out.Canali[i].Membri == nil {
			out.Canali[i].Membri = []string{}
		}
	}
	writeJSON(w, http.StatusOK, out)
}

// existingDevices: membri senza doppioni né dispositivi revocati.
func existingDevices(c *config.Config, ids []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, m := range ids {
		if _, ok := deviceByID(c, m); ok && !seen[m] {
			seen[m] = true
			out = append(out, m)
		}
	}
	return out
}

func cleanNumber(n string) (string, bool) {
	n = strings.TrimSpace(n)
	return n, len(n) <= 30
}

func (s *Server) putMaintIntercom(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Attivo bool                   `json:"attivo"`
		Gruppi []config.IntercomGroup `json:"gruppi"`
		// assente: canali invariati
		Canali *[]config.IntercomChannel `json:"canali"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	if s.o.Intercom == nil {
		jsonErr(w, http.StatusNotFound, "not_found")
		return
	}
	err := s.o.Config.Update(func(c *config.Config) error {
		if len(req.Gruppi) > maxIntercomGroup {
			return &config.ValidationError{Field: "gruppi", Msg: "troppi gruppi"}
		}
		gs := make([]config.IntercomGroup, 0, len(req.Gruppi))
		for _, g := range req.Gruppi {
			g.Nome = strings.TrimSpace(g.Nome)
			if g.Nome == "" || len([]rune(g.Nome)) > 40 {
				return &config.ValidationError{Field: "nome", Msg: "nome del gruppo mancante o troppo lungo"}
			}
			var ok bool
			if g.Numero, ok = cleanNumber(g.Numero); !ok {
				return &config.ValidationError{Field: "numero", Msg: "numero troppo lungo"}
			}
			if g.ID == "" {
				g.ID = auth.NewID("ig")
			}
			g.Membri = existingDevices(c, g.Membri)
			gs = append(gs, g)
		}
		chs := c.Globali.Interfono.Canali
		if req.Canali != nil {
			if len(*req.Canali) > maxIntercomGroup {
				return &config.ValidationError{Field: "canali", Msg: "troppi canali"}
			}
			chs = make([]config.IntercomChannel, 0, len(*req.Canali))
			for _, ch := range *req.Canali {
				ch.Nome = strings.TrimSpace(ch.Nome)
				if ch.Nome == "" || len([]rune(ch.Nome)) > 40 {
					return &config.ValidationError{Field: "canale", Msg: "nome del canale mancante o troppo lungo"}
				}
				if ch.ID == "" {
					ch.ID = auth.NewID("ic")
				}
				ch.Membri = existingDevices(c, ch.Membri)
				chs = append(chs, ch)
			}
		}
		c.Globali.Interfono = config.Intercom{Attivo: req.Attivo, Gruppi: gs, Canali: chs}
		return nil
	})
	if err != nil {
		validationResponse(w, err)
		return
	}
	s.o.Logger.Info("interfono: impostazioni", "attivo", req.Attivo, "gruppi", len(req.Gruppi))
	s.maintIntercom(w, r)
}

func (s *Server) putDeviceIntercom(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Attivo bool   `json:"attivo"`
		Numero string `json:"numero"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	num, ok := cleanNumber(req.Numero)
	if !ok {
		jsonErr(w, http.StatusBadRequest, "invalid", "field", "numero")
		return
	}
	id := r.PathValue("id")
	err := s.o.Config.Update(func(c *config.Config) error {
		for i := range c.Globali.Dispositivi {
			d := &c.Globali.Dispositivi[i]
			if d.ID != id {
				continue
			}
			if d.Interfono == nil {
				// totem e postazioni ricevono sempre; un telefono nuovo parte disponibile
				d.Interfono = &config.DeviceIntercom{Disponibile: true}
			}
			d.Interfono.Attivo, d.Interfono.Numero = req.Attivo, num
			return nil
		}
		return errNotFound
	})
	if errors.Is(err, errNotFound) {
		jsonErr(w, http.StatusNotFound, "not_found")
		return
	}
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "save")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// phoneIntercomCode: codice (e QR) per abbinare un telefono all'interfono. Si inserisce
// nella procedura guidata come quello delle postazioni; il QR apre l'indirizzo del totem
// con il codice già compilato.
func (s *Server) phoneIntercomCode(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Nome string `json:"nome"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	on := false
	s.o.Config.View(func(c *config.Config) { on = c.Globali.Interfono.Attivo })
	if s.o.Intercom == nil || !on {
		jsonErr(w, http.StatusConflict, "intercom_off")
		return
	}
	name := cleanName(req.Nome, "Telefono")
	code := s.newPairCodeGrant(pairGrant{role: roleIntercomPhone, name: name}, phoneIntercomTTL)
	s.o.Logger.Info("interfono: codice per un telefono", "nome", name)
	writeJSON(w, http.StatusOK, map[string]any{"code": code, "path": "/?abbina=" + code, "expiresInSec": int(phoneIntercomTTL.Seconds())})
}

// ---------------------------------------------------------------------------
// Push-to-talk sui canali

func contains(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

// pttListeners: membri del canale che sentono adesso (flusso di eventi aperto; i telefoni
// solo se disponibili).
func (s *Server) pttListeners(c *config.Config, ch config.IntercomChannel, me string) []string {
	out := []string{}
	for _, id := range ch.Membri {
		d, ok := deviceByID(c, id)
		if !ok || id == me || !c.IntercomOn(d) || !s.o.Intercom.Online(id) {
			continue
		}
		if d.Ruolo == RolePhone && !d.Interfono.Disponibile {
			continue
		}
		out = append(out, id)
	}
	return out
}

func (s *Server) pttTalk(w http.ResponseWriter, r *http.Request) {
	me := deviceFrom(r)
	chID := r.PathValue("canale")
	var ch config.IntercomChannel
	var listeners, offline []string
	found := false
	s.o.Config.View(func(c *config.Config) {
		me, _ = deviceByID(c, me.ID)
		for _, x := range c.Globali.Interfono.Canali {
			if x.ID == chID && contains(x.Membri, me.ID) {
				ch, found = x, true
				listeners = s.pttListeners(c, x, me.ID)
				// telefoni disponibili con l'app chiusa: solo una notifica
				for _, id := range x.Membri {
					if d, ok := deviceByID(c, id); ok && id != me.ID && d.Ruolo == RolePhone && c.IntercomOn(d) &&
						d.Interfono.Disponibile && !contains(listeners, id) {
						offline = append(offline, id)
					}
				}
			}
		}
	})
	if !found {
		jsonErr(w, http.StatusNotFound, "not_found")
		return
	}
	t, err := s.o.Intercom.StartTalk(ch.ID, ch.Nome, party(me), listeners)
	if errors.Is(err, intercom.ErrBusy) {
		jsonErr(w, http.StatusConflict, "busy", "speaker", t.Speaker.Name)
		return
	}
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "ptt")
		return
	}
	if len(offline) > 0 {
		s.sendPush(offline, map[string]any{"tipo": "ptt", "id": t.ID, "da": me.Nome, "verso": ch.Nome}, intercom.MaxTalk)
	}
	writeJSON(w, http.StatusOK, map[string]any{"talk": t})
}

func (s *Server) pttAction(w http.ResponseWriter, r *http.Request) {
	id, me := r.PathValue("id"), deviceFrom(r)
	switch r.PathValue("azione") {
	case "release":
		callErr(w, s.o.Intercom.EndTalk(id, me.ID))
	case "signal":
		var req struct {
			To   string          `json:"to"`
			Data json.RawMessage `json:"data"`
		}
		if !readJSON(w, r, &req) {
			return
		}
		callErr(w, s.o.Intercom.TalkSignal(id, me.ID, req.To, req.Data))
	default:
		jsonErr(w, http.StatusNotFound, "not_found")
	}
}
