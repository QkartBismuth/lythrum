// Command lythrum is a tiling window manager for X11.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"lythrum/internal/config"
	"lythrum/internal/wm"
	"lythrum/internal/x11"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "lythrum:", err)
		os.Exit(1)
	}
}

func run() error {
	var (
		display     = flag.String("display", "", "X display to use (default $DISPLAY)")
		configPath  = flag.String("config", defaultConfigPath(), "path to the configuration file")
		logLevel    = flag.String("log", "info", "log level: debug, info, warn, error")
		logFormat   = flag.String("log-format", "text", "log format: text or json")
		replace     = flag.Bool("replace", false, "replace the window manager that is already running")
		showInfo    = flag.Bool("info", false, "query a running window manager and print its state")
		sendCommand = flag.String("cmd", "", "send a control command to the running window manager and exit")
		checkOnly   = flag.Bool("check", false, "validate the configuration and exit")
		wayland     = flag.Bool("wayland", false,
			"force Wayland session mode (the shell still talks X11 through XWayland)")
	)
	flag.Parse()

	level, err := parseLevel(*logLevel)
	if err != nil {
		return err
	}
	log := newLogger(*logFormat, level)

	if *checkOnly {
		cfg, err := config.Load(*configPath)
		if err != nil {
			return err
		}
		fmt.Printf("configuration at %s is valid (%d key bindings, %d desktops)\n",
			*configPath, len(cfg.Keybinds), cfg.Desktops)
		return nil
	}

	if *showInfo {
		return runClient([]string{"info"})
	}
	if *sendCommand != "" {
		return runClient(strings.Fields(*sendCommand))
	}

	cfg, err := config.Load(*configPath)
	if err != nil {
		return err
	}
	if *replace {
		if err := replaceRunning(); err != nil {
			log.Warn("could not replace the running window manager", "error", err)
		}
	}

	d, err := x11.Open(*display)
	if err != nil {
		return err
	}
	defer d.Close()

	x11.SetErrorHandler(func(e x11.ServerError) {
		// Most protocol errors come from clients racing us to destroy a
		// window; they are logged at debug level and never fatal.
		log.Debug("x11 error", "code", e.CodeName, "resource", fmt.Sprintf("0x%x", e.ResourceID))
	})

	manager, err := wm.New(d, cfg, log)
	if err != nil {
		return err
	}
	manager.SetConfigPath(*configPath)

	session := wm.DetectSession()
	if *wayland {
		session.Wayland = true
	}
	manager.SetSession(session)
	if session.Wayland {
		log.Info("wayland session", "session", session.String(), "note", session.Summary())
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	for _, cmd := range cfg.Startup {
		log.Info("startup command", "command", cmd)
		manager.RunStartup(cmd)
	}

	log.Info("running", "display", displayName(d), "session", session.String(),
		"pid", os.Getpid(), "socket", wm.IPCSocketPath())
	return manager.Run(ctx)
}

// runClient talks to a window manager that is already running.
func runClient(args []string) error {
	if len(args) == 0 {
		return errors.New("no command given")
	}
	cmd := wm.Command{Command: args[0], Args: args[1:]}
	reply, err := wm.SendCommand(cmd, 2*time.Second)
	if err != nil {
		return err
	}
	if reply.Data != nil {
		for _, v := range reply.Data {
			fmt.Print(v)
		}
	}
	if !reply.OK {
		if reply.Error != "" {
			return errors.New(reply.Error)
		}
		return errors.New("command failed")
	}
	if len(reply.Result) > 0 && string(reply.Result) != "null" {
		var pretty any
		if err := jsonIndent(reply.Result, &pretty); err == nil {
			fmt.Println(pretty)
		} else {
			fmt.Println(string(reply.Result))
		}
	}
	return nil
}

// replaceRunning asks an existing window manager to quit.
func replaceRunning() error {
	reply, err := wm.SendCommand(wm.Command{Command: "quit"}, time.Second)
	if err != nil {
		return err
	}
	if !reply.OK {
		return errors.New("the running window manager refused to quit")
	}
	time.Sleep(300 * time.Millisecond)
	return nil
}

func displayName(d *x11.Display) string {
	if v := os.Getenv("DISPLAY"); v != "" {
		return v
	}
	return "unknown"
}

func newLogger(format string, level slog.Level) *slog.Logger {
	opts := &slog.HandlerOptions{Level: level}
	if format == "json" {
		return slog.New(slog.NewJSONHandler(os.Stderr, opts))
	}
	return slog.New(slog.NewTextHandler(os.Stderr, opts))
}

func parseLevel(s string) (slog.Level, error) {
	switch strings.ToLower(s) {
	case "debug":
		return slog.LevelDebug, nil
	case "info":
		return slog.LevelInfo, nil
	case "warn", "warning":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	}
	return 0, fmt.Errorf("unknown log level %q", s)
}

// defaultConfigPath points at the per-user configuration file, falling back to
// the system location when the user has none.
func defaultConfigPath() string {
	if v := os.Getenv("LYTHRUM_CONFIG"); v != "" {
		return v
	}
	if dir := os.Getenv("XDG_CONFIG_HOME"); dir != "" {
		p := filepath.Join(dir, "lythrum", "config.json")
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	home, err := os.UserHomeDir()
	if err == nil {
		p := filepath.Join(home, ".config", "lythrum", "config.json")
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

// jsonIndent re-formats a JSON document for humans.
func jsonIndent(data []byte, v *any) error {
	var raw any
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	*v = raw
	return nil
}
