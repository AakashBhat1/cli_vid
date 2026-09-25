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
	state := OSDState{CurrentTime: opts.Start, TotalTime: info.Duration, Mode: opts.Mode,
		Volume: opts.Volume, Muted: opts.Muted || opts.NoAudio, Loop: opts.Loop, SeekStep: opts.SeekStep}
	notify := func(message string) {
		state.Notification = message
		state.NotifyExpiry = time.Now().Add(2 * time.Second)
	}
	if warning != "" {
		notify(warning)
	}
	audio.volume = opts.Volume
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
			}
		case now := <-ticker.C:
			audioError(audio.PollError())
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
					nextFrameAt = nextFrameAt.Add(frameDuration)
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
					resampleAspect(raw, w, h, dw, dh, img, state.Mode.PixelAspect())
					frameBuf.Reset()
					frameBuf.WriteString("\x1b[0m\x1b[40m")
					RenderFrame(&frameBuf, img, termW, termH-2, state.Mode)
					frameDirty = false
					rendered = true
				}
				out.Reset()
				if rendered {
					out.WriteString("\x1b[H")
					out.Write(frameBuf.Bytes())
				}
				fmt.Fprintf(&out, "\x1b[%d;1H", termH-1)
				RenderOSD(&out, &state, termW)
			}
			if _, err := os.Stdout.Write(out.Bytes()); err != nil {
				return fmt.Errorf("write terminal: %w", err)
			}
			displayDirty = false
			lastDraw = now
		}
	}
}
