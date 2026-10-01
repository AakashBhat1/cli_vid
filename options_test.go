package main

import (
	"io"
	"testing"
	"time"
)

func TestOptions(t *testing.T) {
	for _, args := range [][]string{
		{}, {"-mode", "-1", "video.mp4"}, {"-fps", "0", "video.mp4"}, {"-fps", "NaN", "video.mp4"},
		{"-fps", "Inf", "video.mp4"}, {"-quality", "bad", "video.mp4"}, {"-volume", "101", "video.mp4"},
		{"-start", "-1s", "video.mp4"}, {"-seek-step", "0s", "video.mp4"}, {"-speed", "0", "video.mp4"},
		{"-brightness", "200", "video.mp4"}, {"-contrast", "0.01", "video.mp4"}, {"-zoom", "invalid", "video.mp4"},
		{"-osd", "unknown", "video.mp4"}, {"one", "two"},
	} {
		if _, err := parseOptions(args, io.Discard); err == nil {
			t.Errorf("accepted invalid options %v", args)
		}
	}
	o, err := parseOptions([]string{
		"-mode", "neon", "-quality", "low", "-start", "1m20s", "-mute", "-loop",
		"-zoom", "fill", "-osd", "minimal", "-speed", "1.5", "-brightness", "10",
		"-contrast", "1.2", "-sub", "test.srt", "movie.mp4",
	}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if o.Mode != ModeNeon || o.Width != 320 || o.Start != 80*time.Second || !o.Muted || !o.Loop ||
		o.Zoom != ZoomFill || o.OSD != OSDMinimal || o.Speed != 1.5 || o.Brightness != 10 ||
		o.Contrast != 1.2 || o.SubPath != "test.srt" {
		t.Fatalf("wrong options: %+v", o)
	}
}

func TestSeekBounds(t *testing.T) {
	if got := seekTarget(time.Second, -5*time.Second, 10*time.Second, time.Second); got != 0 {
		t.Fatal(got)
	}
	if got := seekTarget(8*time.Second, 5*time.Second, 10*time.Second, time.Second); got != 9*time.Second {
		t.Fatal(got)
	}
	if got := seekTarget(8*time.Second, 5*time.Second, 0, time.Second); got != 13*time.Second {
		t.Fatal(got)
	}
}
