// SPDX-License-Identifier: MIT

//go:build e2e

package e2e

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/otfabric/go-iec60870-5/iec102"
	"github.com/otfabric/go-iec60870-5/iec102/client"
	"github.com/otfabric/go-iec60870-5/iec102/ree"
	"github.com/otfabric/go-iec60870-5/iec102/server"
)

// bin returns the binary under test: $E2E_BIN_DIR/iec102ctl (set by make e2e).
func bin(t *testing.T) string {
	t.Helper()
	dir := os.Getenv("E2E_BIN_DIR")
	if dir == "" {
		t.Fatal("E2E_BIN_DIR is not set; run make e2e")
	}
	return filepath.Join(dir, "iec102ctl")
}

// run executes the binary and returns stdout, stderr and the exit code.
func run(t *testing.T, args ...string) (stdout, stderr string, code int) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	var out, errb bytes.Buffer
	cmd := exec.CommandContext(ctx, bin(t), args...)
	cmd.Stdout, cmd.Stderr = &out, &errb
	err := cmd.Run()
	var exit *exec.ExitError
	switch {
	case err == nil:
		return out.String(), errb.String(), 0
	case errors.As(err, &exit):
		return out.String(), errb.String(), exit.ExitCode()
	default:
		t.Fatalf("run %s: %v", bin(t), err)
		return "", "", -1
	}
}

func TestVersionNamesTheTool(t *testing.T) {
	out, _, code := run(t, "version")
	if code != 0 || !strings.HasPrefix(out, "iec102ctl ") {
		t.Fatalf("exit %d: %s", code, out)
	}
	out, _, code = run(t, "version", "--json")
	var v struct{ Name string }
	if code != 0 || json.Unmarshal([]byte(out), &v) != nil || v.Name != "iec102ctl" {
		t.Fatalf("exit %d: %s", code, out)
	}
}

// An unknown command is a usage error: exit code 2 in docs/exitcodes.md.
func TestUnknownCommandIsAUsageError(t *testing.T) {
	_, stderr, code := run(t, "nosuchcommand")
	if code != 2 || !strings.Contains(stderr, `unknown command "nosuchcommand"`) {
		t.Fatalf("exit %d: %s", code, stderr)
	}
}

// libStation serves go-iec60870-5's integrated total station on a loopback port: a meter
// with hourly totals of record 11, events of record 52, a clock that stands still, the REE
// session with key 7. Every connection gets a fresh server; the clock lives across them.
type libStation struct {
	addr  string
	mu    sync.Mutex
	clock time.Time
}

func (s *libStation) now() time.Time { s.mu.Lock(); defer s.mu.Unlock(); return s.clock }

func startLibStation(t *testing.T) *libStation {
	t.Helper()
	st := &libStation{clock: time.Date(2026, 1, 15, 10, 20, 30, 500e6, time.UTC)}
	store := server.NewStore()
	for h := 1; h <= 3; h++ {
		end := iec102.TimeAOf(time.Date(2026, 1, 1, h, 0, 0, 0, time.UTC), time.UTC)
		store.AddPeriod(11, iec102.IntegratedTotals{T: iec102.M_IT_TG_2, Time: end, Totals: []iec102.IntegratedTotal{
			{Addr: 1, Value: uint32(100 * h)}, {Addr: 2, Value: uint32(h), Quality: iec102.QualityInvalid},
		}})
		store.AddPeriod(11, iec102.IntegratedTotals{T: iec102.M_IT_TK_2, Time: end, Totals: []iec102.IntegratedTotal{{Addr: 1, Value: 100}}})
	}
	store.AddSinglePoints(52, iec102.SinglePoint{Addr: 1, On: true, Time: iec102.TimeBOf(time.Date(2026, 1, 1, 0, 30, 0, 0, time.UTC), time.UTC)})
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	st.addr = ln.Addr().String()
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			srv, err := server.New(c, server.Config{
				LinkAddr: 1, CommonAddr: 1, Params: iec102.Params{Profile: ree.Profile, Location: time.UTC},
				ProductSpec: iec102.ProductSpec{Standard: 42, Manufacturer: 33, Product: 20261115}, Store: store, Clock: st.now,
				Handler: &ree.Session{Key: 7, OnSetTime: func(tb iec102.TimeB) {
					if t, ok := tb.Time(time.UTC); ok {
						st.mu.Lock()
						st.clock = t
						st.mu.Unlock()
					}
				}},
			})
			if err != nil {
				_ = c.Close()
				continue
			}
			<-srv.Done()
			_ = srv.Close()
		}
	}()
	t.Cleanup(func() { _ = ln.Close(); <-done })
	return st
}

// client runs `iec102ctl client <args>` against st with --format json and decodes stdout.
func (s *libStation) client(t *testing.T, v any, args ...string) (int, string) {
	t.Helper()
	args = append(append([]string{"client"}, args...), "--url", "tcp://"+s.addr, "--format", "json", "--timeout", "5000")
	out, stderr, code := run(t, args...)
	if v != nil && code == 0 {
		if err := json.Unmarshal([]byte(out), v); err != nil {
			t.Fatalf("%v: %v\n%s", args, err, out)
		}
	}
	return code, out + stderr
}

func TestClientAgainstLibraryServer(t *testing.T) {
	st := startLibStation(t)
	var info struct{ Standard, Manufacturer, Product uint32 }
	if code, out := st.client(t, &info, "info"); code != 0 || info.Product != 20261115 || info.Manufacturer != 33 {
		t.Fatalf("info: exit %d %+v\n%s", code, info, out)
	}
	var tm struct{ Time string }
	if code, out := st.client(t, &tm, "time"); code != 0 || tm.Time != "2026-01-15T10:20:30.500" {
		t.Fatalf("time: exit %d %+v\n%s", code, tm, out)
	}
	var tot struct {
		Count   int
		Session *struct{ Login, Logout bool }
		Periods []struct {
			Type   string
			End    struct{ Time string }
			Totals []struct {
				Address, Value, Quality uint32
				Flags                   string
			}
		}
	}
	code, out := st.client(t, &tot, "totals", "--from", "2026-01-01T01:00", "--to", "2026-01-01T02:00", "--access-key", "7")
	if code != 0 || tot.Count != 2 || tot.Session == nil || !tot.Session.Logout || tot.Periods[1].End.Time != "2026-01-01T02:00" ||
		tot.Periods[1].Totals[0].Value != 200 || tot.Periods[1].Totals[1].Flags != "IV" {
		t.Fatalf("totals: exit %d %+v\n%s", code, tot, out)
	}
	if code, out := st.client(t, &tot, "totals", "--kind", "incremental", "--oldest"); code != 0 || tot.Count != 1 || tot.Periods[0].Type != "M_IT_TK_2" {
		t.Fatalf("oldest: exit %d\n%s", code, out)
	}
	if code, out := st.client(t, nil, "totals", "--from", "2027-01-01", "--to", "2027-01-02"); code != 6 || !strings.Contains(out, "no_period") {
		t.Fatalf("refusal: exit %d\n%s", code, out)
	}
	var ev struct{ Count int }
	if code, out := st.client(t, &ev, "events", "--record", "52"); code != 0 || ev.Count != 1 {
		t.Fatalf("events: exit %d\n%s", code, out)
	}
	if code, out := st.client(t, nil, "ree", "login", "--access-key", "8"); code != 6 || !strings.Contains(out, "login_refused") {
		t.Fatalf("login refused: exit %d\n%s", code, out)
	}
	if code, out := st.client(t, nil, "ree", "set-time", "--access-key", "7", "--time", "2026-02-03T04:05:06.789"); code != 0 {
		t.Fatalf("set-time: exit %d\n%s", code, out)
	}
	if code, out := st.client(t, &tm, "time"); code != 0 || tm.Time != "2026-02-03T04:05:06.789" {
		t.Fatalf("time after set-time: exit %d %+v\n%s", code, tm, out)
	}
	var rq struct{ Count int }
	if code, out := st.client(t, &rq, "request", "--type", "C_TI_NA_2", "--cause", "request"); code != 0 || rq.Count != 1 {
		t.Fatalf("request: exit %d\n%s", code, out)
	}
	// The library's server has no tariff information: cause 14.
	if code, out := st.client(t, nil, "ree", "tariffs"); code != 6 || !strings.Contains(out, "unknown_type") {
		t.Fatalf("tariffs: exit %d\n%s", code, out)
	}
}

// startServer runs `iec102ctl server start` with the model and returns its address.
func startServer(t *testing.T, model string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "meter.yaml")
	if err := os.WriteFile(p, []byte(model), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, bin(t), "server", "start", "--config", p, "--listen", "127.0.0.1", "--port", freePort(t, "127.0.0.1"), "--format", "json")
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cancel(); _ = cmd.Wait() })
	sc := bufio.NewScanner(stdout)
	for sc.Scan() {
		var e struct{ Event, Remote string }
		if json.Unmarshal(sc.Bytes(), &e) == nil && e.Event == "listening" {
			go func() {
				for sc.Scan() {
				}
			}()
			return e.Remote
		}
	}
	t.Fatal("the server did not start listening")
	return ""
}

const model = `
station: {link_address: 4, common_address: 9, clock: "2026-01-15T10:20:30.500", product: {standard: 1, manufacturer: 2, product: 3}}
totals:
  - record: 11
    kind: absolute
    generate: {start: 2026-01-01T01:00, interval: 1h, count: 24, totals: [{address: 1, start: 100, step: 10}, {address: 2, step: 1}]}
events:
  - record: 52
    points: [{address: 3, on: true, time: "2026-01-01T00:00:01.000"}]
ree:
  access_key: 1234
  tariffs: [{register: 134, current: [{address: 1, active_abs: 5, active_inc: 5, reactive_ind_abs: 0, reactive_ind_inc: 0,
    reactive_cap_abs: 0, reactive_cap_inc: 0, max_power: 9, max_power_time: 2026-01-01T00:00, excess_power: 0,
    period_start: 2026-01-01T00:00, period_end: 2026-01-02T00:00}]}]
`

func TestServerAgainstLibraryClient(t *testing.T) {
	addr := startServer(t, model)
	dial := func() *client.Client {
		c, err := net.Dial("tcp", addr)
		if err != nil {
			t.Fatal(err)
		}
		cl, err := client.New(c, client.Config{LinkAddr: 4, CommonAddr: 9, Params: iec102.Params{Profile: ree.Profile}, RequestTimeout: 5 * time.Second})
		if err != nil {
			t.Fatal(err)
		}
		return cl
	}
	ctx := context.Background()
	c := dial()
	ps, err := c.ReadProductSpec(ctx)
	if err != nil || ps.Product != 3 {
		t.Fatalf("product %+v %v", ps, err)
	}
	tb, err := c.ReadTime(ctx)
	if err != nil || tb.Hour != 10 || tb.Minute != 20 || tb.Second != 30 {
		t.Fatalf("time %+v %v", tb, err)
	}
	if err := ree.Login(ctx, c, 1234); err != nil {
		t.Fatal(err)
	}
	from := iec102.TimeAOf(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), time.UTC)
	to := iec102.TimeAOf(time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC), time.UTC)
	periods, err := c.ReadTotals(ctx, 11, iec102.Read{T: iec102.C_CI_NT_2, First: 1, Last: 8, From: from, To: to})
	if err != nil || len(periods) != 24 || periods[23].Totals[0].Value != 330 {
		t.Fatalf("totals %d %v", len(periods), err)
	}
	pts, err := c.ReadSinglePoints(ctx, 52, nil, nil)
	if err != nil || len(pts) != 1 || pts[0].Addr != 3 {
		t.Fatalf("events %+v %v", pts, err)
	}
	out, err := c.Request(ctx, iec102.New(iec102.CauseActivation, 9, 134, ree.Request{T: ree.C_TA_VC_2}), client.UntilTermination)
	if err != nil || len(out) != 3 {
		t.Fatalf("tariffs %d %v", len(out), err)
	}
	if err := ree.SetTime(ctx, c, iec102.TimeBOf(time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC), time.UTC)); err != nil {
		t.Fatal(err)
	}
	if err := ree.Logout(ctx, c); err != nil {
		t.Fatal(err)
	}
	_, err = c.ReadTotals(ctx, 12, iec102.Read{T: iec102.C_CI_NT_2, First: 1, Last: 8, From: from, To: to})
	if r, ok := errors.AsType[*iec102.RefusedError](err); !ok || r.Cause().Code() != iec102.CauseUnknownRecordAddr {
		t.Fatalf("unknown record: %v", err)
	}
	_ = c.Close()
	// The clock set lives across connections; a wrong key is refused.
	c = dial()
	defer func() { _ = c.Close() }()
	if tb, err := c.ReadTime(ctx); err != nil || tb.Month != 6 {
		t.Fatalf("time after set %+v %v", tb, err)
	}
	if err := ree.Login(ctx, c, 1); err == nil {
		t.Fatal("a wrong key was accepted")
	}
}

// The binary's client against the binary's server: what one sends the other reads.
func TestClientAgainstOwnServer(t *testing.T) {
	addr := startServer(t, model)
	out, stderr, code := run(t, "client", "totals", "--url", "tcp://"+addr, "--link-address", "4", "--common-address", "9",
		"--from", "2026-01-01", "--to", "2026-01-01T03:00", "--access-key", "1234", "--format", "json")
	var r struct{ Count int }
	if code != 0 || json.Unmarshal([]byte(out), &r) != nil || r.Count != 3 {
		t.Fatalf("exit %d: %s %s", code, out, stderr)
	}
	_, _, code = run(t, "client", "info", "--url", "tcp://"+addr, "--link-address", "5", "--link-timeout", "100", "--link-retries", "1", "--timeout", "3000")
	if code != 5 {
		t.Fatalf("wrong link address: exit %d", code)
	}

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
