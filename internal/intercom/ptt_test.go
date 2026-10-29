package intercom

import (
	"encoding/json"
	"testing"
)

func TestPushToTalk(t *testing.T) {
	h := New()
	a, _ := h.Subscribe("a")
	b, _ := h.Subscribe("b")
	c, _ := h.Subscribe("c")
	tk, err := h.StartTalk("ch1", "Magazzino", Party{"a", "Totem A"}, []string{"a", "b", "c"})
	if err != nil || len(tk.Listeners) != 2 {
		t.Fatal(tk, err)
	}
	for _, ch := range []<-chan Event{b, c} {
		if e := next(t, ch); e.Type != "ptt-start" || e.Talk.Speaker.Name != "Totem A" || e.Talk.ChannelName != "Magazzino" {
			t.Fatal(e)
		}
	}
	none(t, a)
	// uno alla volta: chi preme mentre un altro parla sa chi sta parlando
	if cur, err := h.StartTalk("ch1", "Magazzino", Party{"b", "Totem B"}, []string{"a", "b"}); err != ErrBusy || cur.Speaker.ID != "a" {
		t.Fatal(cur, err)
	}
	// un altro canale è libero
	if _, err := h.StartTalk("ch2", "Uffici", Party{"c", "Totem C"}, nil); err != nil {
		t.Fatal(err)
	}
	// segnalazione solo tra chi parla e gli ascoltatori
	if err := h.TalkSignal(tk.ID, "a", "b", json.RawMessage(`{"sdp":"x"}`)); err != nil {
		t.Fatal(err)
	}
	if e := next(t, b); e.Type != "ptt-signal" || e.From != "a" {
		t.Fatal(e)
	}
	if err := h.TalkSignal(tk.ID, "b", "c", json.RawMessage(`{}`)); err != ErrState {
		t.Fatal("segnale tra due ascoltatori", err)
	}
	if err := h.EndTalk(tk.ID, "b"); err != ErrState {
		t.Fatal("rilascio da un ascoltatore", err)
	}
	if err := h.EndTalk(tk.ID, "a"); err != nil {
		t.Fatal(err)
	}
	if e := next(t, b); e.Type != "ptt-end" || e.Reason != "release" {
		t.Fatal(e)
	}
	if _, ok := h.Talking("ch1"); ok {
		t.Fatal("canale ancora occupato")
	}
}
