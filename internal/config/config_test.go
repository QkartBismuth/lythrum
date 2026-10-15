package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDefaultIsValid(t *testing.T) {
	cfg := Default()
	if err := cfg.Validate(); err != nil {
		t.Fatalf("default configuration is invalid: %v", err)
	}
	if cfg.Desktops != len(cfg.DesktopNames) {
		t.Errorf("defaults: %d desktops but %d names", cfg.Desktops, len(cfg.DesktopNames))
	}
}

func TestLoadPartialFileKeepsDefaults(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "lythrum.json")
	if err := os.WriteFile(path, []byte(`{"name":"Test","gap":4}`), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Name != "Test" {
		t.Errorf("name = %q", cfg.Name)
	}
	if cfg.Gap != 4 {
		t.Errorf("gap = %d, want 4", cfg.Gap)
	}
	if cfg.TitleBar != Default().TitleBar {
		t.Errorf("title_bar should have kept its default, got %d", cfg.TitleBar)
	}
	if len(cfg.Keybinds) != len(Default().Keybinds) {
		t.Errorf("keybinds should have kept their defaults")
	}
}

func TestLoadRejectsBadValues(t *testing.T) {
	dir := t.TempDir()
	cases := map[string]string{
		"negative gap":    `{"gap":-1}`,
		"zero desktops":   `{"desktops":0}`,
		"unknown layout":  `{"layout":"spiral"}`,
		"broken keybind":  `{"keybinds":{"Mod4+Nope":"quit"}}`,
		"empty name":      `{"name":""}`,
		"broken keybind2": `{"keybinds":{"Mod4+":"quit"}}`,
	}
	for name, body := range cases {
		path := filepath.Join(dir, strings.ReplaceAll(name, " ", "_")+".json")
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := Load(path); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
}

func TestLoadMissingFile(t *testing.T) {
	if _, err := Load(filepath.Join(t.TempDir(), "nope.json")); err == nil {
		t.Fatal("expected an error for a missing file")
	}
	cfg, err := Load("")
	if err != nil || cfg.Name != Default().Name {
		t.Errorf("empty path should return the defaults, got %v %v", cfg.Name, err)
	}
}

func TestDesktopNamesArePadded(t *testing.T) {
	cfg := Default()
	cfg.Desktops = 6
	cfg.DesktopNames = []string{"web"}
	names := cfg.DesktopNamesOrDefault()
	if len(names) != 6 {
		t.Fatalf("got %d names", len(names))
	}
	if names[0] != "web" || names[1] != "Desktop 2" {
		t.Errorf("names = %v", names)
	}
}

func TestResolvedKeybinds(t *testing.T) {
	cfg := Default()
	binds, err := cfg.ResolvedKeybinds()
	if err != nil {
		t.Fatalf("ResolvedKeybinds: %v", err)
	}
	seen := map[string]string{}
	for _, b := range binds {
		seen[b.Binding.String()] = b.Action
	}
	if seen["super+Return"] != "spawn" {
		t.Errorf("Mod4+Return did not resolve to super+Return: %v", seen)
	}
}

func TestExpandHome(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home directory")
	}
	if got := ExpandHome("~/bin/xterm"); got != filepath.Join(home, "bin/xterm") {
		t.Errorf("ExpandHome = %q", got)
	}
	if got := ExpandHome("/usr/bin/xterm"); got != "/usr/bin/xterm" {
		t.Errorf("ExpandHome touched an absolute path: %q", got)
	}
}

func TestLayoutKind(t *testing.T) {
	cfg := Default()
	if cfg.LayoutKind() != "stack" {
		t.Errorf("layout kind = %q", cfg.LayoutKind())
	}
	cfg.Layout = "Tiled"
	if cfg.LayoutKind() != "tiled" {
		t.Errorf("layout kind should be case-insensitive, got %q", cfg.LayoutKind())
	}
	opts := cfg.LayoutOptions()
	if opts.TitleBar != cfg.TitleBar || opts.Gap != cfg.Gap {
		t.Errorf("layout options = %+v", opts)
	}
}
