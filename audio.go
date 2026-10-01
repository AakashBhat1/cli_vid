package main

import (
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// AudioPlayer is controlled by the playback loop. The child waiter owns stderr.
type AudioPlayer struct {
	file     string
	cmd      *exec.Cmd
	done     chan error
	volume   int
	speed    float64
	hasAudio bool
}

func NewAudioPlayer(file string, hasAudio bool) *AudioPlayer {
	return &AudioPlayer{file: file, volume: 80, speed: 1.0, hasAudio: hasAudio}
}

func (a *AudioPlayer) Play(startAt time.Duration) error {
	a.Stop()
	if !a.hasAudio || a.volume == 0 {
		return nil
	}
	log := &cappedLog{}
	args := []string{"-nodisp", "-vn", "-autoexit", "-hide_banner", "-loglevel", "error",
		"-ss", fmt.Sprintf("%.6f", startAt.Seconds()), "-volume", fmt.Sprint(a.volume)}
	if a.speed != 1.0 && a.speed >= 0.5 && a.speed <= 2.0 {
		args = append(args, "-af", fmt.Sprintf("atempo=%.2f", a.speed))
	}
	args = append(args, "-i", a.file)

	cmd := exec.Command("ffplay", args...)
	cmd.Stderr = log
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start ffplay: %w", err)
	}
	a.cmd = cmd
	a.done = make(chan error, 1)
	done := a.done
	go func() {
		err := cmd.Wait()
		if err != nil {
			err = fmt.Errorf("ffplay: %w: %s", err, strings.TrimSpace(string(log.data)))
		}
		done <- err
	}()
	return nil
}

func (a *AudioPlayer) Stop() {
	if a.cmd == nil {
		return
	}
	_ = a.cmd.Process.Kill()
	<-a.done
	a.cmd, a.done = nil, nil
}

func (a *AudioPlayer) PollError() error {
	if a.done == nil {
		return nil
	}
	select {
	case err := <-a.done:
		a.cmd, a.done = nil, nil
		return err
	default:
		return nil
	}
}

func (a *AudioPlayer) Seek(target time.Duration, playing bool) error {
	a.Stop()
	if playing {
		return a.Play(target)
	}
	return nil
}

func (a *AudioPlayer) SetVolume(volume int, pos time.Duration, playing bool) error {
	a.volume = min(100, max(0, volume))
	return a.Seek(pos, playing)
}

func (a *AudioPlayer) SetSpeed(speed float64, pos time.Duration, playing bool) error {
	a.speed = speed
	return a.Seek(pos, playing)
}
