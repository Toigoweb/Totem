package intercom

import (
	"encoding/json"
	"testing"
	"time"
)

func next(t *testing.T, ch <-chan Event) Event {
	t.Helper()
	select {
	case e := <-ch:
		return e
	case <-time.After(2 * time.Second):
		t.Fatal("nessun evento")
	}
	return Event{}
}

func none(t *testing.T, ch <-chan Event) {
	t.Helper()
	select {
	case e := <-ch:
		t.Fatalf("evento inatteso %+v", e)
	case <-time.After(50 * time.Millisecond):
	}
}

func TestGroupCallFirstAnswerWins(t *testing.T) {
	h := New()
	var rung []string
	h.OnRing = func(c Call, devs []string) { rung = devs }
	totem, _ := h.Subscribe("totem")
	a, _ := h.Subscribe("tel-a")
	b, _ := h.Subscribe("tel-b")
	c, err := h.Start(Party{"totem", "Totem Magazzino"}, "Primo soccorso", "3210", []string{"tel-a", "tel-b", "totem"})
	if err != nil || c.State != StateRinging || len(rung) != 2 {
		t.Fatal(c, err, rung)
	}
	if e := next(t, a); e.Type != "incoming" || e.Call.From.Name != "Totem Magazzino" {
		t.Fatal(e)
	}
	next(t, b)
	if _, err := h.Start(Party{"totem", "x"}, "y", "", []string{"tel-a"}); err != ErrBusy {
		t.Fatal("seconda chiamata dallo stesso dispositivo")
	}
	if _, err := h.Answer(c.ID, Party{"tel-b", "Telefono B"}); err != nil {
		t.Fatal(err)
	}
	if e := next(t, totem); e.Type != "accepted" || e.Call.By.Name != "Telefono B" {
		t.Fatal(e)
	}
	if e := next(t, a); e.Type != "ended" || e.Reason != EndElsewhere {
		t.Fatal(e)
	}
	if _, err := h.Answer(c.ID, Party{"tel-a", "A"}); err != ErrState {
		t.Fatal("seconda risposta accettata")
	}
	// segnalazione WebRTC tra le due parti, mai verso altri
	if err := h.Signal(c.ID, "totem", json.RawMessage(`{"sdp":"offerta"}`)); err != nil {
		t.Fatal(err)
	}
	if e := next(t, b); e.Type != "signal" || string(e.Data) != `{"sdp":"offerta"}` {
		t.Fatal(e)
	}
	if err := h.Signal(c.ID, "tel-a", json.RawMessage(`{}`)); err != ErrState {
		t.Fatal("segnale da un estraneo")
	}
	if err := h.Hangup(c.ID, "tel-b"); err != nil {
		t.Fatal(err)
	}
	if e := next(t, totem); e.Type != "ended" || e.Reason != EndHangup {
		t.Fatal(e)
	}
	none(t, b) // chi riaggancia non riceve il proprio evento
	if h.Busy("totem") || h.Busy("tel-b") {
		t.Fatal("ancora occupati")
	}
}

func TestRejectAndNoAnswer(t *testing.T) {
	h := New()
	caller, _ := h.Subscribe("totem")
	h.Subscribe("tel-a")
	c, _ := h.Start(Party{"totem", "T"}, "Portineria", "3210", []string{"tel-a"})
	if err := h.Reject(c.ID, "tel-a"); err != nil {
		t.Fatal(err)
	}
	if e := next(t, caller); e.Type != "ended" || e.Reason != EndRejected || e.Call.Number != "3210" {
		t.Fatal(e)
	}
	// nessuno raggiungibile
	if c, _ := h.Start(Party{"totem", "T"}, "Portineria", "3210", nil); c.State != StateEnded {
		t.Fatal(c)
	}
}

func TestTimeoutAndLateSubscriber(t *testing.T) {
	h := New()
	caller, _ := h.Subscribe("totem")
	c, _ := h.Start(Party{"totem", "T"}, "Officina", "", []string{"tel-a"})
	// il telefono apre l'app dalla notifica: riceve la chiamata che sta squillando
	late, cancel := h.Subscribe("tel-a")
	if e := next(t, late); e.Type != "incoming" || e.Call.ID != c.ID {
		t.Fatal(e)
	}
	cancel()
	if h.Online("tel-a") {
		t.Fatal("ancora online")
	}
	h.mu.Lock()
	h.calls[c.ID].timer.Reset(10 * time.Millisecond)
	h.mu.Unlock()
	if e := next(t, caller); e.Type != "ended" || e.Reason != EndNoAnswer {
		t.Fatal(e)
	}
}
