package widget

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

func registerAPI(r *Registry) {
	r.Handle("clock.now", clockNow)
	r.Handle("clock.calendar", clockCalendar)
	r.Handle("clock.events", clockEvents)
	r.Handle("sysmon.stats", sysmonStats)
	r.Handle("shell.config", shellConfig)
	r.Handle("shell.theme", shellTheme)
	r.Handle("shell.info", shellInfo)
	r.Handle("shell.actions", shellActions)
	r.Handle("shell.setConfig", shellSetConfig)
	r.Handle("media.list", mediaList)
	r.Handle("media.folders", mediaFolders)
}

// ClockNow is what the clock widget shows.
type ClockNow struct {
	Time      string `json:"time"`
	Seconds   string `json:"seconds"`
	Date      string `json:"date"`
	Weekday   string `json:"weekday"`
	ISO       string `json:"iso"`
	Unix      int64  `json:"unix"`
	Timezone  string `json:"timezone"`
	UTCOffset int    `json:"utcOffsetMinutes"`
}

var weekdays = []string{"воскресенье", "понедельник", "вторник", "среда",
	"четверг", "пятница", "суббота"}
var months = []string{"января", "февраля", "марта", "апреля", "мая", "июня",
	"июля", "августа", "сентября", "октября", "ноября", "декабря"}
var monthsShort = []string{"янв", "фев", "мар", "апр", "мая", "июн",
	"июл", "авг", "сен", "окт", "ноя", "дек"}

func sessionNow(s Session) time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func clockNow(s Session, _ json.RawMessage) (any, error) {
	now := sessionNow(s)
	_, offset := now.Zone()
	return ClockNow{
		Time:      now.Format("15:04"),
		Seconds:   now.Format("05"),
		Date:      russianDate(now),
		Weekday:   weekdays[int(now.Weekday())],
		ISO:       now.Format(time.RFC3339),
		Unix:      now.Unix(),
		Timezone:  now.Location().String(),
		UTCOffset: offset / 60,
	}, nil
}

// CalendarMonth is a month grid, weeks of seven days, weeks[0][0] being a
// Monday.
type CalendarMonth struct {
	Title    string          `json:"title"`
	Month    int             `json:"month"`
	Year     int             `json:"year"`
	First    string          `json:"firstWeekday"`
	Weeks    [][]string      `json:"weeks"`
	Weekdays []string        `json:"weekdayNames"`
	Today    string          `json:"today"`
	Events   []CalendarEvent `json:"events"`
}

func clockCalendar(s Session, params json.RawMessage) (any, error) {
	now := sessionNow(s)
	month, year := int(now.Month()), now.Year()
	var request struct {
		Month int `json:"month"`
		Year  int `json:"year"`
	}
	if len(params) > 0 {
		_ = json.Unmarshal(params, &request)
	}
	if request.Month >= 1 && request.Month <= 12 {
		month = request.Month
	}
	if request.Year >= 1970 && request.Year <= 9999 {
		year = request.Year
	}

	first := time.Date(year, time.Month(month), 1, 0, 0, 0, 0, now.Location())
	// time.Weekday counts from Sunday; the grid starts on Monday.
	offset := (int(first.Weekday()) + 6) % 7
	start := first.AddDate(0, 0, -offset)

	var weeks [][]string
	names := make([]string, 0, 7)
	for i := 0; i < 7; i++ {
		names = append(names, shortWeekday(i))
	}
	day := start
	for w := 0; w < 6; w++ {
		row := make([]string, 0, 7)
		for d := 0; d < 7; d++ {
			row = append(row, day.Format("2006-01-02"))
			day = day.AddDate(0, 0, 1)
		}
		weeks = append(weeks, row)
	}
	// Drop a trailing week that belongs to the next month entirely.
	if len(weeks) == 6 {
		last := weeks[5]
		allNext := true
		for _, d := range last {
			if d[:7] == first.Format("2006-01") {
				allNext = false
			}
		}
		if allNext {
			weeks = weeks[:5]
		}
	}

	return CalendarMonth{
		Title:    russianDate(first),
		Month:    month,
		Year:     year,
		First:    shortWeekdayNames()[offset],
		Weeks:    weeks,
		Weekdays: names,
		Today:    now.Format("2006-01-02"),
		Events:   eventsInMonth(now, month, year),
	}, nil
}

func shortWeekday(i int) string {
	return shortWeekdayNames()[i%7]
}

func shortWeekdayNames() []string {
	return []string{"пн", "вт", "ср", "чт", "пт", "сб", "вс"}
}

// CalendarEvent is one entry from the user's calendar files.
type CalendarEvent struct {
	Start  string `json:"start"`
	Title  string `json:"title"`
	AllDay bool   `json:"allDay"`
	Color  string `json:"color"`
}

func clockEvents(s Session, params json.RawMessage) (any, error) {
	now := sessionNow(s)
	days := 14
	var request struct {
		Days int `json:"days"`
	}
	if len(params) > 0 {
		_ = json.Unmarshal(params, &request)
		if request.Days > 0 && request.Days <= 90 {
			days = request.Days
		}
	}
	events := loadEvents()
	from := now.Truncate(24 * time.Hour)
	until := from.AddDate(0, 0, days)
	var out []CalendarEvent
	for _, e := range events {
		start, err := parseEventTime(e.start, e.allDay)
		if err != nil {
			continue
		}
		if start.Before(from.Add(-24*time.Hour)) || start.After(until) {
			continue
		}
		out = append(out, CalendarEvent{Start: e.start, Title: e.title, AllDay: e.allDay, Color: e.color})
	}
	sortEvents(out)
	return map[string]any{"events": out, "from": from.Format("2006-01-02"), "days": days}, nil
}

func eventsInMonth(now time.Time, month, year int) []CalendarEvent {
	first := time.Date(year, time.Month(month), 1, 0, 0, 0, 0, now.Location())
	last := first.AddDate(0, 1, 0)
	var out []CalendarEvent
	for _, e := range loadEvents() {
		start, err := parseEventTime(e.start, e.allDay)
		if err != nil {
			continue
		}
		if start.Before(first.Add(-24*time.Hour)) || !start.Before(last) {
			continue
		}
		out = append(out, CalendarEvent{Start: e.start, Title: e.title, AllDay: e.allDay, Color: e.color})
	}
	sortEvents(out)
	return out
}

type rawEvent struct {
	start  string
	title  string
	allDay bool
	color  string
}

var eventCache = struct {
	loaded bool
	list   []rawEvent
}{}

// eventFiles lists the calendar files the widget reads.
func eventFiles() []string {
	var files []string
	if dir := os.Getenv("XDG_CONFIG_HOME"); dir != "" {
		files = append(files, filepath.Join(dir, "lythrum", "calendar"))
	} else if home, err := os.UserHomeDir(); err == nil {
		files = append(files, filepath.Join(home, ".config", "lythrum", "calendar"))
	}
	files = append(files, "/etc/lythrum/calendar")
	var out []string
	for _, dir := range files {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".ics") {
				continue
			}
			out = append(out, filepath.Join(dir, e.Name()))
		}
	}
	return out
}

func loadEvents() []rawEvent {
	if eventCache.loaded {
		return eventCache.list
	}
	eventCache.loaded = true
	for _, file := range eventFiles() {
		data, err := os.ReadFile(file)
		if err != nil {
			continue
		}
		eventCache.list = append(eventCache.list, parseICS(string(data))...)
	}
	return eventCache.list
}

// parseICS pulls VEVENT blocks out of an iCalendar file. Only the fields the
// widget shows are read, which keeps it free of dependencies.
func parseICS(text string) []rawEvent {
	var out []rawEvent
	color := ""
	for _, line := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "X-LYTHRUM-COLOR"):
			color = icsValue(line)
		case line == "BEGIN:VEVENT":
			out = append(out, rawEvent{color: color})
		case line == "END:VEVENT":
			if len(out) > 0 && out[len(out)-1].start != "" {
				// finished event
			}
		case strings.HasPrefix(line, "SUMMARY"):
			if len(out) > 0 {
				out[len(out)-1].title = icsValue(line)
			}
		case strings.HasPrefix(line, "DTSTART"):
			if len(out) > 0 {
				value := icsValue(line)
				params := icsParams(line)
				out[len(out)-1].allDay = strings.Contains(params, "VALUE=DATE")
				out[len(out)-1].start = value
			}
		}
	}
	return out
}

func icsValue(line string) string {
	if i := strings.Index(line, ":"); i >= 0 {
		return strings.TrimSpace(line[i+1:])
	}
	return ""
}

func icsParams(line string) string {
	if i := strings.Index(line, ";"); i >= 0 {
		if j := strings.Index(line, ":"); j > i {
			return strings.ToUpper(line[i:j])
		}
	}
	return ""
}

func parseEventTime(value string, allDay bool) (time.Time, error) {
	layouts := []string{"20060102T150405Z", "20060102T150405", "20060102"}
	if allDay {
		layouts = []string{"20060102"}
	}
	for _, layout := range layouts {
		if t, err := time.Parse(layout, value); err == nil {
			return t, nil
		}
	}
	return time.Time{}, os.ErrInvalid
}

func sortEvents(events []CalendarEvent) {
	for i := 1; i < len(events); i++ {
		for j := i; j > 0; j-- {
			a, _ := parseEventTime(events[j-1].Start, events[j-1].AllDay)
			b, _ := parseEventTime(events[j].Start, events[j].AllDay)
			if b.Before(a) {
				events[j-1], events[j] = events[j], events[j-1]
				continue
			}
			break
		}
	}
}

// russianDate formats a day the way Russian users expect it.
func russianDate(t time.Time) string {
	return strconv.Itoa(t.Day()) + " " + MonthName(int(t.Month())) + " " + strconv.Itoa(t.Year())
}

// MonthName returns the Russian month name for a month number.
func MonthName(month int) string {
	if month < 1 || month > 12 {
		return ""
	}
	return months[month-1]
}

// WeekdayName returns the Russian weekday for a time.
func WeekdayName(t time.Time) string { return weekdays[int(t.Weekday())] }
