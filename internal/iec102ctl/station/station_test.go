// SPDX-License-Identifier: MIT

package station

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/otfabric/go-iec60870-5/iec102"
	"github.com/otfabric/go-iec60870-5/iec102/client"
	"github.com/otfabric/go-iec60870-5/iec102/ree"
	serial "github.com/otfabric/go-serial"
	"github.com/otfabric/iec60870ctl/internal/iec102ctl/config"
	"github.com/otfabric/iec60870ctl/internal/iec102ctl/types"
	"github.com/otfabric/iec60870ctl/internal/line"
)

// testModel is a meter with a clock that stands still.
const testModel = `
station: {name: T, link_address: 3, common_address: 5, clock: "2026-01-15T10:20:30.500", product: {standard: 1, manufacturer: 2, product: 3}}
totals:
  - record: 11
    kind: absolute
    periods:
      - {end: 2026-01-01T01:00, totals: [{address: 1, value: 10}]}
  - record: 11
    type: M_IT_TK_2
    generate: {start: 2026-01-01T01:00, interval: 1h, count: 3, totals: [{address: 1, step: 5}]}
events:
  - record: 52
    points: [{address: 1, on: true, time: "2026-01-01T00:00:01.000"}]
ree:
  access_key: 9
`

// events collects the events of a station.
type events struct {
	mu sync.Mutex
	l  []types.ServerEvent
}

func (e *events) log(ev types.ServerEvent) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.l = append(e.l, ev)
}

func (e *events) names() string {
	e.mu.Lock()
	defer e.mu.Unlock()
	var n []string
	for _, ev := range e.l {
		n = append(n, ev.Event)
	}
	return strings.Join(n, " ")
}

// serve runs st on one end of a pipe and returns a client on the other.
func serve(t *testing.T, st *Station, la, ca uint16, p iec102.Params) *client.Client {
	t.Helper()
	a, b := net.Pipe()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); st.Serve(ctx, a, "pipe") }()
	c, err := client.New(b, client.Config{LinkAddr: la, CommonAddr: ca, Params: p, RequestTimeout: 5 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = c.Close()
		cancel()
		<-done
	})
	return c
}

func TestStationServesTheModel(t *testing.T) {
	m, err := Parse([]byte(testModel))
	if err != nil {
		t.Fatal(err)
	}
	var ev events
	st, err := New(m, Options{Log: ev.log, Trace: io.Discard})
	if err != nil {
		t.Fatal(err)
	}
	if st.LinkAddr() != 3 || st.CommonAddr() != 5 || st.Name() != "T" {
		t.Fatalf("addresses %d %d", st.LinkAddr(), st.CommonAddr())
	}
	c := serve(t, st, 3, 5, iec102.Params{Profile: ree.Profile})
	ctx := context.Background()
	ps, err := c.ReadProductSpec(ctx)
	if err != nil || ps.Product != 3 {
		t.Fatalf("product %+v %v", ps, err)
	}
	tb, err := c.ReadTime(ctx)
	if err != nil || tb.Second != 30 || tb.Millisecond != 500 {
		t.Fatalf("time %+v %v", tb, err)
	}
	from := iec102.TimeAOf(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), nil)
	to := iec102.TimeAOf(time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC), nil)
	ps2, err := c.ReadTotals(ctx, 11, iec102.Read{T: iec102.C_CI_NU_2, First: 1, Last: 8, From: from, To: to})
	if err != nil || len(ps2) != 3 || ps2[2].Totals[0].Value != 5 {
		t.Fatalf("generated %+v %v", ps2, err)
	}
	for _, q := range []iec102.Read{
		{T: iec102.C_CI_NT_2, First: 1, Last: 1, From: from, To: to},
		{T: iec102.C_CI_NL_2, From: iec102.TimeAOf(time.Date(2026, 1, 1, 1, 0, 0, 0, time.UTC), nil)},
	} {
		if _, err := c.ReadTotals(ctx, 11, q); err != nil {
			t.Errorf("%s: %v", q.T, err)
		}
	}
	pts, err := c.ReadSinglePoints(ctx, 52, &from, &to)
	if err != nil || len(pts) != 1 {
		t.Fatalf("points %+v %v", pts, err)
	}
	if err := ree.Login(ctx, c, 8); err == nil {
		t.Error("a wrong key was accepted")
	}
	if err := ree.Login(ctx, c, 9); err != nil {
		t.Fatal(err)
	}
	// No tariffs, powers or days in this model.
	for _, q := range []iec102.TypeID{ree.C_TA_VC_2, ree.C_PC_NA_2, ree.C_DF_NA_2, ree.C_RM_NA_2} {
		_, err := c.Request(ctx, iec102.New(6, 5, 134, ree.Request{T: q}), client.UntilTermination)
		if _, ok := errors.AsType[*iec102.RefusedError](err); !ok {
			t.Errorf("%s: %v", ree.Name(q), err)
		}
	}
	set := iec102.TimeBOf(time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC), nil)
	if err := ree.SetTime(ctx, c, set); err != nil {
		t.Fatal(err)
	}
	if got := st.Now(); !got.Equal(time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("clock %s", got)
	}
	bad := set
	bad.Month = 13
	if err := ree.SetTime(ctx, c, bad); err != nil {
		t.Fatal(err)
	}
	if err := ree.Logout(ctx, c); err != nil {
		t.Fatal(err)
	}
	_, _ = c.Request(ctx, &iec102.ASDU{Type: 200, Cause: 6, CommonAddr: 5, Body: iec102.Raw{Data: []byte{1}}}, client.UntilReply)
	_, _ = c.Request(ctx, iec102.New(6, 5, 0, iec102.Read{T: iec102.C_CI_NI_2}), client.UntilTermination)
	_, _ = c.Request(ctx, iec102.New(6, 5, 11, iec102.Read{T: iec102.C_CI_NK_2, First: 1, Last: 2}), client.UntilTermination)
	_, _ = c.Request(ctx, iec102.New(6, 5, 11, iec102.Read{T: iec102.C_CI_NM_2, First: 1, Last: 2, From: from}), client.UntilTermination)
	_, _ = c.Request(ctx, iec102.New(6, 5, 134, ree.Request{T: ree.C_TA_VM_2, From: from, To: to}), client.UntilTermination)
	names := ev.names()
	for _, want := range []string{"link-up", "request", "login", "refused", "set-time", "logout"} {
		if !strings.Contains(names, want) {
			t.Errorf("no %s event in %s", want, names)
		}
	}
}

// A model without the REE section refuses the REE types with cause 14, whatever they hold.
func TestStationWithoutREE(t *testing.T) {
	m, err := Parse([]byte(`station: {clock: running, product: {standard: 1, manufacturer: 2, product: 3}}`))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	st, err := New(m, Options{Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	c := serve(t, st, 1, 1, iec102.Params{})
	_, err = c.Request(context.Background(), &iec102.ASDU{Type: ree.C_AC_NA_2, Cause: 6, CommonAddr: 1, Body: iec102.Raw{Data: []byte{9, 0, 0, 0}}}, client.UntilConfirmation)
	if r, ok := errors.AsType[*iec102.RefusedError](err); !ok || r.Cause().Code() != iec102.CauseUnknownType {
		t.Fatalf("login without REE: %v", err)
	}
	st.setTime(iec102.TimeBOf(now.Add(time.Hour), nil))
	if got := st.Now(); !got.Equal(now.Add(time.Hour)) {
		t.Errorf("running clock set to %s", got)
	}
}

func TestREEReadsAnswered(t *testing.T) {
	m, err := Parse([]byte(DemoYAML))
	if err != nil {
		t.Fatal(err)
	}
	st, err := New(m, Options{})
	if err != nil {
		t.Fatal(err)
	}
	c := serve(t, st, 1, 1, iec102.Params{Profile: ree.Profile})
	for _, q := range []ree.Request{{T: ree.C_PC_NA_2}, {T: ree.C_DF_NA_2}, {T: ree.C_TA_VC_2}} {
		out, err := c.Request(context.Background(), iec102.New(6, 1, 134, q), client.UntilTermination)
		if err != nil || len(out) != 3 {
			t.Errorf("%s: %d answers, %v", ree.Name(q.T), len(out), err)
		}
	}
}

func TestNewRejects(t *testing.T) {
	m, _ := Parse([]byte(testModel))
	big := uint16(300)
	for name, o := range map[string]Options{
		"link address":   {LinkAddrSize: 1, LinkAddr: &big},
		"ca size":        {CASize: 3},
		"common address": {CASize: 1, CommonAddr: &big},
	} {
		if _, err := New(m, o); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	bad, _ := Parse([]byte(`station: {clock: x}`))
	if _, err := New(bad, Options{}); err == nil {
		t.Error("a bad clock was accepted")
	}
}

func listenConfig(t *testing.T, model string) config.ServerConfig {
	t.Helper()
	cfg := config.ServerDefaults()
	cfg.Listen = "127.0.0.1"
	port, _ := strconv.Atoi(freePort(t, "127.0.0.1"))
	cfg.Port = uint16(port) //nolint:gosec // a port number
	if model != "" {
		p := filepath.Join(t.TempDir(), "m.yaml")
		if err := os.WriteFile(p, []byte(model), 0o600); err != nil {
			t.Fatal(err)
		}
		cfg.Config = p
	}
	return cfg
}

func TestRunServesTCPOneAtATime(t *testing.T) {
	cfg := listenConfig(t, testModel)
	cfg.Debug = true
	if err := Check(&cfg); err != nil {
		t.Fatal(err)
	}
	var ev events
	ctx, cancel := context.WithCancel(context.Background())
	addr := make(chan string, 1)
	done := make(chan error, 1)
	go func() { done <- Run(ctx, &cfg, ev.log, io.Discard, func(a string) { addr <- a }) }()
	var a string
	select {
	case a = <-addr:
	case err := <-done:
		t.Fatalf("the server did not listen: %v", err)
	}
	c1, err := net.Dial("tcp", a)
	if err != nil {
		t.Fatal(err)
	}
	cl, err := client.New(c1, client.Config{LinkAddr: 3, CommonAddr: 5, RequestTimeout: 5 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := cl.ReadProductSpec(context.Background()); err != nil {
		t.Fatal(err)
	}
	c2, err := net.Dial("tcp", a)
	if err != nil {
		t.Fatal(err)
	}
	_ = c2.SetReadDeadline(time.Now().Add(5 * time.Second))
	if _, err := c2.Read(make([]byte, 1)); err == nil {
		t.Error("a second line was served")
	}
	_ = c2.Close()
	_ = cl.Close()
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	names := ev.names()
	for _, want := range []string{"listening", "connected", "busy", "disconnected", "stopping"} {
		if !strings.Contains(names, want) {
			t.Errorf("no %s event in %s", want, names)
		}
	}
}

func TestRunDurationAndErrors(t *testing.T) {
	cfg := listenConfig(t, "")
	cfg.Duration = 1
	start := time.Now()
	if err := Run(context.Background(), &cfg, nil, nil, nil); err != nil || time.Since(start) > 5*time.Second {
		t.Fatalf("duration: %v after %s", err, time.Since(start))
	}
	// The port is taken.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ln.Close() }()
	cfg = listenConfig(t, "")
	cfg.Port = uint16(ln.Addr().(*net.TCPAddr).Port)
	if err := Run(context.Background(), &cfg, nil, nil, nil); err == nil {
		t.Error("listened on a taken port")
	}
	for name, edit := range map[string]func(*config.ServerConfig){
		"model":        func(c *config.ServerConfig) { c.Config = "/nonexistent/m.yaml" },
		"link size":    func(c *config.ServerConfig) { c.LinkAddressSize = 3 },
		"zone":         func(c *config.ServerConfig) { c.TimeZone = "Mars/Base" },
		"link address": func(c *config.ServerConfig) { c.LinkAddress = 70000 },
		"common":       func(c *config.ServerConfig) { c.CommonAddress = -2 },
		"parity":       func(c *config.ServerConfig) { c.Serial, c.Parity = "/dev/x", "mark" },
	} {
		c := listenConfig(t, "")
		edit(&c)
		if err := Check(&c); err == nil {
			t.Errorf("%s: accepted", name)
		}
		if err := Run(context.Background(), &c, nil, nil, nil); err == nil {
			t.Errorf("%s: ran", name)
		}
	}
	c := listenConfig(t, "")
	c.LinkAddress, c.CommonAddress = 7, 8
	if err := Check(&c); err != nil {
		t.Errorf("flag addresses: %v", err)
	}
	if listenAddr(&config.ServerConfig{}) != ":2401" {
		t.Error("default listen address")
	}
}

func TestRunSerial(t *testing.T) {
	old := line.OpenSerial
	t.Cleanup(func() { line.OpenSerial = old })
	a, b := net.Pipe()
	line.OpenSerial = func(*serial.Config) (io.ReadWriteCloser, error) { return a, nil }
	cfg := config.ServerDefaults()
	cfg.Serial = "/dev/ttyTEST"
	var ev events
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	ready := make(chan string, 1)
	go func() { done <- Run(ctx, &cfg, ev.log, nil, func(s string) { ready <- s }) }()
	if got := <-ready; got != "/dev/ttyTEST" {
		t.Errorf("ready %q", got)
	}
	cl, err := client.New(b, client.Config{LinkAddr: 1, CommonAddr: 1, RequestTimeout: 5 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := cl.ReadTime(context.Background()); err != nil {
		t.Fatal(err)
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	_ = cl.Close()
	line.OpenSerial = func(*serial.Config) (io.ReadWriteCloser, error) { return nil, errors.New("busy") }
	if err := Run(context.Background(), &cfg, nil, nil, nil); err == nil {
		t.Error("a port that does not open")
	}
}

func TestDemoAndEncode(t *testing.T) {
	m := Demo()
	data, err := m.Encode(true)
	if err != nil {
		t.Fatal(err)
	}
	back, err := Parse(data)
	if err != nil {
		t.Fatalf("the JSON model does not parse back: %v", err)
	}
	if err := back.Validate(nil); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Encode(false); err != nil {
		t.Fatal(err)
	}
	var d Duration
	if j, _ := json.Marshal(Duration(time.Hour)); string(j) != `"1h0m0s"` {
		t.Errorf("duration JSON %s", j)
	}
	if v, _ := Duration(time.Minute).MarshalYAML(); v != "1m0s" {
		t.Errorf("duration YAML %v", v)
	}
	_ = d
}

// freePort returns a TCP port that is free on host now: --port 0 means 2401, the port of the
// interop images, which other tests of the repository use too.
func freePort(t testing.TB, host string) string {
	t.Helper()
	ln, err := net.Listen("tcp", net.JoinHostPort(host, "0"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ln.Close() }()
	return strconv.Itoa(ln.Addr().(*net.TCPAddr).Port)
}
