package widget

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

func fixedSession() Session {
	at := time.Date(2026, time.March, 12, 9, 5, 7, 0, time.Local)
	return Session{Now: func() time.Time { return at }}
}

func TestServeServesWidgetAssets(t *testing.T) {
	r := NewRegistry(fixedSession())
	cases := []struct {
		url  string
		mime string
	}{
		{"lythrum://app/clock/index.html", "text/html; charset=utf-8"},
		{"lythrum://app/clock/../sysmon/index.html", ""},
		{"lythrum://app/common.css", "text/css; charset=utf-8"},
		{"lythrum://app/clock/clock.js", "text/javascript; charset=utf-8"},
		{"lythrum://app/clock/clock.js", "text/javascript; charset=utf-8"},
	}
	for _, c := range cases {
		mime, body, ok := r.Serve(c.url)
		if c.mime == "" {
			if ok {
				t.Errorf("Serve(%q) returned content, want a miss", c.url)
			}
			continue
		}
		if !ok {
			t.Fatalf("Serve(%q) missed", c.url)
		}
		if mime != c.mime {
			t.Errorf("Serve(%q) mime = %q, want %q", c.url, mime, c.mime)
		}
		if len(body) == 0 {
			t.Errorf("Serve(%q) returned an empty body", c.url)
		}
	}
	if _, _, ok := r.Serve("lythrum://app/nope/index.html"); ok {
		t.Error("an unknown widget must not be served")
	}
}

func TestRegistryKnowsBuiltins(t *testing.T) {
	r := NewRegistry(fixedSession())
	for _, id := range []string{"clock", "sysmon", "settings", "photos", "music", "search"} {
		if _, ok := r.Lookup(id); !ok {
			t.Errorf("widget %q is missing", id)
		}
	}
	if w, _ := r.Lookup("clock"); w.Title == "" || w.Group == "" {
		t.Error("a built-in widget needs a title and a group")
	}
	if got := URL("clock"); got != "lythrum://app/clock/index.html" {
		t.Errorf("URL(clock) = %q", got)
	}
}

func TestCallClockNow(t *testing.T) {
	r := NewRegistry(fixedSession())
	data, err := r.Call("clock.now", nil)
	if err != nil {
		t.Fatalf("clock.now: %v", err)
	}
	var got ClockNow
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("answer is not JSON: %v", err)
	}
	if got.Time != "09:05" || got.Seconds != "07" {
		t.Errorf("clock.now time = %q:%q", got.Time, got.Seconds)
	}
	if got.Weekday != "четверг" {
		t.Errorf("weekday = %q, want четверг", got.Weekday)
	}
	if got.Date != "12 марта 2026" {
		t.Errorf("date = %q", got.Date)
	}
}

func TestCallCalendarGrid(t *testing.T) {
	r := NewRegistry(fixedSession())
	data, err := r.Call("clock.calendar", nil)
	if err != nil {
		t.Fatalf("clock.calendar: %v", err)
	}
	var got CalendarMonth
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Weeks) < 4 || len(got.Weeks) > 6 {
		t.Fatalf("weeks = %d, want 4 to 6", len(got.Weeks))
	}
	for i, week := range got.Weeks {
		if len(week) != 7 {
			t.Fatalf("week %d has %d days", i, len(week))
		}
		if i > 0 && week[0] <= got.Weeks[i-1][6] {
			t.Fatalf("week %d starts before the previous week ends", i)
		}
	}
	if got.Today != "2026-03-12" {
		t.Errorf("today = %q", got.Today)
	}
	if got.Title != "1 марта 2026" {
		t.Errorf("title = %q", got.Title)
	}
}

func TestCalendarHonoursRequestedMonth(t *testing.T) {
	r := NewRegistry(fixedSession())
	data, err := r.Call("clock.calendar", json.RawMessage(`{"month":7,"year":2026}`))
	if err != nil {
		t.Fatal(err)
	}
	var got CalendarMonth
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if got.Month != 7 || got.Year != 2026 {
		t.Errorf("month = %d/%d", got.Month, got.Year)
	}
	if !strings.HasPrefix(got.Weeks[0][0], "2026-06-29") && !strings.HasPrefix(got.Weeks[0][0], "2026-06-") {
		t.Errorf("first week starts at %q, want the end of June", got.Weeks[0][0])
	}
}

func TestParseICS(t *testing.T) {
	text := "BEGIN:VCALENDAR\n" +
		"X-LYTHRUM-COLOR:#ff8800\n" +
		"BEGIN:VEVENT\nDTSTART;VALUE=DATE:20260312\nSUMMARY:День рождения\nEND:VEVENT\n" +
		"BEGIN:VEVENT\nDTSTART:20260312T180000Z\nSUMMARY:Встреча с командой\nEND:VEVENT\n" +
		"END:VCALENDAR\n"
	events := parseICS(text)
	if len(events) != 2 {
		t.Fatalf("parsed %d events, want 2", len(events))
	}
	if events[0].title != "День рождения" || !events[0].allDay {
		t.Errorf("first event = %+v", events[0])
	}
	if events[0].color != "#ff8800" {
		t.Errorf("colour = %q", events[0].color)
	}
	if events[1].title != "Встреча с командой" || events[1].allDay {
		t.Errorf("second event = %+v", events[1])
	}
	if _, err := parseEventTime("not-a-date", false); err == nil {
		t.Error("a broken date must not parse")
	}
}

func TestCallSysmonStats(t *testing.T) {
	r := NewRegistry(fixedSession())
	data, err := r.Call("sysmon.stats", nil)
	if err != nil {
		t.Fatalf("sysmon.stats: %v", err)
	}
	var got SystemStats
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if got.Memory.TotalKB == 0 {
		t.Error("total memory should be reported on a real machine")
	}
	if got.Load.One < 0 {
		t.Error("load cannot be negative")
	}
	if got.Uptime <= 0 {
		t.Error("uptime should be positive")
	}
	for _, pct := range got.CPU.PerCore {
		if pct < 0 || pct > 100 {
			t.Errorf("core load %f is out of range", pct)
		}
	}
}

func TestCallUnknownMethod(t *testing.T) {
	r := NewRegistry(fixedSession())
	if _, err := r.Call("nope.nothing", nil); err == nil {
		t.Fatal("an unknown method must fail")
	}
}

func TestBusyPercent(t *testing.T) {
	// 100 jiffies passed, 40 of them idle, so 60 percent were busy.
	if got := busyPercent(100, 60, 200, 100); got < 59 || got > 61 {
		t.Errorf("busyPercent = %f, want about 60", got)
	}
	if got := busyPercent(100, 60, 100, 60); got != 0 {
		t.Errorf("busyPercent without movement = %f, want 0", got)
	}
}

func TestFormatHelpersAreRussian(t *testing.T) {
	if MonthName(3) != "марта" {
		t.Errorf("MonthName(3) = %q", MonthName(3))
	}
	at := time.Date(2026, time.March, 12, 0, 0, 0, 0, time.Local)
	if WeekdayName(at) != "четверг" {
		t.Errorf("WeekdayName = %q", WeekdayName(at))
	}
}

func TestShellActionsListsWhatCanBeBound(t *testing.T) {
	r := NewRegistry(Session{
		Now: time.Now,
		Actions: func() []Action {
			return []Action{
				{ID: "quit", Title: "Выйти", Group: "Оболочка"},
				{ID: "spawn_exec", Title: "Команда", Group: "Окна", NeedsArgument: true},
			}
		},
	})
	data, err := r.Call("shell.actions", nil)
	if err != nil {
		t.Fatalf("shell.actions: %v", err)
	}
	var actions []Action
	if err := json.Unmarshal(data, &actions); err != nil {
		t.Fatalf("answer is not a list of actions: %v", err)
	}
	if len(actions) != 2 {
		t.Fatalf("got %d actions, want 2", len(actions))
	}
	if !actions[1].NeedsArgument {
		t.Error("an action with an argument must say so")
	}
}

func TestShellInfoMergesTheShellDetails(t *testing.T) {
	r := NewRegistry(Session{
		Now: time.Now,
		Info: func() map[string]any {
			return map[string]any{
				"layout": "tiled", "desktops": 4, "desktop": 2,
				"monitors": []string{"eDP-1 1920x1080"}, "clients": 3,
				"uptime": "1h2m", "wallpaper": "/tmp/a.png", "wallpaperEnabled": true,
				"session": "3", "keybinds": 14,
			}
		},
	})
	data, err := r.Call("shell.info", nil)
	if err != nil {
		t.Fatalf("shell.info: %v", err)
	}
	var info map[string]any
	if err := json.Unmarshal(data, &info); err != nil {
		t.Fatal(err)
	}
	for key, want := range map[string]any{
		"layout": "tiled", "desktops": 4.0, "desktop": 2.0,
		"uptime": "1h2m", "wallpaper": "/tmp/a.png", "keybinds": 14.0,
	} {
		if info[key] != want {
			t.Errorf("info[%q] = %v, want %v", key, info[key], want)
		}
	}
	if monitors, ok := info["monitors"].([]any); !ok || len(monitors) != 1 {
		t.Errorf("monitors = %v, want one entry", info["monitors"])
	}
}

func TestShellSetConfigRefusesABrokenPatch(t *testing.T) {
	calls := 0
	r := NewRegistry(Session{
		Now: time.Now,
		Config: func() ([]byte, error) {
			return []byte(`{"wallpaper_fps":12}`), nil
		},
		SetConfig: func(json.RawMessage) ([]byte, error) {
			calls++
			return nil, errors.New("config: wallpaper_fps must be between 1 and 60")
		},
	})
	if _, err := r.Call("shell.setConfig", json.RawMessage(`{"wallpaper_fps":900}`)); err == nil {
		t.Fatal("the page has to learn that the patch was refused")
	}
	if calls != 1 {
		t.Errorf("the shell was asked %d times, want 1", calls)
	}
}

func TestShellSetConfigAnswersWithTheNewSettings(t *testing.T) {
	r := NewRegistry(Session{
		Now: time.Now,
		Config: func() ([]byte, error) {
			return []byte(`{"grid_gap":8}`), nil
		},
		SetConfig: func(raw json.RawMessage) ([]byte, error) {
			return []byte(`{"grid_gap":12,"patched":` + string(raw) + `}`), nil
		},
	})
	data, err := r.Call("shell.setConfig", json.RawMessage(`true`))
	if err != nil {
		t.Fatalf("shell.setConfig: %v", err)
	}
	var out map[string]any
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("answer is not an object: %v", err)
	}
	if out["grid_gap"] != 12.0 {
		t.Errorf("the page should see the applied value, got %v", out["grid_gap"])
	}
}
