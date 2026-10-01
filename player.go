package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"io"
	"math"
	"os"
	"time"

	"golang.org/x/term"
)

func decodeDimensions(info *VideoInfo, limit int) (int, int) {
	factor := math.Min(1, float64(limit)/float64(max(info.Width, info.Height)))
	return max(1, int(float64(info.Width)*factor)), max(1, int(float64(info.Height)*factor))
}

func seekTarget(current, delta, total, frameDuration time.Duration) time.Duration {
	// Saturate before adding to avoid duration overflow on long live streams.
	if delta > 0 && current > time.Duration(math.MaxInt64)-delta {
		return current
	}
	target := max(0, current+delta)
	if total > 0 {
		target = min(target, max(0, total-frameDuration))
	}
	return target
}

var (
	speedPresets      = []float64{0.25, 0.5, 0.75, 1.0, 1.25, 1.5, 2.0}
	brightnessPresets = []int{0, 15, 30, -15}
	contrastPresets   = []float64{1.0, 1.3, 1.6, 0.8}
)

func cycleSpeed(current float64, up bool) float64 {
	idx := 3 // 1.0
	for i, s := range speedPresets {
		if math.Abs(s-current) < 0.05 {
			idx = i
			break
		}
	}
	if up && idx < len(speedPresets)-1 {
		idx++
	} else if !up && idx > 0 {
		idx--
	}
	return speedPresets[idx]
}

func cyclePresetInt(presets []int, current int) int {
	for i, p := range presets {
		if p == current {
			return presets[(i+1)%len(presets)]
		}
	}
	return presets[0]
}

func cyclePresetFloat(presets []float64, current float64) float64 {
	for i, p := range presets {
		if math.Abs(p-current) < 0.05 {
			return presets[(i+1)%len(presets)]
		}
	}
	return presets[0]
}

func renderSubtitleOverlay(buf *bytes.Buffer, subs []SubtitleEntry, curTime time.Duration, termW, termH int) {
	if len(subs) == 0 || termH < 4 {
		return
	}
	text := FindSubtitle(subs, curTime)
	if text == "" {
		return
	}
	runes := []rune(text)
	if len(runes) > termW-4 {
		runes = runes[:termW-4]
	}
	pad := max(0, (termW-len(runes))/2)
	fmt.Fprintf(buf, "\x1b[%d;%dH\x1b[0m\x1b[48;2;0;0;0m\x1b[38;2;254;240;138m\x1b[1m %s \x1b[0m", termH-3, pad+1, string(runes))
}

func play(ctx context.Context, opts options, info *VideoInfo, actions <-chan ActionType, warning string) error {
	w, h := decodeDimensions(info, opts.Width)
	fps := min(opts.FPS, info.FPS)
	frameDuration := time.Duration(float64(time.Second) / fps)
	stream, err := StartFrameStream(opts.Path, opts.Start, w, h, fps)
	if err != nil {
		return err
	}
	defer func() { stream.Close() }()
	audio := NewAudioPlayer(opts.Path, info.HasAudio && !opts.NoAudio)
	defer audio.Stop()

	var subtitles []SubtitleEntry
	if opts.SubPath != "" {
		if data, err := os.ReadFile(opts.SubPath); err == nil {
			if parsed, err := ParseSRT(data); err == nil && len(parsed) > 0 {
				subtitles = parsed
			}
		}
	}

	speed := opts.Speed
	if speed <= 0 {
		speed = 1.0
	}
	brightness := opts.Brightness
	contrast := opts.Contrast
	if contrast <= 0 {
		contrast = 1.0
	}

	state := OSDState{
		CurrentTime:      opts.Start,
		TotalTime:        info.Duration,
		Mode:             opts.Mode,
		Volume:           opts.Volume,
		Muted:            opts.Muted || opts.NoAudio,
		Loop:             opts.Loop,
		SeekStep:         opts.SeekStep,
		OSDView:          opts.OSD,
		Zoom:             opts.Zoom,
		Speed:            speed,
		SubtitlesEnabled: len(subtitles) > 0,
		LastActionTime:   time.Now(),
	}

	notify := func(message string) {
		state.Notification = message
		state.NotifyExpiry = time.Now().Add(2 * time.Second)
	}
	if warning != "" {
		notify(warning)
	} else if len(subtitles) > 0 {
		notify(fmt.Sprintf("Loaded %d subtitles", len(subtitles)))
	}

	audio.volume = opts.Volume
	audio.speed = state.Speed
	if state.Muted {
		audio.volume = 0
	}
	audioError := func(err error) {
		if err != nil {
			audio.Stop()
			audio.hasAudio = false
			state.Muted = true
			notify("Audio unavailable; video continues")
		}
	}

	var raw []byte
	var img *image.RGBA
	var frameBuf, out bytes.Buffer
	lastW, lastH := 0, 0
	frameDirty, displayDirty := true, true
	preview, audioStarted := false, false
	streamStart, frameIndex := opts.Start, int64(0)
	var nextFrameAt, lastDraw time.Time

	reopen := func(target time.Duration, paused bool) error {
		replacement, err := StartFrameStream(opts.Path, target, w, h, fps)
		if err != nil {
			return err
		}
		stream.Release(raw)
		raw = nil
		stream.Close()
		stream = replacement
		audio.Stop()
		state.CurrentTime, state.Paused, state.Finished = target, paused, false
		streamStart, frameIndex = target, 0
		nextFrameAt = time.Time{}
		audioStarted = false
		preview = paused
		displayDirty = true
		return nil
	}

	ticker := time.NewTicker(time.Second / 120)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case action, ok := <-actions:
			if !ok {
				return nil
			}
			displayDirty = true
			state.LastActionTime = time.Now()
			switch action {
			case ActionQuit:
				return nil
			case ActionTogglePause:
				if state.Finished {
					if err := reopen(0, false); err != nil {
						return err
					}
				} else {
					state.Paused = !state.Paused
					audio.Stop()
					audioStarted = false
					nextFrameAt = time.Time{}
				}
			case ActionSeekForward, ActionSeekBackward, ActionRestart:
				target, paused := time.Duration(0), state.Paused
				if action == ActionRestart {
					paused = false
				} else {
					delta := opts.SeekStep
					if action == ActionSeekBackward {
						delta = -delta
					}
					target = seekTarget(state.CurrentTime, delta, state.TotalTime, frameDuration)
				}
				if err := reopen(target, paused); err != nil {
					return err
				}
				notify("Seek " + formatDuration(target))
			case ActionStepForward, ActionStepBackward:
				if !state.Paused {
					state.Paused = true
					audio.Stop()
					audioStarted = false
				}
				delta := frameDuration
				if action == ActionStepBackward {
					delta = -delta
				}
				target := seekTarget(state.CurrentTime, delta, state.TotalTime, frameDuration)
				if err := reopen(target, true); err != nil {
					return err
				}
				notify("Step " + formatDuration(target))
			case ActionVolumeUp, ActionVolumeDown:
				delta := 10
				if action == ActionVolumeDown {
					delta = -10
				}
				state.Volume = min(100, max(0, state.Volume+delta))
				state.Muted = !audio.hasAudio
				audioError(audio.SetVolume(state.Volume, state.CurrentTime, audioStarted && !state.Paused))
				notify(fmt.Sprintf("Volume %d%%", state.Volume))
			case ActionMute:
				if !audio.hasAudio {
					notify("Audio is unavailable for this session")
					break
				}
				state.Muted = !state.Muted
				volume := state.Volume
				if state.Muted {
					volume = 0
				}
				audioError(audio.SetVolume(volume, state.CurrentTime, audioStarted && !state.Paused))
			case ActionLoop:
				state.Loop = !state.Loop
				notify(fmt.Sprintf("Loop: %t", state.Loop))
			case ActionNextMode:
				state.Mode = (state.Mode + 1) % modeCount
				frameDirty = true
				notify(state.Mode.Name())
			case ActionToggleOSD:
				state.OSDView = (state.OSDView + 1) % 4
				frameDirty, displayDirty = true, true
				notify("OSD: " + state.OSDView.Name())
			case ActionToggleZoom:
				state.Zoom = (state.Zoom + 1) % 3
				frameDirty, displayDirty = true, true
				notify("Zoom: " + state.Zoom.Name())
			case ActionSpeedUp, ActionSpeedDown:
				state.Speed = cycleSpeed(state.Speed, action == ActionSpeedUp)
				audioError(audio.SetSpeed(state.Speed, state.CurrentTime, audioStarted && !state.Paused))
				notify(fmt.Sprintf("Speed: %.2gx", state.Speed))
			case ActionSaveSnapshot:
				baseName, err := SaveSnapshot(img, frameBuf.Bytes(), state.CurrentTime)
				if err != nil {
					notify("Snapshot failed: " + err.Error())
				} else {
					notify("Saved " + baseName + ".png & .ans")
				}
			case ActionToggleSubtitles:
				if len(subtitles) == 0 {
					notify("No subtitles loaded")
				} else {
					state.SubtitlesEnabled = !state.SubtitlesEnabled
					frameDirty, displayDirty = true, true
					notify(fmt.Sprintf("Subtitles: %t", state.SubtitlesEnabled))
				}
			case ActionCycleBrightness:
				brightness = cyclePresetInt(brightnessPresets, brightness)
				frameDirty, displayDirty = true, true
				notify(fmt.Sprintf("Brightness: %+d", brightness))
			case ActionCycleContrast:
				contrast = cyclePresetFloat(contrastPresets, contrast)
				frameDirty, displayDirty = true, true
				notify(fmt.Sprintf("Contrast: %.1fx", contrast))
			}
		case now := <-ticker.C:
			audioError(audio.PollError())
			effectiveFrameDuration := frameDuration
			if state.Speed > 0 {
				effectiveFrameDuration = time.Duration(float64(frameDuration) / state.Speed)
			}
			// Catch up against an absolute deadline instead of adding render time
			// to every frame. Bounded work keeps controls responsive.
		advance:
			for i := 0; i < 8 && (!state.Paused || preview) && (nextFrameAt.IsZero() || !now.Before(nextFrameAt)); i++ {
				select {
				case result, ok := <-stream.Frames:
					if !ok {
						result.Err = io.EOF
					}
					if result.Err != nil {
						if !errors.Is(result.Err, io.EOF) {
							return result.Err
						}
						audio.Stop()
						audioStarted = false
						if frameIndex == 0 {
							return fmt.Errorf("decoder produced no frames at %s", formatDuration(streamStart))
						}
						if state.Loop {
							if err := reopen(0, false); err != nil {
								return err
							}
						} else if opts.ExitOnEnd {
							return nil
						} else {
							state.Paused, state.Finished = true, true
							if state.TotalTime > 0 {
								state.CurrentTime = state.TotalTime
							}
							notify("Finished | Space/R replay | Q quit")
						}
						displayDirty = true
						break advance
					}
					stream.Release(raw)
					raw = result.Pixels
					state.CurrentTime = streamStart + time.Duration(float64(frameIndex)*float64(time.Second)/fps)
					frameIndex++
					if state.TotalTime > 0 {
						state.CurrentTime = min(state.CurrentTime, state.TotalTime)
					}
					if nextFrameAt.IsZero() {
						nextFrameAt = now
					}
					nextFrameAt = nextFrameAt.Add(effectiveFrameDuration)
					frameDirty, displayDirty = true, true
					if !state.Paused && !audioStarted {
						audioError(audio.Play(state.CurrentTime))
						audioStarted = true
					}
					if preview {
						preview = false
						break advance
					}
				default:
					break advance
				}
			}

			termW, termH, err := term.GetSize(int(os.Stdout.Fd()))
			if err != nil {
				return fmt.Errorf("read terminal size: %w", err)
			}
			// Leave one column unused to avoid delayed autowrap on full rows.
			termW = max(1, termW-1)
			if termW != lastW || termH != lastH {
				frameDirty, displayDirty = true, true
				frameBuf.Reset()
				lastW, lastH = termW, termH
				out.Reset()
				out.WriteString("\x1b[2J")
				if _, err := os.Stdout.Write(out.Bytes()); err != nil {
					return err
				}
			}
			if !displayDirty && now.Sub(lastDraw) < 250*time.Millisecond {
				continue
			}
			if termH < 3 {
				out.Reset()
				out.WriteString("\x1b[H\x1b[0m\x1b[2K")
				out.WriteString(fitText("Enlarge terminal | Q quit", termW))
			} else {
				rendered := false
				if frameDirty && raw != nil {
					dw, dh := state.Mode.TargetDimensions(termW, termH-2)
					if img == nil || img.Rect.Dx() != dw || img.Rect.Dy() != dh {
						img = image.NewRGBA(image.Rect(0, 0, dw, dh))
					}
					resampleAspectZoomColor(raw, w, h, dw, dh, img, state.Mode.PixelAspect(), state.Zoom, brightness, contrast)
					frameBuf.Reset()
					frameBuf.WriteString("\x1b[0m\x1b[40m")
					RenderFrame(&frameBuf, img, termW, termH-2, state.Mode)
					frameDirty = false
					rendered = true
				}
				out.Reset()
				out.WriteString("\x1b[?2025h")
				if rendered {
					out.WriteString("\x1b[H")
					out.Write(frameBuf.Bytes())
				}
				if state.SubtitlesEnabled {
					renderSubtitleOverlay(&out, subtitles, state.CurrentTime, termW, termH)
				}
				fmt.Fprintf(&out, "\x1b[%d;1H", termH-1)
				RenderOSD(&out, &state, termW)
				out.WriteString("\x1b[?2025l")
			}
			if _, err := os.Stdout.Write(out.Bytes()); err != nil {
				return fmt.Errorf("write terminal: %w", err)
			}
			displayDirty = false
			lastDraw = now
		}
	}
}
