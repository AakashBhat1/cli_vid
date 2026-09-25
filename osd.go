package main

import (
	"bytes"
	"fmt"
	"strings"
	"time"
)

type OSDState struct {
	Paused, Finished, Muted, Loop bool
	CurrentTime, TotalTime        time.Duration
	Volume                        int
	Mode                          RenderMode
	Notification                  string
	NotifyExpiry                  time.Time
	SeekStep                      time.Duration
}

func formatDuration(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	s := int64(d / time.Second)
	if s >= 3600 {
		return fmt.Sprintf("%02d:%02d:%02d", s/3600, s/60%60, s%60)
	}
	return fmt.Sprintf("%02d:%02d", s/60, s%60)
}

func fitText(s string, width int) string {
	if width <= 0 {
		return ""
	}
	r := []rune(s)
	if len(r) > width {
		return string(r[:width])
	}
	return s
}

// RenderOSD writes two bounded rows; no newline on the terminal's last row.
func RenderOSD(buf *bytes.Buffer, state *OSDState, width int) {
	status := "PLAY"
	if state.Paused {
		status = "PAUSED"
	}
	if state.Finished {
		status = "FINISHED"
	}
	total := "LIVE"
	if state.TotalTime > 0 {
		total = formatDuration(state.TotalTime)
	}
	line := fmt.Sprintf("%s %s/%s", status, formatDuration(state.CurrentTime), total)
	vol := fmt.Sprintf("VOL %d%%", state.Volume)
	if state.Muted {
		vol = "MUTED"
	}
	for _, extra := range []string{vol, state.Mode.ShortName(), "LOOP"} {
		if extra == "LOOP" && !state.Loop {
			continue
		}
		if len(line)+len(extra)+1 <= width {
			line += " " + extra
		}
	}
	barWidth := min(width-len(line)-3, 40)
	if barWidth >= 5 && state.TotalTime > 0 {
		ratio := min(1.0, max(0.0, float64(state.CurrentTime)/float64(state.TotalTime)))
		filled := int(ratio * float64(barWidth))
		line += " [" + strings.Repeat("=", filled) + strings.Repeat("-", barWidth-filled) + "]"
	}
	step := state.SeekStep
	if step == 0 {
		step = 5 * time.Second
	}
	helper := fmt.Sprintf("Space pause | A/D +/-%gs | W/S vol | M mode | U mute | L loop | R replay | Q quit", step.Seconds())
	if time.Now().Before(state.NotifyExpiry) {
		helper = state.Notification
	}
	buf.WriteString("\x1b[0m\x1b[2K\x1b[97;44m")
	buf.WriteString(fitText(line, width))
	buf.WriteString("\x1b[0m\r\n\x1b[2K\x1b[90m")
	buf.WriteString(fitText(helper, width))
	buf.WriteString("\x1b[0m")
}
