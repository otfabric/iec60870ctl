// SPDX-License-Identifier: MIT

// Package stationtest starts the simulated station of package station for tests, so that
// nothing in the test suite needs a real device.
package stationtest

import (
	"context"
	"math"
	"net"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/otfabric/iec60870ctl/internal/iec101ctl/config"
	"github.com/otfabric/iec60870ctl/internal/iec101ctl/iec"
	"github.com/otfabric/iec60870ctl/internal/iec101ctl/station"
	"github.com/otfabric/iec60870ctl/internal/iec101ctl/types"
)

// Model is the model the tests run against: a point of every type and a command for each.
// Nothing in it changes by itself, so that results are exact.
const Model = `
station: {name: Test station, common_address: 1}
points:
  - {ioa: 100, type: M_SP_NA_1, value: true, group: 1}
  - {ioa: 101, type: M_SP_NA_1, value: false, quality: [NT, IV], group: 1}
  - {ioa: 200, type: M_DP_NA_1, value: "on", group: 1}
  - {ioa: 300, type: M_ST_NA_1, value: 3, group: 2}
  - {ioa: 400, type: M_BO_NA_1, value: 15, group: 2}
  - {ioa: 1000, type: M_ME_NC_1, value: 230.5, group: 3}
  - {ioa: 1002, type: M_ME_NB_1, value: 1250, group: 3}
  - {ioa: 1003, type: M_ME_NA_1, value: 0.25, group: 3}
  - {ioa: 2000, type: M_IT_NA_1, value: 123456, group: 1, sequence: 1}
  - {ioa: 2001, type: M_IT_NA_1, value: 7, group: 2}
commands:
  - {ioa: 5000, type: C_SC_NA_1, target: 100}
  - {ioa: 5001, type: C_DC_NA_1, target: 200, select_required: true}
  - {ioa: 5002, type: C_RC_NA_1, target: 300}
  - {ioa: 5003, type: C_SE_NC_1, target: 1000}
  - {ioa: 5004, type: C_SE_NB_1, target: 1002, report_cause: 3}
  - {ioa: 5005, type: C_SE_NA_1, target: 1003}
  - {ioa: 5006, type: C_BO_NA_1, target: 400}
  - {ioa: 5007, type: C_SC_NA_1}
`

// LinkTimeout is the link time-out of the test stations and of the flags of [Station.Flags]:
// short, as the line is a loopback connection.
const LinkTimeout = 200 * time.Millisecond

// Station is a station serving on a loopback port.
type Station struct {
	*station.Station
	// Host and Port are where the station listens.
	Host string
	Port uint16
	// Link are the link flags the station was started with.
	Link config.LinkConfig

	mu     sync.Mutex
	events []types.ServerEvent
}

// Addr returns host:port.
func (s *Station) Addr() string { return net.JoinHostPort(s.Host, strconv.Itoa(int(s.Port))) }

// Flags returns the client flags that reach the station: the line and the link flags.
func (s *Station) Flags() []string {
	l := s.Link
	out := []string{"--ip", s.Host, "--port", strconv.Itoa(int(s.Port)),
		"--link-mode", l.Mode, "--link-address", strconv.Itoa(int(l.LinkAddr)),
		"--link-address-size", strconv.Itoa(int(l.LinkAddrSize)), "--cot-size", strconv.Itoa(int(l.CauseSize)),
		"--ca-size", strconv.Itoa(int(l.CASize)), "--ioa-size", strconv.Itoa(int(l.IOASize)),
		"--link-timeout", strconv.Itoa(int(LinkTimeout / time.Millisecond)), "--poll-interval", "10"}
	if l.RemoteLinkAddr != "" {
		out = append(out, "--remote-link-address", l.RemoteLinkAddr)
	}
	return out
}

// Events returns the events the station has logged so far.
func (s *Station) Events() []types.ServerEvent {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]types.ServerEvent(nil), s.events...)
}

// HasEvent reports whether an event of the given kind was logged.
func (s *Station) HasEvent(kind string) bool {
	for _, e := range s.Events() {
		if e.Event == kind {
			return true
		}
	}
	return false
}

// Start serves the model on a loopback port until the test ends, with the link flags lc
// (config.DefaultLink when nil). edit, when given, changes the options before the station
// is built.
func Start(t testing.TB, model string, lc *config.LinkConfig, edit ...func(*station.Options)) *Station {
	t.Helper()
	m, err := station.Parse([]byte(model))
	if err != nil {
		t.Fatalf("stationtest: model: %v", err)
	}
	link := config.DefaultLink()
	if lc != nil {
		link = *lc
	}
	link.LinkTimeout = uint32(LinkTimeout / time.Millisecond)
	l, err := iec.LinkParams(link)
	if err != nil {
		t.Fatalf("stationtest: link: %v", err)
	}
	out := &Station{Host: "127.0.0.1", Link: link}
	opts := station.Options{Link: l, RetryInterval: 50 * time.Millisecond, Log: func(e types.ServerEvent) {
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
	out.Station = st
	if port := ln.Addr().(*net.TCPAddr).Port; port > 0 && port <= math.MaxUint16 {
		out.Port = uint16(port)
	}
	ctx, cancel := context.WithCancel(context.Background())
	st.Simulate(ctx)
	done := make(chan struct{})
	// One line after the other, as the harness of go-iec60870-5 serves: a command that
	// connects while the line of the one before is still being closed waits in the backlog
	// of the listener instead of being turned away as busy (which `server start` tests).
	go func() {
		defer close(done)
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			_ = st.ServeLine(ctx, c, c.RemoteAddr().String())
		}
	}()
	t.Cleanup(func() {
		cancel()
		_ = ln.Close()
		<-done
		st.Wait()
	})
	return out
}
