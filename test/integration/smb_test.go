//go:build integration

// Test di integrazione contro un vero Samba (docker-compose.test.yml).
// Variabili: TOTEM_SMB_HOST (default 127.0.0.1), TOTEM_SMB_PORT (default 1445).
package integration

import (
	"context"
	"io"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"testing"
	"time"

	"github.com/toigoweb/totem/internal/config"
	"github.com/toigoweb/totem/internal/scanner"
	"github.com/toigoweb/totem/internal/smbsrc"
)

func host() string {
	if h := os.Getenv("TOTEM_SMB_HOST"); h != "" {
		return h
	}
	return "127.0.0.1"
}

func port() int {
	if p, err := strconv.Atoi(os.Getenv("TOTEM_SMB_PORT")); err == nil {
		return p
	}
	return 1445
}

func target(share, user, pwd string) smbsrc.Target {
	return smbsrc.Target{Path: `\\` + host() + `\` + share, User: user, Password: pwd, Recursive: true, Port: port()}
}

func ctx(t *testing.T) context.Context {
	c, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	t.Cleanup(cancel)
	return c
}

func TestListAndRead(t *testing.T) {
	src, err := smbsrc.Connect(ctx(t), target("sds", `WORKGROUP\totem`, "Giusta-123"))
	if err != nil {
		t.Fatal(err)
	}
	defer src.Close()
	files, err := src.List(ctx(t))
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, f := range files {
		names = append(names, f.Path)
	}
	sort.Strings(names)
	if len(names) != 2 || names[0] != "Root.pdf" || names[1] != "Solventi/Acetone_rev2.pdf" {
		t.Fatal(names)
	}
	r, err := src.Open(ctx(t), "Solventi/Acetone_rev2.pdf")
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	b, _ := scanner.NewBlobStore(t.TempDir())
	var size int64
	for _, f := range files {
		if f.Path == "Solventi/Acetone_rev2.pdf" {
			size = f.Size
		}
	}
	if _, err := b.Ingest(r, size, 1<<20); err != nil {
		t.Fatal(err)
	}
}

func TestSubfolderAndNonRecursive(t *testing.T) {
	tg := target(`sds\Solventi`, "totem", "Giusta-123")
	src, err := smbsrc.Connect(ctx(t), tg)
	if err != nil {
		t.Fatal(err)
	}
	files, _ := src.List(ctx(t))
	src.Close()
	if len(files) != 1 || files[0].Path != "Acetone_rev2.pdf" {
		t.Fatal(files)
	}
	tg = target("sds", "totem", "Giusta-123")
	tg.Recursive = false
	src, _ = smbsrc.Connect(ctx(t), tg)
	files, _ = src.List(ctx(t))
	src.Close()
	if len(files) != 1 || files[0].Path != "Root.pdf" {
		t.Fatal(files)
	}
}

func TestEncryptedShare(t *testing.T) {
	src, err := smbsrc.Connect(ctx(t), target("cifrata", "totem", "Giusta-123"))
	if err != nil {
		t.Fatal(err)
	}
	defer src.Close()
	if files, err := src.List(ctx(t)); err != nil || len(files) != 1 {
		t.Fatal(files, err)
	}
}

func TestErrorClassification(t *testing.T) {
	cases := []struct {
		name string
		t    smbsrc.Target
		want string
	}{
		{"password errata", target("sds", "totem", "sbagliata"), smbsrc.ErrAuth},
		{"utente inesistente", target("sds", "nessuno", "x"), smbsrc.ErrAuth},
		{"share inesistente", target("manca", "totem", "Giusta-123"), smbsrc.ErrNotFound},
		{"cartella inesistente", target(`sds\Manca`, "totem", "Giusta-123"), smbsrc.ErrNotFound},
		{"accesso negato", target("riservata", "totem", "Giusta-123"), smbsrc.ErrAccessDenied},
		{"porta chiusa", smbsrc.Target{Path: `\\127.0.0.1\sds`, User: "totem", Password: "x", Port: 1}, smbsrc.ErrUnreachable},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			src, err := smbsrc.Connect(ctx(t), c.t)
			if err == nil {
				src.Close()
				t.Fatal("connessione riuscita")
			}
			if got := smbsrc.Code(err); got != c.want {
				t.Fatalf("atteso %s, ottenuto %s (%v)", c.want, got, err)
			}
		})
	}
}

func TestTimeout(t *testing.T) {
	// un server che accetta la connessione ma non risponde mai
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			defer c.Close()
		}
	}()
	p := ln.Addr().(*net.TCPAddr).Port
	c, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, err = smbsrc.Connect(c, smbsrc.Target{Path: `\\127.0.0.1\sds`, User: "totem", Password: "x", Port: p})
	if got := smbsrc.Code(err); got != smbsrc.ErrTimeout {
		t.Fatalf("atteso timeout, ottenuto %s (%v)", got, err)
	}
}

func TestScannerAgainstSamba(t *testing.T) {
	data := t.TempDir()
	cfg, _ := config.Open(filepath.Join(data, "config.json"), filepath.Join(data, "backup"))
	ci, _, _ := config.LoadOrCreateKey(filepath.Join(data, "key"))
	enc, _ := ci.Encrypt("s_sds", "Giusta-123")
	cfg.Update(func(c *config.Config) error {
		c.Sezioni = []config.Section{{ID: "s_sds", Nome: "SDS", Icona: "flacone", Ordine: 1,
			Share: config.Share{Percorso: `\\` + host() + `\sds`, Utente: "totem", Password: enc, Sottocartelle: true}}}
		return nil
	})
	sc, err := scanner.New(scanner.Options{DataDir: data, Config: cfg, Cipher: ci, Settings: config.DefaultSettings(),
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)), Port: port()})
	if err != nil {
		t.Fatal(err)
	}
	sc.ScanOnce(ctx(t))
	sc.ScanOnce(ctx(t))
	m := sc.Current()
	if len(m.Documents) != 2 {
		t.Fatalf("%+v", m.Documents)
	}
	for _, d := range m.Documents {
		if _, ok := sc.BlobPath(d.SHA256); !ok {
			t.Fatal("blob mancante", d.Path)
		}
	}
	if n, err := sc.TestConnection(ctx(t), smbsrc.Target{Path: `\\` + host() + `\sds`, User: "totem", Password: "Giusta-123", Recursive: true}); err != nil || n != 2 {
		t.Fatal(n, err)
	}
}
