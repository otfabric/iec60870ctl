// SPDX-License-Identifier: MIT

package stationtest

import (
	"context"
	"net"
	"runtime"
	"testing"
	"time"

	"github.com/otfabric/go-iec60870-5/iec102/client"
	"github.com/otfabric/iec60870ctl/internal/iec102ctl/station"
)

func TestStart(t *testing.T) {
	s := Start(t)
	c, err := net.Dial("tcp", s.Addr)
	if err != nil {
		t.Fatal(err)
	}
	cl, err := client.New(c, client.Config{LinkAddr: 1, CommonAddr: 1, RequestTimeout: 5 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cl.Close() }()
	if _, err := cl.ReadProductSpec(context.Background()); err != nil {
		t.Fatal(err)
	}
	if s.URL() != "tcp://"+s.Addr || len(s.Events()) == 0 {
		t.Errorf("url %s, events %v", s.URL(), s.Events())
	}
}

// fatalTB records a fatal failure instead of failing the test.
type fatalTB struct {
	testing.TB
	fatal bool
}

func (f *fatalTB) Helper()               {}
func (f *fatalTB) Fatal(...any)          { f.fatal = true; runtime.Goexit() }
func (f *fatalTB) Cleanup(fn func())     { f.TB.Cleanup(fn) }
func (f *fatalTB) Errorf(string, ...any) {}

// A model the station refuses fails the test that starts it.
func TestStartModelRefusesBadModel(t *testing.T) {
	m, err := station.Parse([]byte(`station: {}`))
	if err != nil {
		t.Fatal(err)
	}
	big := uint16(300)
	f := &fatalTB{TB: t}
	done := make(chan struct{})
	go func() { defer close(done); StartModel(f, m, station.Options{CASize: 1, CommonAddr: &big}) }()
	<-done
	if !f.fatal {
		t.Error("a station with a common address of 300 in one octet started")
	}
}
