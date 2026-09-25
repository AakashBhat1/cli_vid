//go:build windows

package main

import (
	"fmt"
	"os"

	"golang.org/x/sys/windows"
)

const cpUTF8 uint32 = 65001

// MakeRaw clears Windows VT input; enable it afterward for ANSI arrow keys.
func enableConsoleInput() error {
	handle := windows.Handle(os.Stdin.Fd())
	var mode uint32
	if err := windows.GetConsoleMode(handle, &mode); err != nil {
		return err
	}
	return windows.SetConsoleMode(handle, mode|windows.ENABLE_VIRTUAL_TERMINAL_INPUT)
}

func initConsole() (func(), error) {
	inputCP, _ := windows.GetConsoleCP()
	outputCP, _ := windows.GetConsoleOutputCP()
	// Ensure UTF-8 output encoding for Unicode and Braille characters
	_ = windows.SetConsoleOutputCP(cpUTF8)
	_ = windows.SetConsoleCP(cpUTF8)

	// Enable Virtual Terminal Processing for ANSI escapes (TrueColor, cursor pos, etc.)
	stdoutHandle := windows.Handle(os.Stdout.Fd())
	var originalStdoutMode uint32
	if err := windows.GetConsoleMode(stdoutHandle, &originalStdoutMode); err == nil {
		newMode := originalStdoutMode | windows.ENABLE_VIRTUAL_TERMINAL_PROCESSING
		if err := windows.SetConsoleMode(stdoutHandle, newMode); err != nil {
			_ = windows.SetConsoleCP(inputCP)
			_ = windows.SetConsoleOutputCP(outputCP)
			return func() {}, fmt.Errorf("enable terminal output: %w", err)
		}
	}

	stderrHandle := windows.Handle(os.Stderr.Fd())
	var originalStderrMode uint32
	if err := windows.GetConsoleMode(stderrHandle, &originalStderrMode); err == nil {
		newMode := originalStderrMode | windows.ENABLE_VIRTUAL_TERMINAL_PROCESSING
		_ = windows.SetConsoleMode(stderrHandle, newMode)
	}

	// Enable alternate screen buffer and hide cursor
	os.Stdout.WriteString("\x1b[?1049h\x1b[?25l")

	restore := func() {
		_ = windows.SetConsoleCP(inputCP)
		_ = windows.SetConsoleOutputCP(outputCP)
		// Restore normal screen buffer, show cursor, reset color
		os.Stdout.WriteString("\x1b[0m\x1b[?25h\x1b[?1049l")
		if originalStdoutMode != 0 {
			_ = windows.SetConsoleMode(stdoutHandle, originalStdoutMode)
		}
		if originalStderrMode != 0 {
			_ = windows.SetConsoleMode(stderrHandle, originalStderrMode)
		}
	}

	return restore, nil
}
