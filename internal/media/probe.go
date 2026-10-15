package media

import (
	"encoding/json"
	"os/exec"
	"time"
)

// ffprobeInfo is what the ffprobe call reports.
type ffprobeInfo struct {
	Width    int           `json:"width"`
	Height   int           `json:"height"`
	Duration time.Duration `json:"-"`
	Video    bool          `json:"-"`
	Audio    bool
	Frames   int `json:"-"`
	HasArt   bool
}

// ffprobe inspects a file with ffprobe. It returns nil when ffprobe is not
// installed or the file cannot be read, so callers fall back to extensions.
func ffprobe(path string) *ffprobeInfo {
	if _, err := exec.LookPath("ffprobe"); err != nil {
		return nil
	}
	cmd := exec.Command("ffprobe", "-v", "error", "-print_format", "json",
		"-show_format", "-show_streams", path)
	out, err := cmd.Output()
	if err != nil {
		return nil
	}
	var parsed struct {
		Format struct {
			Duration string `json:"duration"`
		} `json:"format"`
		Streams []struct {
			CodecType   string         `json:"codec_type"`
			Width       int            `json:"width"`
			Height      int            `json:"height"`
			Duration    string         `json:"duration"`
			NbFrames    string         `json:"nb_frames"`
			Disposition map[string]int `json:"disposition"`
		} `json:"streams"`
	}
	if err := json.Unmarshal(out, &parsed); err != nil {
		return nil
	}
	info := &ffprobeInfo{}
	if d, err := time.ParseDuration(parsed.Format.Duration + "s"); err == nil {
		info.Duration = d
	}
	for _, s := range parsed.Streams {
		switch s.CodecType {
		case "video":
			info.Video = true
			if info.Width == 0 {
				info.Width, info.Height = s.Width, s.Height
			}
			if s.Disposition["attached_pic"] == 1 {
				info.HasArt = true
			}
		case "audio":
			info.Audio = true
		}
	}
	return info
}
