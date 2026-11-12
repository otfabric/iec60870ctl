//go:build interop

// SPDX-License-Identifier: MIT

package interop

import (
	"bufio"
	"context"
	"encoding/json"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"
)

// binary is the iec102ctl that make interop built.
func binary(t *testing.T) string {
	t.Helper()
	dir := os.Getenv("INTEROP_BIN_DIR")
	if dir == "" {
		skipOrFail(t, "INTEROP_BIN_DIR is not set; run make interop")
	}
	return filepath.Join(dir, "iec102ctl")
}

// model converts the fixture to an iec102ctl meter model (docs/iec102ctl/server-config.md):
// the clock stands still at the fixture's, the totals and tariffs are the fixture's, the
// times are UTC (the station's zone), as iec60870-5-interop docs/FIXTURES.md says.
func model(f *fixture) map[string]any {
	type m = map[string]any
	var totals []m
	for _, l := range f.Totals {
		var periods []m
		for _, p := range l.Periods {
			var ts []m
			for _, o := range p.Totals {
				ts = append(ts, m{"address": o.Address, "value": o.Value, "quality": o.Quality})
			}
			periods = append(periods, m{"end": p.End, "totals": ts})
		}
		totals = append(totals, m{"record": l.Record, "kind": l.Kind, "periods": periods})
	}
	rec := func(r tariffRecord) m {
		return m{
			"address": r.Address, "active_abs": r.ActiveAbs, "active_inc": r.ActiveInc, "active_quality": r.ActiveQuality,
			"reactive_ind_abs": r.ReactiveIndAbs, "reactive_ind_inc": r.ReactiveIndInc, "reactive_ind_quality": r.ReactiveIndQuality,
			"reactive_cap_abs": r.ReactiveCapAbs, "reactive_cap_inc": r.ReactiveCapInc, "reactive_cap_quality": r.ReactiveCapQuality,
			"max_power": r.MaxPower, "max_power_time": r.MaxPowerTime, "max_power_quality": r.MaxPowerQuality,
			"excess_power": r.ExcessPower, "excess_power_quality": r.ExcessPowerQuality,
			"period_start": r.PeriodStart, "period_end": r.PeriodEnd,
		}
	}
	var tariffs []m
	for _, tr := range f.Tariffs {
		var cur, sto []m
		for _, r := range tr.Current {
			cur = append(cur, rec(r))
		}
		for _, r := range tr.Stored {
			sto = append(sto, rec(r))
		}
		tariffs = append(tariffs, m{"register": tr.Register, "current": cur, "stored": sto})
	}
	p := f.Station.Product
	return m{
		"station": m{
			"name": "iec60870-5-interop baseline", "link_address": 1, "common_address": f.Station.CommonAddress,
			"clock": f.Station.Clock, "product": m{"standard": p.Standard, "manufacturer": p.Manufacturer, "product": p.Product},
		},
		"totals": totals,
		"ree":    m{"access_key": f.Station.AccessKey, "tariffs": tariffs},
	}
}

// meter is a running `iec102ctl server start` and its event log.
type meter struct {
	port int
	mu   sync.Mutex
	log  []map[string]any
}

// events returns the events of a kind logged so far.
func (s *meter) events(kind string) []map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []map[string]any
	for _, e := range s.log {
		if e["event"] == kind {
			out = append(out, e)
		}
	}
	return out
}

// startMeter serves the fixture with the built binary on all interfaces (the container
// reaches it as host.docker.internal) and waits until it listens.
func startMeter(t *testing.T, f *fixture) *meter {
	t.Helper()
	bin := binary(t)
	if _, err := os.Stat(bin); err != nil {
		skipOrFail(t, "iec102ctl not built: %v", err)
	}
	data, err := json.MarshalIndent(model(f), "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "baseline.json")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, bin, "server", "start", "--config", path, "--listen", "0.0.0.0", "--port", freePort(t, "0.0.0.0"), "--format", "json", "--timezone", "utc")
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if os.Getenv("IEC102_INTEROP_TRACE") == "1" {
		cmd.Args = append(cmd.Args, "--debug")
		cmd.Stderr = os.Stderr
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	s := &meter{}
	ready := make(chan int, 1)
	done := make(chan struct{})
	go func() {
		defer close(done)
		sc := bufio.NewScanner(stdout)
		for sc.Scan() {
			var e map[string]any
			if json.Unmarshal(sc.Bytes(), &e) != nil {
				continue
			}
			s.mu.Lock()
			s.log = append(s.log, e)
			s.mu.Unlock()
			if e["event"] == "listening" {
				if _, p, ok := cutLast(e["remote"].(string)); ok {
					n, _ := strconv.Atoi(p)
					ready <- n
				}
			}
		}
	}()
	t.Cleanup(func() { cancel(); _ = cmd.Wait(); <-done })
	select {
	case s.port = <-ready:
	case <-time.After(30 * time.Second):
		t.Fatal("iec102ctl server start did not listen")
	}
	return s
}

func cutLast(s string) (before, after string, ok bool) {
	for i := len(s) - 1; i >= 0; i-- {
		if s[i] == ':' {
			return s[:i], s[i+1:], true
		}
	}
	return s, "", false
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
