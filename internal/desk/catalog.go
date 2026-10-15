package desk

import (
	"path/filepath"
	"strings"
)

// CatalogOptions says what the start screen should collect. Everything is on by
// default, because a band that is always empty is worse than a long list.
type CatalogOptions struct {
	// Dir is the desktop directory: its launchers, folders and files are always
	// part of the catalog.
	Dir string
	// Home is where the search folders live. An empty value means the home
	// directory of the user the shell runs as.
	Home string
	// Applications adds the installed launchers from the XDG directories.
	Applications bool
	// HomeFiles adds the files of the search folders, which is what fills the
	// document, media and archive bands.
	HomeFiles bool
	// FileLimit caps how many home files the catalog holds.
	FileLimit int
}

// DefaultCatalogOptions returns what the shell asks for out of the box.
func DefaultCatalogOptions(dir string) CatalogOptions {
	return CatalogOptions{
		Dir:          dir,
		Applications: true,
		HomeFiles:    true,
		FileLimit:    600,
	}
}

// Catalog is everything the shell can show as a tile: the desktop directory,
// the installed launchers and the files of the home folders. The list is
// grouped later, so the order here only decides which tile comes first inside
// its band.
//
// The scan touches every .desktop file and walks the home folders, which is too
// much work for the event loop, so the shell runs it in the background.
func Catalog(opts CatalogOptions) ([]Entry, error) {
	var out []Entry
	seen := make(map[string]bool)
	// paths remembers where an entry came from. A launcher on the desktop and
	// the same file found by a search walk are one tile, not two.
	paths := make(map[string]bool)
	add := func(e Entry) {
		key := entryKey(e)
		if key == "" || seen[key] {
			return
		}
		seen[key] = true
		if abs := absolute(e.Path); abs != "" {
			paths[abs] = true
		}
		out = append(out, e)
	}

	desktop, desktopErr := Entries(opts.Dir)
	for _, e := range desktop {
		add(e)
	}

	if opts.Applications {
		for _, e := range Applications() {
			if e.NoDisplay {
				continue
			}
			add(e)
		}
	}

	if opts.HomeFiles {
		for _, e := range SearchFilesFrom(opts.Home, opts.FileLimit) {
			// The .desktop files of the desktop directory are launchers, not
			// documents: they are already in the list above.
			if abs := absolute(e.Path); abs != "" && paths[abs] {
				continue
			}
			add(e)
		}
	}

	Sort(out)
	return out, desktopErr
}

// absolute returns the cleaned absolute path of an entry, or an empty string.
func absolute(path string) string {
	if path == "" {
		return ""
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return ""
	}
	return abs
}

// entryKey identifies an entry across sources: the same launcher can live in a
// system directory and on the desktop, and the same file can be reachable
// through a search folder. Launchers are compared by their Exec line, because
// their file names differ between distributions.
func entryKey(e Entry) string {
	if e.Kind == KindApp || e.Kind == KindShortcut {
		if e.Exec != "" {
			return "exec:" + strings.Join(strings.Fields(e.Exec), " ")
		}
		return "path:" + e.Path
	}
	if e.Path == "" {
		return ""
	}
	// Search results come with an absolute path already, but a relative one
	// would look like a different file.
	if abs := absolute(e.Path); abs != "" {
		return "path:" + abs
	}
	return "path:" + e.Path
}
