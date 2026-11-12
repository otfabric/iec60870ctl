// SPDX-License-Identifier: MIT

package station

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/otfabric/go-iec60870-5/asdu"
	"github.com/otfabric/go-iec60870-5/iec101"
	"github.com/otfabric/go-iec60870-5/iec101/client"
	"github.com/otfabric/go-iec60870-5/telecontrol"
	"github.com/otfabric/iec60870ctl/internal/iec101ctl/config"
	"github.com/otfabric/iec60870ctl/internal/iec101ctl/iec"
	"github.com/otfabric/iec60870ctl/internal/iec101ctl/types"
)

const testModel = `
station: {name: T, common_address: 5, counter_freeze: false}
points:
  - {ioa: 100, type: M_SP_NA_1, value: true}
  - {ioa: 101, type: M_SP_NA_1, value: false, time_tag: false}
  - {ioa: 200, type: M_DP_NA_1, value: 2}
  - {ioa: 300, type: M_ST_NA_1, value: 63}
  - {ioa: 301, type: M_ST_NA_1, value: -64}
  - {ioa: 400, type: M_BO_NA_1, value: "0x10"}
  - {ioa: 500, type: M_ME_NA_1, value: 0.5, quality: [OV]}
  - {ioa: 501, type: M_ME_NB_1, value: -5}
  - {ioa: 502, type: M_ME_NC_1, value: 1.5}
  - {ioa: 600, type: M_IT_NA_1, value: 9, sequence: 3, quality: [IV]}
commands:
  - {ioa: 1000, type: C_SC_NA_1, target: 100, report_cause: 3}
  - {ioa: 1001, type: C_RC_NA_1, target: 300}
  - {ioa: 1002, type: C_RC_NA_1, target: 301, select_timeout: 1ms}
  - {ioa: 1003, type: C_BO_NA_1, target: 400}
  - {ioa: 1004, type: C_SE_NA_1, target: 500}
`

// rig is a station served on one end of an in-memory line and a go-iec60870-5 client on
// the other.
type rig struct {
	st  *Station
	c   *client.Client
	got chan *asdu.ASDU
	mu  sync.Mutex
	ev  []types.ServerEvent
}

func (r *rig) events() []types.ServerEvent {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]types.ServerEvent(nil), r.ev...)
}

func linkOf(t testing.TB, mode string) iec.Link {
	t.Helper()
	lc := config.DefaultLink()
	lc.Mode = mode
	lc.LinkTimeout = 100
	l, err := iec.LinkParams(lc)
	if err != nil {
		t.Fatal(err)
	}
	return l
}

func newRig(t *testing.T, model, mode string, edit ...func(*Options)) *rig {
	t.Helper()
	m, err := Parse([]byte(model))
	if err != nil {
		t.Fatal(err)
	}
	r := &rig{got: make(chan *asdu.ASDU, 256)}
	opts := Options{Link: linkOf(t, mode), RetryInterval: 20 * time.Millisecond, Log: func(e types.ServerEvent) {
		r.mu.Lock()
		r.ev = append(r.ev, e)
		r.mu.Unlock()
	}}
	for _, f := range edit {
		f(&opts)
	}
	r.st, err = New(m, opts)
	if err != nil {
		t.Fatal(err)
	}
	a, b := net.Pipe()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- r.st.ServeLine(ctx, b, "pipe") }()
	l := opts.Link
	r.c, err = client.New(a, client.Config{Params: l.Params, LinkAddr: l.Addr, OwnAddr: l.Remote, PollInterval: 5 * time.Millisecond,
		RetryInterval: 20 * time.Millisecond, Handler: func(x *asdu.ASDU) { r.got <- x }})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = r.c.Close()
		cancel()
		if err := <-done; err != nil {
			t.Logf("line: %v", err)
		}
	})
	wctx, wcancel := context.WithTimeout(ctx, 5*time.Second)
	defer wcancel()
	if err := r.c.WaitUp(wctx); err != nil {
		t.Fatal(err)
	}
	return r
}

func (r *rig) expect(t *testing.T, typ asdu.TypeID, cause asdu.Cause) *asdu.ASDU {
	t.Helper()
	timeout := time.After(5 * time.Second)
	for {
		select {
		case a := <-r.got:
			if a.Type == typ && a.Cause == cause {
				return a
			}
		case <-timeout:
			t.Fatalf("no %s/%s", typ, cause)
		}
	}
}

func negative(t *testing.T, err error, cause asdu.Cause) {
	t.Helper()
	var neg *telecontrol.NegativeError
	if !errors.As(err, &neg) || neg.Cause() != cause {
		t.Fatalf("error %v, want a refusal with cause %s", err, cause)
	}
}

func ctx(t *testing.T) context.Context {
	c, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)
	return c
}

func TestStationServices(t *testing.T) {
	for _, mode := range []string{config.ModeUnbalanced, config.ModeBalanced} {
		t.Run(mode, func(t *testing.T) {
			r := newRig(t, testModel, mode)
			c := ctx(t)
			r.expect(t, asdu.M_EI_NA_1, asdu.CauseInitialized)

			data, err := r.c.Interrogate(c, 5, asdu.QOIStation)
			if err != nil {
				t.Fatal(err)
			}
			// Consecutive points of one type share an ASDU, in address order.
			var order []asdu.TypeID
			for _, a := range data {
				order = append(order, a.Type)
			}
			want := []asdu.TypeID{asdu.M_SP_NA_1, asdu.M_DP_NA_1, asdu.M_ST_NA_1, asdu.M_BO_NA_1, asdu.M_ME_NA_1, asdu.M_ME_NB_1, asdu.M_ME_NC_1}
			if len(order) != len(want) {
				t.Fatalf("interrogation: %v", order)
			}
			// A broadcast interrogation is answered with the station's own address.
			if data, err := r.c.Interrogate(c, 255, asdu.QOIStation); err != nil || data[0].CommonAddr != 5 {
				t.Fatalf("broadcast: %v %v", data, err)
			}
			_, err = r.c.Interrogate(c, 5, asdu.QOIGroup(1))
			negative(t, err, asdu.CauseActivationCon)
			_, err = r.c.Interrogate(c, 6, asdu.QOIStation)
			negative(t, err, asdu.CauseUnknownCommonAddr)

			totals, err := r.c.CounterInterrogate(c, 5, asdu.CounterGeneral, asdu.FreezeRead)
			if err != nil || len(totals) != 1 {
				t.Fatalf("counters: %v %v", totals, err)
			}
			if it := totals[0].First().(asdu.IntegratedTotal); it.Sequence != 3 || !it.Invalid {
				t.Errorf("total %+v", it)
			}
			// The model refuses a freeze.
			_, err = r.c.CounterInterrogate(c, 5, asdu.CounterGeneral, asdu.FreezeWithoutReset)
			negative(t, err, asdu.CauseActivationCon)
			_, err = r.c.CounterInterrogate(c, 5, 9, asdu.FreezeRead)
			negative(t, err, asdu.CauseActivationCon)

			if a, err := r.c.Read(c, 5, 400); err != nil || a.First().(asdu.Bitstring32).Value != 16 {
				t.Fatalf("read: %v %v", a, err)
			}

			// Commands: a report between confirmation and termination, with the model's cause.
			if err := r.c.Command(c, 5, asdu.SingleCommand{IOA: 1000, Value: false}); err != nil {
				t.Fatal(err)
			}
			if a := r.expect(t, asdu.M_SP_TB_1, asdu.CauseSpontaneous); a.First().(asdu.SinglePoint).Value {
				t.Error("the report says on")
			}
			// Step commands at the limit are refused.
			negative(t, r.c.Command(c, 5, asdu.StepCommand{IOA: 1001, Value: asdu.StepHigher}), asdu.CauseActivationCon)
			negative(t, r.c.Command(c, 5, asdu.StepCommand{IOA: 1002, Value: asdu.StepLower}), asdu.CauseActivationCon)
			negative(t, r.c.Command(c, 5, asdu.StepCommand{IOA: 1001, Value: 0}), asdu.CauseActivationCon)
			if err := r.c.Command(c, 5, asdu.StepCommand{IOA: 1001, Value: asdu.StepLower}); err != nil {
				t.Fatal(err)
			}
			// A selection expires.
			if err := r.c.Command(c, 5, asdu.StepCommand{IOA: 1002, Value: asdu.StepHigher, Select: true}); err != nil {
				t.Fatal(err)
			}
			time.Sleep(5 * time.Millisecond)
			if err := r.c.Command(c, 5, asdu.StepCommand{IOA: 1002, Value: asdu.StepHigher}); err != nil {
				t.Fatal(err)
			}
			// A mismatched execute clears the selection; a deactivation always confirms.
			if err := r.c.Command(c, 5, asdu.SingleCommand{IOA: 1000, Value: true, Select: true}); err != nil {
				t.Fatal(err)
			}
			negative(t, r.c.Command(c, 5, asdu.SingleCommand{IOA: 1000, Value: false}), asdu.CauseActivationCon)
			if err := r.c.Deactivate(c, 5, asdu.SingleCommand{IOA: 1000, Value: true, Select: true}); err != nil {
				t.Fatal(err)
			}
			if err := r.c.Command(c, 5, asdu.BitstringCommand{IOA: 1003, Value: 7}); err != nil {
				t.Fatal(err)
			}
			if err := r.c.Command(c, 5, asdu.SetpointNormalized{IOA: 1004, Value: asdu.NormalizedFromFloat(-0.5)}); err != nil {
				t.Fatal(err)
			}
			if v, _ := r.st.Value(500); v != -0.5 {
				t.Errorf("point 500 is %v", v)
			}
			negative(t, r.c.Command(c, 5, asdu.DoubleCommand{IOA: 1000, Value: asdu.DoubleOn}), asdu.CauseUnknownIOA)
			negative(t, r.c.Command(c, 5, asdu.SingleCommand{IOA: 77, Value: true}), asdu.CauseUnknownIOA)

			// Clock synchronization is corrected by the delay that was sent.
			if err := r.c.SetDelay(c, 5, 250*time.Millisecond); err != nil {
				t.Fatal(err)
			}
			if _, err := r.c.MeasureDelay(c, 5); err != nil {
				t.Fatal(err)
			}
			at := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
			if err := r.c.ClockSync(c, 5, at); err != nil {
				t.Fatal(err)
			}
			if d := r.st.now().Sub(at.Add(250 * time.Millisecond)); d < 0 || d > time.Second {
				t.Errorf("the station's clock is off by %s", d)
			}
			if err := r.c.TestCommand(c, 5); err != nil {
				t.Fatal(err)
			}
			if err := r.c.ResetProcess(c, 5, asdu.ResetPendingEvents); err != nil {
				t.Fatal(err)
			}
			if err := r.c.ResetProcess(c, 5, asdu.ResetProcessGeneral); err != nil {
				t.Fatal(err)
			}
			r.expect(t, asdu.M_EI_NA_1, asdu.CauseInitialized)
			if v, _ := r.st.Value(100); v != true {
				t.Errorf("after the reset point 100 is %v", v)
			}
			negative(t, r.c.ResetProcess(c, 5, 7), asdu.CauseActivationCon)

			// Requests the station does not serve, or with a cause it does not take.
			send := func(a *asdu.ASDU) {
				t.Helper()
				if err := r.c.Send(c, a); err != nil {
					t.Fatal(err)
				}
			}
			send(asdu.New(asdu.CauseActivation, 5, asdu.TestCommand{Time: asdu.Now()}))
			r.expect(t, asdu.C_TS_TA_1, asdu.CauseUnknownType)
			send(asdu.New(asdu.CauseActivation, 5, asdu.ParameterActivation{IOA: 1, Qualifier: 1}))
			r.expect(t, asdu.P_AC_NA_1, asdu.CauseUnknownType)
			send(asdu.New(asdu.CauseSpontaneous, 5, asdu.SingleCommand{IOA: 1000, Value: true}))
			r.expect(t, asdu.C_SC_NA_1, asdu.CauseUnknownCause)
			for _, a := range []*asdu.ASDU{
				asdu.New(asdu.CauseDeactivation, 5, asdu.Interrogation{Qualifier: asdu.QOIStation}),
				asdu.New(asdu.CauseDeactivation, 5, asdu.CounterInterrogation{Request: asdu.CounterGeneral}),
				asdu.New(asdu.CauseDeactivation, 5, asdu.ClockSync{Time: asdu.Now()}),
				asdu.New(asdu.CauseDeactivation, 5, asdu.TestCommand{Counter: 0x55AA}),
				asdu.New(asdu.CauseDeactivation, 5, asdu.DelayAcquisition{}),
			} {
				send(a)
				r.expect(t, a.Type, asdu.CauseDeactivationCon)
			}
			ev := r.events()
			if len(ev) < 20 || ev[0].Event != "connected" {
				t.Errorf("events: %+v", ev)
			}
		})
	}
}

func TestStationFreezeAndGroups(t *testing.T) {
	r := newRig(t, `
station: {common_address: 1}
points:
  - {ioa: 1, type: M_SP_NA_1, value: true, group: 2}
  - {ioa: 2, type: M_IT_NA_1, value: 10, group: 1}
  - {ioa: 3, type: M_IT_NA_1, value: 20, group: 2}
`, config.ModeUnbalanced)
	c := ctx(t)
	if data, err := r.c.Interrogate(c, 1, asdu.QOIGroup(2)); err != nil || len(data) != 1 || data[0].Cause != asdu.QOIGroup(2).Cause() {
		t.Fatalf("group: %v %v", data, err)
	}
	if _, err := r.c.CounterInterrogate(c, 1, asdu.CounterGroup1, asdu.FreezeWithReset); err != nil {
		t.Fatal(err)
	}
	if v, _ := r.st.Value(2); v != int64(0) {
		t.Errorf("counter 2 is %v after freeze and reset", v)
	}
	if v, _ := r.st.Value(3); v != int64(20) {
		t.Errorf("counter 3 is %v", v)
	}
	data, err := r.c.CounterInterrogate(c, 1, asdu.CounterGroup1+1, asdu.FreezeRead)
	if err != nil || len(data) != 1 {
		t.Fatalf("group 2: %v %v", data, err)
	}
}

// Events are kept for the next line while none is served; class 2 data is not.
func TestStationBacklogAndSimulation(t *testing.T) {
	m, err := Parse([]byte(`
station: {common_address: 1, cyclic_interval: 100ms}
points:
  - {ioa: 1, type: M_SP_NA_1, value: true}
  - {ioa: 2, type: M_ME_NC_1, value: 1, simulate: {mode: ramp, interval: 50ms, min: 0, max: 3}}
  - {ioa: 3, type: M_DP_NA_1, value: "on", simulate: {mode: toggle, interval: 50ms}}
  - {ioa: 4, type: M_ME_NB_1, value: 0, simulate: {mode: sine, interval: 50ms, min: -10, max: 10}}
  - {ioa: 5, type: M_ME_NA_1, value: 0, simulate: {mode: random, interval: 50ms, min: -0.5, max: 0.5}}
  - {ioa: 6, type: M_IT_NA_1, value: 2147483647, simulate: {mode: count, interval: 50ms}}
  - {ioa: 7, type: M_SP_NA_1, value: true, simulate: {mode: toggle, interval: 50ms}}
`))
	if err != nil {
		t.Fatal(err)
	}
	st, err := New(m, Options{Link: linkOf(t, config.ModeUnbalanced)})
	if err != nil {
		t.Fatal(err)
	}
	if !st.Set(1, false) || st.Set(99, true) || st.Set(1, "maybe") {
		t.Fatal("Set")
	}
	if _, ok := st.Value(99); ok {
		t.Fatal("Value of an unknown point")
	}
	sctx, stop := context.WithCancel(context.Background())
	st.Simulate(sctx)
	time.Sleep(200 * time.Millisecond)
	stop()
	st.Wait()
	st.mu.Lock()
	n := len(st.backlog)
	st.mu.Unlock()
	if n < 2 {
		t.Fatalf("backlog of %d events", n)
	}
	if v, _ := st.Value(6); v.(int64) > 100 {
		t.Errorf("the counter did not wrap: %v", v)
	}
}

func TestStationConfigErrors(t *testing.T) {
	m := Demo()
	wide := config.DefaultLink()
	wide.IOASize = 1
	l, err := iec.LinkParams(wide)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := New(m, Options{Link: l}); err == nil || !strings.Contains(err.Error(), "do not fit") {
		t.Fatalf("addresses that do not fit: %v", err)
	}
	m2, _ := Parse([]byte("station: {common_address: 300}\npoints: [{ioa: 1, type: M_SP_NA_1, value: true}]\n"))
	if _, err := New(m2, Options{}); err == nil {
		t.Fatal("a common address that does not fit was taken")
	}
	bad := &Model{}
	if _, err := New(bad, Options{}); err == nil {
		t.Fatal("an invalid model was taken")
	}
	// A line the library refuses.
	lc := config.DefaultLink()
	l, _ = iec.LinkParams(lc)
	l.Addr = 300
	st, err := New(Demo(), Options{Link: l})
	if err != nil {
		t.Fatal(err)
	}
	a, b := net.Pipe()
	defer func() { _ = a.Close() }()
	if err := st.ServeLine(context.Background(), b, "x"); err == nil {
		t.Fatal("a link address that does not fit was served")
	}
	if !strings.Contains(st.Describe(), "Demo bay") {
		t.Error(st.Describe())
	}
	if st.CommonAddr() != 1 {
		t.Error("common address")
	}
}

func TestRunErrors(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "m.yaml"), []byte(DemoYAML), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := config.ServerConfig{LinkConfig: config.DefaultLink(), Config: filepath.Join(dir, "m.yaml"), Listen: "127.0.0.1", Port: 1, Duration: 1}
	// Port 1 needs privileges here, or is refused: either way the station does not run.
	if err := Run(context.Background(), &cfg, nil, nil, nil); err == nil {
		t.Skip("port 1 could be bound")
	}
	cfg.Serial, cfg.Port, cfg.Listen = "/dev/x", 0, ""
	cfg.Parity = "mark"
	if err := Check(&cfg); err == nil {
		t.Fatal("parity mark was taken")
	}
}

func TestStationStrictProfile(t *testing.T) {
	r := newRig(t, testModel, config.ModeUnbalanced, func(o *Options) { o.Link.Params.Strict = true })
	c := ctx(t)
	if _, err := r.c.Interrogate(c, 5, asdu.QOIStation); err != nil {
		t.Fatal(err)
	}
	_ = iec101.ModeBalanced
}

func TestDelayAfter(t *testing.T) {
	for _, c := range []struct {
		sent     uint16
		reaction time.Duration
		want     uint16
	}{{100, 5 * time.Millisecond, 105}, {59999, 2 * time.Millisecond, 1}, {7, -time.Second, 7}, {7, time.Hour, 7}} {
		if got := delayAfter(c.sent, c.reaction); got != c.want {
			t.Errorf("delayAfter(%d, %s) = %d, want %d", c.sent, c.reaction, got, c.want)
		}
	}
}
