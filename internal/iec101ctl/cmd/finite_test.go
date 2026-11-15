// SPDX-License-Identifier: MIT

package cmd

import (
	"context"
	"encoding/json"
	"math"
	"net"
	"strconv"
	"sync"
	"testing"

	"github.com/otfabric/go-iec60870-5/asdu"
	"github.com/otfabric/go-iec60870-5/iec101/server"
	"github.com/otfabric/iec60870ctl/internal/iec101ctl/config"
	"github.com/otfabric/iec60870ctl/internal/iec101ctl/iec"
)

// floatPoints are the short floats of floatStation: NaN, the infinities and a finite value.
var floatPoints = []asdu.MeasuredFloat{
	{IOA: 1000, Value: float32(math.NaN())},
	{IOA: 1001, Value: float32(math.Inf(1))},
	{IOA: 1002, Value: float32(math.Inf(-1))},
	{IOA: 1003, Value: 7.5},
}

// wantFloat is the JSON form of floatPoints[i]: no value and the name, or the value.
var wantFloat = map[float64]struct {
	value     any
	nonFinite string
}{
	1000: {nil, "NaN"},
	1001: {nil, "+Inf"},
	1002: {nil, "-Inf"},
	1003: {7.5, ""},
}

// floatStation serves a station of common address 1 on loopback with the default link
// flags: its general interrogation and its reads answer with floatPoints.
func floatStation(t *testing.T) []string {
	t.Helper()
	lc := config.DefaultLink()
	lc.LinkTimeout = 200
	l, err := iec.LinkParams(lc)
	if err != nil {
		t.Fatal(err)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	handler := server.HandlerFunc(func(s *server.Server, req *asdu.ASDU) {
		switch o := req.First().(type) {
		case asdu.Interrogation:
			objs := make([]asdu.InformationObject, len(floatPoints))
			for i, p := range floatPoints {
				objs[i] = p
			}
			_ = s.Confirm(req)
			_ = s.Send(asdu.New(asdu.QOIStation.Cause(), 1, objs...))
			_ = s.Terminate(req)
		case asdu.Read:
			for _, p := range floatPoints {
				if p.IOA == o.IOA {
					_ = s.Send(asdu.New(asdu.CauseRequest, 1, p))
					return
				}
			}
			_ = s.Reject(req, asdu.CauseUnknownIOA)
		default:
			_ = s.Reject(req, asdu.CauseUnknownType)
		}
	})
	wg.Go(func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			srv, err := server.New(c, server.Config{Params: l.Params, LinkAddr: l.Addr, Remote: l.Remote, CommonAddr: 1,
				Handler: handler, NoEndOfInit: true})
			if err != nil {
				_ = c.Close()
				continue
			}
			select {
			case <-ctx.Done():
			case <-srv.Done():
			}
			_ = srv.Close()
			_ = c.Close()
		}
	})
	t.Cleanup(func() {
		cancel()
		_ = ln.Close()
		wg.Wait()
	})
	return []string{"--ip", "127.0.0.1", "--port", strconv.Itoa(ln.Addr().(*net.TCPAddr).Port), "--link-timeout", "200", "--poll-interval", "10"}
}

// checkFloat fails the test unless the JSON object of a point has the documented form.
func checkFloat(t *testing.T, p map[string]any) {
	t.Helper()
	ioa, _ := p["ioa"].(float64)
	want, ok := wantFloat[ioa]
	if !ok {
		t.Errorf("point %v", p)
		return
	}
	value, has := p["value"]
	nf, _ := p["non_finite"].(string)
	if !has || value != want.value || nf != want.nonFinite {
		t.Errorf("ioa %v: value %v (present %v), non_finite %q; want %v and %q", ioa, value, has, nf, want.value, want.nonFinite)
	}
	if _, present := p["non_finite"]; want.nonFinite == "" && present {
		t.Errorf("ioa %v: non_finite present for a finite value", ioa)
	}
}

// A NaN or infinite short float from a station is "value": null with "non_finite" naming
// it in JSON, and the name in text and table output; interrogate, read and monitor
// succeed and list every point.
func TestNonFiniteFloats(t *testing.T) {
	flags := floatStation(t)
	client := func(args ...string) result {
		t.Helper()
		return run(t, append(append([]string{"client"}, args...), flags...)...)
	}

	var gi struct {
		Points []map[string]any `json:"points"`
	}
	r := client("interrogate", "--format", "json").want(t, 0)
	r.decodeLoose(t, &gi)
	if len(gi.Points) != len(floatPoints) {
		t.Fatalf("interrogate: %d points\n%s", len(gi.Points), r.stdout)
	}
	for _, p := range gi.Points {
		checkFloat(t, p)
	}
	client("interrogate").want(t, 0).has(t, "NaN", "+Inf", "-Inf", "7.5")
	client("interrogate", "--format", "table").want(t, 0).has(t, "NaN", "+Inf", "-Inf", "7.5")

	var rd struct {
		Failed int `json:"failed"`
		Rows   []struct {
			Point map[string]any `json:"point"`
		} `json:"rows"`
	}
	r = client("read", "--ioa", "1000-1003", "--format", "json").want(t, 0)
	r.decodeLoose(t, &rd)
	if rd.Failed != 0 || len(rd.Rows) != len(floatPoints) {
		t.Fatalf("read: %s", r.stdout)
	}
	for _, row := range rd.Rows {
		checkFloat(t, row.Point)
	}
	client("read", "--ioa", "1000-1002").want(t, 0).has(t, "NaN", "+Inf", "-Inf")

	r = client("monitor", "--interrogate", "--count", "4", "--format", "json").want(t, 0)
	lines := r.lines(t)
	if len(lines) != len(floatPoints) {
		t.Fatalf("monitor: %d lines\n%s", len(lines), r.stdout)
	}
	for _, p := range lines {
		checkFloat(t, p)
	}
	client("monitor", "--interrogate", "--count", "4", "--format", "table").want(t, 0).has(t, "NaN", "+Inf", "-Inf")
}

// decodeLoose parses stdout as one JSON document into v, which may name only some fields.
func (r result) decodeLoose(t testing.TB, v any) {
	t.Helper()
	if err := json.Unmarshal([]byte(r.stdout), v); err != nil {
		t.Fatalf("stdout is not JSON: %v\n%s", err, r.stdout)
	}
}
