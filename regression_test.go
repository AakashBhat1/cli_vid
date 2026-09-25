package main

import (
	"bytes"
	"image"
	"regexp"
	"strings"
	"testing"
	"time"
)

func TestResampleThinImage(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 1, 1))
	ResampleBilinear(make([]byte, 3*1000), 1, 1000, 1, 1, img)
}

func TestResampleIncludesEdges(t *testing.T) {
	src := []byte{255, 0, 0, 0, 255, 0, 0, 0, 255, 255, 255, 255}
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	ResampleBilinear(src, 2, 2, 2, 2, img)
	for i := 0; i < 4; i++ {
		if !bytes.Equal(img.Pix[i*4:i*4+3], src[i*3:i*3+3]) {
			t.Errorf("pixel %d: got %v, want %v", i, img.Pix[i*4:i*4+3], src[i*3:i*3+3])
		}
	}
}

func TestOSDFitsTerminal(t *testing.T) {
	ansi := regexp.MustCompile(`\x1b\[[0-9;]*[A-Za-z]`)
	for _, width := range []int{1, 10, 20, 40, 80, 120} {
		var buf bytes.Buffer
		RenderOSD(&buf, &OSDState{Mode: ModeHalfBlock, TotalTime: time.Hour}, width)
		lines := strings.Split(ansi.ReplaceAllString(buf.String(), ""), "\r\n")
		if len(lines) != 2 {
			t.Errorf("width %d: expected exactly two rows without trailing newline", width)
		}
		for _, line := range lines {
			if len([]rune(line)) > width {
				t.Errorf("width %d: line overflows: %q", width, line)
			}
		}
	}
}

func BenchmarkColorSerialization(b *testing.B) {
	var buf bytes.Buffer
	buf.Grow(64)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buf.Reset()
		appendFgRGB(&buf, i%256, (i+50)%256, (i+100)%256)
		appendBgRGB(&buf, (i+150)%256, (i+200)%256, (i+250)%256)
	}
}

func BenchmarkDetailedFrame(b *testing.B) {
	img := image.NewRGBA(image.Rect(0, 0, 160, 88))
	for i := range img.Pix {
		img.Pix[i] = byte(i*17 + i/11)
	}
	var buf bytes.Buffer
	buf.Grow(512 * 1024)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buf.Reset()
		RenderFrame(&buf, img, 160, 44, ModeHalfBlock)
	}
}
