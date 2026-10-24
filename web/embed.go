// Package web incorpora la PWA compilata (web/dist) nel binario Go.
package web

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var dist embed.FS

// Dist restituisce il contenuto di dist, oppure nil se la PWA non è stata compilata
// (nella cartella c'è solo il segnaposto .keep).
func Dist() fs.FS {
	sub, err := fs.Sub(dist, "dist")
	if err != nil {
		return nil
	}
	if _, err := fs.Stat(sub, "index.html"); err != nil {
		return nil
	}
	return sub
}
