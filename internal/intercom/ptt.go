package intercom

import (
	"encoding/json"
	"time"

	"github.com/toigoweb/totem/internal/auth"
)

// Push-to-talk sui canali: come un walkie-talkie. Chi tiene premuto prende la parola
// (uno alla volta per canale) e gli altri membri collegati lo sentono subito, senza
// rispondere. L'audio va da chi parla a ciascun ascoltatore; gli ascoltatori ricevono
// soltanto, quindi il loro microfono resta chiuso.

// MaxTalk è la durata massima di una pressione: poi la parola si libera da sola.
const MaxTalk = 60 * time.Second

// Talk è una presa di parola su un canale.
type Talk struct {
	ID          string    `json:"id"`
	Channel     string    `json:"channel"`
	ChannelName string    `json:"channelName"`
	Speaker     Party     `json:"speaker"`
	Listeners   []string  `json:"listeners"`
	Started     time.Time `json:"started"`
	timer       *time.Timer
}

// Talking restituisce chi sta parlando su un canale.
func (h *Hub) Talking(channel string) (Talk, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	t := h.talks[channel]
	if t == nil {
		return Talk{}, false
	}
	return *t, true
}

// StartTalk dà la parola a speaker sul canale; listeners sono i membri raggiungibili.
// Se qualcun altro sta parlando restituisce ErrBusy con la sua presa di parola.
func (h *Hub) StartTalk(channel, name string, speaker Party, listeners []string) (Talk, error) {
	h.mu.Lock()
	if cur := h.talks[channel]; cur != nil {
		out := *cur
		h.mu.Unlock()
		return out, ErrBusy
	}
	if h.inCall(speaker.ID) {
		h.mu.Unlock()
		return Talk{}, ErrBusy
	}
	t := &Talk{ID: auth.NewID("pt"), Channel: channel, ChannelName: name, Speaker: speaker, Started: h.Now()}
	for _, l := range listeners {
		if l != speaker.ID {
			t.Listeners = append(t.Listeners, l)
		}
	}
	if t.Listeners == nil {
		t.Listeners = []string{}
	}
	h.talks[channel] = t
	cp := *t
	for _, l := range t.Listeners {
		h.send(l, Event{Type: "ptt-start", Talk: &cp})
	}
	id := t.ID
	t.timer = time.AfterFunc(MaxTalk, func() { h.EndTalk(id, "") })
	h.mu.Unlock()
	return cp, nil
}

// EndTalk libera la parola. dev vuoto: scadenza (MaxTalk); altrimenti deve essere chi parla.
func (h *Hub) EndTalk(id, dev string) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	for ch, t := range h.talks {
		if t.ID != id {
			continue
		}
		if dev != "" && dev != t.Speaker.ID {
			return ErrState
		}
		t.timer.Stop()
		delete(h.talks, ch)
		e := Event{Type: "ptt-end", TalkID: id, Reason: "release"}
		if dev == "" {
			e.Reason = "timeout"
			h.send(t.Speaker.ID, e)
		}
		for _, l := range t.Listeners {
			h.send(l, e)
		}
		return nil
	}
	return ErrNotFound
}

// TalkSignal inoltra un messaggio WebRTC tra chi parla e un ascoltatore.
func (h *Hub) TalkSignal(id, from, to string, data json.RawMessage) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, t := range h.talks {
		if t.ID != id {
			continue
		}
		isListener := func(d string) bool {
			for _, l := range t.Listeners {
				if l == d {
					return true
				}
			}
			return false
		}
		if !(from == t.Speaker.ID && isListener(to)) && !(to == t.Speaker.ID && isListener(from)) {
			return ErrState
		}
		h.send(to, Event{Type: "ptt-signal", TalkID: id, From: from, Data: data})
		return nil
	}
	return ErrNotFound
}
