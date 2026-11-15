// SPDX-License-Identifier: MIT

package station

import (
	"encoding/json"
	"math"
	"strings"
	"testing"

	"github.com/otfabric/go-iec60870-5/asdu"
	"github.com/otfabric/iec60870ctl/internal/iec101ctl/config"
)

const setPointModel = `
station: {common_address: 5}
points:
  - {ioa: 500, type: M_ME_NA_1, value: 0.5}
  - {ioa: 501, type: M_ME_NB_1, value: -5}
  - {ioa: 502, type: M_ME_NC_1, value: 1.5}
commands:
  - {ioa: 1000, type: C_SE_NC_1, target: 502}
  - {ioa: 1001, type: C_SE_NC_1, target: 501}
  - {ioa: 1002, type: C_SE_NC_1, target: 500}
  - {ioa: 1003, type: C_SE_NC_1, target: 502, select_required: true}
`

// A set point that is NaN or infinite is answered with a negative confirmation, whatever
// point it is for; the point keeps its value, and the command event names the number in
// non_finite and encodes as JSON. A finite set point is still carried out.
func TestStationRefusesNonFiniteSetPoint(t *testing.T) {
	r := newRig(t, setPointModel, config.ModeUnbalanced)
	c := ctx(t)
	before := map[uint32]any{}
	for _, ioa := range []uint32{500, 501, 502} {
		before[ioa], _ = r.st.Value(ioa)
	}
	values := []struct {
		f    float32
		name string
	}{{float32(math.NaN()), "NaN"}, {float32(math.Inf(1)), "+Inf"}, {float32(math.Inf(-1)), "-Inf"}}
	for _, cmd := range []asdu.IOA{1000, 1001, 1002} {
		for _, v := range values {
			negative(t, r.c.Command(c, 5, asdu.SetpointFloat{IOA: cmd, Value: v.f}), asdu.CauseActivationCon)
		}
	}
	// A select of such a set point is refused as well.
	negative(t, r.c.Command(c, 5, asdu.SetpointFloat{IOA: 1003, Value: float32(math.NaN()), Select: true}), asdu.CauseActivationCon)
	for ioa, want := range before {
		if v, _ := r.st.Value(ioa); v != want {
			t.Errorf("point %d is %v after the refused set points, want %v", ioa, v, want)
		}
	}
	refused := 0
	for _, e := range r.events() {
		if e.Event != "command" {
			continue
		}
		if e.Value != nil || e.NonFinite == "" || !strings.HasPrefix(e.Detail, "refused: ") {
			t.Errorf("event %+v: want a refusal with non_finite and no value", e)
			continue
		}
		data, err := json.Marshal(e)
		if err != nil || !strings.Contains(string(data), `"non_finite":"`+e.NonFinite+`"`) || strings.Contains(string(data), `"value"`) {
			t.Errorf("event JSON %s, %v", data, err)
		}
		refused++
	}
	if refused != 10 {
		t.Errorf("%d refused command events, want 10", refused)
	}
	if err := r.c.Command(c, 5, asdu.SetpointFloat{IOA: 1000, Value: 2.25}); err != nil {
		t.Fatalf("a finite set point: %v", err)
	}
	if v, _ := r.st.Value(502); v != 2.25 {
		t.Errorf("point 502 is %v after the set point 2.25", v)
	}
}

// The model refuses a simulation whose min, max or step is not a finite number.
func TestModelRefusesNonFiniteSimulation(t *testing.T) {
	for _, sim := range []string{
		"{mode: ramp, interval: 1s, min: .nan, max: 1}",
		"{mode: ramp, interval: 1s, min: 0, max: .inf}",
		"{mode: sine, interval: 1s, min: -.inf, max: 1}",
		"{mode: random, interval: 1s, min: .nan, max: .nan}",
		"{mode: count, interval: 1s, step: .nan}",
		"{mode: count, interval: 1s, step: .inf}",
	} {
		m, err := Parse([]byte("station: {common_address: 1}\npoints:\n  - {ioa: 1, type: M_ME_NC_1, value: 1, simulate: " + sim + "}\n"))
		if err != nil {
			t.Fatal(err)
		}
		if err := m.Validate(); err == nil || !strings.Contains(err.Error(), "use a finite number") {
			t.Errorf("simulate %s: %v, want a refusal", sim, err)
		}
	}
	for _, v := range []string{".nan", ".inf", "-.inf"} {
		m, err := Parse([]byte("station: {common_address: 1}\npoints:\n  - {ioa: 1, type: M_ME_NC_1, value: " + v + "}\n"))
		if err != nil {
			t.Fatal(err)
		}
		if err := m.Validate(); err == nil {
			t.Errorf("value %s validated", v)
		}
	}
}

// A counter interrogation with freeze 3 (counter reset) is confirmed and sets the counters
// of its group to zero, as freeze with reset (2) does.
func TestStationCounterReset(t *testing.T) {
	r := newRig(t, `
station: {common_address: 1}
points:
  - {ioa: 2, type: M_IT_NA_1, value: 10, group: 1}
  - {ioa: 3, type: M_IT_NA_1, value: 20, group: 2}
`, config.ModeUnbalanced)
	c := ctx(t)
	if _, err := r.c.CounterInterrogate(c, 1, asdu.CounterGroup1, asdu.FreezeResetOnly); err != nil {
		t.Fatalf("freeze 3: %v", err)
	}
	if v, _ := r.st.Value(2); v != int64(0) {
		t.Errorf("counter 2 is %v after the reset", v)
	}
	if v, _ := r.st.Value(3); v != int64(20) {
		t.Errorf("counter 3 of another group is %v", v)
	}
	data, err := r.c.CounterInterrogate(c, 1, asdu.CounterGeneral, asdu.FreezeRead)
	if err != nil || len(data) != 1 || len(data[0].Objects) != 2 {
		t.Fatalf("read after the reset: %v %v", data, err)
	}
	if it := data[0].Objects[0].(asdu.IntegratedTotal); it.IOA != 2 || it.Value != 0 {
		t.Errorf("counter read after the reset: %+v", it)
	}
}
