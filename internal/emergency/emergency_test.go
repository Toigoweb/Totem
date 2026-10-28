package emergency

import (
	"testing"
	"time"
)

func TestStartStop(t *testing.T) {
	dir := t.TempDir()
	st, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 28, 10, 0, 0, 0, time.UTC)
	st.Now = func() time.Time { return now }
	if _, err := st.Start("boh", "", "x"); err == nil {
		t.Fatal("tipo non valido accettato")
	}
	if _, err := st.Start(TypeAlert, "", "x"); err == nil {
		t.Fatal("avviso senza testo")
	}
	s, err := st.Start(TypeEvacuation, "", "totem magazzino")
	if err != nil || !s.Attiva || s.Testo != DefaultEvacuationText || s.Announcement()[:12] != "Evacuazione." {
		t.Fatal(s, err)
	}
	// aggiornamento: stessa ora di inizio, nuovo id
	now = now.Add(time.Minute)
	s2, _ := st.Start(TypeAlert, "Fuga di gas in magazzino: non entrare", "API")
	if !s2.Avviata.Equal(s.Avviata) || s2.ID == s.ID || s2.Title() != "Attenzione" {
		t.Fatal(s2)
	}
	// persistenza
	st2, _ := Open(dir)
	if !st2.Active() || st2.Get().Testo != s2.Testo {
		t.Fatal(st2.Get())
	}
	if ok, err := st.Stop("postazione IT"); !ok || err != nil || st.Active() {
		t.Fatal(ok, err)
	}
	if ok, _ := st.Stop("x"); ok {
		t.Fatal("doppia chiusura")
	}
	if st3, _ := Open(dir); st3.Active() || st3.Get().TerminataDa != "postazione IT" {
		t.Fatal(st3.Get())
	}
}
