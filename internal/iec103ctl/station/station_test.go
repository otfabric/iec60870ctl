// SPDX-License-Identifier: MIT

package station

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/otfabric/go-iec60870-5/iec103"
	"github.com/otfabric/go-iec60870-5/iec103/client"
	"github.com/otfabric/go-iec60870-5/link"
	serial "github.com/otfabric/go-serial"
	"github.com/otfabric/iec60870ctl/internal/errs"
	"github.com/otfabric/iec60870ctl/internal/iec103ctl/config"
	"github.com/otfabric/iec60870ctl/internal/iec103ctl/types"
	"github.com/otfabric/iec60870ctl/internal/line"
)

// events collects the events of a station.
type events struct {
	mu sync.Mutex
	es []types.ServerEvent
}

func (e *events) log(ev types.ServerEvent) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.es = append(e.es, ev)
}

func (e *events) has(kind, detail string) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, x := range e.es {
		if x.Event == kind && strings.Contains(x.Detail, detail) {
			return true
		}
	}
	return false
}

// pair serves the demo device on one end of a pipe and returns a controlling system on the
// other, with its link up.
func pair(t *testing.T, edit func(*Model), handler func(*iec103.ASDU)) (*Station, *client.Client, *events) {
	t.Helper()
	m := Demo()
	if edit != nil {
		edit(m)
	}
	ev := &events{}
	st, err := New(m, Options{LinkAddr: -1, CommonAddr: -1, Log: ev.log, FeedEvery: time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	a, b := net.Pipe()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); _ = st.Serve(ctx, b, "pipe") }()
	c, err := client.New(a, client.Config{LinkAddr: 1, NoInitGI: true, NoInitTimeSync: true, PollInterval: 5 * time.Millisecond,
		Link: link.Config{Timeout: 500 * time.Millisecond}, RequestTimeout: 5 * time.Second, Handler: handler})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = c.Close()
		cancel()
		<-done
	})
	wctx, wcancel := context.WithTimeout(ctx, 5*time.Second)
	defer wcancel()
	if err := c.WaitUp(wctx); err != nil {
		t.Fatal(err)
	}
	return st, c, ev
}

func TestDeviceAnswers(t *testing.T) {
	st, c, ev := pair(t, nil, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	gi, err := c.Interrogate(ctx)
	if err != nil || len(gi) != 6 {
		t.Fatalf("%v %d", err, len(gi))
	}
	if _, err := c.ClockSync(ctx, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := c.Command(ctx, 160, 17, iec103.DPIOn); err != nil {
		t.Fatal(err)
	}
	if st.DPI(160, 17) != iec103.DPIOn {
		t.Fatal("command not applied")
	}
	if err := c.Command(ctx, 160, 18, iec103.DPIOn); !errors.Is(err, iec103.ErrNegative) {
		t.Fatal(err)
	}
	h, err := c.ReadHeadings(ctx)
	if err != nil || len(h.Sets) != 2 || h.Sets[1].Value.Text() != "COUNTERS" {
		t.Fatalf("%v %+v", err, h)
	}
	g, err := c.ReadGroup(ctx, 2, iec103.KODActualValue)
	if err != nil || len(g.Sets) != 1 {
		t.Fatalf("%v %+v", err, g)
	}
	if r, err := c.GenericRead(ctx, 242); err == nil {
		t.Fatalf("INF 242 answered: %+v", r)
	}
	sets, err := c.GenericInterrogation(ctx)
	if err != nil || len(sets) != 5 {
		t.Fatalf("%v %d", err, len(sets))
	}
	// A write with confirmation is echoed and only staged; the execution applies it.
	w, err := c.BeginWrite()
	if err != nil {
		t.Fatal(err)
	}
	if err := w.Stage(ctx, iec103.DataSet{GIN: iec103.GIN{Group: 1, Entry: 3}, KOD: 1, Value: iec103.Uints(iec103.DataUint, 2, 450)}); err != nil {
		t.Fatal(err)
	}
	if _, err := w.Execute(ctx, iec103.DataSet{GIN: iec103.GIN{Group: 1, Entry: 4}, KOD: 1, Value: iec103.ASCII("FEEDER 2")}); err != nil {
		t.Fatal(err)
	}
	if v, _ := st.Generic(iec103.GIN{Group: 1, Entry: 3}); v.Data[0] != 0xC2 {
		t.Fatalf("staged set not applied: %x", v.Data)
	}
	res, err := c.GenericWrite(ctx, iec103.InfWriteConfirm, iec103.DataSet{GIN: iec103.GIN{Group: 1, Entry: 2}, KOD: 1, Value: iec103.Float32s(9)})
	if err != nil || res.Cause != iec103.CauseGenericWriteConf {
		t.Fatalf("%v %+v", err, res)
	}
	// Refusals: unknown GIN, read only, another type.
	for _, d := range []iec103.DataSet{
		{GIN: iec103.GIN{Group: 9, Entry: 9}, KOD: 1, Value: iec103.Uints(iec103.DataUint, 2, 1)},
		{GIN: iec103.GIN{Group: 1, Entry: 1}, KOD: 1, Value: iec103.Uints(iec103.DataUint, 2, 1)},
		{GIN: iec103.GIN{Group: 1, Entry: 2}, KOD: 1, Value: iec103.Uints(iec103.DataUint, 2, 1)},
	} {
		if _, err := c.GenericWrite(ctx, iec103.InfWriteExecute, d); !errors.Is(err, iec103.ErrNegative) {
			t.Fatalf("%+v: %v", d, err)
		}
	}
	if _, err := c.GenericWrite(ctx, iec103.InfWriteEntry, iec103.DataSet{GIN: iec103.GIN{Group: 1, Entry: 2}, KOD: 1, Value: iec103.Float32s(1)}); err != nil {
		t.Fatal(err)
	}
	faults, err := c.DisturbanceList(ctx, 160)
	if err != nil || len(faults) != 1 {
		t.Fatalf("%v %+v", err, faults)
	}
	rec, err := c.Disturbance(ctx, 160, 1)
	if err != nil || len(rec.Channels) != 2 || len(rec.Channels[0].Values) != 30 {
		t.Fatalf("%v %+v", err, rec)
	}
	deadline := time.Now().Add(2 * time.Second)
	for !ev.has("disturbance-transmitted", "positive=true") && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	for _, want := range []struct{ kind, detail string }{
		{"interrogation", "scn="}, {"clock-sync", "broadcast=false"}, {"command", "outcome=executed"}, {"command", "outcome=rejected"},
		{"generic-read", "inf=240"}, {"generic-interrogation", ""}, {"generic-write", "outcome=executed"}, {"generic-write", "outcome=rejected"},
		{"disturbance-transmitted", "positive=true"}, {"link", "up"},
	} {
		if !ev.has(want.kind, want.detail) {
			t.Errorf("no %s event with %q", want.kind, want.detail)
		}
	}
}

func TestDeviceSimulatesAndMeasures(t *testing.T) {
	var mu sync.Mutex
	causes := map[iec103.Cause]int{}
	_, _, ev := pair(t, func(m *Model) { m.Indications[4].Simulate = 100 }, func(a *iec103.ASDU) {
		mu.Lock()
		causes[a.Cause]++
		mu.Unlock()
	})
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		ok := causes[iec103.CauseSpontaneous] > 0 && causes[iec103.CauseCyclic] > 0
		mu.Unlock()
		if ok {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	mu.Lock()
	defer mu.Unlock()
	if causes[iec103.CauseSpontaneous] == 0 || causes[iec103.CauseCyclic] == 0 {
		t.Fatalf("%v", causes)
	}
	if !ev.has("spontaneous", "") {
		t.Fatal("no spontaneous event")
	}
}

func TestDirectoryAndUnknownEntries(t *testing.T) {
	_, c, _ := pair(t, nil, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	d, err := c.ReadDirectory(ctx, iec103.GIN{Group: 1, Entry: 2})
	if err != nil || len(d.Elements) != 2 {
		t.Fatalf("%v %+v", err, d)
	}
	if d, err = c.ReadDirectory(ctx, iec103.GIN{Group: 7, Entry: 7}); err != nil || d.Cause != iec103.CauseGenericReadInvalid {
		t.Fatalf("%v %+v", err, d)
	}
	if e, err := c.ReadEntry(ctx, iec103.GIN{Group: 1, Entry: 1}, iec103.KODRange); err != nil || e.Cause != iec103.CauseGenericReadInvalid {
		t.Fatalf("%v %+v", err, e)
	}
	if g, err := c.ReadGroup(ctx, 1, iec103.KODRange); err != nil || g.Cause != iec103.CauseGenericReadInvalid {
		t.Fatalf("%v %+v", err, g)
	}
	if g, err := c.ReadGroup(ctx, 9, iec103.KODActualValue); err != nil || g.Cause != iec103.CauseGenericReadInvalid {
		t.Fatalf("%v %+v", err, g)
	}
}

func TestNewRefusesBadAddresses(t *testing.T) {
	if _, err := New(Demo(), Options{LinkAddr: 255, CommonAddr: -1}); err == nil {
		t.Fatal("link address 255 accepted")
	}
	m := Demo()
	m.Station.Identification.Fun = 0
	if _, err := New(m, Options{LinkAddr: -1, CommonAddr: -1}); err == nil {
		t.Fatal("invalid model accepted")
	}
	st, err := New(Demo(), Options{LinkAddr: 7, CommonAddr: 9})
	if err != nil || st.LinkAddr() != 7 || st.CommonAddr() != 9 {
		t.Fatal(err)
	}
	st.event(types.ServerEvent{Event: "x"}) // no log: dropped
	if st, _ = New(Demo(), Options{LinkAddr: 5, CommonAddr: -1}); st.CommonAddr() != 5 {
		t.Fatalf("common address %d does not follow the link address", st.CommonAddr())
	}
	m = Demo()
	m.Station.CommonAddress = 3
	if st, _ = New(m, Options{LinkAddr: 5, CommonAddr: -1}); st.CommonAddr() != 3 {
		t.Fatalf("common address %d", st.CommonAddr())
	}
}

// syncBuf is an output buffer safe for the goroutines of the server.
type syncBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuf) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuf) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

func TestRunTCP(t *testing.T) {
	ev := &events{}
	cfg := &config.ServerConfig{Listen: "127.0.0.1", Port: freePort(t), Duration: 0, Debug: true, LinkAddress: "1"}
	ctx, cancel := context.WithCancel(context.Background())
	addr := make(chan string, 1)
	done := make(chan error, 1)
	var trace syncBuf
	go func() { done <- Run(ctx, cfg, ev.log, &trace, func(a string) { addr <- a }) }()
	a := <-addr
	c1, err := net.Dial("tcp", a)
	if err != nil {
		t.Fatal(err)
	}
	cl, err := client.New(c1, client.Config{LinkAddr: 1, NoInitGI: true, NoInitTimeSync: true})
	if err != nil {
		t.Fatal(err)
	}
	wctx, wcancel := context.WithTimeout(ctx, 5*time.Second)
	if err := cl.WaitUp(wctx); err != nil {
		t.Fatal(err)
	}
	wcancel()
	// A second line while the first is served: closed at once.
	c2, err := net.Dial("tcp", a)
	if err != nil {
		t.Fatal(err)
	}
	_ = c2.SetReadDeadline(time.Now().Add(5 * time.Second))
	if _, err := c2.Read(make([]byte, 1)); err == nil {
		t.Fatal("second line served")
	}
	_ = c2.Close()
	_ = cl.Close()
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"listening", "connected", "busy", "disconnected", "stopping"} {
		if !ev.has(k, "") {
			t.Errorf("no %s event", k)
		}
	}
	if !strings.Contains(trace.String(), "rx") {
		t.Fatal("no frame trace")
	}
}

func freePort(t *testing.T) uint16 {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ln.Close() }()
	return uint16(ln.Addr().(*net.TCPAddr).Port)
}

func TestRunDurationAndErrors(t *testing.T) {
	cfg := &config.ServerConfig{Listen: "127.0.0.1", Port: freePort(t), Duration: 1}
	if err := Run(context.Background(), cfg, nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	// The port in use.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ln.Close() }()
	cfg.Port = uint16(ln.Addr().(*net.TCPAddr).Port)
	if e, ok := errs.As(Run(context.Background(), cfg, nil, nil, nil)); !ok || e.Code != errs.CodeListenFailed {
		t.Fatal(e)
	}
	dir := t.TempDir()
	badModel := filepath.Join(dir, "bad.yaml")
	_ = os.WriteFile(badModel, []byte("station: {commonAddress: 999}\n"), 0o600)
	for _, bad := range []config.ServerConfig{
		{Serial: "/dev/x", Listen: "127.0.0.1"},
		{SerialConfig: config.SerialConfig{Parity: "mark"}},
		{TimeZone: "Mars/Base"},
		{Config: badModel},
		{LinkAddress: "300"},
		{CommonAddress: "x"},
	} {
		if err := Check(&bad); err == nil {
			t.Errorf("%+v accepted", bad)
		}
		if e, ok := errs.As(Run(context.Background(), &bad, nil, nil, nil)); !ok || e.Kind != errs.KindInvalidInput {
			t.Errorf("%+v: %v", bad, e)
		}
	}
}

func TestRunSerial(t *testing.T) {
	old := line.OpenSerial
	t.Cleanup(func() { line.OpenSerial = old })
	a, b := net.Pipe()
	var opened *serial.Config
	line.OpenSerial = func(c *serial.Config) (io.ReadWriteCloser, error) { opened = c; return b, nil }
	ev := &events{}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	ready := make(chan string, 1)
	go func() {
		done <- Run(ctx, &config.ServerConfig{Serial: "/dev/ttyTEST", SerialConfig: config.SerialConfig{Baud: 19200}}, ev.log, nil, func(s string) { ready <- s })
	}()
	if r := <-ready; r != "serial:///dev/ttyTEST" {
		t.Fatal(r)
	}
	cl, err := client.New(a, client.Config{LinkAddr: 1, NoInitGI: true, NoInitTimeSync: true})
	if err != nil {
		t.Fatal(err)
	}
	wctx, wcancel := context.WithTimeout(ctx, 5*time.Second)
	defer wcancel()
	if err := cl.WaitUp(wctx); err != nil {
		t.Fatal(err)
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	_ = cl.Close()
	if opened == nil || opened.BaudRate != 19200 {
		t.Fatalf("%+v", opened)
	}
	// A port that fails, and one that cannot be opened.
	a, b = net.Pipe()
	line.OpenSerial = func(*serial.Config) (io.ReadWriteCloser, error) { return b, nil }
	go func() { time.Sleep(50 * time.Millisecond); _ = a.Close() }()
	if e, ok := errs.As(Run(context.Background(), &config.ServerConfig{Serial: "/dev/ttyTEST"}, nil, nil, nil)); !ok || e.Code != errs.CodeConnectionLost {
		t.Fatal(e)
	}
	line.OpenSerial = func(*serial.Config) (io.ReadWriteCloser, error) { return nil, errors.New("busy") }
	if e, ok := errs.As(Run(context.Background(), &config.ServerConfig{Serial: "/dev/ttyTEST"}, nil, nil, nil)); !ok || e.Code != errs.CodeTransportConnectFailed {
		t.Fatal(e)
	}
}
