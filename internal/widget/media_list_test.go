package widget

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func writeMediaFixture(t *testing.T, dir, name string, body []byte, age time.Duration) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir for %s: %v", path, err)
	}
	if err := os.WriteFile(path, body, 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	if age > 0 {
		when := time.Now().Add(-age)
		if err := os.Chtimes(path, when, when); err != nil {
			t.Fatalf("chtimes %s: %v", path, err)
		}
	}
	return path
}

func TestMediaListSkipsSourceFilesAndStaysQuick(t *testing.T) {
	root := t.TempDir()
	writeMediaFixture(t, root, "picture.png", []byte("\x89PNG\r\n\x1a\n"), 2*time.Hour)
	writeMediaFixture(t, root, "song.mp3", []byte("ID3\x04\x00"), 3*time.Hour)
	writeMediaFixture(t, root, "clip.mp4", []byte("\x00\x00\x00\x18ftypmp42"), time.Hour)
	writeMediaFixture(t, root, "notes.txt", []byte("just text"), 0)
	writeMediaFixture(t, root, "main.go", []byte("package main"), 0)
	writeMediaFixture(t, root, "sub/deep.gif", []byte("GIF89a"), 30*time.Minute)
	writeMediaFixture(t, filepath.Join(root, "node_modules"), "bundle.mp3", []byte("ID3\x04\x00"), 0)
	writeMediaFixture(t, filepath.Join(root, ".hidden"), "secret.mp3", []byte("ID3\x04\x00"), 0)

	start := time.Now()
	items := scanTree(root, 100)
	elapsed := time.Since(start)

	if elapsed > time.Second {
		t.Errorf("scanTree took %s, want under a second", elapsed)
	}
	var names []string
	for _, item := range items {
		names = append(names, item.Name)
	}
	want := []string{"deep.gif", "clip.mp4", "picture.png", "song.mp3"}
	if len(names) != len(want) {
		t.Fatalf("scanTree returned %v, want %v", names, want)
	}
	for i, name := range want {
		if names[i] != name {
			t.Fatalf("scanTree returned %v, want %v (newest first)", names, want)
		}
	}
	kinds := map[string]string{}
	for _, item := range items {
		kinds[item.Name] = item.Kind
	}
	for name, kind := range map[string]string{
		"picture.png": "photo", "song.mp3": "audio", "clip.mp4": "video", "deep.gif": "gif",
	} {
		if kinds[name] != kind {
			t.Errorf("kind of %s = %q, want %q", name, kinds[name], kind)
		}
	}
}

func TestMediaListRespectsLimitAndFoldsDuplicates(t *testing.T) {
	root := t.TempDir()
	writeMediaFixture(t, root, "a.mp3", []byte("ID3\x04\x00"), time.Hour)
	writeMediaFixture(t, root, "b.mp3", []byte("ID3\x04\x00"), 2*time.Hour)

	both := append(scanTree(root, 10), scanTree(root, 10)...)
	folded := dedupeItems(both, 10)
	if len(folded) != 2 {
		t.Fatalf("dedupeItems kept %d entries, want 2", len(folded))
	}
	if folded[0].Name != "a.mp3" {
		t.Errorf("dedupeItems put %q first, want the newest file", folded[0].Name)
	}
	if kept := dedupeItems(both, 1); len(kept) != 1 {
		t.Errorf("dedupeItems with limit 1 kept %d entries", len(kept))
	}
}

func TestMediaListReturnsFilesInTime(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home directory")
	}
	r := NewRegistry(fixedSession())
	done := make(chan []byte, 1)
	go func() {
		data, err := r.Call("media.list", json.RawMessage(`{"limit":300}`))
		if err != nil {
			t.Errorf("media.list: %v", err)
		}
		done <- data
	}()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatalf("media.list did not answer in 10s under %s", home)
	}
}
