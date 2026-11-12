// SPDX-License-Identifier: MIT

package stationtest

import (
	"fmt"
	"net"
	"runtime"
	"strings"
	"testing"

	"github.com/otfabric/iec60870ctl/internal/iec104ctl/station"
)

// fatalRecorder is a testing.TB whose Fatalf records the failure instead of ending the
// goroutine.
type fatalRecorder struct {
	testing.TB
	failed string
}

func (f *fatalRecorder) Helper() {}

func (f *fatalRecorder) Fatalf(format string, args ...any) { f.failed = fmt.Sprintf(format, args...) }

// port takes a TCP port 1-65535 as it is and refuses anything else instead of truncating it.
func TestPort(t *testing.T) {
	for p, ok := range map[int]bool{1: true, 2404: true, 65535: true, 0: false, -1: false, 65536: false, 65536 + 2404: false} {
		rec := &fatalRecorder{TB: t}
		got := port(rec, &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: p})
		switch {
		case ok && (got != uint16(p) || rec.failed != ""):
			t.Errorf("port %d: %d, failure %q", p, got, rec.failed)
		case !ok && (got != 0 || rec.failed == ""):
			t.Errorf("port %d: %d taken without a failure", p, got)
		}
	}
	// A real listener, through FreePort and Start.
	if p := FreePort(t); p == 0 {
		t.Error("FreePort returned 0")
	}
	if st := Start(t, Model); st.Port == 0 || st.Addr() == "" {
		t.Errorf("Start: port %d", st.Port)
	}
}

// exitRecorder is a testing.TB whose Fatal and Fatalf record the failure and end the
// goroutine, as the real ones do.
type exitRecorder struct {
	testing.TB
	failed string
}

func (e *exitRecorder) Helper() {}

func (e *exitRecorder) Fatalf(format string, args ...any) {
	e.failed = fmt.Sprintf(format, args...)
	runtime.Goexit()
}

func (e *exitRecorder) Fatal(args ...any) {
	e.failed = fmt.Sprint(args...)
	runtime.Goexit()
}

// inGoroutine runs f with an exitRecorder and returns what it failed with.
func inGoroutine(t *testing.T, f func(tb testing.TB)) string {
	rec := &exitRecorder{TB: t}
	done := make(chan struct{})
	go func() {
		defer close(done)
		f(rec)
	}()
	<-done
	return rec.failed
}

// Start fails the test, not the process, on a model it cannot parse or build.
func TestStartRefusesBadModels(t *testing.T) {
	if msg := inGoroutine(t, func(tb testing.TB) { Start(tb, "points: [") }); !strings.Contains(msg, "stationtest: model") {
		t.Errorf("unparsable model: %q", msg)
	}
	if msg := inGoroutine(t, func(tb testing.TB) {
		Start(tb, Model, func(o *station.Options) { o.Link.K = -1 })
	}); !strings.HasPrefix(msg, "stationtest: ") {
		t.Errorf("unusable options: %q", msg)
	}
}
