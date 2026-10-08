package main

import (
	"testing"
	"time"
)

func TestExistingHelmHookAndRejectOtherShellCommands(t *testing.T) {
	var delay time.Duration
	if code := hook([]string{"-c", "sleep 10"}, func(value time.Duration) { delay = value }); code != 0 || delay != 10*time.Second {
		t.Fatal("existing Helm lifecycle hook changed")
	}
	for _, args := range [][]string{nil, {"-c", "sleep 1"}, {"-c", "sleep 10; echo synthetic"}, {"-c", "sleep 10", "extra"}, {"-c", "$(sleep 10)"}} {
		if code := hook(args, func(time.Duration) { t.Fatal("unexpected command executed") }); code != 2 {
			t.Fatal("unsupported command accepted")
		}
	}
}
