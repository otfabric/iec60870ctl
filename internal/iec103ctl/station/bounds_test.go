// SPDX-License-Identifier: MIT

package station

import (
	"context"
	"errors"
	"math"
	"net"
	"runtime"
	"testing"
	"time"

	"github.com/otfabric/go-iec60870-5/ft12"
	"github.com/otfabric/go-iec60870-5/iec103"
	"github.com/otfabric/go-iec60870-5/iec103/client"
	"github.com/otfabric/go-iec60870-5/link"
)

// The period of a simulated indication is 100 ms to one day; every period of an accepted
// model is positive.
func TestSimulatePeriodBounds(t *testing.T) {
	for _, c := range []struct {
		simulate int
		ok       bool
	}{
		{0, true}, {1, false}, {MinSimulate - 1, false}, {MinSimulate, true},
		{MaxSimulate - 1, true}, {MaxSimulate, true}, {MaxSimulate + 1, false},
		{-1, false}, {math.MaxInt32, false}, {math.MaxInt, false}, {math.MinInt, false},
	} {
		err := edit(t, func(m *Model) {
			m.Indications = []Indication{{Fun: 1, Inf: 1, DPI: 1, Simulate: c.simulate}}
		})
		if (err == nil) != c.ok {
			t.Errorf("simulate %d: %v, want accepted %t", c.simulate, err, c.ok)
		}
		if err != nil {
			continue
		}
		m, perr := Parse([]byte(minimal), true)
		if perr != nil {
			t.Fatal(perr)
		}
		m.Indications = []Indication{{Fun: 1, Inf: 1, DPI: 1, Simulate: c.simulate}}
		st, nerr := New(m, Options{LinkAddr: -1, CommonAddr: -1})
		if nerr != nil {
			t.Fatalf("simulate %d: %v", c.simulate, nerr)
		}
		if want := time.Duration(c.simulate) * time.Millisecond; st.periods[0] != want || st.periods[0] < 0 {
			t.Errorf("simulate %d: period %v, want %v", c.simulate, st.periods[0], want)
		}
	}
}

// connect serves st on one end of a pipe and returns a controlling system on the other,
// with its link up, and a function that ends the line and waits for it.
func connect(t *testing.T, st *Station) (*client.Client, func()) {
	t.Helper()
	a, b := net.Pipe()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); _ = st.Serve(ctx, b, "pipe") }()
	c, err := client.New(a, client.Config{LinkAddr: st.LinkAddr(), NoInitGI: true, NoInitTimeSync: true, PollInterval: 5 * time.Millisecond,
		Link: link.Config{Timeout: 500 * time.Millisecond}, RequestTimeout: 5 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	wctx, wcancel := context.WithTimeout(ctx, 5*time.Second)
	defer wcancel()
	if err := c.WaitUp(wctx); err != nil {
		t.Fatal(err)
	}
	var ended bool
	stop := func() {
		if ended {
			return
		}
		ended = true
		_ = c.Close()
		cancel()
		<-done
	}
	t.Cleanup(stop)
	return c, stop
}

func float32Set(group, entry uint8, v float32) iec103.DataSet {
	return iec103.DataSet{GIN: iec103.GIN{Group: group, Entry: entry}, KOD: iec103.KODActualValue, Value: iec103.Float32s(v)}
}

// A write that stages more data sets than one RII may keeps nothing: what the station holds
// stays within the bound, its heap does not grow with what was sent, and the write with
// execution is refused with reply code 7 (too many commands).
func TestStagedWritesAreBounded(t *testing.T) {
	st, err := New(Demo(), Options{LinkAddr: -1, CommonAddr: -1})
	if err != nil {
		t.Fatal(err)
	}
	c, _ := connect(t, st)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	initial, _ := st.Generic(iec103.GIN{Group: 1, Entry: 2})
	w, err := c.BeginWrite()
	if err != nil {
		t.Fatal(err)
	}
	sets := make([]iec103.DataSet, 30)
	for i := range sets {
		sets[i] = float32Set(1, 2, 1)
	}
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	const stages = 2000
	for i := range stages {
		if err := w.Stage(ctx, sets...); err != nil {
			t.Fatalf("stage %d: %v", i, err)
		}
		if n := st.stagedSets(); n > MaxStagedSets*MaxStagedWrites {
			t.Fatalf("after stage %d: %d data sets staged, want at most %d", i, n, MaxStagedSets*MaxStagedWrites)
		}
	}
	// The station handles the ASDUs of a line in order: once a later request is answered,
	// every write entry before it was handled.
	if _, err := c.ReadHeadings(ctx); err != nil {
		t.Fatal(err)
	}
	runtime.GC()
	runtime.ReadMemStats(&after)
	// 60 000 data sets were sent; the station keeps none of them.
	if n := st.stagedSets(); n != 0 {
		t.Errorf("%d data sets staged, want none: the write staged more than %d", n, MaxStagedSets)
	}
	if grown := int64(after.HeapAlloc) - int64(before.HeapAlloc); grown > 1<<20 {
		t.Errorf("heap grew by %d octets over %d staged data sets", grown, stages*len(sets))
	}
	res, err := w.Execute(ctx, float32Set(1, 2, 2))
	if !errors.Is(err, iec103.ErrNegative) || res == nil || len(res.Sets) == 0 || res.Sets[0].Value.Data[0] != byte(iec103.GRCTooManyCommands) {
		t.Fatalf("execute after too many staged data sets: %+v, %v", res, err)
	}
	if v, _ := st.Generic(iec103.GIN{Group: 1, Entry: 2}); mustFloat(t, v) != mustFloat(t, initial) {
		t.Errorf("entry 1.2 changed to %v, want %v", mustFloat(t, v), mustFloat(t, initial))
	}
	if n := st.stagedSets(); n != 0 {
		t.Errorf("%d data sets staged after the execution", n)
	}

	// Within the bound a write is carried out, also by more than one ASDU.
	w, err = c.BeginWrite()
	if err != nil {
		t.Fatal(err)
	}
	half := make([]iec103.DataSet, MaxStagedSets/2)
	for i := range half {
		half[i] = float32Set(1, 2, 1)
	}
	if err := w.Stage(ctx, half...); err != nil {
		t.Fatal(err)
	}
	if err := w.Stage(ctx, half[1:]...); err != nil {
		t.Fatal(err)
	}
	if _, err := w.Execute(ctx, float32Set(1, 2, 3)); err != nil {
		t.Fatalf("execute of %d data sets: %v", MaxStagedSets, err)
	}
	if v, _ := st.Generic(iec103.GIN{Group: 1, Entry: 2}); mustFloat(t, v) != 3 {
		t.Errorf("entry 1.2 is %v, want 3", mustFloat(t, v))
	}
}

func mustFloat(t *testing.T, v iec103.Value) float64 {
	t.Helper()
	f, err := v.Float(0)
	if err != nil {
		t.Fatal(err)
	}
	return f
}

// What a line staged belongs to it: after the line ends, a write with execution under the
// same RII on the next line changes only its own entry.
func TestStagedWritesEndWithTheLine(t *testing.T) {
	st, err := New(Demo(), Options{LinkAddr: -1, CommonAddr: -1})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	before, _ := st.Generic(iec103.GIN{Group: 1, Entry: 2})
	first, stop := connect(t, st)
	w, err := first.BeginWrite()
	if err != nil {
		t.Fatal(err)
	}
	if err := w.Stage(ctx, float32Set(1, 2, 9.75)); err != nil {
		t.Fatal(err)
	}
	if _, err := first.ReadHeadings(ctx); err != nil { // the write entry was handled
		t.Fatal(err)
	}
	if n := st.stagedSets(); n != 1 {
		t.Fatalf("%d data sets staged, want 1", n)
	}
	stop()
	if n := st.stagedSets(); n != 0 {
		t.Errorf("%d data sets staged after the line ended", n)
	}

	second, _ := connect(t, st)
	w2, err := second.BeginWrite()
	if err != nil {
		t.Fatal(err)
	}
	if w2.RII() != w.RII() {
		t.Fatalf("RII %d, want the RII of the first line %d", w2.RII(), w.RII())
	}
	if _, err := w2.Execute(ctx, iec103.DataSet{GIN: iec103.GIN{Group: 1, Entry: 3}, KOD: 1, Value: iec103.Uints(iec103.DataUint, 2, 450)}); err != nil {
		t.Fatal(err)
	}
	if v, _ := st.Generic(iec103.GIN{Group: 1, Entry: 3}); v.Data[0] != 0xC2 {
		t.Errorf("entry 1.3 not written: %x", v.Data)
	}
	if v, _ := st.Generic(iec103.GIN{Group: 1, Entry: 2}); mustFloat(t, v) != mustFloat(t, before) {
		t.Errorf("entry 1.2 is %v, want %v as before", mustFloat(t, v), mustFloat(t, before))
	}
}

// A reset CU or reset FCB of the line drops what it staged; a frame to another link
// address, a secondary frame and another function do not.
func TestStagedWritesEndWithAReset(t *testing.T) {
	st, err := New(Demo(), Options{LinkAddr: -1, CommonAddr: -1})
	if err != nil {
		t.Fatal(err)
	}
	c := st.open()
	defer st.end(c)
	cfg := st.config(c, "test")
	la := uint16(st.LinkAddr())
	for _, f := range []struct {
		name  string
		tx    bool
		frame ft12.Frame
		clear bool
	}{
		{"reset CU", false, ft12.Frame{Kind: ft12.KindFixed, Control: 0x40 | link.FuncResetLink, Addr: la}, true},
		{"reset FCB", false, ft12.Frame{Kind: ft12.KindFixed, Control: 0x40 | link.FuncResetFCB, Addr: la}, true},
		{"reset CU to another address", false, ft12.Frame{Kind: ft12.KindFixed, Control: 0x40 | link.FuncResetLink, Addr: la + 1}, false},
		{"request status", false, ft12.Frame{Kind: ft12.KindFixed, Control: 0x49, Addr: la}, false},
		{"a frame sent", true, ft12.Frame{Kind: ft12.KindFixed, Control: 0x40 | link.FuncResetLink, Addr: la}, false},
		{"secondary", false, ft12.Frame{Kind: ft12.KindFixed, Control: 0x00, Addr: la}, false},
	} {
		st.mu.Lock()
		c.stage(1, []iec103.DataSet{float32Set(1, 2, 1)})
		st.mu.Unlock()
		cfg.Link.OnFrame(f.tx, f.frame)
		n := st.stagedSets()
		if f.clear && n != 0 || !f.clear && n == 0 {
			t.Errorf("%s: %d data sets staged after it", f.name, n)
		}
		c.clear()
	}
}
