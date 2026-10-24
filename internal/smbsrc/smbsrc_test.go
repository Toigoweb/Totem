package smbsrc

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sort"
	"testing"

	smb2 "github.com/hirochachacha/go-smb2"
)

func TestParsePath(t *testing.T) {
	h, s, d, err := ParsePath(`\\srv\HSE\SDS\2026\`)
	if err != nil || h != "srv" || s != "HSE" || d != `SDS\2026` {
		t.Fatal(h, s, d, err)
	}
	h, s, d, err = ParsePath(`\\srv\HSE`)
	if err != nil || d != "" {
		t.Fatal(h, s, d, err)
	}
	for _, bad := range []string{`srv\x`, `\\srv`, `\\srv\x\..\y`, `\\srv\\x`} {
		if _, _, _, err := ParsePath(bad); Code(err) != ErrInvalidPath {
			t.Errorf("%q: %v", bad, err)
		}
	}
}

func TestSplitUser(t *testing.T) {
	d, u := SplitUser(`AZIENDA\mario`)
	if d != "AZIENDA" || u != "mario" {
		t.Fatal(d, u)
	}
	d, u = SplitUser(`mario@azienda.local`)
	if d != "" || u != "mario@azienda.local" {
		t.Fatal(d, u)
	}
}

func TestClassify(t *testing.T) {
	cases := map[string]error{
		ErrAuth:         &smb2.ResponseError{Code: statusLogonFailure},
		ErrNotFound:     &os.PathError{Op: "open", Path: "x", Err: &smb2.ResponseError{Code: statusObjectPathNotFound}},
		ErrAccessDenied: &smb2.ResponseError{Code: statusAccessDenied},
		ErrTimeout:      fmt.Errorf("x: %w", context.DeadlineExceeded),
		ErrUnreachable:  &net.OpError{Op: "dial", Err: errors.New("connection refused")},
		ErrIO:           errors.New("boh"),
	}
	for want, err := range cases {
		if got := Classify(err); got != want {
			t.Errorf("%v: atteso %s, ottenuto %s", err, want, got)
		}
	}
}

func TestSafeRel(t *testing.T) {
	for _, ok := range []string{"a.pdf", "x/y/z.pdf", "con spazi/è.pdf"} {
		if !SafeRel(ok) {
			t.Error(ok)
		}
	}
	for _, ko := range []string{"", "/a.pdf", "../a.pdf", "a/../b.pdf", `a\b.pdf`, "a//b.pdf", "c:/x.pdf", "a\x00.pdf"} {
		if SafeRel(ko) {
			t.Error(ko)
		}
	}
}

func TestDirSource(t *testing.T) {
	root := t.TempDir()
	must := func(p string) {
		os.MkdirAll(filepath.Dir(filepath.Join(root, p)), 0o755)
		os.WriteFile(filepath.Join(root, p), []byte("%PDF-1.4\n%%EOF\n"), 0o644)
	}
	must("a.pdf")
	must("B.PDF")
	must("note.txt")
	must("~$temp.pdf")
	must(".nascosto.pdf")
	must("sub/c.pdf")
	must("sub/deep/d.pdf")
	list := func(rec bool) []string {
		fs, err := (&DirSource{Root: root, Recursive: rec}).List(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		var n []string
		for _, f := range fs {
			n = append(n, f.Path)
		}
		sort.Strings(n)
		return n
	}
	if got := fmt.Sprint(list(true)); got != "[B.PDF a.pdf sub/c.pdf sub/deep/d.pdf]" {
		t.Fatal(got)
	}
	if got := fmt.Sprint(list(false)); got != "[B.PDF a.pdf]" {
		t.Fatal(got)
	}
	if _, err := (&DirSource{Root: root}).Open(context.Background(), "../etc/passwd"); Code(err) != ErrInvalidPath {
		t.Fatal(err)
	}
	if _, err := (&DirSource{Root: filepath.Join(root, "manca")}).List(context.Background()); Code(err) != ErrNotFound {
		t.Fatal(err)
	}
}
