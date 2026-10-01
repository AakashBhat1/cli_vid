package main

import (
	"bufio"
	"bytes"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type SubtitleEntry struct {
	Index int
	Start time.Duration
	End   time.Duration
	Text  string
}

func parseSRTTimestamp(s string) (time.Duration, error) {
	s = strings.TrimSpace(s)
	s = strings.ReplaceAll(s, ",", ".")
	parts := strings.Split(s, ":")
	if len(parts) != 3 {
		return 0, os.ErrInvalid
	}
	h, err := strconv.Atoi(parts[0])
	if err != nil {
		return 0, err
	}
	m, err := strconv.Atoi(parts[1])
	if err != nil {
		return 0, err
	}
	secParts := strings.Split(parts[2], ".")
	sec, err := strconv.Atoi(secParts[0])
	if err != nil {
		return 0, err
	}
	ms := 0
	if len(secParts) > 1 {
		msStr := secParts[1]
		for len(msStr) < 3 {
			msStr += "0"
		}
		if len(msStr) > 3 {
			msStr = msStr[:3]
		}
		ms, _ = strconv.Atoi(msStr)
	}
	return time.Duration(h)*time.Hour + time.Duration(m)*time.Minute + time.Duration(sec)*time.Second + time.Duration(ms)*time.Millisecond, nil
}

// ParseSRT parses standard SRT subtitle data into a slice of SubtitleEntry.
func ParseSRT(data []byte) ([]SubtitleEntry, error) {
	scanner := bufio.NewScanner(bytes.NewReader(data))
	var entries []SubtitleEntry
	var current SubtitleEntry
	state := 0 // 0: index, 1: timestamp, 2: text

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		// Strip UTF-8 BOM if present on first line
		line = strings.TrimPrefix(line, "\xef\xbb\xbf")

		if line == "" {
			if current.Text != "" {
				entries = append(entries, current)
				current = SubtitleEntry{}
			}
			state = 0
			continue
		}

		switch state {
		case 0:
			idx, err := strconv.Atoi(line)
			if err == nil {
				current.Index = idx
				state = 1
			} else if strings.Contains(line, "-->") {
				// Index was omitted in malformed SRT, proceed directly to timestamps
				if parseTimestamps(line, &current) {
					state = 2
				}
			}
		case 1:
			if parseTimestamps(line, &current) {
				state = 2
			}
		case 2:
			if strings.Contains(line, "-->") {
				// Next timestamp without blank line separator
				if current.Text != "" {
					entries = append(entries, current)
					current = SubtitleEntry{}
				}
				if parseTimestamps(line, &current) {
					state = 2
				}
			} else {
				if current.Text != "" {
					current.Text += " " + line
				} else {
					current.Text = line
				}
			}
		}
	}
	if current.Text != "" {
		entries = append(entries, current)
	}
	return entries, scanner.Err()
}

func parseTimestamps(line string, entry *SubtitleEntry) bool {
	arrowIdx := strings.Index(line, "-->")
	if arrowIdx < 0 {
		return false
	}
	startStr := strings.TrimSpace(line[:arrowIdx])
	endStr := strings.TrimSpace(line[arrowIdx+3:])

	// Some SRT files append styling metadata after the end timestamp (e.g. X1:000 Y1:000)
	if spaceIdx := strings.IndexAny(endStr, " \t"); spaceIdx >= 0 {
		endStr = endStr[:spaceIdx]
	}

	start, err1 := parseSRTTimestamp(startStr)
	end, err2 := parseSRTTimestamp(endStr)
	if err1 != nil || err2 != nil {
		return false
	}
	entry.Start = start
	entry.End = end
	return true
}

// FindSubtitle returns the subtitle text matching time t, or empty string.
func FindSubtitle(entries []SubtitleEntry, t time.Duration) string {
	for i := range entries {
		if t >= entries[i].Start && t <= entries[i].End {
			return entries[i].Text
		}
	}
	return ""
}

// DetectSubtitleFile searches for a companion .srt file next to videoPath.
func DetectSubtitleFile(videoPath string) string {
	if videoPath == "" {
		return ""
	}
	ext := filepath.Ext(videoPath)
	base := strings.TrimSuffix(videoPath, ext)
	candidates := []string{
		base + ".srt",
		base + ".SRT",
		base + ".en.srt",
		base + ".default.srt",
	}
	for _, c := range candidates {
		if stat, err := os.Stat(c); err == nil && !stat.IsDir() {
			return c
		}
	}
	return ""
}
