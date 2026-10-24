package smbsrc

import (
	"context"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// DirSource legge da una cartella locale con le stesse regole della sorgente SMB.
// Usata nei test e nello sviluppo locale.
type DirSource struct {
	Root      string
	Recursive bool
}

func (d *DirSource) List(ctx context.Context) ([]File, error) {
	if _, err := os.Stat(d.Root); err != nil {
		return nil, wrap(err)
	}
	var out []File
	err := filepath.WalkDir(d.Root, func(p string, e fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if p == d.Root {
			return nil
		}
		if skipName(e.Name()) {
			if e.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if e.IsDir() {
			if !d.Recursive {
				return filepath.SkipDir
			}
			return nil
		}
		if e.Type()&fs.ModeSymlink != 0 || !IsPDFName(e.Name()) {
			return nil
		}
		info, err := e.Info()
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(d.Root, p)
		out = append(out, File{Path: filepath.ToSlash(rel), Size: info.Size(), ModTime: info.ModTime().UTC().Truncate(time.Second)})
		return nil
	})
	if err != nil {
		return nil, wrap(err)
	}
	return out, nil
}

func (d *DirSource) Open(ctx context.Context, rel string) (io.ReadCloser, error) {
	if !SafeRel(rel) || strings.Contains(rel, "\\") {
		return nil, &Error{ErrInvalidPath, os.ErrInvalid}
	}
	f, err := os.Open(filepath.Join(d.Root, filepath.FromSlash(rel)))
	if err != nil {
		return nil, wrap(err)
	}
	return f, nil
}

func (d *DirSource) Close() error { return nil }
