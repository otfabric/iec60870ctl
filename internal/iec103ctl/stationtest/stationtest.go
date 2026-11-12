// SPDX-License-Identifier: MIT

// Package stationtest starts the simulated protection device of package station for tests,
// so that nothing in the test suite needs a real device.
package stationtest

import (
	"context"
	"fmt"
	"net"
	"sync"
	"testing"

	"github.com/otfabric/iec60870ctl/internal/iec103ctl/station"
	"github.com/otfabric/iec60870ctl/internal/iec103ctl/types"
	"github.com/otfabric/iec60870ctl/internal/line"
)

// Model is the model the tests run against: the IEC 103 baseline fixture of
// otfabric/iec60870-5-interop v0.4.0 (fixtures/iec103/baseline), with the headings of its
// generic group. Nothing in it changes by itself, so that results are exact.
const Model = `{
  "schemaVersion": "1.0",
  "name": "baseline",
  "station": {
    "commonAddress": 1,
    "identification": { "fun": 160, "col": 2, "ascii": "OTFAB103", "software": "01020304" }
  },
  "indications": [
    { "fun": 160, "inf": 16, "dpi": 2 },
    { "fun": 160, "inf": 17, "dpi": 1 },
    { "fun": 160, "inf": 18, "dpi": 2 },
    { "fun": 160, "inf": 19, "dpi": 1 },
    { "fun": 160, "inf": 23, "dpi": 2 },
    { "fun": 160, "inf": 24, "dpi": 1 },
    { "fun": 160, "inf": 27, "dpi": 1 },
    { "fun": 160, "inf": 36, "dpi": 1 },
    { "fun": 160, "inf": 68, "dpi": 1, "gi": false }
  ],
  "measurands": [
    { "fun": 160, "inf": 144, "type": 3, "values": [ { "value": 2048 } ] },
    { "fun": 160, "inf": 145, "type": 3, "values": [ { "value": 1024 }, { "value": -512, "ov": true } ] },
    { "fun": 160, "inf": 148, "type": 9, "values": [
      { "value": 100 }, { "value": 200 }, { "value": 300 },
      { "value": 3000 }, { "value": 3001 }, { "value": 2999 },
      { "value": -4096 }, { "value": 4095, "ov": true }, { "value": 0, "er": true }
    ] }
  ],
  "commands": [ { "fun": 160, "inf": 16 }, { "fun": 160, "inf": 17 }, { "fun": 160, "inf": 19 } ],
  "groups": [ { "group": 1, "description": "SETTINGS" } ],
  "generic": [
    { "gin": { "group": 1, "entry": 1 }, "description": "RATED I", "type": 3, "size": 2, "value": 1000 },
    { "gin": { "group": 1, "entry": 2 }, "description": "PICKUP I>", "type": 7, "size": 4, "value": 1.5, "writable": true },
    { "gin": { "group": 1, "entry": 3 }, "description": "DELAY t>", "type": 3, "size": 2, "value": 300, "writable": true },
    { "gin": { "group": 1, "entry": 4 }, "description": "FEEDER", "type": 1, "size": 8, "value": "FEEDER 1" },
    { "gin": { "group": 1, "entry": 5 }, "description": "OFFSET", "type": 4, "size": 2, "value": -20 }
  ],
  "disturbances": [
    {
      "fan": 7, "sof": { "tp": true }, "time": "2026-10-09T15:30:00.012Z", "nof": 3, "interval": 1000, "start": "15:30:00.000",
      "tags": [
        { "pos": 0, "fun": 160, "inf": 84, "dpi": 1 }, { "pos": 0, "fun": 160, "inf": 90, "dpi": 1 },
        { "pos": 12, "fun": 160, "inf": 84, "dpi": 2 }, { "pos": 20, "fun": 160, "inf": 90, "dpi": 2 }
      ],
      "channels": [
        { "acc": 1, "rpv": 600, "rsv": 1, "rfa": 1, "values": [0, 1854, 3527, 4854, 5706, 6000, 5706, 4854, 3527, 1854, 0, -1854, -12343, -16989, -19972, -21000, -19972, -16989, -12343, -6489, 0, 6489, 12343, 16989, 19972, 21000, 19972, 16989, 12343, 6489, 0, -6489, -12343, -16989, -19972, -21000, -19972, -16989, -12343, -6489] },
        { "acc": 2, "rpv": 600, "rsv": 1, "rfa": 1, "values": [-5196, -5869, -5967, -5481, -4459, -3000, -1247, 627, 2440, 4015, 5196, 5869, 20885, 19184, 15606, 10500, 4366, -2195, -8541, -14052, -18187, -20541, -20885, -19184, -15606, -10500, -4366, 2195, 8541, 14052, 18187, 20541, 20885, 19184, 15606, 10500, 4366, -2195, -8541, -14052] }
      ]
    }
  ]
}`

// Station is a simulated device serving on a loopback port.
type Station struct {
	*station.Station
	// Addr is host:port of the line.
	Addr string

	mu     sync.Mutex
	events []types.ServerEvent
}

// URL returns the --url of the line.
func (s *Station) URL() string { return "tcp://" + s.Addr }

// Events returns the events the device has logged so far.
func (s *Station) Events() []types.ServerEvent {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]types.ServerEvent(nil), s.events...)
}

// HasEvent reports whether an event of the kind was logged.
func (s *Station) HasEvent(kind string) bool {
	for _, e := range s.Events() {
		if e.Event == kind {
			return true
		}
	}
	return false
}

// Start serves model (JSON or YAML) on a loopback port until the test ends.
func Start(t testing.TB, model string, edit ...func(*station.Options)) *Station {
	t.Helper()
	m, err := station.Parse([]byte(model), false)
	if err != nil {
		t.Fatalf("stationtest: model: %v", err)
	}
	out := &Station{}
	opts := station.Options{LinkAddr: -1, CommonAddr: -1, Log: func(e types.ServerEvent) {
		out.mu.Lock()
		out.events = append(out.events, e)
		out.mu.Unlock()
	}}
	for _, f := range edit {
		f(&opts)
	}
	st, err := station.New(m, opts)
	if err != nil {
		t.Fatalf("stationtest: %v", err)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("stationtest: listen: %v", err)
	}
	out.Station, out.Addr = st, ln.Addr().String()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = line.Serve(ctx, ln, func(ctx context.Context, c line.Conn) { _ = st.Serve(ctx, c, c.Remote) }, nil)
	}()
	t.Cleanup(func() {
		cancel()
		<-done
	})
	return out
}

// Silent is a line that accepts connections and never answers: a device that is off.
func Silent(t testing.TB) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("stationtest: listen: %v", err)
	}
	var (
		mu    sync.Mutex
		conns []net.Conn
		wg    sync.WaitGroup
	)
	wg.Go(func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			mu.Lock()
			conns = append(conns, c)
			mu.Unlock()
		}
	})
	t.Cleanup(func() {
		_ = ln.Close()
		wg.Wait()
		mu.Lock()
		defer mu.Unlock()
		for _, c := range conns {
			_ = c.Close()
		}
	})
	return fmt.Sprintf("tcp://%s", ln.Addr())
}

// Refused returns the URL of a port nothing listens on.
func Refused(t testing.TB) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("stationtest: listen: %v", err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()
	return "tcp://" + addr
}
