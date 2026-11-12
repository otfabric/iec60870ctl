// SPDX-License-Identifier: MIT

package stationtest

import (
	"fmt"
	"net"
	"testing"
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
