// SPDX-License-Identifier: MIT

package iec_test

// The client functions against the simulated station of package station. They live in an
// external test package: station imports iec.

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/otfabric/iec104ctl/internal/config"
	"github.com/otfabric/iec104ctl/internal/iec"
	"github.com/otfabric/iec104ctl/internal/stationtest"
	"github.com/otfabric/iec104ctl/internal/types"
)

func connTo(st *stationtest.Station) config.ConnConfig {
	return config.ConnConfig{IP: st.Host, Port: st.Port, Timeout: 2000, LinkConfig: config.LinkConfig{T1: 2, T2: 1}}
}

// A sweep whose end is the last common address terminates: the counter of the loop does
// not wrap around at 65535. 65535 is the broadcast address, which the station answers as
// itself.
func TestSweepToTheLastCommonAddress(t *testing.T) {
	st := stationtest.Start(t, stationtest.Model)
	cfg := &config.ScanConfig{ConnConfig: connTo(st), Method: iec.ScanSweep, Start: 65534, End: 65535}
	done := make(chan struct{})
	var (
		res *types.ScanResult
		err error
	)
	go func() {
		defer close(done)
		res, err = iec.Scan(context.Background(), cfg, nil, io.Discard)
	}()
	select {
	case <-done:
	case <-time.After(15 * time.Second):
		t.Fatal("a sweep of 65534-65535 did not end")
	}
	if err != nil {
		t.Fatal(err)
	}
	if res.Probed != 2 {
		t.Errorf("probed %d addresses, want 2: %+v", res.Probed, res)
	}
	for _, s := range res.Stations {
		if s.CommonAddr == 65534 {
			t.Errorf("station 1 answered at 65534: %+v", res.Stations)
		}
	}
}

// --count stops monitor after exactly that many values, also when one ASDU carries more.
func TestMonitorStopsAtCount(t *testing.T) {
	st := stationtest.Start(t, stationtest.Model)
	for _, count := range []uint32{1, 2, 5} {
		var emitted atomic.Int32
		cfg := &config.MonitorConfig{ConnConfig: connTo(st), Interrogate: true, Count: count, Duration: 10}
		sum, err := iec.Monitor(context.Background(), cfg, io.Discard, func(*types.Point) error { emitted.Add(1); return nil })
		if err != nil {
			t.Fatal(err)
		}
		if n := emitted.Load(); n != int32(count) || sum.Values != int(count) {
			t.Errorf("--count %d: %d values emitted, summary %d", count, n, sum.Values)
		}
		if sum.Duration > 5*time.Second {
			t.Errorf("--count %d: monitor ran %s", count, sum.Duration)
		}
	}
}

// A fetched file is written readable by its owner only.
func TestFileGetWritesAPrivateFile(t *testing.T) {
	st := stationtest.Start(t, stationtest.Model)
	out := filepath.Join(t.TempDir(), "file.bin")
	cfg := &config.FileGetConfig{StationConfig: config.StationConfig{ConnConfig: connTo(st), CommonAddr: 1}, IOA: 30001, Name: 2, Output: out}
	if _, err := iec.FileGet(context.Background(), cfg, nil, io.Discard); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(out)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm&0o077 != 0 {
		t.Errorf("%s has mode %v", out, perm)
	}
}
