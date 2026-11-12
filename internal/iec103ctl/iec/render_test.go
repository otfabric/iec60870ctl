// SPDX-License-Identifier: MIT

package iec

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/otfabric/go-iec60870-5/cp"
	"github.com/otfabric/go-iec60870-5/iec103"
	"github.com/otfabric/iec60870ctl/internal/errs"
	"github.com/otfabric/iec60870ctl/internal/iec103ctl/config"
	"github.com/otfabric/iec60870ctl/internal/iec103ctl/stationtest"
	"github.com/otfabric/iec60870ctl/internal/iec103ctl/types"
)

func TestMessageOfEveryBody(t *testing.T) {
	ts := cp.At(time.Date(2026, 1, 2, 3, 4, 5, 6e6, time.UTC))
	ts.Invalid = true
	for _, c := range []struct {
		a    *iec103.ASDU
		want string
	}{
		{iec103.New(1, 1, 160, 16, iec103.TimeTagged{DPI: 2, Time: ts, SIN: 4}), "dpi=ON time=03:04:05.006(invalid) sin=4"},
		{iec103.New(1, 1, 128, 85, iec103.RelTimeTagged{DPI: 1, RET: 12, FAN: 3, Time: ts, SIN: 0}), "ret=12ms fan=3"},
		{iec103.New(2, 1, 160, 148, iec103.Measurands{Set2: true, Values: []iec103.MEA{{Raw: 5, Overflow: true}, {Raw: -1, Error: true}}}), "values=5(ov),-1(er)"},
		{iec103.New(1, 1, 128, 73, iec103.RelTimeMeasurand{SCL: 1.5, RET: 1, FAN: 2, Time: ts}), "scl=1.5"},
		{iec103.New(4, 1, 160, 3, iec103.IdentificationOf(2, "ABC", [4]byte{1, 2, 3, 4})), `manufacturer="ABC" software=01020304`},
		{iec103.New(8, 1, 255, 0, iec103.TimeSync{Time: ts}), "time=2026-01-02T03:04:05.006Z(invalid)"},
		{iec103.New(10, 1, 255, 0, iec103.GITermination{SCN: 9}), "scn=9"},
		{iec103.New(9, 1, 255, 0, iec103.GIInit{SCN: 8}), "scn=8"},
		{iec103.New(31, 1, 160, 0, iec103.DisturbanceList{Faults: []iec103.RecordedFault{{FAN: 1, Time: ts}}}), "data="},
		{&iec103.ASDU{Type: 200, VSQ: 0x81, Cause: 1, CommonAddr: 1, Fun: 1, Inf: 2, Body: iec103.Raw{Data: []byte{0xAB}}}, "data=ab"},
	} {
		m := MessageOf(c.a)
		if !strings.Contains(m.Summary(), c.want) {
			t.Errorf("%v: %q, want %q", c.a, m.Summary(), c.want)
		}
		if m.Type == "" || m.CauseName == "" {
			t.Errorf("%+v", m)
		}
	}
	if TypeName(15) != "reserved" || TypeName(200) != "private" || TypeName(1) != "time-tagged-message" {
		t.Fatal("type names")
	}
	if KODName(99) != "kod-99" || DataTypeName(99) != "type-99" || GRCName(99) != "grc-99" || ACCName(99) != "acc-99" || ACCName(4) != "IN" {
		t.Fatal("names")
	}
	if _, ok := IdentificationOf(iec103.New(1, 1, 1, 1, iec103.GIInit{})); ok {
		t.Fatal("not an identification")
	}
	// An ASDU that cannot be encoded renders without data.
	bad := iec103.New(1, 1, 1, 1, iec103.Measurands{Values: make([]iec103.MEA, 200)})
	if elementsHex(bad) != "" {
		t.Fatal("too long ASDU rendered")
	}
}

func TestValuesOf(t *testing.T) {
	st, err := iec103.Structure(iec103.ASCII("A"), iec103.Uints(iec103.DataUint, 1, 7))
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		v    iec103.Value
		want string
	}{
		{iec103.ASCII("X\x00"), "[X]"},
		{iec103.Uints(iec103.DataUint, 2, 1, 2), "[1 2]"},
		{iec103.Ints(2, -1), "[-1]"},
		{iec103.Float64s(2.5), "[2.5]"},
		{iec103.Reply(iec103.GRCReadOnly), "[8]"},
		{st, "type:ascii"},
		{iec103.Value{GDD: iec103.GDD{Type: iec103.DataTime, Size: 7, Number: 1}, Data: make([]byte, 7)}, "[]"},
		{iec103.Value{GDD: iec103.GDD{Type: iec103.DataUint, Size: 9, Number: 1}, Data: make([]byte, 9)}, "[]"},
		{iec103.Value{GDD: iec103.GDD{Type: iec103.DataInt, Size: 9, Number: 1}, Data: make([]byte, 9)}, "[]"},
		{iec103.Value{GDD: iec103.GDD{Type: iec103.DataFloat32, Size: 2, Number: 1}, Data: make([]byte, 2)}, "[]"},
		{iec103.Value{GDD: iec103.GDD{Type: iec103.DataStructure, Size: 1, Number: 1}, Data: []byte{1}}, "[]"},
	} {
		if got := fmtAny(ValuesOf(c.v)); !strings.Contains(got, c.want) {
			t.Errorf("%+v: %s, want %s", c.v.GDD, got, c.want)
		}
	}
	deep := iec103.ASCII("x")
	for range maxStructureDepth + 1 {
		deep, _ = iec103.Structure(deep)
	}
	if !strings.Contains(fmtAny(ValuesOf(deep)), "values:[]") {
		t.Fatal(fmtAny(ValuesOf(deep)))
	}
	d := DescElementOf(iec103.DescElement{KOD: 10, Value: iec103.Reply(1)})
	if d.Reply != "invalid-gin" || d.KODName != "description" {
		t.Fatalf("%+v", d)
	}
	if replyOf(iec103.Value{GDD: iec103.GDD{Type: iec103.DataGRC}}) != "" {
		t.Fatal("empty reply")
	}
}

func fmtAny(v []any) string {
	parts := make([]string, 0, len(v))
	for _, x := range v {
		if m, ok := x.(map[string]any); ok {
			parts = append(parts, "type:"+m["type"].(string)+" values:"+fmtAny(asSlice(m["values"])))
			continue
		}
		parts = append(parts, fmt.Sprint(x))
	}
	return "[" + strings.Join(parts, " ") + "]"
}

func asSlice(v any) []any {
	s, _ := v.([]any)
	return s
}

func TestDisturbanceOf(t *testing.T) {
	d := DisturbanceOf(&iec103.Disturbance{Fun: 160, FAN: 1, Channels: []iec103.Channel{{ACC: 1}}})
	if d.NOC != 1 || d.Channels[0].Values == nil || d.Tags == nil {
		t.Fatalf("%+v", d)
	}
	if s := SOFOf(iec103.SOFTrip | iec103.SOFTest | iec103.SOFTM | iec103.SOFOTEV); !s.TP || !s.TM || !s.Test || !s.OTEV {
		t.Fatalf("%+v", s)
	}
}

func TestMonitor(t *testing.T) {
	st := stationtest.Start(t, stationtest.Model)
	cfg := config.MonitorConfig{ConnConfig: conn(st.URL()), Interrogate: true, Count: 20}
	stderr := &syncWriter{w: &bytes.Buffer{}}
	var got []types.Message
	sum, err := Monitor(context.Background(), &cfg, stderr, func(m *types.Message) error {
		got = append(got, *m)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if sum.Messages != 20 || len(got) != 20 || sum.Target != st.URL() {
		t.Fatalf("%+v %d", sum, len(got))
	}
	causes := map[uint8]bool{}
	for _, m := range got {
		causes[m.Cause] = true
		if m.Received == "" {
			t.Fatal("no time of arrival")
		}
	}
	if !causes[9] || !causes[2] {
		t.Fatalf("causes %v", causes)
	}
	if !strings.Contains(stderr.String(), "link up") {
		t.Fatal(stderr.String())
	}
	// --duration ends it without an error; an output error ends it with one.
	cfg.Count, cfg.Duration, cfg.Interrogate = 0, 1, false
	if _, err := Monitor(context.Background(), &cfg, io.Discard, func(*types.Message) error { return nil }); err != nil {
		t.Fatal(err)
	}
	_, err = Monitor(context.Background(), &cfg, io.Discard, func(*types.Message) error { return errors.New("pipe") })
	if kindOf(t, err) != errs.KindOutput {
		t.Fatal(err)
	}
	if err := CheckMonitor(&cfg); err != nil {
		t.Fatal(err)
	}
	cfg.URL = stationtest.Refused(t)
	if _, err := Monitor(context.Background(), &cfg, io.Discard, nil); kindOf(t, err) != errs.KindTransport {
		t.Fatal(err)
	}
}

// A line that closes: monitor reconnects, or with --no-reconnect ends with exit 4.
func TestMonitorReconnects(t *testing.T) {
	old := ReconnectDelay
	ReconnectDelay = 20 * time.Millisecond
	t.Cleanup(func() { ReconnectDelay = old })
	st := stationtest.Start(t, stationtest.Model)
	px := newProxy(t, strings.TrimPrefix(st.URL(), "tcp://"))
	cfg := config.MonitorConfig{ConnConfig: conn("tcp://" + px.addr())}
	n := 0
	var cuts sync.Once
	sum, err := Monitor(context.Background(), &cfg, io.Discard, func(*types.Message) error {
		n++
		if n == 5 {
			cuts.Do(px.cut)
		}
		if n == 40 {
			return errStop
		}
		return nil
	})
	if !errors.Is(err, errStop) || sum.Reconnects < 1 {
		t.Fatalf("%v %+v", err, sum)
	}
	cfg.NoReconnect = true
	n = 0
	_, err = Monitor(context.Background(), &cfg, io.Discard, func(*types.Message) error {
		n++
		if n == 3 {
			px.cut()
		}
		return nil
	})
	if kindOf(t, err) != errs.KindTransport || codeOf(t, err) != errs.CodeConnectionLost {
		t.Fatal(err)
	}
}

var errStop = errors.New("stop")

// proxy passes TCP connections through to a target; cut closes the open ones.
type proxy struct {
	t      *testing.T
	ln     net.Listener
	target string
	mu     sync.Mutex
	conns  []net.Conn
	wg     sync.WaitGroup
}

func newProxy(t *testing.T, target string) *proxy {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	p := &proxy{t: t, ln: ln, target: target}
	p.wg.Go(p.accept)
	t.Cleanup(func() {
		_ = ln.Close()
		p.cut()
		p.wg.Wait()
	})
	return p
}

func (p *proxy) addr() string { return p.ln.Addr().String() }

func (p *proxy) accept() {
	for {
		c, err := p.ln.Accept()
		if err != nil {
			return
		}
		u, err := net.Dial("tcp", p.target)
		if err != nil {
			_ = c.Close()
			continue
		}
		p.mu.Lock()
		p.conns = append(p.conns, c, u)
		p.mu.Unlock()
		p.wg.Go(func() { _, _ = io.Copy(u, c); _ = u.Close() })
		p.wg.Go(func() { _, _ = io.Copy(c, u); _ = c.Close() })
	}
}

func (p *proxy) cut() {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, c := range p.conns {
		_ = c.Close()
	}
	p.conns = nil
}

func TestScan(t *testing.T) {
	st := stationtest.Start(t, stationtest.Model)
	cfg := config.ScanConfig{ConnConfig: conn(st.URL()), Start: 0, End: 2}
	cfg.Timeout = 300
	res, err := Scan(context.Background(), &cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.Tried != 3 || len(res.Found) != 1 || res.Found[0].LinkAddress != 1 || !res.Found[0].Identified || res.Found[0].Manufacturer != "OTFAB103" {
		t.Fatalf("%+v", res)
	}
	for _, bad := range []config.ScanConfig{{ConnConfig: cfg.ConnConfig, Start: 3, End: 2}, {ConnConfig: cfg.ConnConfig, End: 255}, {ConnConfig: config.ConnConfig{}}} {
		if err := CheckScan(&bad); err == nil {
			t.Errorf("%+v accepted", bad)
		}
		if _, err := Scan(context.Background(), &bad, nil); kindOf(t, err) != errs.KindInvalidInput {
			t.Error(err)
		}
	}
	cfg.URL = stationtest.Refused(t)
	if _, err := Scan(context.Background(), &cfg, nil); kindOf(t, err) != errs.KindTransport {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	cfg.URL = st.URL()
	if _, err := Scan(ctx, &cfg, nil); err == nil {
		t.Fatal("canceled scan ran")
	}
}

// A line that closes in the middle of a scan ends it with a connection error.
func TestScanLineClosed(t *testing.T) {
	st := stationtest.Start(t, stationtest.Model)
	px := newProxy(t, strings.TrimPrefix(st.URL(), "tcp://"))
	cfg := config.ScanConfig{ConnConfig: conn("tcp://" + px.addr()), Start: 1, End: 1}
	cfg.Timeout = 2000
	go func() {
		time.Sleep(20 * time.Millisecond)
		px.cut()
	}()
	// Either the cut came during the link start-up, or after it; both end with an error
	// of the transport, or the device was found before the cut.
	res, err := Scan(context.Background(), &cfg, nil)
	if err != nil && kindOf(t, err) != errs.KindTransport {
		t.Fatal(err)
	}
	if err == nil && len(res.Found) != 1 {
		t.Fatalf("%+v", res)
	}
}

func TestSharedLine(t *testing.T) {
	a, b := net.Pipe()
	l := newSharedLine(a)
	one := l.attach()
	go func() { _, _ = b.Write([]byte("hi")) }()
	buf := make([]byte, 2)
	if _, err := io.ReadFull(one, buf); err != nil || string(buf) != "hi" {
		t.Fatal(err, buf)
	}
	_ = one.Close()
	two := l.attach()
	go func() { _, _ = io.ReadFull(b, make([]byte, 3)) }()
	if _, err := two.Write([]byte("abc")); err != nil {
		t.Fatal(err)
	}
	_ = b.Close()
	if _, err := two.Read(buf); err == nil {
		t.Fatal("read after the line closed")
	}
	three := l.attach()
	if _, err := three.Read(buf); err == nil {
		t.Fatal("attach after the line closed")
	}
	_ = one.Close()
	l.close()
}
