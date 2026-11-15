// SPDX-License-Identifier: MIT

package cli

import (
	"context"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"github.com/otfabric/iec60870ctl/internal/errs"
)

// ExitInterrupted is the exit code of a process ended by a second interrupt: the code of an
// interrupted command ([errs.ExitInterrupted]), as a shell reports a process ended by SIGINT.
const ExitInterrupted = errs.ExitInterrupted

// exitProcess is os.Exit; a test replaces it.
var exitProcess = os.Exit

// SignalContext returns a context that ends at the first SIGINT or SIGTERM, so that a
// command stops what it is doing and cleans up (a logout, the end of a session, the last
// lines of a stream). A second signal ends the process at once with [ExitInterrupted],
// also while that clean-up still waits on a peer or on a blocked output. stop releases the
// signals and ends the context; Execute calls it when the command has returned.
func SignalContext(parent context.Context) (ctx context.Context, stop func()) {
	ctx, cancel := context.WithCancel(parent)
	signals := make(chan os.Signal, 2)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	done := make(chan struct{})
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		select {
		case <-signals:
			cancel()
		case <-done:
			return
		}
		select {
		case <-signals:
			exitProcess(ExitInterrupted)
		case <-done:
		}
	}()
	var once sync.Once
	return ctx, func() {
		once.Do(func() {
			signal.Stop(signals)
			close(done)
			<-finished
			cancel()
		})
	}
}
