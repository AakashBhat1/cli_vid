package main

import (
	"context"
	"encoding/json"
	"fmt"
	"image"
	"io"
	"math"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

type VideoInfo struct {
	Width, Height int
	Duration      time.Duration
	FPS           float64
	HasAudio      bool
}

type probeStream struct {
	CodecType    string `json:"codec_type"`
	Width        int    `json:"width"`
	Height       int    `json:"height"`
	RFrameRate   string `json:"r_frame_rate"`
	AvgFrameRate string `json:"avg_frame_rate"`
	Duration     string `json:"duration"`
	Disposition  struct {
		AttachedPic int `json:"attached_pic"`
	} `json:"disposition"`
}
type ffprobeOutput struct {
	Streams []probeStream `json:"streams"`
	Format  struct {
		Duration string `json:"duration"`
	} `json:"format"`
}

func ProbeVideo(path string) (*VideoInfo, error) {
	return probeVideo(context.Background(), path)
}

func probeVideo(parent context.Context, path string) (*VideoInfo, error) {
	ctx, cancel := context.WithTimeout(parent, 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "ffprobe", "-v", "error",
		"-show_entries", "stream=codec_type,width,height,r_frame_rate,avg_frame_rate,duration:stream_disposition=attached_pic:format=duration",
		"-of", "json", "-i", path)
	out, err := cmd.Output()
	if err != nil {
		if ctx.Err() != nil {
			return nil, fmt.Errorf("ffprobe: %w", ctx.Err())
		}
		if e, ok := err.(*exec.ExitError); ok {
			return nil, fmt.Errorf("ffprobe: %s", strings.TrimSpace(string(e.Stderr)))
		}
		return nil, fmt.Errorf("ffprobe: %w", err)
	}
	return parseVideoInfo(out)
}

func parseRate(s string) float64 {
	parts := strings.SplitN(s, "/", 2)
	n, _ := strconv.ParseFloat(parts[0], 64)
	if len(parts) == 2 {
		d, _ := strconv.ParseFloat(parts[1], 64)
		if d <= 0 {
			return 0
		}
		n /= d
	}
	if math.IsNaN(n) || math.IsInf(n, 0) || n <= 0 || n > 1000 {
		return 0
	}
	return n
}

func parseDuration(s string) time.Duration {
	n, err := strconv.ParseFloat(s, 64)
	if err != nil || math.IsNaN(n) || math.IsInf(n, 0) || n <= 0 || n >= float64(math.MaxInt64)/float64(time.Second) {
		return 0
	}
	return time.Duration(n * float64(time.Second))
}

func parseVideoInfo(out []byte) (*VideoInfo, error) {
	var data ffprobeOutput
	if err := json.Unmarshal(out, &data); err != nil {
		return nil, fmt.Errorf("invalid ffprobe JSON: %w", err)
	}
	info := &VideoInfo{FPS: 30, Duration: parseDuration(data.Format.Duration)}
	for _, s := range data.Streams {
		if s.CodecType == "audio" {
			info.HasAudio = true
		}
		// Match ffmpeg's first non-attached video stream (0:V:0).
		if s.CodecType != "video" || s.Disposition.AttachedPic != 0 || info.Width != 0 {
			continue
		}
		if s.Width <= 0 || s.Height <= 0 {
			return nil, fmt.Errorf("video has invalid dimensions")
		}
		info.Width, info.Height = s.Width, s.Height
		rate := parseRate(s.AvgFrameRate)
		if rate == 0 {
			rate = parseRate(s.RFrameRate)
		}
		if rate > 0 {
			info.FPS = rate
		}
		if info.Duration == 0 {
			info.Duration = parseDuration(s.Duration)
		}
	}
	if info.Width == 0 {
		return nil, fmt.Errorf("input has no playable video stream")
	}
	return info, nil
}

type FrameResult struct {
	Pixels []byte
	Err    error
}

// FrameStream owns the decoder and a fixed pool of four frame buffers.
// The UI releases a buffer only after it has stopped rendering it.
type FrameStream struct {
	cmd      *exec.Cmd
	stdout   io.ReadCloser
	Frames   <-chan FrameResult
	free     chan []byte
	cancel   chan struct{}
	done     chan struct{}
	once     sync.Once
	previous []byte
}

type cappedLog struct{ data []byte }

func (b *cappedLog) Write(p []byte) (int, error) {
	n := len(p)
	const limit = 8192
	if len(p) >= limit {
		b.data = append(b.data[:0], p[len(p)-limit:]...)
		return n, nil
	}
	if len(b.data)+len(p) > limit {
		b.data = b.data[len(b.data)+len(p)-limit:]
	}
	b.data = append(b.data, p...)
	return n, nil
}

func StartFrameStream(file string, startAt time.Duration, w, h int, fps float64) (*FrameStream, error) {
	if w <= 0 || h <= 0 || w > 4096 || h > 4096 || w*h > 16*1024*1024 {
		return nil, fmt.Errorf("invalid decode dimensions %dx%d", w, h)
	}
	if fps <= 0 || fps > 120 || math.IsNaN(fps) || math.IsInf(fps, 0) {
		return nil, fmt.Errorf("invalid frame rate")
	}
	if startAt < 0 {
		return nil, fmt.Errorf("negative start time")
	}
	filter := fmt.Sprintf("fps=%.6f,scale=%d:%d:flags=lanczos", fps, w, h)
	cmd := exec.Command("ffmpeg", "-nostdin", "-hide_banner", "-loglevel", "error",
		"-ss", fmt.Sprintf("%.6f", startAt.Seconds()), "-i", file,
		"-map", "0:V:0", "-an", "-sn", "-dn", "-vf", filter,
		"-f", "rawvideo", "-pix_fmt", "rgb24", "pipe:1")
	log := &cappedLog{}
	cmd.Stderr = log
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err = cmd.Start(); err != nil {
		stdout.Close()
		return nil, fmt.Errorf("start ffmpeg: %w", err)
	}
	frames := make(chan FrameResult, 1)
	fs := &FrameStream{cmd: cmd, stdout: stdout, Frames: frames, free: make(chan []byte, 4),
		cancel: make(chan struct{}), done: make(chan struct{})}
	for i := 0; i < cap(fs.free); i++ {
		fs.free <- make([]byte, w*h*3)
	}
	go func() {
		defer close(fs.done)
		defer close(frames)
		var readErr error
	decode:
		for {
			var buf []byte
			select {
			case buf = <-fs.free:
			case <-fs.cancel:
				break decode
			}
			if _, readErr = io.ReadFull(stdout, buf); readErr != nil {
				break
			}
			select {
			case frames <- FrameResult{Pixels: buf}:
			case <-fs.cancel:
				break decode
			}
		}
		// Every started child is waited exactly once, including cancellation.
		stdout.Close()
		if readErr != nil && readErr != io.EOF {
			_ = cmd.Process.Kill()
		}
		waitErr := cmd.Wait()
		select {
		case <-fs.cancel:
			return
		default:
		}
		err := readErr
		if waitErr != nil {
			err = fmt.Errorf("ffmpeg: %w: %s", waitErr, strings.TrimSpace(string(log.data)))
		}
		if err == nil {
			err = io.EOF
		}
		select {
		case frames <- FrameResult{Err: err}:
		case <-fs.cancel:
		}
	}()
	return fs, nil
}

func (fs *FrameStream) Release(frame []byte) {
	if frame == nil {
		return
	}
	select {
	case fs.free <- frame:
	case <-fs.cancel:
	}
}

// ReadNextFrame is a synchronous convenience API. Its bytes are valid until the next call.
func (fs *FrameStream) ReadNextFrame() ([]byte, error) {
	fs.Release(fs.previous)
	result, ok := <-fs.Frames
	fs.previous = result.Pixels
	if !ok {
		return nil, io.EOF
	}
	return result.Pixels, result.Err
}

func (fs *FrameStream) Close() {
	if fs == nil {
		return
	}
	fs.once.Do(func() {
		close(fs.cancel)
		_ = fs.cmd.Process.Kill()
		_ = fs.stdout.Close()
		<-fs.done
	})
}

type ZoomMode int

const (
	ZoomFit ZoomMode = iota // Letterbox/Pillarbox (default)
	ZoomFill                // Zoom/Crop to fill without black bars
	ZoomStretch             // Stretch to fill entire grid
)

func (z ZoomMode) Name() string {
	switch z {
	case ZoomFit:
		return "Fit (Letterbox)"
	case ZoomFill:
		return "Fill (Crop)"
	case ZoomStretch:
		return "Stretch"
	default:
		return "Fit"
	}
}

func (z ZoomMode) ShortName() string {
	switch z {
	case ZoomFit:
		return "FIT"
	case ZoomFill:
		return "FILL"
	case ZoomStretch:
		return "STRETCH"
	default:
		return "FIT"
	}
}

func clampByte(v int) byte {
	if v < 0 {
		return 0
	}
	if v > 255 {
		return 255
	}
	return byte(v)
}

// ResampleBilinear resamples raw RGB24 bytes to image.RGBA with smooth bilinear filtering and aspect-ratio preservation
func ResampleBilinear(src []byte, srcW, srcH, dstW, dstH int, outImg *image.RGBA) {
	resampleAspect(src, srcW, srcH, dstW, dstH, outImg, 1)
}

// pixelAspect is the display width / height of one sample in the render mode.
func resampleAspect(src []byte, srcW, srcH, dstW, dstH int, outImg *image.RGBA, pixelAspect float64) {
	resampleAspectZoomColor(src, srcW, srcH, dstW, dstH, outImg, pixelAspect, ZoomFit, 0, 1.0)
}

// resampleAspectZoomColor performs high-speed multi-threaded bilinear resampling with zoom modes and color adjustments
func resampleAspectZoomColor(src []byte, srcW, srcH, dstW, dstH int, outImg *image.RGBA, pixelAspect float64, zoom ZoomMode, brightness int, contrast float64) {
	if srcW <= 0 || srcH <= 0 || dstW <= 0 || dstH <= 0 || outImg == nil || len(src)/3/srcW < srcH || outImg.Rect.Dx() < dstW || outImg.Rect.Dy() < dstH {
		return
	}

	srcAspect := float64(srcW) / float64(srcH) / pixelAspect
	dstAspect := float64(dstW) / float64(dstH)

	var targetW, targetH int
	var startX, startY int

	switch zoom {
	case ZoomFill:
		if srcAspect > dstAspect {
			targetH = dstH
			targetW = max(1, int(float64(dstH)*srcAspect))
			startX = (dstW - targetW) / 2
			startY = 0
		} else {
			targetW = dstW
			targetH = max(1, int(float64(dstW)/srcAspect))
			startX = 0
			startY = (dstH - targetH) / 2
		}
	case ZoomStretch:
		targetW = dstW
		targetH = dstH
		startX = 0
		startY = 0
	default: // ZoomFit
		if srcAspect > dstAspect {
			targetW = dstW
			targetH = max(1, int(float64(dstW)/srcAspect))
			startX = 0
			startY = (dstH - targetH) / 2
		} else {
			targetH = dstH
			targetW = max(1, int(float64(dstH)*srcAspect))
			startX = (dstW - targetW) / 2
			startY = 0
		}
	}

	// Fill background black
	for i := 0; i < len(outImg.Pix); i += 4 {
		outImg.Pix[i] = 0
		outImg.Pix[i+1] = 0
		outImg.Pix[i+2] = 0
		outImg.Pix[i+3] = 255
	}

	// Bilinear resampling with fixed-point math for maximum speed
	xRatio := ((srcW - 1) << 16) / max(1, targetW-1)
	yRatio := ((srcH - 1) << 16) / max(1, targetH-1)

	numWorkers := 1
	if targetH >= 32 {
		numWorkers = runtime.GOMAXPROCS(0)
		if numWorkers > 4 {
			numWorkers = 4
		}
	}

	processRows := func(yStart, yEnd int) {
		for y := yStart; y < yEnd; y++ {
			destY := startY + y
			if destY < 0 || destY >= dstH {
				continue
			}

			sy := (y * yRatio) >> 16
			yDiff := (y * yRatio) & 0xFFFF
			yDiffInv := 0x10000 - yDiff

			destRowOffset := destY * outImg.Stride
			srcRow0 := sy * srcW * 3
			srcRow1 := (sy + 1) * srcW * 3
			if sy+1 >= srcH {
				srcRow1 = srcRow0
			}

			for x := 0; x < targetW; x++ {
				destX := startX + x
				if destX < 0 || destX >= dstW {
					continue
				}

				sx := (x * xRatio) >> 16
				xDiff := (x * xRatio) & 0xFFFF
				xDiffInv := 0x10000 - xDiff

				sx0 := sx * 3
				sx1 := (sx + 1) * 3
				if sx+1 >= srcW {
					sx1 = sx0
				}

				// 4 neighbor pixel samples
				p00 := srcRow0 + sx0
				p10 := srcRow0 + sx1
				p01 := srcRow1 + sx0
				p11 := srcRow1 + sx1

				// Weights
				w00 := (xDiffInv * yDiffInv) >> 16
				w10 := (xDiff * yDiffInv) >> 16
				w01 := (xDiffInv * yDiff) >> 16
				w11 := (xDiff * yDiff) >> 16

				r := (int(src[p00])*w00 + int(src[p10])*w10 + int(src[p01])*w01 + int(src[p11])*w11) >> 16
				g := (int(src[p00+1])*w00 + int(src[p10+1])*w10 + int(src[p01+1])*w01 + int(src[p11+1])*w11) >> 16
				b := (int(src[p00+2])*w00 + int(src[p10+2])*w10 + int(src[p01+2])*w01 + int(src[p11+2])*w11) >> 16

				if contrast != 1.0 || brightness != 0 {
					r = int(float64(r-128)*contrast) + 128 + brightness
					g = int(float64(g-128)*contrast) + 128 + brightness
					b = int(float64(b-128)*contrast) + 128 + brightness
				}

				dOffset := destRowOffset + destX*4
				outImg.Pix[dOffset] = clampByte(r)
				outImg.Pix[dOffset+1] = clampByte(g)
				outImg.Pix[dOffset+2] = clampByte(b)
				outImg.Pix[dOffset+3] = 255
			}
		}
	}

	if numWorkers <= 1 {
		processRows(0, targetH)
	} else {
		var wg sync.WaitGroup
		chunkSize := (targetH + numWorkers - 1) / numWorkers
		for w := 0; w < numWorkers; w++ {
			y0 := w * chunkSize
			y1 := y0 + chunkSize
			if y1 > targetH {
				y1 = targetH
			}
			if y0 >= targetH {
				break
			}
			wg.Add(1)
			go func(start, end int) {
				defer wg.Done()
				processRows(start, end)
			}(y0, y1)
		}
		wg.Wait()
	}
}
