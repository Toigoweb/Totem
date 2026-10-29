// Package intercom coordina le chiamate dell'interfono tra totem, postazioni e telefoni.
//
// Il server non trasporta l'audio: mette in contatto i due browser (segnalazione WebRTC)
// e l'audio passa direttamente tra i dispositivi in rete interna, cifrato. Il server
// tiene in memoria solo le chiamate in corso: niente registrazioni né cronologia.
//
// Una chiamata squilla su uno o più dispositivi (un gruppo: risponde il primo); senza
// risposta entro RingTimeout termina con "noanswer" e chi chiama vede il numero di
// ripiego.
package intercom

import (
	"encoding/json"
	"errors"
	"sync"
	"time"

	"github.com/toigoweb/totem/internal/auth"
)

const RingTimeout = 30 * time.Second

// Stati della chiamata.
const (
	StateRinging = "ringing"
	StateActive  = "active"
	StateEnded   = "ended"
)

// Motivi di fine chiamata.
const (
	EndHangup    = "hangup"
	EndRejected  = "rejected"
	EndNoAnswer  = "noanswer"
	EndElsewhere = "elsewhere" // ha risposto un altro membro del gruppo
	EndOffline   = "offline"   // nessuno raggiungibile
)

var (
	ErrNotFound = errors.New("chiamata inesistente")
	ErrBusy     = errors.New("già in una chiamata")
	ErrState    = errors.New("operazione non valida in questo stato")
)

// Party è un dispositivo coinvolto.
type Party struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// Call è una chiamata.
type Call struct {
	ID      string    `json:"id"`
	From    Party     `json:"from"`
	Label   string    `json:"label"`  // chi si chiama: "Primo soccorso", "Totem Magazzino"
	Number  string    `json:"number"` // numero di ripiego
	State   string    `json:"state"`
	By      *Party    `json:"by,omitempty"` // chi ha risposto
	Started time.Time `json:"started"`
	ringing map[string]bool
	timer   *time.Timer
}

// Event va a un dispositivo sul suo flusso di eventi.
type Event struct {
	Type   string          `json:"type"` // incoming | accepted | signal | ended | ptt-start | ptt-signal | ptt-end
	Call   *Call           `json:"call,omitempty"`
	CallID string          `json:"callId,omitempty"`
	Talk   *Talk           `json:"talk,omitempty"`
	TalkID string          `json:"talkId,omitempty"`
	From   string          `json:"from,omitempty"` // ptt-signal: chi lo manda
	Reason string          `json:"reason,omitempty"`
	Data   json.RawMessage `json:"data,omitempty"`
}

// Hub tiene chiamate e iscritti al flusso di eventi.
type Hub struct {
	mu    sync.Mutex
	subs  map[string]map[chan Event]struct{}
	calls map[string]*Call
	talks map[string]*Talk // per canale
	// OnRing è chiamata (fuori dal lock) per i dispositivi che devono squillare: il
	// livello API manda le notifiche push ai telefoni.
	OnRing func(c Call, devices []string)
	// OnStop è chiamata (in background) per i dispositivi che hanno smesso di squillare
	// senza rispondere: toglie la notifica dai telefoni.
	OnStop func(callID string, devices []string)
	Now    func() time.Time
}

// stopped va chiamata con il lock preso.
func (h *Hub) stopped(id string, devs []string) {
	if h.OnStop != nil && len(devs) > 0 {
		go h.OnStop(id, devs)
	}
}

func New() *Hub {
	return &Hub{subs: map[string]map[chan Event]struct{}{}, calls: map[string]*Call{}, talks: map[string]*Talk{}, Now: time.Now}
}

// Subscribe apre il flusso di eventi di un dispositivo; cancel lo chiude. Se una
// chiamata sta squillando per lui (aperto da una notifica), la riceve subito.
func (h *Hub) Subscribe(dev string) (<-chan Event, func()) {
	ch := make(chan Event, 32)
	h.mu.Lock()
	if h.subs[dev] == nil {
		h.subs[dev] = map[chan Event]struct{}{}
	}
	h.subs[dev][ch] = struct{}{}
	for _, c := range h.calls {
		if c.State == StateRinging && c.ringing[dev] {
			cp := *c
			ch <- Event{Type: "incoming", Call: &cp}
		}
	}
	h.mu.Unlock()
	return ch, func() {
		h.mu.Lock()
		if m := h.subs[dev]; m != nil {
			delete(m, ch)
			if len(m) == 0 {
				delete(h.subs, dev)
			}
		}
		h.mu.Unlock()
	}
}

// Online dice se il dispositivo ha il flusso di eventi aperto.
func (h *Hub) Online(dev string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.subs[dev]) > 0
}

// send va chiamata con il lock preso.
func (h *Hub) send(dev string, e Event) {
	for ch := range h.subs[dev] {
		select {
		case ch <- e:
		default: // flusso intasato: l'evento si perde, il client si riallinea
		}
	}
}

func (h *Hub) inCall(dev string) bool {
	for _, c := range h.calls {
		if c.State == StateEnded {
			continue
		}
		if c.From.ID == dev || (c.By != nil && c.By.ID == dev) {
			return true
		}
	}
	return false
}

// Start avvia una chiamata verso i dispositivi indicati (già filtrati: raggiungibili e
// disponibili). Senza destinatari la chiamata nasce già chiusa per "offline".
func (h *Hub) Start(from Party, label, number string, targets []string) (Call, error) {
	h.mu.Lock()
	if h.inCall(from.ID) {
		h.mu.Unlock()
		return Call{}, ErrBusy
	}
	c := &Call{ID: auth.NewID("ch"), From: from, Label: label, Number: number, State: StateRinging, Started: h.Now(), ringing: map[string]bool{}}
	var ring []string
	for _, t := range targets {
		if t != from.ID && !h.inCall(t) {
			c.ringing[t] = true
			ring = append(ring, t)
		}
	}
	if len(ring) == 0 {
		c.State = StateEnded
		out := *c
		h.mu.Unlock()
		return out, nil
	}
	h.calls[c.ID] = c
	cp := *c
	for _, t := range ring {
		h.send(t, Event{Type: "incoming", Call: &cp})
	}
	id := c.ID
	c.timer = time.AfterFunc(RingTimeout, func() { h.timeout(id) })
	h.mu.Unlock()
	if h.OnRing != nil {
		h.OnRing(cp, ring)
	}
	return cp, nil
}

func (h *Hub) timeout(id string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	c := h.calls[id]
	if c == nil || c.State != StateRinging {
		return
	}
	h.end(c, EndNoAnswer, "")
}

// end chiude la chiamata e avvisa tutti; con il lock preso. skip non riceve l'evento.
func (h *Hub) end(c *Call, reason, skip string) {
	if c.timer != nil {
		c.timer.Stop()
	}
	c.State = StateEnded
	e := Event{Type: "ended", CallID: c.ID, Reason: reason}
	if c.From.ID != skip {
		ev := e
		if reason == EndNoAnswer || reason == EndRejected {
			cp := *c
			ev.Call = &cp // chi chiama riceve il numero di ripiego
		}
		h.send(c.From.ID, ev)
	}
	var stop []string
	for t := range c.ringing {
		if t != skip {
			h.send(t, e)
		}
		stop = append(stop, t)
	}
	h.stopped(c.ID, stop)
	if c.By != nil && c.By.ID != skip {
		h.send(c.By.ID, e)
	}
	delete(h.calls, c.ID)
}

// Answer: il primo che risponde prende la chiamata, agli altri smette di squillare.
func (h *Hub) Answer(id string, by Party) (Call, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	c := h.calls[id]
	if c == nil {
		return Call{}, ErrNotFound
	}
	if c.State != StateRinging || !c.ringing[by.ID] {
		return Call{}, ErrState
	}
	if c.timer != nil {
		c.timer.Stop()
	}
	c.State, c.By = StateActive, &by
	var stop []string
	for t := range c.ringing {
		if t != by.ID {
			h.send(t, Event{Type: "ended", CallID: c.ID, Reason: EndElsewhere})
			stop = append(stop, t)
		}
	}
	h.stopped(c.ID, stop)
	c.ringing = map[string]bool{}
	cp := *c
	h.send(c.From.ID, Event{Type: "accepted", Call: &cp})
	return cp, nil
}

// Reject: chi rifiuta smette di squillare; se non resta nessuno la chiamata termina.
func (h *Hub) Reject(id, dev string) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	c := h.calls[id]
	if c == nil {
		return ErrNotFound
	}
	if c.State != StateRinging || !c.ringing[dev] {
		return ErrState
	}
	delete(c.ringing, dev)
	h.stopped(c.ID, []string{dev})
	if len(c.ringing) == 0 {
		h.end(c, EndRejected, dev)
	}
	return nil
}

// Hangup: chi chiama o chi ha risposto chiude la chiamata.
func (h *Hub) Hangup(id, dev string) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	c := h.calls[id]
	if c == nil {
		return ErrNotFound
	}
	if c.From.ID != dev && (c.By == nil || c.By.ID != dev) {
		return ErrState
	}
	h.end(c, EndHangup, dev)
	return nil
}

// Signal inoltra un messaggio WebRTC (offerta, risposta, candidati) all'altra parte.
func (h *Hub) Signal(id, dev string, data json.RawMessage) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	c := h.calls[id]
	if c == nil {
		return ErrNotFound
	}
	if c.State != StateActive || c.By == nil {
		return ErrState
	}
	var to string
	switch dev {
	case c.From.ID:
		to = c.By.ID
	case c.By.ID:
		to = c.From.ID
	default:
		return ErrState
	}
	h.send(to, Event{Type: "signal", CallID: id, Data: data})
	return nil
}

// Get restituisce una chiamata in corso.
func (h *Hub) Get(id string) (Call, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	c := h.calls[id]
	if c == nil {
		return Call{}, false
	}
	return *c, true
}

// Busy dice se il dispositivo è in una chiamata (in corso o in uscita).
func (h *Hub) Busy(dev string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.inCall(dev)
}
