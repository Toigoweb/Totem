// Package sources apre l'origine dei PDF di una sezione in base al tipo: cartella di rete
// SMB, WebDAV, OneDrive/SharePoint o caricamento diretto. Tutte espongono la stessa
// interfaccia smbsrc.Source, quindi scanner, stabilità e validazione non cambiano.
package sources

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/toigoweb/totem/internal/davsrc"
	"github.com/toigoweb/totem/internal/graphsrc"
	"github.com/toigoweb/totem/internal/smbsrc"
)

// Connect apre l'origine indicata da t.Type ("" = SMB).
func Connect(ctx context.Context, t smbsrc.Target) (smbsrc.Source, error) {
	switch t.Type {
	case "", "smb":
		return smbsrc.Connect(ctx, t)
	case "webdav":
		return davsrc.Connect(ctx, t)
	case "onedrive":
		return graphsrc.Connect(ctx, t)
	case "upload":
		// cartella locale in /data/uploads/<sezione>: si crea se manca (sezione appena nata)
		if err := os.MkdirAll(t.Path, 0o700); err != nil {
			return nil, smbsrc.Wrap(err)
		}
		return &smbsrc.DirSource{Root: t.Path, Recursive: true}, nil
	}
	return nil, &smbsrc.Error{Code: smbsrc.ErrInvalidPath, Err: fmt.Errorf("tipo di origine %q sconosciuto", t.Type)}
}

// UploadDir è la cartella dei PDF caricati direttamente per una sezione.
func UploadDir(dataDir, sectionID string) string {
	return filepath.Join(dataDir, "uploads", sectionID)
}
