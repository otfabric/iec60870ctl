// SPDX-License-Identifier: MIT

package cli

import (
	"context"
	"os"
	"testing"
)

// stop without a signal ends the context and releases the signals; calling it again does
// nothing, and the process is not ended.
func TestSignalContextStop(t *testing.T) {
	exitProcess = func(code int) { t.Errorf("exit %d without a signal", code) }
	t.Cleanup(func() { exitProcess = os.Exit })
	parent, cancel := context.WithCancel(context.Background())
	defer cancel()
	ctx, stop := SignalContext(parent)
	if ctx.Err() != nil {
		t.Fatal("the context ended before a signal")
	}
	stop()
	stop()
	if ctx.Err() == nil {
		t.Error("stop did not end the context")
	}
	if parent.Err() != nil {
		t.Error("stop ended the parent")
	}
}
