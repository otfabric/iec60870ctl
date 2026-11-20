// SPDX-License-Identifier: MIT

package stationtest

import (
	"math"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/otfabric/iec60870ctl/internal/iec103ctl/station"
)

func TestStartServesTheModel(t *testing.T) {
	s := Start(t, Model, func(o *station.Options) { o.LinkAddr = 1 })
	if !strings.HasPrefix(s.URL(), "tcp://127.0.0.1:") || s.LinkAddr() != 1 {
		t.Fatalf("url %s, link %d", s.URL(), s.LinkAddr())
	}
	c, err := net.DialTimeout("tcp", s.Addr, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	_ = c.Close()
	// The line's state change ("link", closed) is logged from the line's own goroutine and
	// may already be there; nothing else is, since nothing was sent.
	for _, e := range s.Events() {
		if e.Event != "link" {
			t.Fatalf("events %v", s.Events())
		}
	}
	if s.HasEvent("nothing") {
		t.Fatal("HasEvent reported an event that was not logged")
	}
}

// StartChanged serves the model as changed after the device was built from it: a value no
// valid model holds.
func TestStartChangedServesTheChange(t *testing.T) {
	s := StartChanged(t, Model, func(m *station.Model) { m.Disturbances[0].Channels[0].RPV = math.Inf(1) })
	rec, ok := s.Record(7) // the first disturbance of Model
	if !ok || !math.IsInf(float64(rec.Channels[0].RPV), 1) {
		t.Fatalf("record %v %+v", ok, rec)
	}
}

func TestSilentAndRefused(t *testing.T) {
	u := Silent(t)
	c, err := net.DialTimeout("tcp", strings.TrimPrefix(u, "tcp://"), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.Close() }()
	if _, err := net.DialTimeout("tcp", strings.TrimPrefix(Refused(t), "tcp://"), time.Second); err == nil {
		t.Fatal("refused port accepted")
	}
}

func TestEventsAreRecorded(t *testing.T) {
	s := Start(t, Model)
	s.Transmitted(7, true)
	if !s.HasEvent("disturbance-transmitted") || s.HasEvent("other") {
		t.Fatal(s.Events())
	}
}
