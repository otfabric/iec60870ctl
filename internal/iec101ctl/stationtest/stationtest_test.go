// SPDX-License-Identifier: MIT

package stationtest

import (
	"fmt"
	"runtime"
	"testing"

	"github.com/otfabric/iec60870ctl/internal/iec101ctl/config"
	"github.com/otfabric/iec60870ctl/internal/iec101ctl/station"
)

// fatalTB records the first Fatalf and stops the goroutine, as testing.T does.
type fatalTB struct {
	testing.TB
	msg string
}

func (f *fatalTB) Helper() {}

func (f *fatalTB) Fatalf(format string, args ...any) {
	f.msg = fmt.Sprintf(format, args...)
	runtime.Goexit()
}

func fatal(t *testing.T, call func(tb testing.TB)) string {
	f := &fatalTB{TB: t}
	done := make(chan struct{})
	go func() {
		defer close(done)
		call(f)
	}()
	<-done
	return f.msg
}

func TestStart(t *testing.T) {
	bal := config.DefaultLink()
	bal.Mode, bal.RemoteLinkAddr = config.ModeBalanced, "4"
	st := Start(t, Model, &bal)
	if st.Addr() == "" || st.HasEvent("nothing-like-this") || len(st.Flags()) < 20 {
		t.Fatalf("station %s, flags %v", st.Addr(), st.Flags())
	}
	if msg := fatal(t, func(tb testing.TB) { Start(tb, "station: [", nil) }); msg == "" {
		t.Error("a broken model started")
	}
	bad := config.DefaultLink()
	bad.Mode = "half"
	if msg := fatal(t, func(tb testing.TB) { Start(tb, Model, &bad) }); msg == "" {
		t.Error("an invalid link started")
	}
	if msg := fatal(t, func(tb testing.TB) {
		Start(tb, Model, nil, func(o *station.Options) { o.Link.Params.ASDU.IOASize = 1 })
	}); msg == "" {
		t.Error("addresses that do not fit started")
	}
}
