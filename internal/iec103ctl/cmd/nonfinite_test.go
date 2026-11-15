// SPDX-License-Identifier: MIT

package cmd

import (
	"encoding/json"
	"math"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/otfabric/go-iec60870-5/iec103"
	"github.com/otfabric/go-iec60870-5/iec103/server"
	"github.com/otfabric/iec60870ctl/internal/iec103ctl/station"
	"github.com/otfabric/iec60870ctl/internal/iec103ctl/stationtest"
)

// scls are the short-circuit locations the device of nonFiniteDevice reports.
var scls = []float32{1.5, float32(math.NaN()), float32(math.Inf(1)), float32(math.Inf(-1))}

// nonFiniteDevice serves, on a loopback port, a device (link address 1) that reports NaN
// and the infinities: after each reset of its link the time-tagged measurands (ASDU 4) of
// scls, and to every generic read of entries (INF 244) the R32 values NaN, +Inf for entry
// 1.2 and the R64 values -Inf, 2.5 for any other entry. Only its address is set.
func nonFiniteDevice(t *testing.T) *stationtest.Station {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	wg.Go(func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			wg.Go(func() {
				defer func() { _ = c.Close() }()
				var srv *server.Server
				ready := make(chan struct{})
				srv, err := server.New(c, server.Config{LinkAddr: 1, CommonAddr: 1, Fun: 160,
					Identification: iec103.IdentificationOf(2, "NONFINIT", [4]byte{1, 2, 3, 4}),
					OnState: func(st iec103.State, _ error) {
						if st != iec103.StateUp {
							return
						}
						<-ready
						for i, scl := range scls {
							_ = srv.Send(iec103.New(iec103.CauseSpontaneous, 1, 160, 73, iec103.RelTimeMeasurand{SCL: scl, RET: uint16(i), FAN: 1}), server.Class1)
						}
					},
					Handler: server.HandlerFunc(func(srv *server.Server, a *iec103.ASDU) {
						b, ok := a.Body.(iec103.GenericCommand)
						if !ok || a.Cause != iec103.CauseGenericRead || a.Inf != iec103.InfReadEntry {
							return
						}
						var sets []iec103.DataSet
						for _, it := range b.Items {
							v := iec103.Float64s(math.Inf(-1), 2.5)
							if it.GIN == (iec103.GIN{Group: 1, Entry: 2}) {
								v = iec103.Float32s(float32(math.NaN()), float32(math.Inf(1)))
							}
							sets = append(sets, iec103.DataSet{GIN: it.GIN, KOD: it.KOD, Value: v})
						}
						_ = srv.GenericAnswer(a, iec103.CauseGenericReadValid, sets...)
					}),
				})
				if err != nil {
					t.Error(err)
					return
				}
				close(ready)
				<-srv.Done()
			})
		}
	})
	t.Cleanup(func() {
		_ = ln.Close()
		wg.Wait()
	})
	return &stationtest.Station{Addr: ln.Addr().String()}
}

// A short-circuit location that is NaN or infinite is streamed in the documented form, and
// the monitor goes on: every message is written.
func TestMonitorNonFiniteSCL(t *testing.T) {
	dev := nonFiniteDevice(t)
	// Two identifications (reset CU, power on), then the four measurands.
	r := cli(t, dev, "monitor", "--count", "6", "--no-reconnect", "--format", "json").want(t, 0)
	var got []map[string]any
	for line := range strings.SplitSeq(strings.TrimSpace(r.stdout), "\n") {
		var m map[string]any
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			t.Fatalf("%v: %s", err, line)
		}
		if m["type"] == "time-tagged-measurands" {
			got = append(got, m)
		}
	}
	if len(got) != len(scls) {
		t.Fatalf("%d measurands, want %d:\n%s", len(got), len(scls), r.stdout)
	}
	for i, want := range []string{"", "NaN", "+Inf", "-Inf"} {
		m := got[i]
		if want == "" {
			if m["scl"] != 1.5 || m["non_finite"] != nil {
				t.Errorf("measurand %d: %v", i, m)
			}
			continue
		}
		if _, ok := m["scl"]; ok || m["non_finite"] != want {
			t.Errorf("measurand %d: %v, want non_finite %q in place of scl", i, m, want)
		}
	}
	for _, format := range []string{"text", "table"} {
		cli(t, dev, "monitor", "--count", "6", "--no-reconnect", "--format", format).want(t, 0).
			has(t, "scl=1.5", "scl=NaN", "scl=+Inf", "scl=-Inf")
	}
}

// Generic values that are NaN or infinite are null with non_finite naming them, in the
// order of the values; text and table output show the names.
func TestGenericReadNonFinite(t *testing.T) {
	dev := nonFiniteDevice(t)
	r := cli(t, dev, "generic", "read", "--inf", "244", "--items", "1.2,1.3", "--format", "json").want(t, 0)
	var res struct {
		Sets []struct {
			GIN       string   `json:"gin"`
			Values    []any    `json:"values"`
			NonFinite []string `json:"non_finite"`
		} `json:"sets"`
	}
	if err := json.Unmarshal([]byte(r.stdout), &res); err != nil {
		t.Fatalf("%v\n%s", err, r.stdout)
	}
	if len(res.Sets) != 2 {
		t.Fatalf("sets: %+v", res.Sets)
	}
	for i, want := range []struct {
		values    []any
		nonFinite []string
	}{
		{[]any{nil, nil}, []string{"NaN", "+Inf"}},
		{[]any{nil, 2.5}, []string{"-Inf", ""}},
	} {
		d := res.Sets[i]
		if len(d.Values) != len(want.values) || d.Values[0] != want.values[0] || d.Values[1] != want.values[1] ||
			strings.Join(d.NonFinite, ",") != strings.Join(want.nonFinite, ",") {
			t.Errorf("set %s: values %v non_finite %q, want %v %q", d.GIN, d.Values, d.NonFinite, want.values, want.nonFinite)
		}
	}
	for _, format := range []string{"text", "table"} {
		cli(t, dev, "generic", "read", "--inf", "244", "--items", "1.2,1.3", "--format", format).want(t, 0).
			has(t, "NaN, +Inf", "-Inf, 2.5")
	}
}

// Rated values of a disturbance channel that are NaN or infinite are null with non_finite
// naming them, on stdout and in --output; the transfer is written in full.
func TestDisturbanceNonFiniteRatedValues(t *testing.T) {
	st := stationtest.StartChanged(t, stationtest.Model, func(m *station.Model) {
		c := &m.Disturbances[0].Channels[0]
		c.RPV, c.RSV, c.RFA = math.NaN(), math.Inf(1), math.Inf(-1)
	})
	dir := t.TempDir()
	out, csv := filepath.Join(dir, "fault.json"), filepath.Join(dir, "fault.csv")
	r := cli(t, st, "disturbance", "get", "--fan", "7", "--output", out, "--csv", csv, "--format", "json").want(t, 0)
	file, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	for name, doc := range map[string]string{"stdout": r.stdout, "--output": string(file)} {
		var d struct {
			Channels []map[string]any `json:"channels"`
		}
		if err := json.Unmarshal([]byte(doc), &d); err != nil || len(d.Channels) != 2 {
			t.Fatalf("%s: %v\n%s", name, err, doc)
		}
		c := d.Channels[0]
		nf, _ := c["non_finite"].(map[string]any)
		if c["rpv"] != nil || c["rsv"] != nil || c["rfa"] != nil || nf["rpv"] != "NaN" || nf["rsv"] != "+Inf" || nf["rfa"] != "-Inf" {
			t.Errorf("%s: channel %v", name, c)
		}
		if c := d.Channels[1]; c["rpv"] != 600.0 || c["non_finite"] != nil {
			t.Errorf("%s: channel %v", name, c)
		}
	}
	if _, err := os.Stat(csv); err != nil {
		t.Error(err)
	}
	cli(t, st, "disturbance", "get", "--fan", "7").want(t, 0).has(t, "rpv=NaN rsv=+Inf rfa=-Inf", "rpv=600 rsv=1 rfa=1")
	cli(t, st, "disturbance", "get", "--fan", "7", "--format", "table").want(t, 0).has(t, "NaN", "+Inf", "-Inf")
}
