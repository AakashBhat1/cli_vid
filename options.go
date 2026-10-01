package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"math"
	"strings"
	"time"
)

type options struct {
	Mode                                  RenderMode
	FPS                                   float64
	Width, Volume                         int
	Start, SeekStep                       time.Duration
	Muted, Loop, ExitOnEnd, NoAudio, Info bool
	Path                                  string
	SubPath                               string
	Zoom                                  ZoomMode
	OSD                                   OSDMode
	Speed                                 float64
	Brightness                            int
	Contrast                              float64
}

func parseOptions(args []string, output io.Writer) (options, error) {
	o := options{}
	flags := flag.NewFlagSet("cli_vid", flag.ContinueOnError)
	flags.SetOutput(output)
	mode := flags.String("mode", "block", "Render mode: block, ascii, braille, matrix, amber, neon, noir (or 0-6)")
	quality := flags.String("quality", "high", "Maximum decode dimension: low (320px), medium (640px), high (1280px)")
	zoom := flags.String("zoom", "fit", "Aspect ratio zoom mode: fit, fill, stretch")
	osd := flags.String("osd", "full", "On-screen display mode: full, minimal, hidden, auto")
	sub := flags.String("sub", "", "Path to .srt subtitle file (auto-detected if alongside video)")
	flags.Float64Var(&o.FPS, "fps", 30, "Playback frame rate, 1-120 (lower uses less CPU)")
	flags.Float64Var(&o.Speed, "speed", 1.0, "Initial playback speed, 0.25-4.0")
	flags.IntVar(&o.Brightness, "brightness", 0, "Brightness adjustment (-100 to 100)")
	flags.Float64Var(&o.Contrast, "contrast", 1.0, "Contrast multiplier (0.2 to 4.0)")
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
		fmt.Fprintln(output, "\nKeys: Space pause | arrows/WASD seek/vol | M mode | O osd | Z zoom | [ / ] spd | . step | P shot | T sub | Q/Esc quit")
	}
	if err := flags.Parse(args); err != nil {
		return o, err
	}
	if flags.NArg() != 1 {
		return o, errors.New("provide exactly one video file or URL; use -help for options")
	}
	o.Path = flags.Arg(0)
	o.SubPath = *sub

	switch strings.ToLower(*mode) {
	case "block", "half-block", "0":
		o.Mode = ModeHalfBlock
	case "ascii", "1":
		o.Mode = ModeASCII
	case "braille", "2":
		o.Mode = ModeBraille
	case "matrix", "3":
		o.Mode = ModeMatrix
	case "amber", "4":
		o.Mode = ModeAmber
	case "neon", "edge", "sobel", "5":
		o.Mode = ModeNeon
	case "noir", "gray", "grey", "mono", "6":
		o.Mode = ModeNoir
	default:
		return o, fmt.Errorf("invalid mode %q; use block, ascii, braille, matrix, amber, neon, noir, or 0-6", *mode)
	}

	switch strings.ToLower(*zoom) {
	case "fit", "letterbox":
		o.Zoom = ZoomFit
	case "fill", "crop":
		o.Zoom = ZoomFill
	case "stretch":
		o.Zoom = ZoomStretch
	default:
		return o, fmt.Errorf("invalid zoom %q; use fit, fill, or stretch", *zoom)
	}

	switch strings.ToLower(*osd) {
	case "full", "2":
		o.OSD = OSDFull
	case "minimal", "1":
		o.OSD = OSDMinimal
	case "hidden", "none", "0":
		o.OSD = OSDHidden
	case "auto":
		o.OSD = OSDAuto
	default:
		return o, fmt.Errorf("invalid osd %q; use full, minimal, hidden, or auto", *osd)
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
	if math.IsNaN(o.Speed) || math.IsInf(o.Speed, 0) || o.Speed < 0.25 || o.Speed > 4.0 {
		return o, errors.New("speed must be between 0.25 and 4.0")
	}
	if o.Brightness < -100 || o.Brightness > 100 {
		return o, errors.New("brightness must be between -100 and 100")
	}
	if math.IsNaN(o.Contrast) || math.IsInf(o.Contrast, 0) || o.Contrast < 0.2 || o.Contrast > 4.0 {
		return o, errors.New("contrast must be between 0.2 and 4.0")
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
