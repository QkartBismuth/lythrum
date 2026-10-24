package main

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// settings holds every preference of the shell: key bindings, pointer speeds,
// the active style, the appearance of the widgets and the state of the system
// services the settings app can drive. The file lives in
// ~/.config/lythrum/settings.json and is rewritten whenever something changes.
type settings struct {
	Keybinds map[string][]string `json:"keybinds"`

	Mouse    mouseSettings   `json:"mouse"`
	Display  displaySettings `json:"display"`
	Icons    iconSettings    `json:"icons"`
	Cursor   cursorSettings  `json:"cursor"`
	Network  networkSettings `json:"network"`
	Clock    clockSettings   `json:"clock"`
	Sound    soundSettings   `json:"sound"`
	Screen   screenSettings  `json:"screen"`
	Gestures gestureSettings `json:"gestures"`
	Anim     animSettings    `json:"anim"`

	// Style is the name of the active style preset: metro, winui3, material3,
	// win95 or glass.
	Style string `json:"style"`
}

// mouseSettings holds the pointer speeds of the board itself. The system wide
// pointer acceleration is left alone: the canvas only knows about the speeds
// it applies to its own gestures.
type mouseSettings struct {
	PanSpeed   float64 `json:"pan_speed"`
	WheelSpeed float64 `json:"wheel_speed"`
	ZoomSpeed  float64 `json:"zoom_speed"`
	Threshold  int     `json:"threshold"`
}

type displaySettings struct {
	FontSize int    `json:"font_size"`
	ShowGrid bool   `json:"show_grid"`
	ShowAxes bool   `json:"show_axes"`
	Accent   string `json:"accent"`
	Theme    string `json:"theme"`
}

type iconSettings struct {
	Size   int    `json:"size"`
	Labels bool   `json:"labels"`
	Style  string `json:"style"`
}

type cursorSettings struct {
	Shape string `json:"shape"`
}

type networkSettings struct {
	Wifi bool `json:"wifi"`
	BT   bool `json:"bluetooth"`
}

type clockSettings struct {
	Timezone   string `json:"timezone"`
	TwentyFour bool   `json:"24h"`
	NTP        bool   `json:"ntp"`
}

type soundSettings struct {
	Volume int    `json:"volume"`
	Muted  bool   `json:"muted"`
	Theme  string `json:"theme"`
}

type screenSettings struct {
	Brightness int    `json:"brightness"`
	Output     string `json:"output"`
}

type gestureSettings struct {
	Enabled     bool    `json:"enabled"`
	Invert      bool    `json:"invert"`
	Sensitivity float64 `json:"sensitivity"`
}

type animSettings struct {
	GifEnabled bool `json:"gif_enabled"`
	GifFps     int  `json:"gif_fps"`
	VideoWall  bool `json:"video_wall"`
	Animations bool `json:"animations"`
}

// defaultSettings is the shell as it looks right after the installation: a
// dark board, the Metro style and the classic keyboard shortcuts.
func defaultSettings() settings {
	return settings{
		Keybinds: defaultKeybinds(),
		Mouse: mouseSettings{
			PanSpeed:   1,
			WheelSpeed: 1,
			ZoomSpeed:  1,
			Threshold:  4,
		},
		Display: displaySettings{
			FontSize: 10,
			ShowGrid: true,
			ShowAxes: true,
			Theme:    "dark",
		},
		Icons: iconSettings{
			Size:   88,
			Labels: true,
			Style:  "auto",
		},
		Cursor: cursorSettings{Shape: ""},
		Clock:  clockSettings{TwentyFour: true},
		Sound:  soundSettings{Volume: 50},
		Screen: screenSettings{Brightness: 100},
		Gestures: gestureSettings{
			Enabled:     true,
			Sensitivity: 1,
		},
		Anim: animSettings{
			GifEnabled: true,
			GifFps:     12,
			VideoWall:  true,
			Animations: true,
		},
		Style: "metro",
	}
}

// settingsPath is the file the preferences are stored in.
func settingsPath() string {
	if base, err := os.UserConfigDir(); err == nil {
		return filepath.Join(base, "lythrum", "settings.json")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "lythrum", "settings.json")
}

// loadSettings reads the preferences, falling back to the defaults for every
// value the file does not mention.
func loadSettings() settings {
	cfg := defaultSettings()
	data, err := os.ReadFile(settingsPath())
	if err == nil {
		_ = json.Unmarshal(data, &cfg)
	}
	if cfg.Keybinds == nil {
		cfg.Keybinds = defaultKeybinds()
	}
	return cfg
}

// save writes the preferences back to disk, creating the folder if needed.
func (s *settings) save() {
	path := settingsPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return
	}
	_ = os.WriteFile(path, data, 0o644)
}
