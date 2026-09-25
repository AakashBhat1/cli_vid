//go:build !windows

package main

import "os"

func enableConsoleInput() error { return nil }

func initConsole() (func(), error) {
	os.Stdout.WriteString("\x1b[?1049h\x1b[?25l")
	restore := func() {
		os.Stdout.WriteString("\x1b[?2025l\x1b[0m\x1b[?25h\x1b[?1049l")
	}
	return restore, nil
}
