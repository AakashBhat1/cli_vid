package main

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestKeyDecoder(t *testing.T) {
	for _, tc := range []struct {
		name   string
		chunks []string
		want   []ActionType
	}{
		{"batched keys", []string{" mruLq"}, []ActionType{ActionTogglePause, ActionNextMode, ActionRestart, ActionMute, ActionLoop, ActionQuit}},
		{"fragmented arrow", []string{"\x1b", "[", "D"}, []ActionType{ActionSeekBackward}},
		{"modified arrows", []string{"\x1b[1;5C\x1bOA"}, []ActionType{ActionSeekForward, ActionVolumeUp}},
		{"legacy arrow", []string{"\xe0", "\x50"}, []ActionType{ActionVolumeDown}},
		{"unknown sequence", []string{"\x1b[99~q"}, []ActionType{ActionQuit}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var decoder KeyDecoder
			var got []ActionType
			for _, chunk := range tc.chunks {
				got = append(got, decoder.Feed([]byte(chunk))...)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
		})
	}
}

func TestReadActionsEOF(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	actions := make(chan ActionType, 4)
	go ReadActions(ctx, strings.NewReader("mq"), actions)
	var got []ActionType
	for a := range actions {
		got = append(got, a)
	}
	if !reflect.DeepEqual(got, []ActionType{ActionNextMode, ActionQuit}) {
		t.Fatal(got)
	}
}
