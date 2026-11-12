// SPDX-License-Identifier: MIT

package types

import (
	"strings"
	"testing"

	"github.com/otfabric/iec60870ctl/internal/format"
)

func u8(v uint8) *uint8 { return &v }

// render writes v in every format and checks the text contains want.
func render(t *testing.T, v any, want ...string) {
	t.Helper()
	for _, f := range []format.OutputFormat{format.FormatText, format.FormatTable, format.FormatJSON} {
		var b strings.Builder
		if err := format.Write(&b, f, v); err != nil {
			t.Fatalf("%s: %v", f, err)
		}
		if f == format.FormatText {
			for _, w := range want {
				if !strings.Contains(b.String(), w) {
					t.Errorf("%q not in %q", w, b.String())
				}
			}
		}
	}
}

func TestRenderings(t *testing.T) {
	scl, ret, fan := 1.5, uint16(3), uint16(4)
	m := Message{Received: "now", Type: "x", Name: "n", DPI: u8(2), State: "ON", Time: "t", TimeInvalid: true, SIN: u8(1), SCN: u8(2), COL: u8(3),
		RET: &ret, FAN: &fan, SCL: &scl, Values: []Measurand{{Raw: 1, OV: true, ER: true}}, Manufacturer: "M", Software: "00", Data: "ab"}
	render(t, &m, "now  <0> x", "(n)", "dpi=ON", "time=t(invalid)", "sin=1", "scn=2", "col=3", "ret=3ms", "fan=4", "scl=1.5", "values=1(ov)(er)", `manufacturer="M"`, "software=00", "data=ab")
	d := Device{Target: "tcp://h:1", LinkAddress: 1, CA: 1}
	render(t, &InterrogateResult{Device: d, Count: 1, Messages: []Message{m}}, "General interrogation of device 1")
	render(t, &CommandResult{Device: d, Fun: 160, Inf: 16, Name: "AR", Command: "on", Report: &m}, "(AR) ON", "change of state")
	render(t, &CommandResult{Device: d, Command: "off"}, "OFF")
	off := 1.0
	render(t, &ClockResult{Device: d, Sent: "s", DeviceTime: "d", OffsetMs: &off}, "(+1 ms)")
	render(t, &ClockResult{Device: d, Broadcast: true, Sent: "s"}, "broadcast")
	render(t, &IdentifyResult{Device: d, Identifications: []Identification{{CauseName: "reset CU", Manufacturer: "M"}}}, `"M"`)
	render(t, &ScanResult{Target: "x", Found: []ScanRow{{LinkAddress: 1, Identified: true, Manufacturer: "M"}, {LinkAddress: 2}}}, `"M"`, "no identification")
	render(t, &GenericResult{Device: d, Service: "write", Mode: "abort", GIN: "1.1",
		Elements: []DescElement{{KOD: 1, Values: []any{"a", 1}}, {Reply: "invalid-gin"}},
		Sets:     []DataSet{{GIN: "1.1", Data: "ff"}}}, "(abort)", "directory of 1.1", `"a", 1`, "reply invalid-gin", "data ff")
	render(t, &DisturbanceListResult{Device: d, Faults: []Fault{{FAN: 7, SOF: SOF{TP: true, TM: true, Test: true, OTEV: true}}, {FAN: 8}}}, "trip,transmitting,test,other-event", "FAN 8")
	render(t, &DisturbanceResult{Device: d, Output: "o", CSV: "c", Disturbance: Disturbance{Tags: []Tag{{Pos: 1}},
		Channels: []Channel{{ACC: 1, Values: []int16{3, -2, 5}}, {ACC: 2}}}}, "min=-2 max=5", "min=0 max=0", "record written to o", "values written to c")
	render(t, &ServerEvent{Timestamp: "t", Event: "command", Remote: "r", Fun: u8(1), Inf: u8(2), Value: "ON", Detail: "x"}, "fun=1 inf=2", "ON", "x")
	render(t, &ServerEvent{Timestamp: "t", Event: "stopping"}, "stopping")
	if s := (MonitorSummary{Target: "x", Messages: 2}).String(); !strings.Contains(s, "2 messages") {
		t.Fatal(s)
	}
}
