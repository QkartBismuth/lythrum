package desk

import (
	"path/filepath"
	"strings"
)

// Group keys. The start screen shows one band per group, in this order.
const (
	GroupApps    = "apps"
	GroupGames   = "games"
	GroupMedia   = "media"
	GroupOffice  = "office"
	GroupDocs    = "documents"
	GroupArchive = "archive"
	GroupFolders = "folders"
	GroupFiles   = "files"
	// GroupWidgets holds the built-in live tiles.
	GroupWidgets = "widgets"
)

// DefaultGroupOrder is the order the bands appear in.
var DefaultGroupOrder = []string{
	GroupWidgets, GroupApps, GroupGames, GroupMedia, GroupOffice, GroupDocs, GroupArchive, GroupFolders, GroupFiles,
}

// GroupTitle maps a group key to its heading.
var GroupTitle = map[string]string{
	GroupApps:    "Applications",
	GroupGames:   "Games",
	GroupMedia:   "Media",
	GroupOffice:  "Office",
	GroupDocs:    "Documents",
	GroupArchive: "Archives",
	GroupFolders: "Folders",
	GroupFiles:   "Files",
	GroupWidgets: "Виджеты",
}

// desktopCategoryKeys maps the freedesktop menu categories onto our groups.
// The first match wins, so the map order matters.
var desktopCategoryKeys = []struct {
	match string
	key   string
}{
	{"Game", GroupGames},
	{"ActionGame", GroupGames},
	{"ArcadeGame", GroupGames},
	{"AudioVideo", GroupMedia},
	{"Audio", GroupMedia},
	{"Video", GroupMedia},
	{"Image", GroupMedia},
	{"Photography", GroupMedia},
	{"Office", GroupOffice},
	{"WordProcessor", GroupOffice},
	{"Spreadsheet", GroupOffice},
	{"Presentation", GroupOffice},
	{"TextEditor", GroupDocs},
	{"Viewer", GroupDocs},
	{"Documentation", GroupDocs},
	{"Network", GroupApps},
	{"System", GroupApps},
	{"Settings", GroupApps},
	{"Development", GroupApps},
	{"Utility", GroupApps},
	{"Core", GroupApps},
}

// extensionGroups maps a file extension onto a group.
var extensionGroups = map[string]string{
	".pdf": GroupDocs, ".odt": GroupDocs, ".ods": GroupOffice, ".odp": GroupDocs,
	".doc": GroupDocs, ".docx": GroupDocs, ".rtf": GroupDocs, ".txt": GroupDocs,
	".md": GroupDocs, ".tex": GroupDocs, ".djvu": GroupDocs, ".epub": GroupDocs,
	".xls": GroupOffice, ".xlsx": GroupOffice, ".ppt": GroupOffice, ".pptx": GroupOffice,
	".csv": GroupOffice, ".org": GroupDocs,

	".png": GroupMedia, ".jpg": GroupMedia, ".jpeg": GroupMedia, ".gif": GroupMedia,
	".bmp": GroupMedia, ".webp": GroupMedia, ".svg": GroupMedia, ".tif": GroupMedia,
	".tiff": GroupMedia, ".ico": GroupMedia, ".mp3": GroupMedia, ".flac": GroupMedia,
	".ogg": GroupMedia, ".opus": GroupMedia, ".wav": GroupMedia, ".m4a": GroupMedia,
	".mp4": GroupMedia, ".mkv": GroupMedia, ".webm": GroupMedia, ".avi": GroupMedia,
	".mov": GroupMedia, ".gifv": GroupMedia,

	".zip": GroupArchive, ".tar": GroupArchive, ".gz": GroupArchive,
	".bz2": GroupArchive, ".xz": GroupArchive, ".zst": GroupArchive,
	".7z": GroupArchive, ".rar": GroupArchive, ".lz": GroupArchive,
}

// GroupOf decides which band an entry belongs to: the launcher's own menu
// category for applications, the extension for files, and the kind otherwise.
func GroupOf(e Entry) string {
	switch e.Kind {
	case KindLive:
		if e.Widget != "" {
			return GroupWidgets
		}
		return GroupFiles
	case KindApp, KindShortcut:
		if key := groupFromCategories(e.Categories); key != "" {
			return key
		}
		return GroupApps
	case KindDir:
		return GroupFolders
	}
	if key, ok := extensionGroups[strings.ToLower(filepath.Ext(e.Path))]; ok {
		return key
	}
	return GroupFiles
}

// groupFromCategories maps a semicolon separated Categories= value.
func groupFromCategories(categories string) string {
	if categories == "" {
		return ""
	}
	for _, category := range strings.Split(categories, ";") {
		category = strings.TrimSpace(category)
		if category == "" {
			continue
		}
		for _, known := range desktopCategoryKeys {
			if strings.EqualFold(category, known.match) {
				return known.key
			}
		}
	}
	return ""
}

// Group is a band of the start screen.
type Group struct {
	// Key identifies the group, for example "media".
	Key string
	// Title is the heading shown above the band.
	Title string
	// First is the index of the first entry of the group in the flat list.
	First int
	// Count is how many entries the group holds.
	Count int
}

// Grouped buckets entries into bands, keeping the order given by order and
// appending any group that is not listed there at the end. Entries of an
// unlisted group are placed in the catch-all band instead of being dropped.
func Grouped(entries []Entry, order []string) ([]Group, []Entry) {
	if len(order) == 0 {
		order = DefaultGroupOrder
	}

	buckets := make(map[string][]Entry, len(order))
	for _, e := range entries {
		key := GroupOf(e)
		if !contains(order, key) {
			key = GroupFiles
		}
		buckets[key] = append(buckets[key], e)
	}

	// Groups appear in the configured order, with anything the configuration
	// forgot about appended so no entry is ever dropped.
	keys := make([]string, 0, len(order)+1)
	for _, key := range order {
		if len(buckets[key]) > 0 {
			keys = append(keys, key)
		}
	}
	for _, key := range DefaultGroupOrder {
		if len(buckets[key]) > 0 && !contains(keys, key) {
			keys = append(keys, key)
		}
	}

	groups := make([]Group, 0, len(keys))
	flat := make([]Entry, 0, len(entries))
	for _, key := range keys {
		items := buckets[key]
		groups = append(groups, Group{
			Key:   key,
			Title: TitleFor(key),
			First: len(flat),
			Count: len(items),
		})
		flat = append(flat, items...)
	}
	return groups, flat
}

// TitleFor returns the heading of a group key.
func TitleFor(key string) string {
	if title, ok := GroupTitle[key]; ok {
		return title
	}
	return key
}

func contains(list []string, want string) bool {
	for _, item := range list {
		if item == want {
			return true
		}
	}
	return false
}
