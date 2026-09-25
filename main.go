package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"syscall"

	"golang.org/x/term"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	opts, err := parseOptions(args, os.Stderr)
	if errors.Is(err, flag.ErrHelp) {
		return nil
	}
	if err != nil {
		return err
	}
	u, _ := url.Parse(opts.Path)
	isURL := u != nil && (u.Scheme == "http" || u.Scheme == "https" || u.Scheme == "rtsp")
	if !isURL {
		stat, err := os.Stat(opts.Path)
		if err != nil {
			return err
		}
		if !stat.Mode().IsRegular() {
			return fmt.Errorf("input must be a regular video file")
		}
		opts.Path, err = filepath.Abs(opts.Path)
		if err != nil {
			return err
		}
	}
	if _, err := exec.LookPath("ffprobe"); err != nil {
		return fmt.Errorf("ffprobe is required; install FFmpeg and add its bin directory to PATH")
	}
	if !opts.Info {
		if _, err := exec.LookPath("ffmpeg"); err != nil {
			return fmt.Errorf("ffmpeg is required; install FFmpeg and add its bin directory to PATH")
		}
		if !term.IsTerminal(int(os.Stdin.Fd())) || !term.IsTerminal(int(os.Stdout.Fd())) {
			return fmt.Errorf("playback requires an interactive terminal; use -info for metadata")
		}
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	info, err := probeVideo(ctx, opts.Path)
	if err != nil {
		return err
	}
	if opts.Info {
		return json.NewEncoder(os.Stdout).Encode(struct {
			Width    int     `json:"width"`
			Height   int     `json:"height"`
			FPS      float64 `json:"fps"`
			Duration float64 `json:"duration_seconds"`
			HasAudio bool    `json:"has_audio"`
		}{info.Width, info.Height, info.FPS, info.Duration.Seconds(), info.HasAudio})
	}
	if info.Duration > 0 && opts.Start >= info.Duration {
		return fmt.Errorf("start must be before the end of the video (%s)", formatDuration(info.Duration))
	}
	audioWarning := ""
	if info.HasAudio && !opts.NoAudio {
		if _, err := exec.LookPath("ffplay"); err != nil {
			opts.NoAudio = true
			audioWarning = "ffplay unavailable: playing without audio"
		}
	}
	restore, err := initConsole()
	if err != nil {
		restore()
		return err
	}
	defer restore()
	fd := int(os.Stdin.Fd())
	oldState, err := term.MakeRaw(fd)
	if err != nil {
		return fmt.Errorf("enable raw input: %w", err)
	}
	defer term.Restore(fd, oldState)
	if err := enableConsoleInput(); err != nil {
		return err
	}
	actions := make(chan ActionType, 32)
	go ReadActions(ctx, os.Stdin, actions)
	return play(ctx, opts, info, actions, audioWarning)
}
