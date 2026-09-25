package main

import (
	"context"
	"io"
	"time"
)

type ActionType int

const (
	ActionNone ActionType = iota
	ActionTogglePause
	ActionSeekForward
	ActionSeekBackward
	ActionVolumeUp
	ActionVolumeDown
	ActionNextMode
	ActionRestart
	ActionQuit
	ActionMute
	ActionLoop
)

// KeyDecoder retains fragmented escape sequences between reads.
type KeyDecoder struct{ pending []byte }

func (d *KeyDecoder) Feed(data []byte) []ActionType {
	d.pending = append(d.pending, data...)
	var actions []ActionType
	for len(d.pending) > 0 {
		b := d.pending[0]
		n, action := 1, ActionNone
		if b == 27 {
			if len(d.pending) < 2 {
				break
			}
			if d.pending[1] == '[' || d.pending[1] == 'O' {
				n = 2
				for n < len(d.pending) && (d.pending[n] < 0x40 || d.pending[n] > 0x7e) {
					n++
				}
				if n == len(d.pending) {
					if n > 32 {
						d.pending = nil
					}
					break
				}
				action = arrowAction(d.pending[n])
				n++
			} else {
				action = ActionQuit
			}
		} else if b == 0 || b == 0xe0 {
			if len(d.pending) < 2 {
				break
			}
			n = 2
			switch d.pending[1] {
			case 0x48:
				action = ActionVolumeUp
			case 0x50:
				action = ActionVolumeDown
			case 0x4d:
				action = ActionSeekForward
			case 0x4b:
				action = ActionSeekBackward
			}
		} else {
			switch b {
			case ' ':
				action = ActionTogglePause
			case 'q', 'Q', 3:
				action = ActionQuit
			case 'm', 'M':
				action = ActionNextMode
			case 'r', 'R':
				action = ActionRestart
			case 'w', 'W', '+', '=':
				action = ActionVolumeUp
			case 's', 'S', '-':
				action = ActionVolumeDown
			case 'd', 'D':
				action = ActionSeekForward
			case 'a', 'A':
				action = ActionSeekBackward
			case 'u', 'U':
				action = ActionMute
			case 'l', 'L':
				action = ActionLoop
			}
		}
		d.pending = d.pending[n:]
		if action != ActionNone {
			actions = append(actions, action)
		}
	}
	return actions
}

func arrowAction(b byte) ActionType {
	switch b {
	case 'A':
		return ActionVolumeUp
	case 'B':
		return ActionVolumeDown
	case 'C':
		return ActionSeekForward
	case 'D':
		return ActionSeekBackward
	}
	return ActionNone
}

// ReadActions stops on EOF instead of spinning. An Escape timeout distinguishes
// a lone Escape from an arrow sequence split across terminal reads.
func ReadActions(ctx context.Context, reader io.Reader, out chan<- ActionType) {
	defer close(out)
	chunks := make(chan []byte)
	go func() {
		defer close(chunks)
		var buf [64]byte
		for {
			n, err := reader.Read(buf[:])
			if n > 0 {
				data := append([]byte(nil), buf[:n]...)
				select {
				case chunks <- data:
				case <-ctx.Done():
					return
				}
			}
			if err != nil || n == 0 {
				return
			}
		}
	}()
	var decoder KeyDecoder
	timer := time.NewTimer(time.Hour)
	timer.Stop()
	defer timer.Stop()
	var timeout <-chan time.Time
	send := func(action ActionType) bool {
		select {
		case out <- action:
			return true
		case <-ctx.Done():
			return false
		}
	}
	for {
		select {
		case <-ctx.Done():
			return
		case <-timeout:
			if len(decoder.pending) == 1 && decoder.pending[0] == 27 {
				if !send(ActionQuit) {
					return
				}
			}
			decoder.pending = nil
			timeout = nil
		case data, ok := <-chunks:
			if !ok {
				return
			}
			for _, action := range decoder.Feed(data) {
				if !send(action) {
					return
				}
			}
			if len(decoder.pending) > 0 {
				timer.Reset(80 * time.Millisecond)
				timeout = timer.C
			} else {
				timer.Stop()
				timeout = nil
			}
		}
	}
}
