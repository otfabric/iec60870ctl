// SPDX-License-Identifier: MIT

package station_test

import (
	"context"
	"encoding/json"
	"math"
	"strings"
	"testing"

	"github.com/otfabric/go-iec60870-5/asdu"
	"github.com/otfabric/iec60870ctl/internal/iec104ctl/station"
	"github.com/otfabric/iec60870ctl/internal/iec104ctl/stationtest"
	"github.com/otfabric/iec60870ctl/internal/iec104ctl/types"
)

// setPoints has a floating-point set point for a point of each measured type.
const setPoints = `
station: {common_address: 1}
points:
  - {ioa: 1000, type: M_ME_NC_1, value: 230.5}
  - {ioa: 1002, type: M_ME_NB_1, value: 1250}
  - {ioa: 1003, type: M_ME_NA_1, value: 0.25}
commands:
  - {ioa: 5003, type: C_SE_NC_1, target: 1000}
  - {ioa: 5010, type: C_SE_NC_1, target: 1002}
  - {ioa: 5011, type: C_SE_NC_1, target: 1003}
`

// A set point that is NaN or infinite is outside the range of every point: it is answered
// with a negative confirmation, as a select and as an execute, and the point keeps its
// value. The event log names the value in the field non_finite.
func TestNonFiniteSetPointRefused(t *testing.T) {
	st := stationtest.Start(t, setPoints)
	c := dial(t, st)
	ctx := context.Background()
	values := map[uint32]any{}
	for _, ioa := range []uint32{1000, 1002, 1003} {
		values[ioa], _ = st.Value(ioa)
	}
	for _, v := range []float64{math.NaN(), math.Inf(1), math.Inf(-1)} {
		for _, ioa := range []asdu.IOA{5003, 5010, 5011} {
			negative(t, c.Command(ctx, 1, asdu.SetpointFloat{IOA: ioa, Value: float32(v)}), asdu.CauseActivationCon)
			negative(t, c.Command(ctx, 1, asdu.SetpointFloat{IOA: ioa, Value: float32(v), Select: true}), asdu.CauseActivationCon)
		}
	}
	for ioa, want := range values {
		if got, _ := st.Value(ioa); got != want {
			t.Errorf("point %d is %v after refused set points, want %v", ioa, got, want)
		}
	}
	// A finite set point is still carried out.
	if err := c.Command(ctx, 1, asdu.SetpointFloat{IOA: 5003, Value: 7.5}); err != nil {
		t.Fatal(err)
	}
	if got, _ := st.Value(1000); got != 7.5 {
		t.Errorf("point 1000 is %v after a set point of 7.5", got)
	}

	refused := map[string]int{}
	for _, e := range st.Events() {
		if e.Event != "command" || e.NonFinite == "" {
			continue
		}
		refused[e.NonFinite]++
		if e.Value != nil || e.Detail != "refused: the value is not a finite number" {
			t.Errorf("event %+v", e)
		}
		data, err := json.Marshal(&e)
		if err != nil || !strings.Contains(string(data), `"non_finite":"`+e.NonFinite+`"`) || strings.Contains(string(data), `"value"`) {
			t.Errorf("JSON of the event: %s, %v", data, err)
		}
		text, _ := e.MarshalTextOutput()
		if !strings.Contains(text, "  "+e.NonFinite+"  refused") {
			t.Errorf("text of the event: %q", text)
		}
		if row := e.TableRows()[0]; row[6] != e.NonFinite {
			t.Errorf("table row of the event: %q", row)
		}
	}
	for _, name := range []string{"NaN", "+Inf", "-Inf"} {
		if refused[name] != 6 {
			t.Errorf("%d refused set points of %s in the event log, want 6", refused[name], name)
		}
	}
	// A finite value is shown as before, without non_finite.
	e := types.ServerEvent{Event: "command", Value: 7.5}
	if data, _ := json.Marshal(&e); strings.Contains(string(data), "non_finite") {
		t.Errorf("JSON of a finite value: %s", data)
	}
	if text, _ := e.MarshalTextOutput(); !strings.Contains(text, "  7.5") {
		t.Errorf("text of a finite value: %q", text)
	}
}

// A simulation whose min, max or step is NaN or infinite is refused when the model is
// checked.
func TestSimulationBoundsAreFinite(t *testing.T) {
	for _, sim := range []string{
		"{mode: ramp, interval: 1s, min: .nan, max: 10}",
		"{mode: ramp, interval: 1s, min: 0, max: .inf}",
		"{mode: random, interval: 1s, min: -.inf, max: 10}",
		"{mode: sine, interval: 1s, min: 0, max: 10, step: .nan}",
		"{mode: count, interval: 1s, step: .inf}",
	} {
		m, err := station.Parse([]byte("station: {common_address: 1}\npoints:\n  - {ioa: 1, type: M_ME_NC_1, value: 1, simulate: " + sim + "}\n"))
		if err != nil {
			t.Fatal(err)
		}
		if err := m.Validate(); err == nil || !strings.Contains(err.Error(), "use a finite number") {
			t.Errorf("simulate %s: %v", sim, err)
		}
	}
	m, err := station.Parse([]byte("station: {common_address: 1}\npoints:\n  - {ioa: 1, type: M_ME_NC_1, value: 1, simulate: {mode: ramp, interval: 1s, min: -1e300, max: 1e300, step: 1e299}}\n"))
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Validate(); err != nil {
		t.Errorf("finite bounds: %v", err)
	}
}
