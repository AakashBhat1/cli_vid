package main

import (
	"bytes"
	"image"
	"image/color"
	"testing"
)

func TestModesRender(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 160, 80))
	for y := 0; y < 80; y++ {
		for x := 0; x < 160; x++ {
			img.Set(x, y, color.RGBA{
				R: uint8(x * 3),
				G: uint8(y * 6),
				B: uint8((x + y) * 2),
				A: 255,
			})
		}
	}

	modes := []RenderMode{ModeHalfBlock, ModeASCII, ModeBraille, ModeMatrix}

	for _, mode := range modes {
		t.Run(mode.Name(), func(t *testing.T) {
			buf := &bytes.Buffer{}
			RenderFrame(buf, img, 80, 40, mode)
			if buf.Len() == 0 {
				t.Fatalf("Expected non-empty output for mode %s", mode.Name())
			}
		})
	}
}

func BenchmarkHalfBlock(b *testing.B) {
	img := image.NewRGBA(image.Rect(0, 0, 160, 80))
	buf := bytes.NewBuffer(make([]byte, 0, 64*1024))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buf.Reset()
		RenderFrame(buf, img, 120, 40, ModeHalfBlock)
	}
}

func BenchmarkBraille(b *testing.B) {
	img := image.NewRGBA(image.Rect(0, 0, 240, 160))
	buf := bytes.NewBuffer(make([]byte, 0, 64*1024))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buf.Reset()
		RenderFrame(buf, img, 120, 40, ModeBraille)
	}
}

func TestProbeVideo(t *testing.T) {
	info, err := ProbeVideo(testVideo(t))
	if err != nil {
		t.Fatalf("ProbeVideo failed: %v", err)
	}

	if info.Width != 160 || info.Height != 90 {
		t.Errorf("Unexpected dimensions: %dx%d (expected 160x90)", info.Width, info.Height)
	}

	if info.FPS != 12 {
		t.Errorf("Unexpected FPS: %.2f (expected 12)", info.FPS)
	}

	if !info.HasAudio {
		t.Errorf("Expected video to have audio")
	}

	t.Logf("Video probed successfully: %+v", info)
}
