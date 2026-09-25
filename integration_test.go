package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func requireMediaTools(t *testing.T) {
	t.Helper()
	if testing.Short() {
		t.Skip("media integration test")
	}
	for _, tool := range []string{"ffmpeg", "ffprobe"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skip(tool + " unavailable")
		}
	}
}

func testVideo(t *testing.T) string {
	t.Helper()
	requireMediaTools(t)
	file := filepath.Join(t.TempDir(), "fixture.mkv")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "ffmpeg", "-nostdin", "-v", "error", "-f", "lavfi", "-i", "testsrc2=size=160x90:rate=12",
		"-f", "lavfi", "-i", "sine=frequency=440:sample_rate=8000", "-t", "0.5", "-c:v", "ffv1", "-c:a", "pcm_s16le", file)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("generate fixture: %v: %s", err, out)
	}
	return file
}

func TestDecodeEndAndSeek(t *testing.T) {
	file := testVideo(t)
	for _, start := range []time.Duration{0, 250 * time.Millisecond} {
		stream, err := StartFrameStream(file, start, 80, 45, 12)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(stream.Close)
		count := 0
		for {
			select {
			case result := <-stream.Frames:
				if result.Err != nil {
					if !errors.Is(result.Err, io.EOF) {
						t.Fatal(result.Err)
					}
					want := 6
					if start > 0 {
						want = 3
					}
					if count != want {
						t.Fatalf("seek %v: got %d frames, want %d", start, count, want)
					}
					stream.Close()
					goto next
				}
				if len(result.Pixels) != 80*45*3 {
					t.Fatal("wrong frame size")
				}
				stream.Release(result.Pixels)
				count++
			case <-time.After(5 * time.Second):
				t.Fatal("decode stalled")
			}
		}
	next:
	}
}

func TestStreamCloseWhileBackpressured(t *testing.T) {
	stream, err := StartFrameStream(testVideo(t), 0, 80, 45, 12)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(stream.Close)
	// Hold a frame while the decoder fills its remaining buffers.
	frame, err := stream.ReadNextFrame()
	if err != nil {
		t.Fatal(err)
	}
	copyOfFrame := append([]byte(nil), frame...)
	done := make(chan struct{})
	go func() { stream.Close(); stream.Close(); close(done) }()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("close blocked")
	}
	if !bytes.Equal(frame, copyOfFrame) {
		t.Fatal("frame mutated before being released")
	}
}

func TestDecoderFailureIsNotEOF(t *testing.T) {
	requireMediaTools(t)
	stream, err := StartFrameStream(filepath.Join(t.TempDir(), "missing.mp4"), 0, 8, 8, 30)
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	select {
	case result := <-stream.Frames:
		if result.Err == nil || errors.Is(result.Err, io.EOF) {
			t.Fatalf("expected decoder diagnostic, got %v", result.Err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("decoder error stalled")
	}
}

func TestAudioLifecycle(t *testing.T) {
	requireMediaTools(t)
	if _, err := exec.LookPath("ffplay"); err != nil {
		t.Skip("ffplay unavailable")
	}
	t.Setenv("SDL_AUDIODRIVER", "dummy")
	audio := NewAudioPlayer(testVideo(t), true)
	t.Cleanup(audio.Stop)
	if err := audio.Play(0); err != nil {
		t.Fatal(err)
	}
	if err := audio.SetVolume(40, 100*time.Millisecond, true); err != nil {
		t.Fatal(err)
	}
	if err := audio.Seek(0, true); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-audio.done:
		audio.cmd, audio.done = nil, nil
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("audio did not finish")
	}
	audio.Stop()
	audio.Stop()
}
