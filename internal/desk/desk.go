// Package desk discovers the launchers and files a user keeps on their
// desktop, which is what the window manager shows in its start screen.
package desk

import (
	"bufio"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// Kind classifies a desktop entry.
type Kind string

// Entry kinds.
const (
	KindApp      Kind = "app"  // a .desktop launcher
	KindDir      Kind = "dir"  // a directory
	KindFile     Kind = "file" // any other file
	KindShortcut Kind = "shortcut"
	// KindLive is a tile that runs a mini program instead of an application.
	KindLive Kind = "live"
)

// Entry is one tile on the start screen.
type Entry struct {
	// Name is what the tile shows: the localized Name of a launcher, or the
	// file name.
	Name string
	// Path is the absolute path of the entry.
	Path string
	Kind Kind
	// Exec is the command line of a launcher, already stripped of the
	// field codes such as %f.
	Exec string
	// Icon is the Icon= key of a launcher. Theme names are kept as they are;
	// resolving them needs an icon theme lookup, which this package does not
	// do.
	Icon string
	// Categories is the semicolon separated Categories= key of a launcher.
	Categories string
	// Widget names the live tile program of a launcher, taken from
	// X-Lythrum-Widget. An empty value means the tile is drawn from the icon.
	Widget string
}

// Dir returns the user's desktop directory. It follows XDG_DESKTOP_DIR from
// user-dirs.dirs and falls back to ~/Desktop.
func Dir() string {
	if d := os.Getenv("XDG_DESKTOP_DIR"); d != "" {
		if expanded := expandHome(d); expanded != "" {
			if isDir(expanded) {
				return expanded
			}
		}
	}
	if home, err := os.UserHomeDir(); err == nil {
		candidate := filepath.Join(home, "Desktop")
		if isDir(candidate) {
			return candidate
		}
	}
	if home, err := os.UserHomeDir(); err == nil {
		return filepath.Join(home, "Desktop")
	}
	return "."
}

// DirFrom resolves the desktop directory using an explicit XDG config file,
// which keeps the lookup testable.
func DirFrom(xdgConfig string) string {
	if d := readDesktopDirSetting(xdgConfig); d != "" {
		return expandHome(d)
	}
	if home, err := os.UserHomeDir(); err == nil {
		return filepath.Join(home, "Desktop")
	}
	return "."
}

// readDesktopDirSetting parses XDG_DESKTOP_DIR out of user-dirs.dirs, whose
// lines look like XDG_DESKTOP_DIR="$HOME/Desktop".
func readDesktopDirSetting(path string) string {
	if path == "" {
		return ""
	}
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok || strings.TrimSpace(key) != "XDG_DESKTOP_DIR" {
			continue
		}
		value = strings.TrimSpace(value)
		value = strings.Trim(value, `"`)
		return value
	}
	return ""
}

func expandHome(path string) string {
	if !strings.HasPrefix(path, "$HOME/") && path != "$HOME" {
		return path
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return path
	}
	if path == "$HOME" {
		return home
	}
	return filepath.Join(home, strings.TrimPrefix(path, "$HOME/"))
}

// Entries lists the desktop directory: launchers first, then directories,
// then files, each group sorted case-insensitively.
func Entries(dir string) ([]Entry, error) {
	items, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	entries := make([]Entry, 0, len(items))
	for _, item := range items {
		name := item.Name()
		if strings.HasPrefix(name, ".") {
			continue // hidden files are not tiles
		}
		path := filepath.Join(dir, name)
		switch {
		case strings.HasSuffix(name, ".desktop"):
			entry, err := parseLauncher(path)
			if err != nil || entry.Name == "" {
				// A launcher we cannot read still deserves a tile.
				entries = append(entries, Entry{Name: strings.TrimSuffix(name, ".desktop"), Path: path, Kind: KindApp})
				continue
			}
			entries = append(entries, entry)
		case item.IsDir():
			entries = append(entries, Entry{Name: name, Path: path, Kind: KindDir})
		case item.Type()&os.ModeSymlink != 0:
			target, err := os.Stat(path)
			if err == nil && target.IsDir() {
				entries = append(entries, Entry{Name: name, Path: path, Kind: KindDir})
				continue
			}
			entries = append(entries, Entry{Name: name, Path: path, Kind: KindFile})
		default:
			entries = append(entries, Entry{Name: name, Path: path, Kind: KindFile})
		}
	}
	Sort(entries)
	return entries, nil
}

// Sort orders entries by kind and then by name, which is what makes the grid
// predictable between runs.
func Sort(entries []Entry) {
	rank := map[Kind]int{KindLive: 0, KindApp: 1, KindShortcut: 2, KindDir: 3, KindFile: 4}
	sort.SliceStable(entries, func(i, j int) bool {
		if rank[entries[i].Kind] != rank[entries[j].Kind] {
			return rank[entries[i].Kind] < rank[entries[j].Kind]
		}
		return strings.ToLower(entries[i].Name) < strings.ToLower(entries[j].Name)
	})
}

// parseLauncher reads a freedesktop .desktop file. Only the keys the start
// screen needs are understood.
func parseLauncher(path string) (Entry, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Entry{}, err
	}
	entry, err := ParseLauncher(string(data))
	if err != nil {
		return Entry{}, err
	}
	entry.Path = path
	entry.Kind = KindApp
	if entry.Name == "" {
		entry.Name = strings.TrimSuffix(filepath.Base(path), ".desktop")
	}
	return entry, nil
}

// ParseLauncher parses the contents of a .desktop file.
func ParseLauncher(body string) (Entry, error) {
	entry := Entry{Kind: KindApp}
	locale := localePrefix()

	section := ""
	scanner := bufio.NewScanner(strings.NewReader(body))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			section = strings.Trim(line, "[]")
			continue
		}
		if section != "Desktop Entry" {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)

		// Name[ru]=... wins over Name=... when the locale matches.
		if strings.HasPrefix(key, "Name[") {
			if localized, ok := localizedName(key, value, locale); ok {
				entry.Name = localized
			}
			continue
		}

		switch key {
		case "Name":
			entry.Name = value
		case "Icon":
			entry.Icon = value
		case "Exec":
			entry.Exec = stripFieldCodes(value)
		case "Categories":
			entry.Categories = value
		case "X-Lythrum-Widget":
			entry.Widget = value
		case "Type":
			if value == "Link" {
				entry.Kind = KindShortcut
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return Entry{}, err
	}
	return entry, nil
}

// localizedName reports the value of a Name[lang]= key when the key matches
// the current locale.
func localizedName(key, value, locale string) (string, bool) {
	if locale == "" {
		return "", false
	}
	lang := strings.TrimSuffix(strings.TrimPrefix(key, "Name["), "]")
	if !localeMatches(lang, locale) {
		return "", false
	}
	return strings.TrimSpace(value), true
}

// localeMatches reports whether a desktop entry locale key selects the
// current locale. "ru" matches "ru_RU", but "de" does not.
func localeMatches(key, locale string) bool {
	key = strings.SplitN(key, "@", 2)[0]
	if key == locale {
		return true
	}
	language := strings.SplitN(locale, "_", 2)[0]
	return key == language
}

// localePrefix turns LANG or LC_ALL into a language code such as "ru".
func localePrefix() string {
	for _, env := range []string{"LC_ALL", "LC_MESSAGES", "LANG"} {
		v := os.Getenv(env)
		if v == "" || v == "C" || v == "POSIX" {
			continue
		}
		if i := strings.IndexAny(v, ".@"); i > 0 {
			v = v[:i]
		}
		return v
	}
	return ""
}

// stripFieldCodes removes the %f/%U/%u/%i placeholders of a desktop entry
// Exec line and returns the resulting command line.
func stripFieldCodes(exec string) string {
	fields := strings.Fields(exec)
	out := make([]string, 0, len(fields))
	for _, f := range fields {
		if strings.HasPrefix(f, "%") && !strings.HasPrefix(f, "%%") {
			continue
		}
		out = append(out, f)
	}
	return strings.Join(out, " ")
}

// Command returns the command line that starts the entry: the Exec of a
// launcher, or xdg-open for everything else.
func (e Entry) Command() []string {
	if e.Kind == KindApp && e.Exec != "" {
		fields := strings.Fields(e.Exec)
		return fields
	}
	for _, opener := range []string{"xdg-open", "gio", "kde-open5"} {
		if path, err := exec.LookPath(opener); err == nil {
			if opener == "gio" {
				return []string{path, "open", e.Path}
			}
			return []string{path, e.Path}
		}
	}
	return nil
}

// Launch starts the entry in the background.
func (e Entry) Launch() error {
	argv := e.Command()
	if len(argv) == 0 {
		return errNoOpener
	}
	path, err := exec.LookPath(argv[0])
	if err != nil {
		return err
	}
	cmd := exec.Command(path, argv[1:]...)
	if f, err := os.Open("/dev/null"); err == nil {
		cmd.Stdin = f
		defer f.Close()
	}
	cmd.Stdout, cmd.Stderr = nil, nil
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() { _ = cmd.Wait() }()
	return nil
}

// errNoOpener is returned when the desktop has neither a launcher command nor
// a helper able to open files.
var errNoOpener = errors.New("desk: no way to open the entry")

// Label wraps a tile caption into at most maxLines lines of maxRunes runes,
// appending an ellipsis when text had to be dropped.
func Label(name string, maxLines, maxRunes int) []string {
	name = strings.TrimSpace(name)
	if maxLines < 1 || maxRunes < 1 {
		return nil
	}
	if name == "" {
		return []string{""}
	}

	words := strings.Fields(name)
	lines := make([]string, 0, maxLines)
	cur := ""
	dropped := false

	for _, w := range words {
		candidate := w
		if cur != "" {
			candidate = cur + " " + w
		}
		if runeLen(candidate) <= maxRunes {
			cur = candidate
			continue
		}
		if cur == "" {
			// A single word longer than the caption: cut it.
			runes := []rune(w)
			cur = string(runes[:maxRunes-1]) + "…"
			dropped = len(runes) > maxRunes
			continue
		}
		lines = append(lines, cur)
		if len(lines) == maxLines {
			dropped = true
			cur = ""
			break
		}
		cur = w
	}
	if cur != "" && len(lines) < maxLines {
		lines = append(lines, cur)
	}
	if dropped && len(lines) > 0 {
		last := []rune(lines[len(lines)-1])
		for len(last) > 1 && runeLen(string(last)) > maxRunes-1 {
			last = last[:len(last)-1]
		}
		lines[len(lines)-1] = string(last) + "…"
	}
	return lines
}

func runeLen(s string) int { return len([]rune(s)) }

func isDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// ApplicationDirs are the folders desktop launchers live in.
func ApplicationDirs() []string {
	var dirs []string
	if home, err := os.UserHomeDir(); err == nil {
		dirs = append(dirs,
			filepath.Join(home, ".local", "share", "applications"),
			filepath.Join(home, "Desktop"))
	}
	dirs = append(dirs, "/usr/local/share/applications", "/usr/share/applications")
	return dirs
}

// Applications lists the installed launchers, newest directories first so a
// launcher on the desktop wins over the system one.
func Applications() []Entry {
	var out []Entry
	seen := map[string]bool{}
	for _, dir := range ApplicationDirs() {
		items, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, item := range items {
			if item.IsDir() || !strings.HasSuffix(item.Name(), ".desktop") {
				continue
			}
			path := filepath.Join(dir, item.Name())
			entry, err := parseLauncher(path)
			if err != nil || entry.Name == "" || entry.Exec == "" {
				continue
			}
			key := strings.ToLower(entry.Name)
			if seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, entry)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	return out
}

// SearchDirs are the folders the shell looks through when the user searches
// for a file.
func SearchDirs() []string {
	home, err := os.UserHomeDir()
	if err != nil {
		return []string{"."}
	}
	return []string{
		filepath.Join(home, "Desktop"),
		filepath.Join(home, "Documents"),
		filepath.Join(home, "Downloads"),
	}
}

// SearchFiles lists files from the search folders, up to limit entries so a
// large home directory cannot stall the shell.
func SearchFiles(limit int) []Entry {
	if limit <= 0 {
		limit = 800
	}
	var out []Entry
	for _, dir := range SearchDirs() {
		_ = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if len(out) >= limit {
				return fs.SkipAll
			}
			if d.IsDir() {
				if strings.HasPrefix(d.Name(), ".") && path != dir {
					return fs.SkipDir
				}
				return nil
			}
			out = append(out, Entry{Name: d.Name(), Path: path, Kind: KindFile})
			return nil
		})
	}
	return out
}

// EntryAt builds an entry for one path, whatever it is: a launcher, a folder
// or a file. The search bar uses it to open what the user picked.
func EntryAt(path string) (Entry, error) {
	info, err := os.Stat(path)
	if err != nil {
		return Entry{}, err
	}
	if !info.IsDir() && strings.HasSuffix(path, ".desktop") {
		return parseLauncher(path)
	}
	name := filepath.Base(path)
	if info.IsDir() {
		return Entry{Name: name, Path: path, Kind: KindDir}, nil
	}
	return Entry{Name: name, Path: path, Kind: KindFile}, nil
}
