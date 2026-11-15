// SPDX-License-Identifier: MIT

package cmd

import (
	"encoding/json"
	"math"
	"net"
	"strconv"
	"strings"
	"testing"

	"github.com/otfabric/go-iec60870-5/asdu"
	"github.com/otfabric/go-iec60870-5/iec104/server"
	"github.com/otfabric/iec60870ctl/internal/iec104ctl/stationtest"
)

// nonFiniteValues are the short floating point values of the station of
// startNonFiniteStation, by address: NaN, +Inf and -Inf, which JSON has no number for, and
// one ordinary value.
var nonFiniteValues = map[asdu.IOA]float32{
	1000: float32(math.NaN()),
	1001: float32(math.Inf(1)),
	1002: float32(math.Inf(-1)),
	1003: 230.5,
}

// nonFiniteNames are the names the output gives those values.
var nonFiniteNames = map[float64]string{1000: "NaN", 1001: "+Inf", 1002: "-Inf"}

// startNonFiniteStation serves station 1 with the values of nonFiniteValues as M_ME_NC_1,
// answering a general interrogation and reads, and returns the flags that address it.
func startNonFiniteStation(t *testing.T) []string {
	t.Helper()
	objects := func(ioas ...asdu.IOA) []asdu.InformationObject {
		var out []asdu.InformationObject
		for _, ioa := range ioas {
			out = append(out, asdu.MeasuredFloat{IOA: ioa, Value: nonFiniteValues[ioa]})
		}
		return out
	}
	mux := server.NewMux()
	mux.HandleFunc(asdu.C_IC_NA_1, func(s *server.Session, req *asdu.ASDU) {
		qoi := req.First().(asdu.Interrogation).Qualifier
		_ = s.Confirm(req)
		_ = s.Send(s.Context(), asdu.New(qoi.Cause(), req.CommonAddr, objects(1000, 1001, 1002, 1003)...))
		_ = s.Terminate(req)
	})
	mux.HandleFunc(asdu.C_RD_NA_1, func(s *server.Session, req *asdu.ASDU) {
		ioa := req.First().Address()
		if _, ok := nonFiniteValues[ioa]; !ok {
			_ = s.Reject(req, asdu.CauseUnknownIOA)
			return
		}
		_ = s.Send(s.Context(), asdu.New(asdu.CauseRequest, req.CommonAddr, objects(ioa)...))
	})
	srv, err := server.New(mux, server.WithParams(stationtest.Params()), server.WithCommonAddrs(1))
	if err != nil {
		t.Fatal(err)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = srv.Serve(ln)
	}()
	t.Cleanup(func() {
		_ = srv.Close()
		<-done
	})
	_, port, _ := net.SplitHostPort(ln.Addr().String())
	return []string{"--ip", "127.0.0.1", "--port", port, "--t1", "2", "--t2", "1", "--ca", "1"}
}

// checkNonFinitePoint checks one point of JSON output: NaN and the infinities are a null
// value with their name in non_finite, any other value is a number without non_finite.
func checkNonFinitePoint(t *testing.T, where string, p map[string]any) {
	t.Helper()
	ioa, _ := p["ioa"].(float64)
	name, nonFinite := nonFiniteNames[ioa]
	switch {
	case nonFinite && (p["value"] != nil || p["non_finite"] != name):
		t.Errorf("%s: ioa %v: value %v, non_finite %v; want null and %q", where, ioa, p["value"], p["non_finite"], name)
	case !nonFinite && (p["value"] != 230.5 || p["non_finite"] != nil):
		t.Errorf("%s: ioa %v: value %v, non_finite %v; want 230.5 and none", where, ioa, p["value"], p["non_finite"])
	}
	if _, ok := p["value"]; !ok {
		t.Errorf("%s: ioa %v has no value field", where, ioa)
	}
}

// A station that reports NaN, +Inf or -Inf in a short floating point value: interrogate,
// read and monitor succeed, JSON gives the value as null with its name in non_finite, and
// text and table output show the name.
func TestNonFiniteValues(t *testing.T) {
	flags := startNonFiniteStation(t)
	client := func(sub string, args ...string) result {
		t.Helper()
		return run(t, append(append([]string{"client", sub}, flags...), args...)...)
	}

	t.Run("interrogate json", func(t *testing.T) {
		r := client("interrogate", "--format", "json").want(t, 0)
		var res struct {
			Count  int              `json:"count"`
			Points []map[string]any `json:"points"`
		}
		if err := json.Unmarshal([]byte(r.stdout), &res); err != nil {
			t.Fatalf("stdout is not JSON: %v\n%s", err, r.stdout)
		}
		if res.Count != 4 || len(res.Points) != 4 {
			t.Fatalf("%d points (count %d), want 4:\n%s", len(res.Points), res.Count, r.stdout)
		}
		for _, p := range res.Points {
			checkNonFinitePoint(t, "interrogate", p)
		}
	})

	t.Run("read json", func(t *testing.T) {
		r := client("read", "--ioa", "1000-1003", "--format", "json").want(t, 0)
		var res struct {
			Failed int `json:"failed"`
			Rows   []struct {
				Point map[string]any `json:"point"`
			} `json:"rows"`
		}
		if err := json.Unmarshal([]byte(r.stdout), &res); err != nil {
			t.Fatalf("stdout is not JSON: %v\n%s", err, r.stdout)
		}
		if res.Failed != 0 || len(res.Rows) != 4 {
			t.Fatalf("%d rows, %d failed, want 4 and 0:\n%s", len(res.Rows), res.Failed, r.stdout)
		}
		for _, row := range res.Rows {
			checkNonFinitePoint(t, "read", row.Point)
		}
	})

	t.Run("monitor json", func(t *testing.T) {
		r := client("monitor", "--format", "json", "--interrogate", "--count", "4", "--duration", "20").want(t, 0)
		lines := r.lines(t)
		if len(lines) != 4 {
			t.Fatalf("%d records, want 4:\n%s", len(lines), r.stdout)
		}
		for _, p := range lines {
			checkNonFinitePoint(t, "monitor", p)
		}
	})

	for _, f := range []string{"text", "table"} {
		t.Run("interrogate "+f, func(t *testing.T) {
			r := client("interrogate", "--format", f).want(t, 0)
			for _, name := range []string{"NaN", "+Inf", "-Inf", "230.5"} {
				if !strings.Contains(r.stdout, name) {
					t.Errorf("%s output does not show %s:\n%s", f, name, r.stdout)
				}
			}
		})
	}

	t.Run("monitor text", func(t *testing.T) {
		r := client("monitor", "--interrogate", "--count", "4", "--duration", "20").want(t, 0)
		for ioa, name := range nonFiniteNames {
			want := "ioa=" + strconv.Itoa(int(ioa))
			found := false
			for line := range strings.SplitSeq(r.stdout, "\n") {
				if strings.Contains(line, want) && strings.Contains(line, name) {
					found = true
				}
			}
			if !found {
				t.Errorf("no line shows %s at %s:\n%s", name, want, r.stdout)
			}
		}
	})
}
