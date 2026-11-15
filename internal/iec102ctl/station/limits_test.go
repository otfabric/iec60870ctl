// SPDX-License-Identifier: MIT

package station

import (
	"fmt"
	"net"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/otfabric/go-iec60870-5/iec102"
	"github.com/otfabric/go-iec60870-5/iec102/ree"
	"github.com/otfabric/go-iec60870-5/iec102/server"
)

// generated returns a model with one list per entry: n generated totals (addresses 0..n-1,
// modulo 256) for count periods, after listed totals (0 for none) in one listed period.
func generated(lists ...struct{ n, count, listed int }) string {
	var b strings.Builder
	b.WriteString("station: {product: {standard: 1, manufacturer: 1, product: 1}}\ntotals:\n")
	for i, l := range lists {
		fmt.Fprintf(&b, "  - record: %d\n    kind: absolute\n", i+1)
		if l.listed > 0 {
			b.WriteString("    periods: [{end: 2025-12-31T00:00, totals: [")
			for a := range l.listed {
				fmt.Fprintf(&b, "{address: %d, value: 1},", a)
			}
			b.WriteString("]}]\n")
		}
		b.WriteString("    generate: {start: 2026-01-01, interval: 1m, count: ")
		fmt.Fprintf(&b, "%d, totals: [", l.count)
		for a := range l.n {
			fmt.Fprintf(&b, "{address: %d, step: 1},", a%256)
		}
		b.WriteString("]}\n")
	}
	return b.String()
}

// validated parses and validates model and returns the octets that took.
func validated(t *testing.T, model string) (uint64, error) {
	t.Helper()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	m, err := Parse([]byte(model))
	if err == nil {
		err = m.Validate(time.UTC)
	}
	runtime.ReadMemStats(&after)
	return after.TotalAlloc - before.TotalAlloc, err
}

// The totals a model generates are checked before they are made: at most 256 per period,
// each address once, and at most MaxTotals in the whole model, listed ones included. A
// model beyond a limit is refused within a small allocation.
func TestGeneratedTotalsLimits(t *testing.T) {
	type list = struct{ n, count, listed int }
	const refusedBudget = 4 << 20
	for _, c := range []struct {
		name  string
		lists []list
		want  string // "" for a model that is accepted
	}{
		{"256 addresses", []list{{MaxPeriodTotals, 10, 0}}, ""},
		{"257 addresses", []list{{MaxPeriodTotals + 1, MaxPeriods, 0}}, "at most 256 addresses"},
		{"at the limit", []list{{250, MaxTotals / 250, 0}}, ""},
		{"one period over the limit", []list{{250, MaxTotals/250 + 1, 0}}, "totals in all"},
		{"256 addresses for every period", []list{{MaxPeriodTotals, MaxPeriods, 0}}, "totals in all"},
		{"listed totals count", []list{{250, MaxTotals / 250, 1}}, "totals in all"},
		{"over two lists", []list{{100, MaxTotals / 200, 0}, {100, MaxTotals/200 + 1, 0}}, "totals in all"},
	} {
		t.Run(c.name, func(t *testing.T) {
			alloc, err := validated(t, generated(c.lists...))
			switch {
			case c.want == "" && err != nil:
				t.Fatalf("refused: %v", err)
			case c.want != "" && (err == nil || !strings.Contains(err.Error(), c.want)):
				t.Fatalf("%v, want %q", err, c.want)
			case c.want != "" && len(c.lists) == 1 && alloc > refusedBudget:
				t.Errorf("refused after %d octets were allocated, want at most %d", alloc, refusedBudget)
			}
		})
	}
}

// A generator that names an address twice is refused before any period is made, also with
// many periods.
func TestGeneratedAddressTwice(t *testing.T) {
	model := "station: {product: {standard: 1, manufacturer: 1, product: 1}}\n" +
		"totals: [{record: 1, kind: absolute, generate: {start: 2026-01-01, interval: 1m, count: 3900, totals: [" +
		strings.Repeat("{address: 1, step: 1},", 255) + "]}}]"
	alloc, err := validated(t, model)
	if err == nil || !strings.Contains(err.Error(), "address 1 twice") {
		t.Fatalf("%v, want address 1 twice", err)
	}
	if alloc > 4<<20 {
		t.Errorf("refused after %d octets were allocated, want at most %d", alloc, 4<<20)
	}
}

// reeReads returns a model with powers contracted powers and days special days.
func reeReads(powers, days int) string {
	var p, d []string
	for i := range powers {
		p = append(p, fmt.Sprintf("{address: %d, power_kw: %d}", i+1, 10+i))
	}
	for i := range days {
		d = append(d, fmt.Sprintf("2026-%02d-%02d", 1+i/28, 1+i%28))
	}
	return "station: {product: {standard: 1, manufacturer: 1, product: 1}}\nree:\n  access_key: 7\n" +
		"  contracted_powers: [" + strings.Join(p, ", ") + "]\n  special_days: [" + strings.Join(d, ", ") + "]\n"
}

// The contracted powers and the special days are answered by one ASDU each: a model with
// more than one frame carries (48 powers, 49 days, with every size of the link address and
// of the common address) is refused by Validate, and so before a station is made.
func TestREEAnswerFitsOneFrame(t *testing.T) {
	for _, c := range []struct {
		powers, days int
		want         string
	}{
		{48, 49, ""},
		{49, 49, "ree.contracted_powers: 49, one answer carries at most 48"},
		{48, 50, "ree.special_days: 50, one answer carries at most 49"},
		{127, 127, "at most 48"},
		{0, 0, ""},
		{1, 1, ""},
	} {
		m, err := Parse([]byte(reeReads(c.powers, c.days)))
		if err != nil {
			t.Fatal(err)
		}
		if err := m.Validate(time.UTC); (err == nil) != (c.want == "") {
			t.Errorf("%d powers, %d days: Validate: %v", c.powers, c.days, err)
		}
		for _, la := range []int{1, 2} {
			for _, ca := range []int{1, 2} {
				_, err := New(m, Options{LinkAddrSize: la, CASize: ca})
				switch {
				case c.want == "" && err != nil:
					t.Errorf("%d powers, %d days, link address %d, common address %d: %v", c.powers, c.days, la, ca, err)
				case c.want != "" && (err == nil || !strings.Contains(err.Error(), c.want)):
					t.Errorf("%d powers, %d days, link address %d, common address %d: %v, want %q", c.powers, c.days, la, ca, err, c.want)
				}
			}
		}
	}
}

// The class 2 queue of a line holds the longest answer of the model on top of the
// library's default, also at the limits of a model: every period of a list read at once, a
// whole list of events, the tariff records of a register.
func TestQueueHoldsTheLongestAnswer(t *testing.T) {
	const deflt = 4096 // server.DefaultQueueSize
	parsed := func(model string) *Model {
		m, err := Parse([]byte(model))
		if err != nil {
			t.Fatal(err)
		}
		return m
	}
	type list = struct{ n, count, listed int }
	for _, c := range []struct {
		name  string
		model *Model
		least int
	}{
		{"one period", parsed(generated(list{1, 1, 0})), 1 + 2},
		{"periods at the limit", parsed(generated(list{1, MaxPeriods, 0})), MaxPeriods + 2},
		// 250 four-octet totals take more than one frame each: 6 octets a total, at most 40 a frame.
		{"totals at the limit", parsed(generated(list{250, MaxTotals / 250, 0})), 7*(MaxTotals/250) + 2},
		{"events at the limit", eventsModel(MaxEvents), MaxEvents/27 + 2},
		{"tariff records at the limit", tariffModel(MaxEvents), MaxEvents + 2},
	} {
		t.Run(c.name, func(t *testing.T) {
			st, err := New(c.model, Options{})
			if err != nil {
				t.Fatal(err)
			}
			if st.queue < deflt+c.least {
				t.Errorf("queue %d, want at least %d", st.queue, deflt+c.least)
			}
		})
	}
}

// tariffModel returns a model with n stored tariff records of register 134.
func tariffModel(n int) *Model {
	recs := make([]TariffRecordSpec, n)
	for i := range recs {
		recs[i] = TariffRecordSpec{Address: 1, MaxPowerTime: "2025-01-01T00:00", PeriodStart: "2025-01-01T00:00", PeriodEnd: "2025-02-01T00:00"}
	}
	return &Model{REE: &REESpec{AccessKey: 7, Tariffs: []TariffSpec{{Register: 134, Stored: recs}}}}
}

// eventsModel returns a model with n events of record 52.
func eventsModel(n int) *Model {
	pts := make([]PointSpec, n)
	for i := range pts {
		pts[i] = PointSpec{Address: 1, Time: "2026-01-01T00:00:00.000"}
	}
	return &Model{Events: []EventsSpec{{Record: 52, Points: pts}}}
}

// An answer that cannot be queued (here: the line has closed) is logged as a warning, and
// the request ends there, without its termination.
func TestAnswerNotQueuedIsLogged(t *testing.T) {
	m, err := Parse([]byte(reeReads(2, 2)))
	if err != nil {
		t.Fatal(err)
	}
	var ev events
	st, err := New(m, Options{Log: ev.log})
	if err != nil {
		t.Fatal(err)
	}
	a, b := net.Pipe()
	defer func() { _ = b.Close() }()
	srv, err := server.New(a, server.Config{LinkAddr: 1, CommonAddr: 1, Params: st.params()})
	if err != nil {
		t.Fatal(err)
	}
	_ = srv.Close()
	h := &handler{s: st, remote: "pipe"}
	for _, typ := range []iec102.TypeID{ree.C_PC_NA_2, ree.C_DF_NA_2, ree.C_TA_VC_2, ree.C_RM_NA_2} {
		h.HandleASDU(srv, iec102.New(iec102.CauseActivation, 1, 0, ree.Request{T: typ}))
	}
	ev.mu.Lock()
	defer ev.mu.Unlock()
	if len(ev.l) != 4 {
		t.Fatalf("events %+v, want four warnings", ev.l)
	}
	for _, e := range ev.l {
		if e.Event != "warning" || !strings.Contains(e.Detail, "answer not queued") || !strings.Contains(e.Detail, "without its termination") || e.Remote != "pipe" {
			t.Errorf("event %+v", e)
		}
	}
}

// The warnings and errors of the library's server are events of the log; its debug and
// informational entries are not.
func TestLibraryLog(t *testing.T) {
	var ev events
	st, err := New(&Model{}, Options{Log: ev.log})
	if err != nil {
		t.Fatal(err)
	}
	l := libraryLog{s: st, remote: "r"}
	l.Debugf("d %d", 1)
	l.Infof("i %d", 2)
	l.Warnf("w %d", 3)
	l.Errorf("e %d", 4)
	if got := ev.names(); got != "warning error" {
		t.Fatalf("events %q", got)
	}
	if ev.l[0].Detail != "w 3" || ev.l[1].Detail != "e 4" || ev.l[1].Remote != "r" {
		t.Errorf("events %+v", ev.l)
	}
}
