package main

import (
	"image"
	"os"
	"testing"
	"time"
)

func TestSaveSnapshot(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 10, 10))
	buf := []byte("\x1b[31mTest\x1b[0m")
	name, err := SaveSnapshot(img, buf, 65*time.Second)
	if err != nil {
		t.Fatalf("SaveSnapshot failed: %v", err)
	}
	defer os.Remove(name + ".png")
	defer os.Remove(name + ".ans")

	if _, err := os.Stat(name + ".png"); err != nil {
		t.Errorf("PNG file not created: %v", err)
	}
	if _, err := os.Stat(name + ".ans"); err != nil {
		t.Errorf("ANS file not created: %v", err)
	}
}
