// SPDX-License-Identifier: MIT

package stationtest

import (
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
	if s.HasEvent("nothing") || len(s.Events()) != 0 {
		t.Fatalf("events %v", s.Events())
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
