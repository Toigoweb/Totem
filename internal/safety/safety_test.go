package safety

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/toigoweb/totem/internal/config"
)

// PNG di prova w×h.
func pngOf(w, h int) []byte {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	img.Set(1, 1, color.Black)
	var b bytes.Buffer
	png.Encode(&b, img)
	return b.Bytes()
}

func open(t *testing.T) (*Store, string) {
	dir := t.TempDir()
	st, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	st.Now = func() time.Time { return time.Date(2026, 10, 24, 9, 0, 0, 0, time.UTC) }
	return st, dir
}

func field(err error) string {
	if v, ok := err.(*config.ValidationError); ok {
		return v.Field
	}
	return ""
}

func TestMapsAndEvents(t *testing.T) {
	st, dir := open(t)
	if _, err := st.AddMap("Stabilimento", []byte("non un'immagine")); field(err) != "immagine" {
		t.Fatal(err)
	}
	if _, err := st.AddMap("Piccola", pngOf(20, 20)); field(err) != "immagine" {
		t.Fatal(err)
	}
	m, err := st.AddMap(" Stabilimento ", pngOf(400, 300))
	if err != nil || m.Nome != "Stabilimento" || m.Larghezza != 400 || m.Formato != "image/png" {
		t.Fatal(m, err)
	}
	if p, ok := st.ImagePath(m.Immagine); !ok || p != filepath.Join(dir, "maps", m.Immagine) {
		t.Fatal(p)
	}
	if _, ok := st.ImagePath("../safety.json"); ok {
		t.Fatal("percorso arbitrario accettato")
	}

	bad := []struct {
		e     Event
		field string
	}{
		{Event{Mappa: m.ID, X: 1.2, Y: .5, Data: "2026-10-01", Tipo: TypeInjury}, "posizione"},
		{Event{Mappa: m.ID, X: .2, Y: .5, Data: "2026-10-01", Tipo: "boh"}, "tipo"},
		{Event{Mappa: m.ID, X: .2, Y: .5, Data: "2027-01-01", Tipo: TypeInjury}, "data"},
		{Event{Mappa: m.ID, X: .2, Y: .5, Data: "01/10/2026", Tipo: TypeInjury}, "data"},
		{Event{Mappa: "m_manca", X: .2, Y: .5, Data: "2026-10-01", Tipo: TypeInjury}, "mappa"},
	}
	for _, b := range bad {
		if _, err := st.PutEvent(b.e); field(err) != b.field {
			t.Errorf("%+v: %v", b.e, err)
		}
	}
	e, err := st.PutEvent(Event{Mappa: m.ID, X: .2, Y: .5, Data: "2026-10-01", Tipo: TypeNearMiss, Nota: " muletto "})
	if err != nil || e.ID == "" || e.Nota != "muletto" {
		t.Fatal(e, err)
	}
	e.Tipo = TypeInjury
	if _, err := st.PutEvent(e); err != nil || st.Get().Eventi[0].Tipo != TypeInjury {
		t.Fatal(err)
	}

	// persistenza
	st2, _ := Open(dir)
	if d := st2.Get(); len(d.Mappe) != 1 || len(d.Eventi) != 1 {
		t.Fatal(d)
	}
	// eliminazione della planimetria: spariscono eventi e immagine
	if err := st.DeleteMap(m.ID); err != nil {
		t.Fatal(err)
	}
	if d := st.Get(); len(d.Mappe) != 0 || len(d.Eventi) != 0 {
		t.Fatal(d)
	}
	if _, err := os.Stat(filepath.Join(dir, "maps", m.Immagine)); !os.IsNotExist(err) {
		t.Fatal("immagine rimasta")
	}
	if err := st.DeleteEvent(e.ID); err != ErrNotFound {
		t.Fatal(err)
	}
}

func TestMapsFor(t *testing.T) {
	d := Data{Mappe: []Map{{ID: "a"}, {ID: "b", Reparti: []string{"r1"}}, {ID: "c", Reparti: []string{"cancellato"}}}}
	known := map[string]bool{"r1": true, "r2": true}
	ids := func(ms []Map) (s string) {
		for _, m := range ms {
			s += m.ID
		}
		return
	}
	if got := ids(d.MapsFor("r1", false, known)); got != "abc" {
		t.Fatal(got)
	}
	if got := ids(d.MapsFor("r2", false, known)); got != "ac" {
		t.Fatal(got)
	}
	if got := ids(d.MapsFor("", true, known)); got != "abc" {
		t.Fatal(got)
	}
}

func TestStartDate(t *testing.T) {
	st, _ := open(t)
	if err := st.SetStart("2030-01-01"); field(err) != "inizio" {
		t.Fatal(err)
	}
	if err := st.SetStart("2026-01-01"); err != nil || st.Get().Inizio != "2026-01-01" {
		t.Fatal(err)
	}
}
