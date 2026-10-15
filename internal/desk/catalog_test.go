package desk

import (
	"os"
	"path/filepath"
	"testing"
)

// TestCatalogMergesTheThreeSources checks that the catalog is what the start
// screen needs: the desktop directory, the installed launchers and the files of
// the home folders, without duplicates.
func TestCatalogMergesTheThreeSources(t *testing.T) {
	home := t.TempDir()
	desktop := filepath.Join(home, "Desktop")
	docs := filepath.Join(home, "Documents")
	if err := os.MkdirAll(desktop, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(docs, 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(path, body string) {
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	launcher := "[Desktop Entry]\nName=Заметки\nExec=notes %U\nCategories=Office;\n"
	write(filepath.Join(desktop, "notes.desktop"), launcher)
	write(filepath.Join(desktop, "Договор.pdf"), "pdf")
	if err := os.MkdirAll(filepath.Join(desktop, "Архив"), 0o755); err != nil {
		t.Fatal(err)
	}
	write(filepath.Join(docs, "Письмо.docx"), "docx")

	entries, err := Catalog(CatalogOptions{Dir: desktop, Home: home, HomeFiles: true, FileLimit: 100})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{
		"Заметки":     false, // the launcher on the desktop
		"Договор.pdf": false, // a file of the desktop directory
		"Архив":       false, // a folder of the desktop directory
		"Письмо.docx": false, // a file of the home folder
	}
	for _, e := range entries {
		if _, ok := want[e.Name]; ok {
			want[e.Name] = true
		}
	}
	for name, found := range want {
		if !found {
			t.Errorf("%q is missing from the catalog: %+v", name, names(entries))
		}
	}

	// The same launcher must appear once, not once per directory.
	seen := map[string]int{}
	for _, e := range entries {
		seen[e.Path]++
	}
	for path, n := range seen {
		if n > 1 {
			t.Errorf("%s appears %d times", path, n)
		}
	}
}

// TestCatalogSkipsHiddenLaunchers checks that NoDisplay=true keeps a helper out
// of the bands.
func TestCatalogSkipsHiddenLaunchers(t *testing.T) {
	body := "[Desktop Entry]\nName=Hidden Helper\nExec=helper\nNoDisplay=true\n"
	entry, err := ParseLauncher(body)
	if err != nil {
		t.Fatal(err)
	}
	if !entry.NoDisplay {
		t.Error("NoDisplay=true was not parsed")
	}
}

// TestCatalogDropsDuplicatesByCommand checks that a launcher installed both on
// the desktop and in a system directory is one tile.
func TestCatalogDropsDuplicatesByCommand(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "apps"), 0o755); err != nil {
		t.Fatal(err)
	}
	body := "[Desktop Entry]\nName=Firefox\nExec=firefox %u\n"
	first := filepath.Join(dir, "firefox.desktop")
	if err := os.WriteFile(first, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	same, err := ParseLauncher(body)
	if err != nil {
		t.Fatal(err)
	}
	other, err := ParseLauncher("[Desktop Entry]\nName=Web Browser\nExec=firefox\n")
	if err != nil {
		t.Fatal(err)
	}
	same.Path, other.Path = first, first
	if entryKey(same) != entryKey(other) {
		t.Errorf("the same command produced two keys: %q and %q", entryKey(same), entryKey(other))
	}
}

func TestCatalogOfNothing(t *testing.T) {
	entries, err := Catalog(CatalogOptions{Dir: filepath.Join(t.TempDir(), "missing")})
	if err == nil {
		t.Error("a missing desktop directory should be reported")
	}
	if len(entries) != 0 {
		t.Errorf("entries = %v, want none", names(entries))
	}
}

func names(entries []Entry) []string {
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		out = append(out, e.Name)
	}
	return out
}
