package main

import (
	"image"
	"testing"
)

func TestProbeParsing(t *testing.T) {
	for _, tc := range []struct {
		name, data string
		width      int
		fps        float64
		invalid    bool
	}{
		{"no video", `{"streams":[{"codec_type":"audio"}]}`, 0, 0, true},
		{"invalid dimensions", `{"streams":[{"codec_type":"video","width":0,"height":2}]}`, 0, 0, true},
		{"fallback rate", `{"streams":[{"codec_type":"video","width":10,"height":5,"avg_frame_rate":"0/0","r_frame_rate":"0/1"}]}`, 10, 30, false},
		{"average rate", `{"streams":[{"codec_type":"video","width":10,"height":5,"avg_frame_rate":"24/1","r_frame_rate":"30/1"}]}`, 10, 24, false},
		{"first video", `{"streams":[{"codec_type":"video","width":10,"height":5},{"codec_type":"video","width":20,"height":10}]}`, 10, 30, false},
		{"cover art", `{"streams":[{"codec_type":"video","width":2,"height":2,"disposition":{"attached_pic":1}},{"codec_type":"video","width":10,"height":5}]}`, 10, 30, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			info, err := parseVideoInfo([]byte(tc.data))
			if tc.invalid {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if info.Width != tc.width || info.FPS != tc.fps {
				t.Fatalf("unexpected info: %+v", info)
			}
		})
	}
}

func TestAspectAcrossModes(t *testing.T) {
	src := make([]byte, 16*9*3)
	for i := range src {
		src[i] = 255
	}
	for mode := ModeHalfBlock; mode < modeCount; mode++ {
		w, h := mode.TargetDimensions(80, 40)
		img := image.NewRGBA(image.Rect(0, 0, w, h))
		resampleAspect(src, 16, 9, w, h, img, mode.PixelAspect())
		minY, maxY := h, -1
		for y := 0; y < h; y++ {
			if img.Pix[y*img.Stride+(w/2)*4] > 0 {
				minY = min(minY, y)
				maxY = max(maxY, y)
			}
		}
		ratio := float64(w) * mode.PixelAspect() / float64(maxY-minY+1)
		if ratio < 1.7 || ratio > 1.85 {
			t.Errorf("%s: distorted aspect %f", mode.Name(), ratio)
		}
	}
}

func TestInvalidFrameOptions(t *testing.T) {
	if _, err := StartFrameStream("none", 0, 0, 100, 30); err == nil {
		t.Fatal("accepted zero width")
	}
	if _, err := StartFrameStream("none", 0, 100, 100, 0); err == nil {
		t.Fatal("accepted zero FPS")
	}
}
