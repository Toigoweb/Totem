package scanner

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/toigoweb/totem/internal/config"
	"github.com/toigoweb/totem/internal/smbsrc"
	"github.com/toigoweb/totem/internal/synclog"
)

func pdf(body string) []byte { return []byte("%PDF-1.4\n" + body + "\n%%EOF\n") }

type env struct {
	t       *testing.T
	data    string
	shares  map[string]string // percorso UNC → cartella locale
	offline map[string]bool
	opens   int
	cfg     *config.Store
	cipher  *config.Cipher
	sc      *Scanner
	now     time.Time
}

func newEnv(t *testing.T) *env {
	t.Helper()
	e := &env{t: t, data: t.TempDir(), shares: map[string]string{}, offline: map[string]bool{}, now: time.Date(2026, 10, 24, 8, 0, 0, 0, time.UTC)}
	var err error
	e.cfg, err = config.Open(filepath.Join(e.data, "config.json"), filepath.Join(e.data, "backup"))
	if err != nil {
		t.Fatal(err)
	}
	e.cipher, _, _ = config.LoadOrCreateKey(filepath.Join(e.data, "key"))
	e.start()
	return e
}

func (e *env) start() {
	lg, _ := synclog.Open(filepath.Join(e.data, "logs", "sync.log"), 1<<20, 3)
	var err error
	e.sc, err = New(Options{
		DataDir: e.data, Config: e.cfg, Cipher: e.cipher, Settings: config.DefaultSettings(), Log: lg,
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Now:    func() time.Time { return e.now },
		Connect: func(ctx context.Context, t smbsrc.Target) (smbsrc.Source, error) {
			if e.offline[t.Path] {
				return nil, &smbsrc.Error{Code: smbsrc.ErrUnreachable, Err: &net.OpError{Op: "dial", Err: errors.New("no route")}}
			}
			if t.Password != "giusta" {
				return nil, &smbsrc.Error{Code: smbsrc.ErrAuth, Err: errors.New("logon failure")}
			}
			return &countingSource{DirSource: smbsrc.DirSource{Root: e.shares[t.Path], Recursive: t.Recursive}, n: &e.opens}, nil
		},
	})
	if err != nil {
		e.t.Fatal(err)
	}
}

type countingSource struct {
	smbsrc.DirSource
	n *int
}

func (c *countingSource) Open(ctx context.Context, rel string) (io.ReadCloser, error) {
	*c.n++
	return c.DirSource.Open(ctx, rel)
}

func (e *env) addSection(id, unc string, emergency bool) string {
	dir := filepath.Join(e.t.TempDir(), id)
	os.MkdirAll(dir, 0o755)
	e.shares[unc] = dir
	enc, _ := e.cipher.Encrypt(id, "giusta")
	err := e.cfg.Update(func(c *config.Config) error {
		c.Sezioni = append(c.Sezioni, config.Section{ID: id, Nome: "Sezione " + id, Icona: "flacone", Ordine: len(c.Sezioni) + 1, Emergenza: emergency,
			Share: config.Share{Percorso: unc, Utente: `AZ\totem`, Password: enc, Sottocartelle: true}})
		return nil
	})
	if err != nil {
		e.t.Fatal(err)
	}
	return dir
}

func (e *env) write(dir, rel string, b []byte, mtime time.Time) {
	p := filepath.Join(dir, filepath.FromSlash(rel))
	os.MkdirAll(filepath.Dir(p), 0o755)
	if err := os.WriteFile(p, b, 0o644); err != nil {
		e.t.Fatal(err)
	}
	os.Chtimes(p, mtime, mtime)
}

func (e *env) scan() bool {
	e.now = e.now.Add(2 * time.Minute)
	return e.sc.ScanOnce(context.Background())
}

func (e *env) manifest() *Manifest {
	body, _ := e.sc.ManifestBody()
	var m Manifest
	if err := json.Unmarshal(body, &m); err != nil {
		e.t.Fatal(err)
	}
	return &m
}

func (e *env) doc(id string) *MDocument {
	for _, d := range e.manifest().Documents {
		if d.ID == id {
			return &d
		}
	}
	return nil
}

var t0 = time.Date(2026, 10, 1, 10, 0, 0, 0, time.UTC)

func TestStabilityAndAccept(t *testing.T) {
	e := newEnv(t)
	dir := e.addSection("s_a", `\\srv\a`, false)
	e.write(dir, "Solventi/SDS_Acetone_rev3.pdf", pdf("acetone"), t0)

	if !e.scan() {
		t.Fatal("al primo passaggio il file deve risultare in attesa")
	}
	if e.doc("s_a/Solventi/SDS_Acetone_rev3.pdf") != nil {
		t.Fatal("file accettato dopo una sola scansione")
	}
	v1 := e.manifest().Version
	if e.scan() {
		t.Fatal("nessun file dovrebbe restare in attesa")
	}
	d := e.doc("s_a/Solventi/SDS_Acetone_rev3.pdf")
	if d == nil {
		t.Fatal("file non accettato dopo due scansioni stabili")
	}
	if d.Title != "SDS Acetone" || d.Rev != "3" || d.Folder != "Solventi" || d.Section != "s_a" {
		t.Fatalf("%+v", d)
	}
	m := e.manifest()
	if m.Version != v1+1 || m.Hash == "" {
		t.Fatal("versione non incrementata")
	}
	// nessuna informazione sulla share nel manifest
	body, _ := e.sc.ManifestBody()
	for _, bad := range []string{`srv`, `AZ\\totem`, "giusta", "password"} {
		if strings.Contains(string(body), bad) {
			t.Fatalf("il manifest contiene %q", bad)
		}
	}
	// scansione senza modifiche: nessuna rilettura, nessuna nuova versione
	opens := e.opens
	e.scan()
	if e.opens != opens {
		t.Fatal("file riletto senza modifiche di dimensione o data")
	}
	if e.manifest().Version != m.Version {
		t.Fatal("versione cambiata senza modifiche")
	}
	if p, ok := e.sc.BlobPath(d.SHA256); !ok || !strings.HasSuffix(p, d.SHA256) {
		t.Fatal("blob non servibile")
	}
}

func TestFileBeingWritten(t *testing.T) {
	e := newEnv(t)
	dir := e.addSection("s_a", `\\srv\a`, false)
	e.write(dir, "big.pdf", []byte("%PDF-1.4\nparziale"), t0)
	e.scan()
	e.write(dir, "big.pdf", []byte("%PDF-1.4\nparziale, ancora"), t0.Add(time.Minute))
	e.scan()
	if e.doc("s_a/big.pdf") != nil {
		t.Fatal("accettato un file che cambia tra due scansioni")
	}
	e.write(dir, "big.pdf", pdf("completo"), t0.Add(2*time.Minute))
	e.scan()
	e.scan()
	if e.doc("s_a/big.pdf") == nil {
		t.Fatal("file completo non accettato")
	}
}

func TestCorruptKeepsPrevious(t *testing.T) {
	e := newEnv(t)
	dir := e.addSection("s_a", `\\srv\a`, false)
	e.write(dir, "x.pdf", pdf("v1"), t0)
	e.scan()
	e.scan()
	good := e.doc("s_a/x.pdf").SHA256

	e.write(dir, "x.pdf", []byte("%PDF-1.4\ntroncato senza fine"), t0.Add(time.Hour))
	e.scan()
	e.scan()
	if d := e.doc("s_a/x.pdf"); d == nil || d.SHA256 != good {
		t.Fatal("la versione precedente valida non è stata mantenuta")
	}
	// lo scarto non si ripete a ogni scansione
	opens := e.opens
	e.scan()
	if e.opens != opens {
		t.Fatal("file corrotto riletto a ogni scansione")
	}
	recent := e.sc.o.Log.Recent()
	if len(recent) == 0 || recent[0].Errors != 1 || recent[0].Details[0].Code != ErrCodeCorrupt {
		t.Fatalf("errore non registrato: %+v", recent)
	}
	pr := e.sc.Problems()
	if len(pr) != 1 || pr[0].Path != "x.pdf" || pr[0].Code != ErrCodeCorrupt || !pr[0].HasPrev || pr[0].At.IsZero() {
		t.Fatalf("problemi: %+v", pr)
	}
	// un file corrotto mai visto prima non entra
	e.write(dir, "nuovo.pdf", []byte("non sono un pdf %%EOF"), t0)
	e.scan()
	e.scan()
	if e.doc("s_a/nuovo.pdf") != nil {
		t.Fatal("PDF senza intestazione accettato")
	}
	if pr := e.sc.Problems(); len(pr) != 2 || pr[0].Path != "nuovo.pdf" || pr[0].HasPrev {
		t.Fatalf("problemi: %+v", pr)
	}
	// il file corretto sparisce dall'elenco
	e.write(dir, "nuovo.pdf", pdf("ora va"), t0.Add(2*time.Hour))
	e.scan()
	e.scan()
	if pr := e.sc.Problems(); len(pr) != 1 {
		t.Fatalf("problemi dopo la correzione: %+v", pr)
	}
}

func TestUpdateRemoveAndGC(t *testing.T) {
	e := newEnv(t)
	dir := e.addSection("s_a", `\\srv\a`, false)
	e.write(dir, "a.pdf", pdf("v1"), t0)
	e.write(dir, "b.pdf", pdf("b"), t0)
	e.scan()
	e.scan()
	old := e.doc("s_a/a.pdf")

	e.write(dir, "a.pdf", pdf("v2 più lunga"), t0.Add(time.Hour))
	e.scan()
	e.scan()
	d := e.doc("s_a/a.pdf")
	if d.SHA256 == old.SHA256 || !d.Updated.After(old.Updated) || !d.FirstSeen.Equal(old.FirstSeen) {
		t.Fatalf("aggiornamento errato: %+v", d)
	}
	// il blob vecchio resta finché è nel manifest precedente
	if _, ok := e.sc.BlobPath(old.SHA256); !ok {
		t.Fatal("blob del manifest precedente eliminato troppo presto")
	}
	os.Remove(filepath.Join(dir, "b.pdf"))
	e.scan()
	if e.doc("s_a/b.pdf") != nil {
		t.Fatal("file rimosso ancora nel manifest")
	}
	if e.sc.blobs.Has(old.SHA256) {
		t.Fatal("blob non più referenziato non eliminato")
	}
	r := e.sc.o.Log.Recent()
	if r[0].Removed != 1 || r[1].Updated != 1 {
		t.Fatalf("%+v", r[:2])
	}
}

func TestShareOfflineKeepsDocuments(t *testing.T) {
	e := newEnv(t)
	dir := e.addSection("s_a", `\\srv\a`, false)
	e.write(dir, "a.pdf", pdf("a"), t0)
	e.scan()
	e.scan()
	v := e.manifest().Version

	e.offline[`\\srv\a`] = true
	e.scan()
	first := e.now
	e.scan()
	if e.doc("s_a/a.pdf") == nil || e.manifest().Version != v {
		t.Fatal("documenti persi con la share offline")
	}
	st, _ := e.sc.Status()
	if st["s_a"].OK || st["s_a"].Error != smbsrc.ErrUnreachable || !st["s_a"].Since.Equal(first) {
		t.Fatalf("%+v", st["s_a"])
	}
	// l'errore ripetuto si registra una sola volta
	if r := e.sc.o.Log.Recent(); len(r) != 2 || r[0].Errors != 1 || r[1].Added != 1 {
		t.Fatalf("%+v", r)
	}
	e.offline[`\\srv\a`] = false
	e.scan()
	st, _ = e.sc.Status()
	if !st["s_a"].OK || st["s_a"].Since != nil {
		t.Fatal("stato non ripristinato")
	}
}

func TestWrongCredentials(t *testing.T) {
	e := newEnv(t)
	dir := e.addSection("s_a", `\\srv\a`, false)
	e.write(dir, "a.pdf", pdf("a"), t0)
	e.cfg.Update(func(c *config.Config) error {
		c.Sezioni[0].Share.Password, _ = e.cipher.Encrypt("s_a", "sbagliata")
		return nil
	})
	e.scan()
	st, _ := e.sc.Status()
	if st["s_a"].Error != smbsrc.ErrAuth {
		t.Fatal(st)
	}
	// chiave persa: password non decifrabile
	e.cfg.Update(func(c *config.Config) error { c.Sezioni[0].Share.Password = "v1:AAAA"; return nil })
	e.scan()
	st, _ = e.sc.Status()
	if st["s_a"].Error != ErrCodeNoPassword {
		t.Fatal(st)
	}
}

func TestSectionDeleted(t *testing.T) {
	e := newEnv(t)
	da := e.addSection("s_a", `\\srv\a`, false)
	db := e.addSection("s_b", `\\srv\b`, true)
	e.write(da, "a.pdf", pdf("a"), t0)
	e.write(db, "b.pdf", pdf("b"), t0)
	e.scan()
	e.scan()
	if len(e.manifest().Documents) != 2 || !e.manifest().Sections[1].Emergency {
		t.Fatal(e.manifest())
	}
	e.cfg.Update(func(c *config.Config) error { c.Sezioni = c.Sezioni[:1]; return nil })
	e.scan()
	m := e.manifest()
	if len(m.Documents) != 1 || len(m.Sections) != 1 || m.Documents[0].Section != "s_a" {
		t.Fatalf("%+v", m)
	}
}

func TestRestartAndLostIndex(t *testing.T) {
	e := newEnv(t)
	dir := e.addSection("s_a", `\\srv\a`, false)
	e.write(dir, "a.pdf", pdf("a"), t0)
	e.scan()
	e.scan()
	m := e.manifest()

	// riavvio normale: stesso manifest, nessuna rilettura
	e.start()
	opens := e.opens
	e.scan()
	if e.manifest().Hash != m.Hash || e.opens != opens {
		t.Fatal("riavvio ha cambiato il manifest")
	}
	// index.json perso: i documenti restano pubblicati
	os.Remove(filepath.Join(e.data, "state", "index.json"))
	e.start()
	e.scan()
	if e.doc("s_a/a.pdf") == nil {
		t.Fatal("documenti spariti dopo la perdita di index.json")
	}
	// blob cancellato a mano: si riscarica senza attendere la stabilità
	os.Remove(e.sc.blobs.Path(m.Documents[0].SHA256))
	e.scan()
	if _, ok := e.sc.BlobPath(m.Documents[0].SHA256); !ok {
		t.Fatal("blob mancante non ripristinato")
	}
}

func TestBlobPathRejectsUnknown(t *testing.T) {
	e := newEnv(t)
	e.scan()
	for _, bad := range []string{"../config.json", "tmp", strings.Repeat("a", 64), "ABC"} {
		if _, ok := e.sc.BlobPath(bad); ok {
			t.Fatal(bad)
		}
	}
}

func TestTitle(t *testing.T) {
	re := config.DefaultSettings().RevisionRegex
	cases := map[string][2]string{
		"SDS_Acetone_rev3.pdf":            {"SDS Acetone", "3"},
		"Piano emergenza Rev. 12.PDF":     {"Piano emergenza", "12"},
		"Scheda-EP400-R04.pdf":            {"Scheda EP400", "4"},
		"Planimetria_revisione_2.pdf":     {"Planimetria", "2"},
		"Olio da taglio.pdf":              {"Olio da taglio", ""},
		"cartella/Primer__epossidico.pdf": {"Primer epossidico", ""},
		"Rev3.pdf":                        {"Rev3", "3"},
		"Motore r2d2.pdf":                 {"Motore r2d2", ""},
	}
	for in, want := range cases {
		ti, rev := Title(in, re)
		if ti != want[0] || rev != want[1] {
			t.Errorf("%s → (%q,%q), atteso (%q,%q)", in, ti, rev, want[0], want[1])
		}
	}
}

func TestIngestValidation(t *testing.T) {
	b, _ := NewBlobStore(t.TempDir())
	if _, err := b.Ingest(strings.NewReader("ciao"), 4, 100); !errors.Is(err, ErrNotPDF) {
		t.Fatal(err)
	}
	if _, err := b.Ingest(strings.NewReader("%PDF-1.7 senza fine"), 19, 100); !errors.Is(err, ErrNoEOF) {
		t.Fatal(err)
	}
	if _, err := b.Ingest(strings.NewReader(string(pdf("x"))), 3, 100); !errors.Is(err, ErrChanged) {
		t.Fatal(err)
	}
	big := string(pdf(strings.Repeat("x", 200)))
	if _, err := b.Ingest(strings.NewReader(big), int64(len(big)), 100); !errors.Is(err, ErrTooLarge) {
		t.Fatal(err)
	}
	// %%EOF seguito da spazzatura entro 1024 byte: valido
	ok := string(pdf(strings.Repeat("y", 5000))) + strings.Repeat("\n", 100)
	sha, err := b.Ingest(strings.NewReader(ok), int64(len(ok)), 1<<20)
	if err != nil || !b.Has(sha) {
		t.Fatal(err)
	}
	ents, _ := os.ReadDir(filepath.Join(b.dir, "tmp"))
	if len(ents) != 0 {
		t.Fatal("file temporanei rimasti")
	}
}
