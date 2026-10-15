// Package config loads the window manager settings from a JSON file. The
// schema is intentionally small: everything has a default, so an empty file is
// a valid configuration.
package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"lythrum/internal/desk"
	"lythrum/internal/layout"
	"lythrum/internal/x11"
)

// Config is the full window manager configuration.
type Config struct {
	// Name is published to the session as the window manager name.
	Name string `json:"name"`
	// Terminal is launched by the "spawn" action.
	Terminal []string `json:"terminal"`
	// Menu is the dmenu-compatible launcher.
	Menu []string `json:"menu"`
	// Layout is the initial layout.
	Layout string `json:"layout"`
	// Desktops is the number of virtual desktops.
	Desktops int `json:"desktops"`
	// DesktopNames names each desktop.
	DesktopNames []string `json:"desktop_names"`
	// Gap is the space between windows, Border the space around them,
	// TitleBar the height of the frame title bar.
	Gap      int `json:"gap"`
	Border   int `json:"border"`
	TitleBar int `json:"title_bar"`
	// BorderColors are the frame colours as #rrggbb values.
	ActiveBorder    string `json:"active_border"`
	InactiveBorder  string `json:"inactive_border"`
	TitleBackground string `json:"title_background"`
	TitleForeground string `json:"title_foreground"`
	// Start-screen grid: spacing, tile shape and colours.
	GridGap     int     `json:"grid_gap"`
	GridBorder  int     `json:"grid_border"`
	GridAspect  float64 `json:"grid_aspect"`
	GridMaxCell int     `json:"grid_max_cell"`
	GridMinCell int     `json:"grid_min_cell"`
	// GridLarge draws the first tile of a group twice as wide and tall, and
	// GridLargeMin is how many entries that group needs first.
	GridLarge    bool `json:"grid_large"`
	GridLargeMin int  `json:"grid_large_min"`
	// GridHeaderHeight is the vertical space a group heading takes.
	GridHeaderHeight int `json:"grid_header_height"`
	// GridLiveLimit caps how many live tile pages may run at once, and
	// GridLiveFPS is how often they refresh.
	GridLiveLimit int `json:"grid_live_limit"`
	GridLiveFPS   int `json:"grid_live_fps"`
	// Wallpaper settings. WallpaperImage may point at a picture, an animated
	// GIF or a video; WallpaperFPS and WallpaperDim tune playback.
	WallpaperEnabled bool    `json:"wallpaper_enabled"`
	WallpaperImage   string  `json:"wallpaper_image"`
	WallpaperFPS     int     `json:"wallpaper_fps"`
	WallpaperDim     float64 `json:"wallpaper_dim"`
	// GridWidgets shows the built-in live tiles in the start screen.
	GridWidgets bool `json:"grid_widgets"`
	// GridApps adds the installed launchers to the start screen, and
	// GridHomeFiles the files of the home folders. Without them the bands of
	// applications, games and documents stay empty on a fresh session.
	GridApps      bool `json:"grid_apps"`
	GridHomeFiles bool `json:"grid_home_files"`
	// GridFileLimit caps how many home files the start screen collects.
	GridFileLimit int `json:"grid_file_limit"`
	// DesktopIcons draws the icons of the desktop directory over the wallpaper.
	DesktopIcons bool `json:"desktop_icons"`
	// GridGroups lists the bands and their order, by key.
	GridGroups     []string `json:"grid_groups"`
	GridBackground string   `json:"grid_background"`
	GridTile       string   `json:"grid_tile"`
	GridSelection  string   `json:"grid_selection"`
	GridAccent     string   `json:"grid_accent"`
	GridText       string   `json:"grid_text"`
	GridMuted      string   `json:"grid_muted"`

	// FollowMouse makes the pointer follow the focused window.
	FollowMouse bool `json:"follow_mouse"`
	// FocusNewWindows focuses a window as soon as it is mapped.
	FocusNewWindows bool `json:"focus_new_windows"`
	// Keybinds maps a key combination to an action name with arguments.
	Keybinds map[string]string `json:"keybinds"`
	// Startup lists commands run once the window manager is up.
	Startup []string `json:"startup"`
}

// Default returns the built-in configuration.
func Default() Config {
	return Config{
		Name:            "Lythrum",
		Terminal:        []string{"xterm"},
		Menu:            []string{"dmenu_run"},
		Layout:          string(layout.LayoutStack),
		Desktops:        4,
		DesktopNames:    []string{"One", "Two", "Three", "Four"},
		Gap:             0,
		Border:          2,
		TitleBar:        18,
		ActiveBorder:    "#88c0d0",
		InactiveBorder:  "#4c566a",
		TitleBackground: "#2e3440",
		TitleForeground: "#eceff4",

		GridGap:          8,
		GridLiveLimit:    6,
		GridLiveFPS:      12,
		GridWidgets:      true,
		GridApps:         true,
		GridHomeFiles:    true,
		GridFileLimit:    400,
		DesktopIcons:     true,
		WallpaperEnabled: true,
		WallpaperFPS:     12,
		WallpaperDim:     0,
		GridBorder:       24,
		GridAspect:       1.0,
		GridMaxCell:      132,
		GridMinCell:      72,
		GridLarge:        true,
		GridLargeMin:     3,
		GridHeaderHeight: 34,
		GridGroups:       desk.DefaultGroupOrder[:],
		GridBackground:   "#1c1f26",
		GridTile:         "#2e3440",
		GridSelection:    "#434c5e",
		GridAccent:       "#88c0d0",
		GridText:         "#eceff4",
		GridMuted:        "#8f9aad",

		FollowMouse:     true,
		FocusNewWindows: true,
		Keybinds:        DefaultKeybinds(),
	}
}

// DefaultKeybinds returns the built-in key bindings.
func DefaultKeybinds() map[string]string {
	return map[string]string{
		"Mod4+space":   "start_screen",
		"Mod4+Return":  "spawn",
		"Mod4+s":       "search",
		"Mod4+d":       "spawn_menu",
		"Mod4+j":       "focus_next",
		"Mod4+k":       "focus_prev",
		"Mod4+q":       "close_focused",
		"Mod4+Shift+q": "quit",
		// Super+L opens the power menu; focus moves on the arrow keys so the
		// letter pair stays free.
		"Mod4+l":     "power",
		"Mod4+Left":  "focus_left",
		"Mod4+Right": "focus_right",
		"Mod4+1":     "desktop_1",
		"Mod4+2":     "desktop_2",
		"Mod4+3":     "desktop_3",
		"Mod4+4":     "desktop_4",
	}
}

// Load reads a configuration file and fills in every value the file leaves
// unset from the defaults.
func Load(path string) (Config, error) {
	cfg := Default()
	if path == "" {
		return cfg, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return cfg, fmt.Errorf("config: %w", err)
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return Default(), fmt.Errorf("config: %s: %w", path, err)
	}
	cfg.expandPaths()
	if err := cfg.Validate(); err != nil {
		return Default(), err
	}
	return cfg, nil
}

// expandPaths resolves ~/ in command paths.
func (c *Config) expandPaths() {
	expand := func(cmd []string) []string {
		out := make([]string, len(cmd))
		for i, a := range cmd {
			out[i] = ExpandHome(a)
		}
		return out
	}
	c.Terminal = expand(c.Terminal)
	c.Menu = expand(c.Menu)
}

// ExpandHome replaces a leading ~ with the user's home directory.
func ExpandHome(path string) string {
	if !strings.HasPrefix(path, "~/") {
		return path
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return path
	}
	return filepath.Join(home, path[2:])
}

// Validate reports configuration mistakes that would otherwise surface as
// confusing runtime failures.
func (c *Config) Validate() error {
	if c.Name == "" {
		return fmt.Errorf("config: name must not be empty")
	}
	if c.Desktops < 1 {
		return fmt.Errorf("config: desktops must be at least 1, got %d", c.Desktops)
	}
	if c.Desktops > 32 {
		return fmt.Errorf("config: desktops must be at most 32, got %d", c.Desktops)
	}
	if c.Gap < 0 || c.Border < 0 || c.TitleBar < 0 {
		return fmt.Errorf("config: gap, border and title_bar must not be negative")
	}
	if c.GridGap < 0 || c.GridBorder < 0 {
		return fmt.Errorf("config: grid_gap and grid_border must not be negative")
	}
	if c.GridAspect <= 0 {
		return fmt.Errorf("config: grid_aspect must be greater than zero, got %v", c.GridAspect)
	}
	if c.WallpaperFPS < 1 || c.WallpaperFPS > 60 {
		return fmt.Errorf("config: wallpaper_fps must be between 1 and 60, got %d", c.WallpaperFPS)
	}
	if c.WallpaperDim < 0 || c.WallpaperDim > 1 {
		return fmt.Errorf("config: wallpaper_dim must be between 0 and 1, got %v", c.WallpaperDim)
	}
	if c.GridLargeMin < 1 {
		return fmt.Errorf("config: grid_large_min must be at least 1, got %d", c.GridLargeMin)
	}
	if c.GridHeaderHeight < 0 {
		return fmt.Errorf("config: grid_header_height must not be negative")
	}
	for _, key := range c.GridGroups {
		switch key {
		case desk.GroupWidgets, desk.GroupApps, desk.GroupGames, desk.GroupMedia, desk.GroupOffice,
			desk.GroupDocs, desk.GroupArchive, desk.GroupFolders, desk.GroupFiles:
		default:
			return fmt.Errorf("config: unknown grid group %q, valid groups are %s",
				key, strings.Join(desk.DefaultGroupOrder, ", "))
		}
	}
	if c.GridMinCell > 0 && c.GridMaxCell > 0 && c.GridMinCell > c.GridMaxCell {
		return fmt.Errorf("config: grid_min_cell (%d) is larger than grid_max_cell (%d)",
			c.GridMinCell, c.GridMaxCell)
	}
	if _, err := layout.Parse(c.Layout); err != nil {
		return fmt.Errorf("config: layout: %w", err)
	}
	for combo, action := range c.Keybinds {
		if _, err := x11.ParseBinding(combo); err != nil {
			return fmt.Errorf("config: keybind %q: %w", combo, err)
		}
		if strings.TrimSpace(action) == "" {
			return fmt.Errorf("config: keybind %q has an empty action", combo)
		}
	}
	return nil
}

// LayoutKind returns the configured layout.
func (c *Config) LayoutKind() layout.StackKind {
	k, err := layout.Parse(c.Layout)
	if err != nil {
		return layout.LayoutStack
	}
	return k
}

// LayoutOptions returns the geometry options derived from the configuration.
func (c *Config) LayoutOptions() layout.Options {
	return layout.Options{Gap: c.Gap, Border: c.Border, TitleBar: c.TitleBar}
}

// DesktopNamesOrDefault returns one name per desktop, inventing names for
// desktops the configuration did not name.
func (c *Config) DesktopNamesOrDefault() []string {
	names := make([]string, c.Desktops)
	for i := 0; i < c.Desktops; i++ {
		if i < len(c.DesktopNames) && c.DesktopNames[i] != "" {
			names[i] = c.DesktopNames[i]
			continue
		}
		names[i] = fmt.Sprintf("Desktop %d", i+1)
	}
	return names
}

// ResolvedKeybinds parses the configured key combinations.
func (c *Config) ResolvedKeybinds() ([]Keybind, error) {
	out := make([]Keybind, 0, len(c.Keybinds))
	for combo, action := range c.Keybinds {
		b, err := x11.ParseBinding(combo)
		if err != nil {
			return nil, err
		}
		out = append(out, Keybind{Binding: b, Action: action})
	}
	return out, nil
}

// Keybind pairs a key combination with the action string it triggers.
type Keybind struct {
	Binding x11.Binding
	Action  string
}

// Merge applies a partial configuration, which is what the settings widget
// sends. Only keys the payload actually carries are replaced.
func (c *Config) Merge(raw []byte) error {
	if len(raw) == 0 {
		return nil
	}
	patch := map[string]json.RawMessage{}
	if err := json.Unmarshal(raw, &patch); err != nil {
		return err
	}
	full, err := json.Marshal(c)
	if err != nil {
		return err
	}
	merged := map[string]json.RawMessage{}
	if err := json.Unmarshal(full, &merged); err != nil {
		return err
	}
	for key, value := range patch {
		merged[key] = value
	}
	joined, err := json.Marshal(merged)
	if err != nil {
		return err
	}
	// The result is decoded into a fresh value: json would otherwise reuse the
	// slices and maps of the receiver and change them even when the patch is
	// rejected, which would leave a half applied setting behind.
	var next Config
	if err := json.Unmarshal(joined, &next); err != nil {
		return fmt.Errorf("config: %w", err)
	}
	if err := next.Validate(); err != nil {
		return err
	}
	*c = next
	return nil
}
