// SPDX-License-Identifier: MIT

// Package stationtest starts the simulated meter of package station for tests, so that
// nothing in the test suite needs a real meter.
package stationtest

import (
	"context"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/otfabric/iec60870ctl/internal/iec102ctl/station"
	"github.com/otfabric/iec60870ctl/internal/iec102ctl/types"
	"github.com/otfabric/iec60870ctl/internal/line"
)

// Model is the model the tests run against: a clock that stands still, absolute and
// incremental totals of record 11, events of record 52 and the REE section. Nothing in it
// changes by itself, so that results are exact.
const Model = `
station:
  name: Test meter
  link_address: 1
  common_address: 1
  clock: "2026-01-15T10:20:30.500"
  product: {standard: 42, manufacturer: 33, product: 20261115}
totals:
  - record: 11
    kind: absolute
    periods:
      - end: 2026-01-01T01:00
        totals: [{address: 1, value: 100}, {address: 2, value: 5}, {address: 3, value: 7}]
      - end: 2026-01-01T02:00
        totals: [{address: 1, value: 150, quality: 16}, {address: 2, value: 6}, {address: 3, value: 9}]
      - end: 2026-01-01T03:00
        totals: [{address: 1, value: 210}, {address: 2, value: 6}, {address: 3, value: 12, quality: 128}]
  - record: 11
    kind: incremental
    periods:
      - end: 2026-01-01T01:00
        totals: [{address: 1, value: 100}, {address: 2, value: 5}, {address: 3, value: 7}]
      - end: 2026-01-01T02:00
        totals: [{address: 1, value: 50}, {address: 2, value: 1}, {address: 3, value: 2}]
  - record: 1
    type: M_IT_TA_2
    signed: true
    periods:
      - end: 2025-12-31T00:00
        totals: [{address: 1, value: 999, signature: 170}]
events:
  - record: 52
    points:
      - {address: 1, on: false, time: "2026-01-01T03:12:44.120"}
      - {address: 1, on: true, qualifier: 3, time: "2026-01-02T03:14:02.500"}
ree:
  access_key: 7
  contracted_powers: [{address: 1, power_kw: 15}, {address: 2, power_kw: 20}]
  special_days: [2026-01-01]
  tariffs:
    - register: 134
      current:
        - {address: 1, active_abs: 2160, active_inc: 2160, reactive_ind_abs: 192, reactive_ind_inc: 192,
           reactive_cap_abs: 0, reactive_cap_inc: 0, max_power: 3100, max_power_time: 2026-01-01T19:00,
           excess_power: 0, period_start: 2026-01-01T00:00, period_end: 2026-01-15T10:00}
      stored:
        - {address: 1, active_abs: 310000, active_inc: 31000, active_quality: 128, reactive_ind_abs: 27000,
           reactive_ind_inc: 2700, reactive_cap_abs: 1, reactive_cap_inc: 1, max_power: 4200,
           max_power_time: 2025-11-18T20:00, excess_power: 5, period_start: 2025-11-01T00:00, period_end: 2025-12-01T00:00}
        - {address: 1, active_abs: 340000, active_inc: 30000, reactive_ind_abs: 29000, reactive_ind_inc: 2000,
           reactive_cap_abs: 1, reactive_cap_inc: 0, max_power: 4000, max_power_time: 2025-12-18T20:00,
           excess_power: 0, period_start: 2025-12-01T00:00, period_end: 2026-01-01T00:00}
`

// Station is a meter serving on a loopback port.
type Station struct {
	*station.Station
	// Addr is host:port of the listener.
	Addr string

	mu     sync.Mutex
	events []types.ServerEvent
}

// URL returns the --url of the station.
func (s *Station) URL() string { return "tcp://" + s.Addr }

// Events returns the events the station has logged so far.
func (s *Station) Events() []types.ServerEvent {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]types.ServerEvent(nil), s.events...)
}

// Start serves Model on a loopback port until the test ends.
func Start(t testing.TB) *Station {
	t.Helper()
	m, err := station.Parse([]byte(Model))
	if err != nil {
		t.Fatal(err)
	}
	return StartModel(t, m, station.Options{})
}

// StartModel serves m with opts (its Log is replaced) on a loopback port until the test ends.
func StartModel(t testing.TB, m *station.Model, opts station.Options) *Station {
	t.Helper()
	s := &Station{}
	opts.Log = func(e types.ServerEvent) {
		s.mu.Lock()
		s.events = append(s.events, e)
		s.mu.Unlock()
	}
	st, err := station.New(m, opts)
	if err != nil {
		t.Fatal(err)
	}
	s.Station = st
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	s.Addr = ln.Addr().String()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = line.Serve(ctx, ln, func(ctx context.Context, c line.Conn) { st.Serve(ctx, c, c.Remote) }, nil)
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(10 * time.Second):
			t.Error("the station did not stop")
		}
	})
	return s
}
