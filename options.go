package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"math"
	"time"
)

type options struct {
	Mode                                  RenderMode
	FPS                                   float64
	Width, Volume                         int
	Start, SeekStep                       time.Duration
	Muted, Loop, ExitOnEnd, NoAudio, Info bool
	Path                                  string
}

func parseOptions(args []string, output io.Writer) (options, error) {
	o := options{}
	flags := flag.NewFlagSet("cli_vid", flag.ContinueOnError)
	flags.SetOutput(output)
	mode := flags.String("mode", "block", "Render mode: block, ascii, braille, matrix (or 0-3)")
	quality := flags.String("quality", "high", "Maximum decode dimension: low (320px), medium (640px), high (1280px)")
	flags.Float64Var(&o.FPS, "fps", 30, "Playback frame rate, 1-120 (lower uses less CPU)")
	flags.IntVar(&o.Volume, "volume", 80, "Initial volume, 0-100")
	flags.DurationVar(&o.Start, "start", 0, "Start position, e.g. 30s or 1m20s")
	flags.DurationVar(&o.SeekStep, "seek-step", 5*time.Second, "Seek interval, e.g. 10s")
	flags.BoolVar(&o.Muted, "mute", false, "Start muted (U toggles mute)")
	flags.BoolVar(&o.NoAudio, "no-audio", false, "Disable the audio subprocess")
	flags.BoolVar(&o.Loop, "loop", false, "Replay automatically at the end")
	flags.BoolVar(&o.ExitOnEnd, "exit-on-end", false, "Exit when playback finishes")
	flags.BoolVar(&o.Info, "info", false, "Print media metadata as JSON and exit")
	flags.Usage = func() {
		fmt.Fprintln(output, "CLI Video Player\n\nUsage: cli_vid [options] <video-file-or-url>\n\nOptions:")
		flags.PrintDefaults()
		fmt.Fprintln(output, "\nKeys: Space pause | arrows or WASD seek/volume | M mode | U mute | L loop | R replay | Q/Esc quit")
	}
	if err := flags.Parse(args); err != nil {
		return o, err
	}
	if flags.NArg() != 1 {
		return o, errors.New("provide exactly one video file or URL; use -help for options")
	}
	o.Path = flags.Arg(0)
	switch *mode {
	case "block", "half-block", "0":
		o.Mode = ModeHalfBlock
	case "ascii", "1":
		o.Mode = ModeASCII
	case "braille", "2":
		o.Mode = ModeBraille
	case "matrix", "3":
		o.Mode = ModeMatrix
	default:
		return o, fmt.Errorf("invalid mode %q; use block, ascii, braille, matrix, or 0-3", *mode)
	}
	switch *quality {
	case "low":
		o.Width = 320
	case "medium":
		o.Width = 640
	case "high":
		o.Width = 1280
	default:
		return o, fmt.Errorf("invalid quality %q; use low, medium, or high", *quality)
	}
	if math.IsNaN(o.FPS) || math.IsInf(o.FPS, 0) || o.FPS < 1 || o.FPS > 120 {
		return o, errors.New("fps must be between 1 and 120")
	}
	if o.Volume < 0 || o.Volume > 100 {
		return o, errors.New("volume must be between 0 and 100")
	}
	if o.Start < 0 {
		return o, errors.New("start must be nonnegative")
	}
	if o.SeekStep <= 0 || o.SeekStep > 24*time.Hour {
		return o, errors.New("seek-step must be greater than zero and at most 24h")
	}
	return o, nil
}
