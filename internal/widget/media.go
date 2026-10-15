package widget

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"lythrum/internal/media"
)

// ErrNoFile means the page asked for something the shell will not serve.
var ErrNoFile = errors.New("widget: file is not available")

// mediaRoots are the folders widgets may read from. Anything outside them is
// refused, so a page cannot walk the whole disk.
func mediaRoots() []string {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	return []string{home}
}

// MediaItem is one file the media widgets can show.
type MediaItem struct {
	Name     string `json:"name"`
	Path     string `json:"path"`
	URL      string `json:"url"`
	Kind     string `json:"kind"`
	Size     int64  `json:"size"`
	Modified int64  `json:"modified"`
	Hidden   bool   `json:"hidden"`
}

// allowedPath makes sure a path is inside the home directory.
func allowedPath(path string) (string, error) {
	if path == "" {
		return "", ErrNoFile
	}
	clean := filepath.Clean(path)
	if !filepath.IsAbs(clean) {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", ErrNoFile
		}
		clean = filepath.Join(home, clean)
	}
	abs, err := filepath.Abs(clean)
	if err != nil {
		return "", ErrNoFile
	}
	for _, root := range mediaRoots() {
		rootAbs, err := filepath.Abs(root)
		if err != nil {
			continue
		}
		if abs == rootAbs || strings.HasPrefix(abs, rootAbs+string(os.PathSeparator)) {
			return abs, nil
		}
	}
	return "", fmt.Errorf("%w: %s is outside the home directory", ErrNoFile, path)
}

// FileURL is the address a page uses to load a local file.
func FileURL(path string) string {
	return "lythrum://app/file/" + base64.RawURLEncoding.EncodeToString([]byte(path))
}

// mediaFileItem describes one file for the widget list.
func mediaFileItem(path string) MediaItem {
	name := filepath.Base(path)
	item := MediaItem{Name: name, Path: path, URL: FileURL(path), Kind: "file"}
	info, err := os.Stat(path)
	if err == nil {
		item.Size = info.Size()
		item.Modified = info.ModTime().Unix()
	}
	switch media.Classify(path) {
	case media.KindStill:
		item.Kind = "photo"
	case media.KindGIF:
		item.Kind = "gif"
	case media.KindVideo:
		item.Kind = "video"
	case media.KindAudio:
		item.Kind = "audio"
	}
	return item
}

// scanFolder lists the media files of one folder, without descending into
// sub folders so a widget stays quick.
func scanFolder(dir string, limit int) []MediaItem {
	var out []MediaItem
	entries, err := os.ReadDir(dir)
	if err != nil {
		return out
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		if entry.IsDir() {
			continue
		}
		if isMediaKind(media.Classify(path)) {
			out = append(out, mediaFileItem(path))
		}
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	sortByNewest(out)
	return out
}

// isMediaKind reports whether a classified file belongs on a media tile.
func isMediaKind(kind media.Kind) bool {
	switch kind {
	case media.KindStill, media.KindGIF, media.KindVideo, media.KindAudio:
		return true
	}
	return false
}

// dedupeItems drops repeated paths and keeps the newest entries.
func dedupeItems(items []MediaItem, limit int) []MediaItem {
	seen := make(map[string]bool, len(items))
	out := make([]MediaItem, 0, len(items))
	for _, item := range items {
		if seen[item.Path] {
			continue
		}
		seen[item.Path] = true
		out = append(out, item)
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out
}

// sortByNewest puts the files a user touched last in front, which is what a
// widget wants to show.
func sortByNewest(items []MediaItem) {
	sort.SliceStable(items, func(i, j int) bool { return items[i].Modified > items[j].Modified })
}

// scanBudget caps the work of one listing: without it a home directory that
// holds a whole source tree would keep the page waiting.
const (
	scanBudget   = 2 * time.Second
	scanFileCap  = 40000
	scanDirDepth = 3
)

// scanTree lists media files below a folder, up to limit entries. It stays
// close to the root: a widget wants the pictures next door, not every file of a
// development tree.
func scanTree(root string, limit int) []MediaItem {
	return scanTreeDepth(root, limit, scanDirDepth)
}

func scanTreeDepth(root string, limit, depth int) []MediaItem {
	var out []MediaItem
	deadline := time.Now().Add(scanBudget)
	visited := 0
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if limit > 0 && len(out) >= limit {
			return fs.SkipAll
		}
		if visited >= scanFileCap || time.Now().After(deadline) {
			return fs.SkipAll
		}
		visited++
		if d.IsDir() {
			if path != root && (skipUnderscore(d.Name()) || depth <= 0) {
				return fs.SkipDir
			}
			return nil
		}
		if skipUnderscore(d.Name()) {
			return nil
		}
		if isMediaKind(media.Classify(path)) {
			out = append(out, mediaFileItem(path))
		}
		return nil
	})
	sortByNewest(out)
	return out
}

// mediaList answers the widget list of pictures, videos and songs.
func mediaList(s Session, params json.RawMessage) (any, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	var request struct {
		Folder string `json:"folder"`
		Limit  int    `json:"limit"`
	}
	if len(params) > 0 {
		_ = json.Unmarshal(params, &request)
	}
	limit := request.Limit
	if limit <= 0 {
		limit = 400
	}

	folder := strings.TrimSpace(request.Folder)
	var items []MediaItem
	if folder != "" {
		root, err := allowedPath(folder)
		if err != nil {
			return nil, err
		}
		items = scanTree(root, limit)
	} else {
		// Only the folders a media file is expected in. Walking the whole home
		// directory would stall the browser for minutes on a large disk.
		for _, name := range []string{"Pictures", "Videos", "Music", "Downloads", "Desktop"} {
			dir := filepath.Join(home, name)
			if info, err := os.Stat(dir); err != nil || !info.IsDir() {
				continue
			}
			items = append(items, scanTree(dir, limit-len(items))...)
			if len(items) >= limit {
				break
			}
		}
		// The same file can sit in two of the watched folders, and a shared
		// folder may have been cut short by its own budget.
		sortByNewest(items)
		items = dedupeItems(items, limit)
	}

	photos, videos, tracks := splitByKind(items)
	return map[string]any{
		"folder": folder,
		"photos": photos,
		"videos": videos,
		"tracks": tracks,
		"all":    items,
		"home":   home,
	}, nil
}

func splitByKind(items []MediaItem) (photos, videos, tracks []MediaItem) {
	for _, item := range items {
		switch item.Kind {
		case "photo", "gif":
			photos = append(photos, item)
		case "video":
			videos = append(videos, item)
		case "audio":
			tracks = append(tracks, item)
		}
	}
	return
}

// mediaFolders lists the folders a widget may browse.
func mediaFolders(s Session, _ json.RawMessage) (any, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	var out []string
	for _, name := range []string{"Pictures", "Videos", "Music", "Downloads", "Desktop", "Documents"} {
		path := filepath.Join(home, name)
		if info, err := os.Stat(path); err == nil && info.IsDir() {
			out = append(out, path)
		}
	}
	return map[string]any{"home": home, "folders": out}, nil
}

// mimeForFile guesses a content type for the file stream.
func mimeForFile(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".webp":
		return "image/webp"
	case ".gif":
		return "image/gif"
	case ".avif":
		return "image/avif"
	case ".bmp":
		return "image/bmp"
	case ".svg":
		return "image/svg+xml"
	case ".mp4", ".m4v":
		return "video/mp4"
	case ".webm":
		return "video/webm"
	case ".mkv":
		return "video/x-matroska"
	case ".mov":
		return "video/quicktime"
	case ".mp3":
		return "audio/mpeg"
	case ".flac":
		return "audio/flac"
	case ".ogg", ".oga":
		return "audio/ogg"
	case ".opus":
		return "audio/opus"
	case ".wav":
		return "audio/wav"
	case ".m4a":
		return "audio/mp4"
	}
	return "application/octet-stream"
}

// parseRange reads a Range header such as "bytes=0-1023".
func parseRange(header string, size int64) (start, end int64, ok bool) {
	if !strings.HasPrefix(header, "bytes=") {
		return 0, 0, false
	}
	spec := strings.TrimPrefix(header, "bytes=")
	if strings.Contains(spec, ",") {
		spec = strings.SplitN(spec, ",", 2)[0]
	}
	parts := strings.SplitN(spec, "-", 2)
	if len(parts) != 2 {
		return 0, 0, false
	}
	if parts[0] == "" {
		// A suffix range asks for the last bytes.
		n, err := strconv.ParseInt(parts[1], 10, 64)
		if err != nil || n <= 0 {
			return 0, 0, false
		}
		if n > size {
			n = size
		}
		return size - n, size - 1, true
	}
	start, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || start < 0 || start >= size {
		return 0, 0, false
	}
	end = size - 1
	if parts[1] != "" {
		if v, err := strconv.ParseInt(parts[1], 10, 64); err == nil && v < end {
			end = v
		}
	}
	if end < start {
		return 0, 0, false
	}
	return start, end, true
}

// StreamFile serves a local file to a page, honouring range requests so audio
// and video can seek.
func StreamFile(rawURL string, fileRange string) (string, []byte, int, string, error) {
	const prefix = "lythrum://app/file/"
	if !strings.HasPrefix(rawURL, prefix) {
		return "", nil, 0, "", ErrNoFile
	}
	encoded := strings.TrimPrefix(rawURL, prefix)
	decoded, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return "", nil, 0, "", ErrNoFile
	}
	path, err := allowedPath(string(decoded))
	if err != nil {
		return "", nil, 0, "", err
	}
	file, err := os.Open(path)
	if err != nil {
		return "", nil, 0, "", err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return "", nil, 0, "", err
	}
	mime := mimeForFile(path)

	start, end := int64(0), info.Size()-1
	status := 200
	contentRange := ""
	if start, end, ok := parseRange(fileRange, info.Size()); ok {
		status = 206
		contentRange = fmt.Sprintf("bytes %d-%d/%d", start, end, info.Size())
	} else if fileRange != "" {
		// An unusable range means the whole file, which keeps playback alive.
		start = 0
	}
	if _, err := file.Seek(start, io.SeekStart); err != nil {
		return "", nil, 0, "", err
	}
	length := end - start + 1
	if length < 0 {
		length = 0
	}
	if length > 256<<20 {
		length = 256 << 20
	}
	body := make([]byte, length)
	if _, err := io.ReadFull(file, body); err != nil && err != io.ErrUnexpectedEOF {
		return "", nil, 0, "", err
	}
	return mime, body, status, contentRange, nil
}

// busyFolders never hold anything a person wants to see in a widget.
var busyFolders = map[string]bool{
	"node_modules": true, "target": true, "build": true, "dist": true,
	"vendor": true, "__pycache__": true, "out": true, "bin": true, "obj": true,
}

// skipUnderscore reports whether a folder or file stays out of the index.
func skipUnderscore(name string) bool {
	return strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_") || busyFolders[name]
}
