package main

import (
	"bytes"
	"fmt"
	"strings"
	"time"
)

type OSDMode int

const (
	OSDFull OSDMode = iota // 2 rows (info + helper/notifications)
	OSDMinimal             // 1 row (compact info bar)
	OSDHidden              // 0 rows (no OSD)
	OSDAuto                // Auto-hides after 3s of inactivity
)

func (m OSDMode) Name() string {
	switch m {
	case OSDFull:
		return "Full"
	case OSDMinimal:
		return "Minimal"
	case OSDHidden:
		return "Hidden"
	case OSDAuto:
		return "Auto-Hide"
	default:
		return "Full"
	}
}

type OSDState struct {
	Paused, Finished, Muted, Loop bool
	CurrentTime, TotalTime        time.Duration
	Volume                        int
	Mode                          RenderMode
	Notification                  string
	NotifyExpiry                  time.Time
	SeekStep                      time.Duration
	OSDView                       OSDMode
	Zoom                          ZoomMode
	Speed                         float64
	SubtitlesEnabled              bool
	ActiveSubtitle                string
	LastActionTime                time.Time
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
	if state.OSDView == OSDHidden || (state.OSDView == OSDAuto && !state.LastActionTime.IsZero() && time.Since(state.LastActionTime) > 3*time.Second) {
		buf.WriteString("\x1b[0m\x1b[2K\r\n\x1b[2K")
		return
	}

	status := "▶ PLAY"
	if state.Paused {
		status = "⏸ PAUSE"
	}
	if state.Finished {
		status = "⏹ END"
	}
	total := "LIVE"
	if state.TotalTime > 0 {
		total = formatDuration(state.TotalTime)
	}
	line := fmt.Sprintf("%s %s/%s", status, formatDuration(state.CurrentTime), total)

	vol := fmt.Sprintf("VOL %d%%", state.Volume)
	if state.Muted {
		vol = "🔇 MUTED"
	} else if state.Volume == 0 {
		vol = "🔈 0%"
	} else if state.Volume < 50 {
		vol = fmt.Sprintf("🔉 %d%%", state.Volume)
	} else {
		vol = fmt.Sprintf("🔊 %d%%", state.Volume)
	}

	extras := []string{vol, state.Mode.ShortName()}
	if state.Speed > 0 && state.Speed != 1.0 {
		extras = append(extras, fmt.Sprintf("⚡%.2gx", state.Speed))
	}
	if state.Zoom != ZoomFit {
		extras = append(extras, state.Zoom.ShortName())
	}
	if state.SubtitlesEnabled {
		extras = append(extras, "SUB")
	}
	if state.Loop {
		extras = append(extras, "LOOP")
	}

	for _, extra := range extras {
		if len([]rune(line))+len([]rune(extra))+1 <= width {
			line += " " + extra
		}
	}

	barWidth := min(width-len([]rune(line))-3, 40)
	if barWidth >= 5 && state.TotalTime > 0 {
		ratio := min(1.0, max(0.0, float64(state.CurrentTime)/float64(state.TotalTime)))
		subBlocks := []rune{'─', '▏', '▎', '▍', '▌', '▋', '▊', '▉'}
		totalSubUnits := int(ratio * float64(barWidth*8))
		fullCells := totalSubUnits / 8
		subRemainder := totalSubUnits % 8

		var bar strings.Builder
		bar.WriteString(" [")
		for i := 0; i < fullCells && i < barWidth; i++ {
			bar.WriteRune('█')
		}
		if fullCells < barWidth {
			bar.WriteRune(subBlocks[subRemainder])
			for i := fullCells + 1; i < barWidth; i++ {
				bar.WriteRune('─')
			}
		}
		bar.WriteByte(']')
		line += bar.String()
	}

	step := state.SeekStep
	if step == 0 {
		step = 5 * time.Second
	}
	helper := fmt.Sprintf("Space pause | A/D +/-%gs | W/S vol | M mode | O osd | Z zoom | [ / ] spd | . step | P shot | T sub | Q quit", step.Seconds())
	isNotify := false
	if time.Now().Before(state.NotifyExpiry) && state.Notification != "" {
		helper = "🔔 " + state.Notification
		isNotify = true
	}

	if state.OSDView == OSDMinimal {
		buf.WriteString("\x1b[0m\x1b[2K\x1b[48;2;18;24;38m\x1b[38;2;241;245;249m")
		buf.WriteString(fitText(line, width))
		buf.WriteString("\x1b[0m\r\n\x1b[2K")
		return
	}

	// Row 1: Slate Frosted Glass with bright TrueColor text
	buf.WriteString("\x1b[0m\x1b[2K\x1b[48;2;18;24;38m\x1b[38;2;241;245;249m")
	buf.WriteString(fitText(line, width))
	buf.WriteString("\x1b[0m\r\n")

	// Row 2: Helper row (amber alert when notification active, else dark slate)
	if isNotify {
		buf.WriteString("\x1b[2K\x1b[48;2;28;25;23m\x1b[38;2;251;191;36m\x1b[1m")
	} else {
		buf.WriteString("\x1b[2K\x1b[48;2;13;17;23m\x1b[38;2;148;163;184m")
	}
	buf.WriteString(fitText(helper, width))
	buf.WriteString("\x1b[0m")
}
