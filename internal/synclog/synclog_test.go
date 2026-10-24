package synclog

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestRotation(t *testing.T) {
	dir := t.TempDir()
	l, _ := Open(filepath.Join(dir, "sync.log"), 300, 3)
	for i := 0; i < 40; i++ {
		if err := l.Write(Entry{At: time.Now(), Added: i}); err != nil {
			t.Fatal(err)
		}
	}
	ents, _ := os.ReadDir(dir)
	if len(ents) != 4 { // sync.log + .1 .2 .3
		t.Fatalf("file attesi 4, trovati %d", len(ents))
	}
	r := l.Recent()
	if len(r) != 10 || r[0].Added != 39 {
		t.Fatal(r)
	}
}
