package cmd

import (
	"testing"
	"time"
)

func TestWaitForDoneNilChannel(t *testing.T) {
	t.Parallel()

	if !waitForDone(nil, 10*time.Millisecond) {
		t.Fatal("waitForDone(nil, timeout) = false, want true")
	}
}

func TestWaitForDoneClosedChannel(t *testing.T) {
	t.Parallel()

	done := make(chan struct{})
	close(done)

	if !waitForDone(done, 50*time.Millisecond) {
		t.Fatal("waitForDone(closed, timeout) = false, want true")
	}
}

func TestWaitForDoneTimeout(t *testing.T) {
	t.Parallel()

	done := make(chan struct{})
	start := time.Now()

	if waitForDone(done, 20*time.Millisecond) {
		t.Fatal("waitForDone(open, timeout) = true, want false")
	}

	if elapsed := time.Since(start); elapsed < 15*time.Millisecond {
		t.Fatalf("waitForDone returned too early: %s", elapsed)
	}
}
