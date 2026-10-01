package main

import (
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"time"
)

// SaveSnapshot saves the current RGBA image as a PNG and the rendered ANSI buffer as a .ans text file.
func SaveSnapshot(img *image.RGBA, ansiBuf []byte, currentTime time.Duration) (string, error) {
	secs := int64(currentTime / time.Second)
	m := secs / 60
	s := secs % 60
	base := fmt.Sprintf("snapshot_%02dm%02ds", m, s)

	// Ensure unique filename
	candidate := base
	for i := 1; ; i++ {
		pngPath := candidate + ".png"
		if _, err := os.Stat(pngPath); os.IsNotExist(err) {
			break
		}
		candidate = fmt.Sprintf("%s_%d", base, i)
	}

	pngPath := candidate + ".png"
	ansPath := candidate + ".ans"

	if img != nil {
		f, err := os.Create(pngPath)
		if err != nil {
			return "", fmt.Errorf("create png: %w", err)
		}
		err = png.Encode(f, img)
		f.Close()
		if err != nil {
			return "", fmt.Errorf("encode png: %w", err)
		}
	}

	if len(ansiBuf) > 0 {
		if err := os.WriteFile(ansPath, ansiBuf, 0644); err != nil {
			return "", fmt.Errorf("write ans: %w", err)
		}
	}

	return filepath.Base(candidate), nil
}
