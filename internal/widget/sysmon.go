package widget

import (
	"bufio"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// SystemStats is the payload the monitor widget draws.
type SystemStats struct {
	CPU     CPUStats      `json:"cpu"`
	Memory  MemoryStats   `json:"memory"`
	Load    LoadStats     `json:"load"`
	Disks   []DiskStats   `json:"disks"`
	Network NetworkStats  `json:"network"`
	Sensors []SensorStats `json:"sensors"`
	Uptime  float64       `json:"uptimeSeconds"`
	Host    string        `json:"host"`
	Kernel  string        `json:"kernel"`
}

// CPUStats describes processor use.
type CPUStats struct {
	Percent  float64   `json:"percent"`
	Cores    int       `json:"cores"`
	FreqMHz  float64   `json:"freqMHz"`
	PerCore  []float64 `json:"perCore"`
	UserNice float64   `json:"userNicePercent"`
	System   float64   `json:"systemPercent"`
}

// MemoryStats describes RAM use.
type MemoryStats struct {
	TotalKB     uint64  `json:"totalKb"`
	UsedKB      uint64  `json:"usedKb"`
	AvailableKB uint64  `json:"availableKb"`
	CachedKB    uint64  `json:"cachedKb"`
	Percent     float64 `json:"percent"`
	SwapTotalKB uint64  `json:"swapTotalKb"`
	SwapUsedKB  uint64  `json:"swapUsedKb"`
}

// LoadStats is the classic load average.
type LoadStats struct {
	One     float64 `json:"one"`
	Five    float64 `json:"five"`
	Fifteen float64 `json:"fifteen"`
	Running int     `json:"running"`
	Total   int     `json:"total"`
}

// DiskStats is one mounted filesystem.
type DiskStats struct {
	Mount   string  `json:"mount"`
	Device  string  `json:"device"`
	FSType  string  `json:"fsType"`
	TotalKB uint64  `json:"totalKb"`
	FreeKB  uint64  `json:"freeKb"`
	Percent float64 `json:"percent"`
}

// NetworkStats counts traffic since boot.
type NetworkStats struct {
	RxBytes   uint64  `json:"rxBytes"`
	TxBytes   uint64  `json:"txBytes"`
	RxPerSec  float64 `json:"rxPerSecond"`
	TxPerSec  float64 `json:"txPerSecond"`
	RxPackets uint64  `json:"rxPackets"`
	TxPackets uint64  `json:"txPackets"`
}

// SensorStats is a temperature reading.
type SensorStats struct {
	Label   string  `json:"label"`
	Celsius float64 `json:"celsius"`
	High    float64 `json:"high"`
}

// cpuSample keeps the jiffies of every CPU line: the busy part and the idle
// part, because only their difference gives a usage percentage.
type cpuSample struct {
	total []uint64
	idle  []uint64
}

var cpuState = struct {
	last     cpuSample
	haveLast bool
}{}

// cpuTimes reads the busy and idle jiffies of every CPU from /proc/stat.
func cpuTimes() (cpuSample, bool) {
	file, err := os.Open("/proc/stat")
	if err != nil {
		return cpuSample{}, false
	}
	defer file.Close()
	var out cpuSample
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 5 || !strings.HasPrefix(fields[0], "cpu") {
			continue
		}
		var total, idle uint64
		for i, f := range fields[1:] {
			v, _ := strconv.ParseUint(f, 10, 64)
			total += v
			// The third and fourth columns are idle and iowait.
			if i == 3 || i == 4 {
				idle += v
			}
		}
		out.total = append(out.total, total)
		out.idle = append(out.idle, idle)
	}
	return out, len(out.total) > 0
}

// busyPercent compares two samples of one CPU.
func busyPercent(prevTotal, prevIdle, total, idle uint64) float64 {
	if total <= prevTotal {
		return 0
	}
	deltaTotal := float64(total - prevTotal)
	deltaIdle := float64(0)
	if idle > prevIdle {
		deltaIdle = float64(idle - prevIdle)
	}
	used := (deltaTotal - deltaIdle) / deltaTotal * 100
	switch {
	case used < 0:
		return 0
	case used > 100:
		return 100
	}
	return used
}

// SysmonStats samples the machine. A missing file only removes that part of
// the answer, so the widget still works inside a container.
func SysmonStats() SystemStats {
	stats := SystemStats{Host: hostname(), Kernel: kernelVersion()}
	now := time.Now()

	if sample, ok := cpuTimes(); ok {
		stats.CPU.Cores = len(sample.total)
		stats.CPU.PerCore = make([]float64, 0, len(sample.total))
		if cpuState.haveLast && len(cpuState.last.total) == len(sample.total) {
			var busy, total float64
			for i := range sample.total {
				pct := busyPercent(cpuState.last.total[i], cpuState.last.idle[i],
					sample.total[i], sample.idle[i])
				stats.CPU.PerCore = append(stats.CPU.PerCore, pct)
				busy += pct
				total += pct
			}
			if total > 0 {
				stats.CPU.Percent = busy / float64(len(sample.total))
			}
		} else {
			for range sample.total {
				stats.CPU.PerCore = append(stats.CPU.PerCore, 0)
			}
		}
		cpuState.last = sample
		cpuState.haveLast = true
	}
	stats.CPU.FreqMHz = cpuFrequency()

	stats.Memory = memoryStats()
	stats.Load = loadStats()
	stats.Disks = diskStats()
	stats.Network = networkStats(now)
	stats.Sensors = sensorStats()
	stats.Uptime = uptime()
	return stats
}

func cpuFrequency() float64 {
	data, err := os.ReadFile("/proc/cpuinfo")
	if err != nil {
		return 0
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "cpu MHz") {
			parts := strings.SplitN(line, ":", 2)
			if len(parts) == 2 {
				v, _ := strconv.ParseFloat(strings.TrimSpace(parts[1]), 64)
				return v
			}
		}
	}
	return 0
}

func memoryStats() MemoryStats {
	out := MemoryStats{}
	data, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return out
	}
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		value, _ := strconv.ParseUint(fields[1], 10, 64)
		switch strings.TrimSuffix(fields[0], ":") {
		case "MemTotal":
			out.TotalKB = value
		case "MemFree":
			out.AvailableKB += value
		case "MemAvailable":
			out.AvailableKB = value
		case "Buffers", "Cached", "SReclaimable":
			out.CachedKB += value
		case "SwapTotal":
			out.SwapTotalKB = value
		case "SwapFree":
			out.SwapUsedKB = out.SwapTotalKB - value
		}
	}
	if out.TotalKB > 0 {
		out.UsedKB = out.TotalKB - out.AvailableKB
		out.Percent = float64(out.UsedKB) / float64(out.TotalKB) * 100
	}
	return out
}

func loadStats() LoadStats {
	out := LoadStats{}
	if data, err := os.ReadFile("/proc/loadavg"); err == nil {
		fields := strings.Fields(string(data))
		if len(fields) >= 3 {
			out.One, _ = strconv.ParseFloat(fields[0], 64)
			out.Five, _ = strconv.ParseFloat(fields[1], 64)
			out.Fifteen, _ = strconv.ParseFloat(fields[2], 64)
			if slash := strings.Index(fields[3], "/"); slash > 0 {
				out.Running, _ = strconv.Atoi(fields[3][:slash])
				out.Total, _ = strconv.Atoi(fields[3][slash+1:])
			}
		}
	}
	return out
}

func diskStats() []DiskStats {
	var out []DiskStats
	file, err := os.Open("/proc/mounts")
	if err != nil {
		return out
	}
	defer file.Close()
	seen := map[string]bool{}
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 3 {
			continue
		}
		device, mount, fstype := fields[0], fields[1], fields[2]
		if !strings.HasPrefix(device, "/dev/") {
			continue
		}
		if seen[device] || seen[mount] {
			continue
		}
		var stat syscall.Statfs_t
		if err := syscall.Statfs(mount, &stat); err != nil {
			continue
		}
		blockSize := uint64(stat.Bsize)
		total := stat.Blocks * blockSize / 1024
		free := stat.Bavail * blockSize / 1024
		used := total - stat.Bfree*blockSize/1024
		pct := 0.0
		if total > 0 {
			pct = float64(used) / float64(total) * 100
		}
		seen[device] = true
		seen[mount] = true
		out = append(out, DiskStats{
			Mount: mount, Device: filepath.Base(device), FSType: fstype,
			TotalKB: total, FreeKB: free, Percent: pct,
		})
	}
	return out
}

var netState = struct {
	rx, tx, packets uint64
	at              time.Time
}{}

func networkStats(now time.Time) NetworkStats {
	out := NetworkStats{}
	data, err := os.ReadFile("/proc/net/dev")
	if err != nil {
		return out
	}
	for _, line := range strings.Split(string(data), "\n")[2:] {
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 || strings.TrimSpace(parts[0]) == "lo" {
			continue
		}
		fields := strings.Fields(parts[1])
		if len(fields) < 10 {
			continue
		}
		rx, _ := strconv.ParseUint(fields[0], 10, 64)
		tx, _ := strconv.ParseUint(fields[8], 10, 64)
		out.RxBytes += rx
		out.TxBytes += tx
		out.RxPackets += mustUint(fields[1])
		out.TxPackets += mustUint(fields[9])
	}
	if netState.at.IsZero() {
		netState.at = now
		netState.rx, netState.tx = out.RxBytes, out.TxBytes
	} else {
		if elapsed := now.Sub(netState.at).Seconds(); elapsed > 0 {
			if out.RxBytes > netState.rx {
				out.RxPerSec = float64(out.RxBytes-netState.rx) / elapsed
			}
			if out.TxBytes > netState.tx {
				out.TxPerSec = float64(out.TxBytes-netState.tx) / elapsed
			}
		}
		netState.at = now
		netState.rx, netState.tx = out.RxBytes, out.TxBytes
	}
	return out
}

func mustUint(s string) uint64 {
	v, _ := strconv.ParseUint(s, 10, 64)
	return v
}

func sensorStats() []SensorStats {
	var out []SensorStats
	entries, err := os.ReadDir("/sys/class/hwmon")
	if err != nil {
		return out
	}
	for _, e := range entries {
		name := readString(filepath.Join("/sys/class/hwmon", e.Name(), "name"))
		labels := map[int]string{}
		if dir, err := os.ReadDir(filepath.Join("/sys/class/hwmon", e.Name())); err == nil {
			for _, f := range dir {
				if !strings.HasPrefix(f.Name(), "temp") || !strings.HasSuffix(f.Name(), "_label") {
					continue
				}
				index := tempIndex(f.Name())
				labels[index] = readString(filepath.Join("/sys/class/hwmon", e.Name(), f.Name()))
			}
		}
		items, _ := os.ReadDir(filepath.Join("/sys/class/hwmon", e.Name()))
		for _, f := range items {
			if !strings.HasPrefix(f.Name(), "temp") || !strings.HasSuffix(f.Name(), "_input") {
				continue
			}
			raw := readString(filepath.Join("/sys/class/hwmon", e.Name(), f.Name()))
			value, err := strconv.ParseFloat(strings.TrimSpace(raw), 64)
			if err != nil {
				continue
			}
			index := tempIndex(f.Name())
			label := labels[index]
			if label == "" {
				label = name
			}
			out = append(out, SensorStats{Label: label, Celsius: value / 1000, High: 90})
		}
	}
	return out
}

func tempIndex(name string) int {
	n := strings.TrimSuffix(strings.TrimPrefix(name, "temp"), "_input")
	v, _ := strconv.Atoi(n)
	return v
}

func readString(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

func uptime() float64 {
	data, err := os.ReadFile("/proc/uptime")
	if err != nil {
		return 0
	}
	fields := strings.Fields(string(data))
	if len(fields) == 0 {
		return 0
	}
	v, _ := strconv.ParseFloat(fields[0], 64)
	return v
}

func hostname() string {
	if h, err := os.Hostname(); err == nil {
		return h
	}
	return ""
}

func kernelVersion() string {
	var uname syscall.Utsname
	if err := syscall.Uname(&uname); err != nil {
		return ""
	}
	buf := make([]byte, 0, len(uname.Release))
	for _, c := range uname.Release {
		if c == 0 {
			break
		}
		buf = append(buf, byte(c))
	}
	return string(buf)
}

func sysmonStats(s Session, _ json.RawMessage) (any, error) {
	return SysmonStats(), nil
}

func shellConfig(s Session, _ json.RawMessage) (any, error) {
	if s.Config == nil {
		return map[string]any{}, nil
	}
	data, err := s.Config()
	if err != nil {
		return nil, err
	}
	return json.RawMessage(data), nil
}

func shellTheme(s Session, _ json.RawMessage) (any, error) {
	if s.Theme != nil {
		return s.Theme(), nil
	}
	return map[string]string{}, nil
}

// ShellInfo describes the shell for the settings widget. Everything the
// settings page shows about the running system comes from here, so the page
// never has to guess what the shell is doing.
type ShellInfo struct {
	Name         string   `json:"name"`
	Version      string   `json:"version"`
	StartScreen  string   `json:"startScreen"`
	Search       string   `json:"search"`
	Power        string   `json:"power"`
	Widgets      int      `json:"widgets"`
	WebEngine    string   `json:"webEngine"`
	Layout       string   `json:"layout"`
	Desktops     int      `json:"desktops"`
	Desktop      int      `json:"desktop"`
	Monitors     []string `json:"monitors"`
	Clients      int      `json:"clients"`
	Uptime       string   `json:"uptime"`
	Wallpaper    string   `json:"wallpaper"`
	WallpaperOn  bool     `json:"wallpaperEnabled"`
	Session      string   `json:"session"`
	SessionType  string   `json:"sessionType"`
	SessionNote  string   `json:"sessionNote"`
	ConfigPath   string   `json:"configPath"`
	KeybindCount int      `json:"keybinds"`
}

// shellSetConfig applies a partial configuration from the settings widget.
func shellSetConfig(s Session, params json.RawMessage) (any, error) {
	if s.SetConfig == nil {
		return nil, errors.New("widget: the shell does not accept settings")
	}
	data, err := s.SetConfig(params)
	if err != nil {
		return nil, err
	}
	return json.RawMessage(data), nil
}

// shellActions lists the actions a key binding may trigger, grouped for the
// editor in the settings page.
func shellActions(s Session, _ json.RawMessage) (any, error) {
	if s.Actions == nil {
		return []Action{}, nil
	}
	return s.Actions(), nil
}

func shellInfo(s Session, _ json.RawMessage) (any, error) {
	info := ShellInfo{
		Name:        "Lythrum",
		Version:     Version,
		StartScreen: "Mod4+space, поиск Mod4+s, питание Mod4+l",
		Widgets:     len(builtin()),
		WebEngine:   "WebKitGTK",
	}
	if extra := s.Info(); extra != nil {
		if v, ok := extra["layout"].(string); ok {
			info.Layout = v
		}
		if v, ok := extra["desktops"].(int); ok {
			info.Desktops = v
		}
		if v, ok := extra["desktop"].(int); ok {
			info.Desktop = v
		}
		if v, ok := extra["monitors"].([]string); ok {
			info.Monitors = v
		}
		if v, ok := extra["clients"].(int); ok {
			info.Clients = v
		}
		if v, ok := extra["uptime"].(string); ok {
			info.Uptime = v
		}
		if v, ok := extra["wallpaper"].(string); ok {
			info.Wallpaper = v
		}
		if v, ok := extra["wallpaperEnabled"].(bool); ok {
			info.WallpaperOn = v
		}
		if v, ok := extra["session"].(string); ok {
			info.Session = v
		}
		if v, ok := extra["sessionType"].(string); ok {
			info.SessionType = v
		}
		if v, ok := extra["sessionNote"].(string); ok {
			info.SessionNote = v
		}
		if v, ok := extra["configPath"].(string); ok {
			info.ConfigPath = v
		}
		if v, ok := extra["keybinds"].(int); ok {
			info.KeybindCount = v
		}
	}
	if len(info.Monitors) == 0 {
		info.Monitors = []string{}
	}
	return info, nil
}

// Version is the shell version reported to widgets.
const Version = "0.4"
