// SPDX-License-Identifier: MIT

package station_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	iec104 "github.com/otfabric/go-iec104"
	"github.com/otfabric/go-iec104/asdu"
	"github.com/otfabric/go-iec104/client"
	"github.com/otfabric/iec104ctl/internal/stationtest"
)

// lively is a model whose values change by themselves, quickly.
const lively = `
station: {common_address: 7, cyclic_interval: 100ms}
points:
  - {ioa: 1, type: M_SP_NA_1, value: false, simulate: {mode: toggle, interval: 60ms}}
  - {ioa: 2, type: M_ME_NC_1, value: 0, simulate: {mode: ramp, interval: 60ms, min: 0, max: 100, step: 1}}
  - {ioa: 3, type: M_ME_NB_1, value: 5, time_tag: false}
  - {ioa: 4, type: M_DP_NA_1, value: off}
commands:
  - {ioa: 10, type: C_DC_NA_1, target: 4, select_required: true, select_timeout: 150ms}
  - {ioa: 11, type: C_SC_NA_1}
`

// recorder collects what a station sends.
type recorder struct {
	mu   sync.Mutex
	seen []*asdu.ASDU
}

func (r *recorder) HandleASDU(a *asdu.ASDU) {
	r.mu.Lock()
	r.seen = append(r.seen, a)
	r.mu.Unlock()
}

func (r *recorder) count(match func(*asdu.ASDU) bool) (n int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, a := range r.seen {
		if match(a) {
			n++
		}
	}
	return n
}

func dial(t *testing.T, st *stationtest.Station, opts ...client.Option) *client.Client {
	t.Helper()
	opts = append([]client.Option{client.WithParams(stationtest.Params()), client.WithRequestTimeout(3 * time.Second)}, opts...)
	c, err := client.Dial(context.Background(), st.Addr(), opts...)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c
}

func eventually(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestSimulationAndEvents(t *testing.T) {
	st := stationtest.Start(t, lively)

	// Changes of state that happen while nobody is connected are kept and delivered; the
	// measured values of that time are not.
	time.Sleep(300 * time.Millisecond)
	rec := &recorder{}
	c := dial(t, st, client.WithHandler(rec))
	isEvent := func(typ asdu.TypeID) func(*asdu.ASDU) bool {
		return func(a *asdu.ASDU) bool { return a.Type == typ && a.Cause == asdu.CauseSpontaneous }
	}
	eventually(t, "the queued state changes", func() bool { return rec.count(isEvent(asdu.M_SP_TB_1)) >= 3 })
	if n := rec.count(isEvent(asdu.M_ME_TF_1)); n > 2 {
		t.Errorf("%d measured values from before the connection were delivered: they are not queued", n)
	}
	// From now on both arrive as they change, with the time tag of the station.
	eventually(t, "spontaneous measured values", func() bool { return rec.count(isEvent(asdu.M_ME_TF_1)) >= 3 })
	eventually(t, "cyclic measured values", func() bool {
		return rec.count(func(a *asdu.ASDU) bool { return a.Cause == asdu.CausePeriodic && a.Type == asdu.M_ME_NB_1 }) >= 2
	})
	rec.mu.Lock()
	for _, a := range rec.seen {
		if a.CommonAddr != 7 {
			t.Errorf("an ASDU for station %d from station 7", a.CommonAddr)
		}
		if sp, ok := a.First().(asdu.SinglePoint); ok && a.Cause == asdu.CauseSpontaneous {
			if d := time.Since(sp.Time.Time); d < 0 || d > time.Minute {
				t.Errorf("time tag of an event is %s old", d)
			}
		}
	}
	rec.mu.Unlock()

	// A point without time tag is reported without one.
	st.Set(3, 6)
	eventually(t, "the untagged event", func() bool {
		return rec.count(func(a *asdu.ASDU) bool { return a.Type == asdu.M_ME_NB_1 && a.Cause == asdu.CauseSpontaneous }) == 1
	})

	// A clock synchronization moves the time tags.
	future := time.Now().Add(48 * time.Hour)
	if err := c.ClockSync(context.Background(), 7, future); err != nil {
		t.Fatal(err)
	}
	eventually(t, "an event with the new time", func() bool {
		return rec.count(func(a *asdu.ASDU) bool {
			sp, ok := a.First().(asdu.SinglePoint)
			return ok && a.Cause == asdu.CauseSpontaneous && sp.Time.After(time.Now().Add(47*time.Hour))
		}) > 0
	})
}

func negative(t *testing.T, err error, cause asdu.Cause) {
	t.Helper()
	var neg *iec104.NegativeError
	if !errors.As(err, &neg) || neg.Cause() != cause {
		t.Errorf("error %v, want a refusal with cause %s", err, cause)
	}
}

func TestSelectBeforeOperate(t *testing.T) {
	st := stationtest.Start(t, lively)
	ctx := context.Background()
	rec := &recorder{}
	a, b := dial(t, st, client.WithHandler(rec)), dial(t, st)
	off := asdu.DoubleCommand{IOA: 10, Value: asdu.DoubleOff}
	on := asdu.DoubleCommand{IOA: 10, Value: asdu.DoubleOn}
	sel := func(c asdu.DoubleCommand) asdu.DoubleCommand { c.Select = true; return c }
	state := func() any { v, _ := st.Value(4); return v }

	negative(t, a.Command(ctx, 7, on), asdu.CauseActivationCon) // execute without select
	negative(t, a.Deactivate(ctx, 7, sel(on)), asdu.CauseDeactivationCon)

	// Selected by one controlling station: the other can neither select nor execute.
	if err := a.Command(ctx, 7, sel(on)); err != nil {
		t.Fatal(err)
	}
	negative(t, b.Command(ctx, 7, sel(on)), asdu.CauseActivationCon)
	negative(t, b.Command(ctx, 7, on), asdu.CauseActivationCon)
	// That execute did not match the selection, which is gone with it.
	negative(t, a.Command(ctx, 7, on), asdu.CauseActivationCon)

	// An execute for another value than the selected one is refused.
	if err := a.Command(ctx, 7, sel(on)); err != nil {
		t.Fatal(err)
	}
	negative(t, a.Command(ctx, 7, off), asdu.CauseActivationCon)
	if state() != "off" {
		t.Fatalf("point 4 is %v after refused commands", state())
	}

	// Select, cancel: nothing to execute any more.
	if err := a.Command(ctx, 7, sel(on)); err != nil {
		t.Fatal(err)
	}
	if err := a.Deactivate(ctx, 7, sel(on)); err != nil {
		t.Fatal(err)
	}
	negative(t, a.Command(ctx, 7, on), asdu.CauseActivationCon)

	// A selection times out.
	if err := a.Command(ctx, 7, sel(on)); err != nil {
		t.Fatal(err)
	}
	time.Sleep(250 * time.Millisecond)
	negative(t, a.Command(ctx, 7, on), asdu.CauseActivationCon)

	// Select and execute: the point follows, and the change is reported as the return
	// information of a remote command, to the station that has data transfer started last.
	if err := b.Command(ctx, 7, sel(on)); err != nil {
		t.Fatal(err)
	}
	if err := b.Command(ctx, 7, on); err != nil {
		t.Fatal(err)
	}
	if state() != "on" {
		t.Errorf("point 4 is %v after select and execute", state())
	}
	// A selection ends with its connection.
	if err := b.Command(ctx, 7, sel(off)); err != nil {
		t.Fatal(err)
	}
	_ = b.Close()
	eventually(t, "the selection to end with its connection", func() bool {
		return a.Command(ctx, 7, sel(off)) == nil
	})

	// What the station does not have, or cannot do.
	negative(t, a.Command(ctx, 7, asdu.SingleCommand{IOA: 99, Value: true}), asdu.CauseUnknownIOA)
	negative(t, a.Command(ctx, 7, asdu.DoubleCommand{IOA: 10, Value: asdu.DoubleIndeterminate}), asdu.CauseActivationCon)
	negative(t, a.Command(ctx, 7, asdu.SingleCommand{IOA: 10, Value: true}), asdu.CauseActivationCon)
	_, err := a.Interrogate(ctx, 7, asdu.QOIGroup(1))
	if err != nil {
		t.Errorf("interrogation of an empty group: %v", err)
	}
	_, err = a.CounterInterrogate(ctx, 7, 9, asdu.FreezeRead)
	negative(t, err, asdu.CauseActivationCon)
	negative(t, a.ResetProcess(ctx, 7, 9), asdu.CauseActivationCon)
	if _, err := a.Read(ctx, 7, 99); err == nil {
		t.Error("a read of an unknown address was answered")
	}
	if _, err := a.ListFiles(ctx, 7, 0); err == nil {
		t.Error("a station without files answered a directory call")
	}
	if err := a.PutFile(ctx, 7, 500, 1, []byte("x")); err == nil {
		t.Error("a station that takes no files took one")
	}
}

func TestBroadcastRequestsAreAnsweredAsTheStation(t *testing.T) {
	st := stationtest.Start(t, stationtest.Model)
	c := dial(t, st)
	ctx := context.Background()
	data, err := c.Interrogate(ctx, asdu.Broadcast, asdu.QOIStation)
	if err != nil || len(data) == 0 {
		t.Fatalf("broadcast interrogation: %d ASDUs, %v", len(data), err)
	}
	for _, a := range data {
		if a.CommonAddr != 1 {
			t.Errorf("answer to a broadcast with common address %d, want the station's own", a.CommonAddr)
		}
	}
	if err := c.ClockSync(ctx, asdu.Broadcast, time.Now()); err != nil {
		t.Errorf("broadcast clock synchronization: %v", err)
	}
	if err := c.ResetProcess(ctx, asdu.Broadcast, asdu.ResetProcessGeneral); err != nil {
		t.Errorf("broadcast reset: %v", err)
	}
	if _, err := c.CounterInterrogate(ctx, asdu.Broadcast, asdu.CounterGeneral, asdu.FreezeWithoutReset); err != nil {
		t.Errorf("broadcast counter interrogation: %v", err)
	}
	if n := st.Sessions(); n != 1 {
		t.Errorf("%d sessions", n)
	}
	// The event log tells a connection from a STOPDT.
	if err := c.StopDT(ctx); err != nil {
		t.Fatal(err)
	}
	eventually(t, "the stop to be logged", func() bool { return st.HasEvent("stopped") })
	var kinds []string
	for _, e := range st.Events() {
		if e.Event == "connected" || e.Event == "started" || e.Event == "stopped" {
			kinds = append(kinds, e.Event)
		}
	}
	if got := strings.Join(kinds, " "); got != "connected started stopped" {
		t.Errorf("session events: %s", got)
	}
}
