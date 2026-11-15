// SPDX-License-Identifier: MIT

//go:build unix

package cli

import (
	"context"
	"os"
	"syscall"
	"testing"
	"time"
)

// interrupt sends SIGINT to this process; SignalContext has the signal, so the test goes on.
func interrupt(t *testing.T) {
	t.Helper()
	if err := syscall.Kill(os.Getpid(), syscall.SIGINT); err != nil {
		t.Fatal(err)
	}
}

// The first interrupt ends the context; a second one, while the command is still cleaning
// up, ends the process with exit code 130.
func TestSecondInterruptEndsTheProcess(t *testing.T) {
	exited := make(chan int, 1)
	exitProcess = func(code int) { exited <- code }
	t.Cleanup(func() { exitProcess = os.Exit })

	ctx, stop := SignalContext(context.Background())
	defer stop()
	interrupt(t)
	select {
	case <-ctx.Done():
	case <-time.After(10 * time.Second):
		t.Fatal("the first interrupt did not end the context")
	}
	select {
	case code := <-exited:
		t.Fatalf("the first interrupt ended the process with %d", code)
	default:
	}
	interrupt(t)
	select {
	case code := <-exited:
		if code != ExitInterrupted {
			t.Errorf("exit code %d, want %d", code, ExitInterrupted)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("a second interrupt did not end the process")
	}
}

// SIGTERM ends the context as SIGINT does.
func TestTerminateEndsTheContext(t *testing.T) {
	exitProcess = func(code int) { t.Errorf("exit %d after one signal", code) }
	t.Cleanup(func() { exitProcess = os.Exit })
	ctx, stop := SignalContext(context.Background())
	defer stop()
	if err := syscall.Kill(os.Getpid(), syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	select {
	case <-ctx.Done():
	case <-time.After(10 * time.Second):
		t.Fatal("SIGTERM did not end the context")
	}
}
