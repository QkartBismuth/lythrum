package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMoveIntoFolder(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "Папка")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(root, "файл.txt")
	if err := os.WriteFile(file, []byte("привет"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := moveIntoFolder(file, dir); err != nil {
		t.Fatalf("moveIntoFolder: %v", err)
	}
	if _, err := os.Stat(file); !os.IsNotExist(err) {
		t.Error("the file is still on the desktop")
	}
	got, err := os.ReadFile(filepath.Join(dir, "файл.txt"))
	if err != nil {
		t.Fatalf("the file did not arrive in the folder: %v", err)
	}
	if string(got) != "привет" {
		t.Errorf("content changed: %q", string(got))
	}
}

func TestMoveIntoFolderClash(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "Папка")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("старый"), 0o644); err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(root, "a.txt")
	if err := os.WriteFile(src, []byte("новый"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := moveIntoFolder(src, dir); err != nil {
		t.Fatalf("moveIntoFolder: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "a (1).txt")); err != nil {
		t.Errorf("the clashing name was not resolved: %v", err)
	}
	old, _ := os.ReadFile(filepath.Join(dir, "a.txt"))
	if string(old) != "старый" {
		t.Errorf("the existing file was overwritten: %q", string(old))
	}
}

func TestMoveFolderIntoItself(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "Папка")
	sub := filepath.Join(dir, "внутрь")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := moveIntoFolder(dir, sub); err == nil {
		t.Error("a folder must not be moved into its own child")
	}
}

func TestFolderAt(t *testing.T) {
	v := &view{
		zoom:   1,
		ox:     0,
		oy:     0,
		selset: map[int]bool{},
		icons: []boardIcon{
			{name: "Папка", path: "/tmp/Папка", isDir: true, x: 0, y: 0, w: 120, h: 146},
			{name: "файл", path: "/tmp/файл", x: 200, y: 0, w: 120, h: 146},
			{name: "..", path: "/tmp", isDir: true, up: true, x: 400, y: 0, w: 120, h: 146},
		},
	}
	if got := v.folderAt(60, 60); got != 0 {
		t.Errorf("the pointer is over the folder, got %d", got)
	}
	if got := v.folderAt(260, 60); got != -1 {
		t.Errorf("the pointer is over a file, got %d", got)
	}
	if got := v.folderAt(460, 60); got != -1 {
		t.Errorf("the parent shortcut is not a drop target, got %d", got)
	}
	// A folder that is dragged itself is not a target.
	v.selset[0] = true
	if got := v.folderAt(60, 60); got != -1 {
		t.Errorf("a dragged folder is not a target, got %d", got)
	}
}
