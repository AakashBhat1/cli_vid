package main

import (
	"testing"
	"time"
)

func TestParseSRT(t *testing.T) {
	srtData := `1
00:00:01,500 --> 00:00:04,000
Welcome to cli_vid!

2
00:00:05,000 --> 00:00:08,250
High performance terminal
video playback with TrueColor.

3
00:00:10.000 --> 00:00:12.000 X1:100 Y1:200
Enjoy the show!
`
	entries, err := ParseSRT([]byte(srtData))
	if err != nil {
		t.Fatalf("ParseSRT error: %v", err)
	}
	if len(entries) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(entries))
	}

	if entries[0].Start != 1500*time.Millisecond || entries[0].End != 4*time.Second {
		t.Errorf("entry 0 bad timing: %v - %v", entries[0].Start, entries[0].End)
	}
	if entries[0].Text != "Welcome to cli_vid!" {
		t.Errorf("entry 0 bad text: %q", entries[0].Text)
	}

	if entries[1].Text != "High performance terminal video playback with TrueColor." {
		t.Errorf("entry 1 bad multiline text: %q", entries[1].Text)
	}

	// Test lookup
	if got := FindSubtitle(entries, 500*time.Millisecond); got != "" {
		t.Errorf("expected empty before first sub, got %q", got)
	}
	if got := FindSubtitle(entries, 2*time.Second); got != "Welcome to cli_vid!" {
		t.Errorf("expected entry 0, got %q", got)
	}
	if got := FindSubtitle(entries, 6*time.Second); got != "High performance terminal video playback with TrueColor." {
		t.Errorf("expected entry 1, got %q", got)
	}
	if got := FindSubtitle(entries, 11*time.Second); got != "Enjoy the show!" {
		t.Errorf("expected entry 2, got %q", got)
	}
}
