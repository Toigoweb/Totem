// Package synclog scrive il log delle scansioni in JSON, una riga per evento, con
// rotazione automatica per dimensione. Non riceve mai password: chi scrive passa solo
// contatori, id di sezione, percorsi relativi e codici di errore.
package synclog

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type Entry struct {
	At      time.Time `json:"at"`
	Added   int       `json:"added"`
	Updated int       `json:"updated"`
	Removed int       `json:"removed"`
	Errors  int       `json:"errors"`
	// Details elenca gli errori (sezione, percorso relativo, codice), al massimo 50.
	Details []Detail `json:"details,omitempty"`
}

type Detail struct {
	Section string `json:"section"`
	Path    string `json:"path,omitempty"`
	Code    string `json:"code"`
	Msg     string `json:"msg,omitempty"`
}

type Log struct {
	mu       sync.Mutex
	path     string
	maxBytes int64
	keep     int
	recent   []Entry
}

// Open prepara il log in path; maxBytes e keep regolano la rotazione (sync.log.1 … .keep).
func Open(path string, maxBytes int64, keep int) (*Log, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	return &Log{path: path, maxBytes: maxBytes, keep: keep}, nil
}

// Write aggiunge una riga e la tiene tra le ultime 10 in memoria per /api/status.
func (l *Log) Write(e Entry) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(e.Details) > 50 {
		e.Details = e.Details[:50]
	}
	l.recent = append([]Entry{e}, l.recent...)
	if len(l.recent) > 10 {
		l.recent = l.recent[:10]
	}
	b, err := json.Marshal(e)
	if err != nil {
		return err
	}
	if st, err := os.Stat(l.path); err == nil && st.Size()+int64(len(b))+1 > l.maxBytes {
		l.rotate()
	}
	f, err := os.OpenFile(l.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.Write(append(b, '\n'))
	return err
}

func (l *Log) rotate() {
	_ = os.Remove(fmt.Sprintf("%s.%d", l.path, l.keep))
	for i := l.keep - 1; i >= 1; i-- {
		_ = os.Rename(fmt.Sprintf("%s.%d", l.path, i), fmt.Sprintf("%s.%d", l.path, i+1))
	}
	_ = os.Rename(l.path, l.path+".1")
}

// Recent restituisce le ultime voci, dalla più recente.
func (l *Log) Recent() []Entry {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]Entry(nil), l.recent...)
}
