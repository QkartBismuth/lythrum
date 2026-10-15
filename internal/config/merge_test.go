package config

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestMergeKeepsUnrelatedValues(t *testing.T) {
	cfg := Default()
	if err := cfg.Merge([]byte(`{"wallpaper_fps": 24}`)); err != nil {
		t.Fatalf("merge: %v", err)
	}
	if cfg.WallpaperFPS != 24 {
		t.Errorf("wallpaper_fps = %d, want 24", cfg.WallpaperFPS)
	}
	if cfg.GridGap != Default().GridGap {
		t.Errorf("grid_gap changed to %d by an unrelated patch", cfg.GridGap)
	}
}

func TestMergeRejectsBadValuesAndKeepsTheOldConfig(t *testing.T) {
	cases := map[string]string{
		"wallpaper fps too high": `{"wallpaper_fps": 900}`,
		"dim out of range":       `{"wallpaper_dim": 5}`,
		"unknown group":          `{"grid_groups": ["nope"]}`,
		"bad key combination":    `{"keybinds": {"Mod4+": "quit"}}`,
		"wrong type":             `{"grid_gap": "wide"}`,
		"empty desktops":         `{"desktops": 0}`,
	}
	for name, patch := range cases {
		cfg := Default()
		before, err := json.Marshal(cfg)
		if err != nil {
			t.Fatal(err)
		}
		err = cfg.Merge([]byte(patch))
		if err == nil {
			t.Errorf("%s: merge accepted %s", name, patch)
			continue
		}
		after, err := json.Marshal(cfg)
		if err != nil {
			t.Fatal(err)
		}
		if string(after) != string(before) {
			t.Errorf("%s: a rejected patch changed the configuration", name)
		}
	}
}

func TestMergeAcceptsAWholeSettingsForm(t *testing.T) {
	cfg := Default()
	patch := `{
		"desktops": 6,
		"desktop_names": ["Work", "Web", "Mail", "Chat", "Play", "Rest"],
		"gap": 6, "border": 3, "title_bar": 20,
		"active_border": "#ffcc00", "title_background": "#222222",
		"layout": "tiled",
		"grid_groups": ["widgets", "apps", "media", "files"],
		"grid_live_fps": 8, "grid_live_limit": 4,
		"keybinds": {"Mod4+s": "search", "Mod4+l": "power", "Mod4+space": "start_screen"},
		"terminal": ["foot"], "menu": ["wofi", "--dmenu"],
		"wallpaper_image": "/tmp/wall.png", "wallpaper_enabled": true
	}`
	if err := cfg.Merge([]byte(patch)); err != nil {
		t.Fatalf("merge: %v", err)
	}
	if cfg.Desktops != 6 || len(cfg.DesktopNames) != 6 {
		t.Errorf("desktops = %d names = %v", cfg.Desktops, cfg.DesktopNames)
	}
	if string(cfg.LayoutKind()) != "tiled" {
		t.Errorf("layout = %q, want tiled", cfg.Layout)
	}
	if got := cfg.Keybinds["Mod4+l"]; got != "power" {
		t.Errorf("keybind Mod4+l = %q, want power", got)
	}
	if len(cfg.Menu) != 2 || cfg.Menu[1] != "--dmenu" {
		t.Errorf("menu = %v", cfg.Menu)
	}
	if cfg.DesktopNamesOrDefault()[3] != "Chat" {
		t.Errorf("desktop names = %v", cfg.DesktopNamesOrDefault())
	}
}

func TestDefaultKeybindsOpenThePowerMenu(t *testing.T) {
	binds := DefaultKeybinds()
	if binds["Mod4+space"] != "start_screen" {
		t.Errorf("Mod4+space = %q, want start_screen", binds["Mod4+space"])
	}
	if binds["Mod4+l"] != "power" {
		t.Errorf("Mod4+l = %q, want power", binds["Mod4+l"])
	}
	for _, combo := range []string{"Mod4+Left", "Mod4+Right"} {
		if _, ok := binds[combo]; !ok {
			t.Errorf("%s should be bound now that Mod4+l took over", combo)
		}
	}
	if _, err := (&Config{Keybinds: binds}).ResolvedKeybinds(); err != nil {
		t.Errorf("the default bindings must parse: %v", err)
	}
}

func TestMergeReportsTheOffendingKey(t *testing.T) {
	cfg := Default()
	err := cfg.Merge([]byte(`{"keybinds": {"Mod4+nope": "quit"}}`))
	if err == nil {
		t.Fatal("an unknown key name must be rejected")
	}
	if !strings.Contains(err.Error(), "keybind") {
		t.Errorf("error %q should name the key binding", err)
	}
}

func TestShellConfigRoundTripsThroughJSON(t *testing.T) {
	cfg := Default()
	cfg.WallpaperImage = "/tmp/a.png"
	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	var back Config
	if err := json.Unmarshal(data, &back); err != nil {
		t.Fatal(err)
	}
	if back.WallpaperImage != cfg.WallpaperImage || back.GridLiveLimit != cfg.GridLiveLimit {
		t.Errorf("round trip changed the settings: %+v", back)
	}
}
