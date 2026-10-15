package desk

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseLauncher(t *testing.T) {
	body := `[Desktop Entry]
Type=Application
Version=1.0
Name=Text Editor
Name[ru]=Текстовый редактор
Comment=Edit text files
Exec=gnome-text-editor %U
Icon=accessories-text-editor
Terminal=false
Categories=Utility;TextEditor;

[Desktop Action new]
Name=New Window
Exec=gnome-text-editor
`
	entry, err := ParseLauncher(body)
	if err != nil {
		t.Fatalf("ParseLauncher: %v", err)
	}
	if entry.Name != "Text Editor" {
		t.Errorf("Name = %q", entry.Name)
	}
	if entry.Exec != "gnome-text-editor" {
		t.Errorf("Exec = %q, want the field code stripped", entry.Exec)
	}
	if entry.Icon != "accessories-text-editor" {
		t.Errorf("Icon = %q", entry.Icon)
	}
	if entry.Kind != KindApp {
		t.Errorf("Kind = %q", entry.Kind)
	}
}

func TestParseLauncherLocalizedName(t *testing.T) {
	body := "[Desktop Entry]\nName=Calculator\nName[de]=Rechner\nName[ru]=Калькулятор\n"
	t.Setenv("LC_ALL", "ru_RU.UTF-8")
	entry, err := ParseLauncher(body)
	if err != nil {
		t.Fatalf("ParseLauncher: %v", err)
	}
	if entry.Name != "Калькулятор" {
		t.Errorf("Name = %q, want the Russian one", entry.Name)
	}

	t.Setenv("LC_ALL", "fr_FR.UTF-8")
	entry, _ = ParseLauncher(body)
	if entry.Name != "Calculator" {
		t.Errorf("Name = %q, want the untranslated fallback", entry.Name)
	}
}

func TestParseLauncherIgnoresOtherSections(t *testing.T) {
	body := `[Desktop Entry]
Name=Real

[Desktop Action hidden]
Name=Hidden Action
Exec=nothing
`
	entry, _ := ParseLauncher(body)
	if entry.Name != "Real" {
		t.Errorf("Name = %q, want the entry from the main section", entry.Name)
	}
}

func TestParseLauncherShortcuts(t *testing.T) {
	entry, _ := ParseLauncher("[Desktop Entry]\nType=Link\nName=Doc\nURL=https://example.org\n")
	if entry.Kind != KindShortcut {
		t.Errorf("Kind = %q, want %q", entry.Kind, KindShortcut)
	}
}

func TestStripFieldCodes(t *testing.T) {
	cases := map[string]string{
		"app %U":             "app",
		"app --flag %f":      "app --flag",
		"app %f":             "app",
		"app":                "app",
		"env FOO=1 app %U":   "env FOO=1 app",
		"app %% literal":     "app %% literal",
		"sh -c 'echo hi' %i": "sh -c 'echo hi'",
	}
	for in, want := range cases {
		if got := stripFieldCodes(in); got != want {
			t.Errorf("stripFieldCodes(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestDirFrom(t *testing.T) {
	dir := t.TempDir()
	xdg := filepath.Join(dir, "user-dirs.dirs")
	body := "# comment\n" +
		"XDG_DESKTOP_DIR=\"$HOME/Desktop\"\n" +
		"XDG_DOWNLOAD_DIR=\"$HOME/Downloads\"\n"
	if err := os.WriteFile(xdg, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", "/home/tester")
	if got := DirFrom(xdg); got != "/home/tester/Desktop" {
		t.Errorf("DirFrom = %q", got)
	}

	empty := filepath.Join(dir, "missing.dirs")
	if got := DirFrom(empty); got != "/home/tester/Desktop" {
		t.Errorf("DirFrom with no file = %q", got)
	}
}

func TestEntries(t *testing.T) {
	dir := t.TempDir()
	files := map[string]string{
		"zebra.txt":      "",
		"Alpha.txt":      "",
		"editor.desktop": "[Desktop Entry]\nType=Application\nName=Editor\nExec=ed %f\n",
		"broken.desktop": "not a desktop file",
		".hidden":        "",
		"sub/keep.txt":   "",
		"photo.png":      "",
		"stray.desktop":  "garbage",
	}
	for name, body := range files {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	entries, err := Entries(dir)
	if err != nil {
		t.Fatalf("Entries: %v", err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name)
	}
	for _, hidden := range []string{".hidden", "keep.txt"} {
		for _, n := range names {
			if n == hidden {
				t.Errorf("hidden or nested file %q showed up in %v", hidden, names)
			}
		}
	}

	// Launchers first, then directories, then files, sorted by name.
	if len(entries) < 5 {
		t.Fatalf("got %d entries: %v", len(entries), names)
	}
	if entries[0].Name != "broken" || entries[1].Name != "Editor" || entries[2].Name != "stray" {
		t.Errorf("launchers should come first and sort case-insensitively: %v", names)
	}
	for i := 1; i < len(entries); i++ {
		if entries[i-1].Kind > entries[i].Kind && rankOf(entries[i-1].Kind) > rankOf(entries[i].Kind) {
			t.Errorf("entries out of kind order at %d: %v", i, names)
		}
	}

	var dirFound, fileFound bool
	for _, e := range entries {
		if e.Name == "sub" && e.Kind == KindDir {
			dirFound = true
		}
		if e.Name == "photo.png" && e.Kind == KindFile {
			fileFound = true
		}
	}
	if !dirFound || !fileFound {
		t.Errorf("directory or file entry missing: %v", names)
	}
}

func TestEntriesMissingDir(t *testing.T) {
	if _, err := Entries(filepath.Join(t.TempDir(), "nope")); err == nil {
		t.Fatal("expected an error for a missing directory")
	}
}

func TestCommand(t *testing.T) {
	app := Entry{Kind: KindApp, Path: "/tmp/x", Exec: "run-me --flag"}
	if got := app.Command(); strings.Join(got, " ") != "run-me --flag" {
		t.Errorf("Command = %v", got)
	}
	file := Entry{Kind: KindFile, Path: "/tmp/x.txt"}
	if got := file.Command(); len(got) == 0 {
		t.Error("a file should get an opener command")
	}
	if got := file.Command(); got[len(got)-1] != file.Path {
		t.Errorf("opener should end with the path, got %v", got)
	}
}

func TestLabelWraps(t *testing.T) {
	lines := Label("Положение Лига твоего имени", 2, 12)
	if len(lines) != 2 {
		t.Fatalf("got %d lines: %q", len(lines), lines)
	}
	for _, l := range lines {
		if runeLen(l) > 12 {
			t.Errorf("line %q exceeds the caption width", l)
		}
	}
	if !strings.HasSuffix(lines[len(lines)-1], "…") {
		t.Errorf("dropped text should end with an ellipsis: %q", lines)
	}

	if got := Label("short", 2, 20); len(got) != 1 || got[0] != "short" {
		t.Errorf("Label = %q", got)
	}
	if got := Label("", 2, 20); len(got) != 1 || got[0] != "" {
		t.Errorf("empty label = %q", got)
	}
	// A word longer than the caption is cut, with an ellipsis, onto one line.
	if got := Label("abcdefghijklmnopqrstuvwxyz", 2, 10); len(got) != 1 || !strings.HasSuffix(got[0], "…") {
		t.Errorf("long single word = %q", got)
	}
	if got := Label("x", 0, 10); got != nil {
		t.Errorf("no lines allowed should return nil, got %q", got)
	}
}

func TestSortIsStable(t *testing.T) {
	entries := []Entry{
		{Name: "b", Kind: KindFile},
		{Name: "a", Kind: KindFile},
		{Name: "z", Kind: KindApp},
	}
	Sort(entries)
	if entries[0].Name != "z" || entries[1].Name != "a" || entries[2].Name != "b" {
		t.Errorf("Sort = %+v", entries)
	}
}

func rankOf(k Kind) int {
	switch k {
	case KindApp:
		return 0
	case KindShortcut:
		return 1
	case KindDir:
		return 2
	}
	return 3
}
