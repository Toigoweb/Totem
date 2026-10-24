package scanner

import (
	"path"
	"regexp"
	"strings"
)

var spaces = regexp.MustCompile(`\s+`)

// Title ricava il nome leggibile e la revisione dal nome del file:
// senza estensione, "-" e "_" diventano spazi, la revisione si toglie dal nome.
// "SDS_Acetone_rev3.pdf" → ("SDS Acetone", "3").
func Title(p string, rev *regexp.Regexp) (title, revision string) {
	name := path.Base(p)
	name = strings.TrimSuffix(name, path.Ext(name))
	if rev != nil {
		if m := rev.FindStringSubmatchIndex(name); m != nil && len(m) >= 4 && m[2] >= 0 {
			revision = strings.TrimLeft(name[m[2]:m[3]], "0")
			if revision == "" {
				revision = "0"
			}
			name = name[:m[0]] + name[m[1]:]
		}
	}
	name = strings.NewReplacer("_", " ", "-", " ").Replace(name)
	name = strings.TrimSpace(spaces.ReplaceAllString(name, " "))
	if name == "" {
		name = strings.TrimSuffix(path.Base(p), path.Ext(p))
	}
	return name, revision
}

// Folder restituisce la prima sottocartella del percorso relativo ("" se alla radice).
func Folder(p string) string {
	if i := strings.IndexByte(p, '/'); i >= 0 {
		return p[:i]
	}
	return ""
}
