// SPDX-License-Identifier: MIT

package types

import (
	"strings"
	"testing"
)

type textual interface{ MarshalTextOutput() (string, error) }

type tabular interface {
	TableHeaders() []string
	TableRows() [][]string
}

func TestRendering(t *testing.T) {
	fail := &ErrorInfo{Kind: "timeout", Code: "request_timeout", Message: "no answer"}
	measured, set := 12.0, uint16(12)
	p := Point{CommonAddr: 1, IOA: 2, Type: "M_SP_NA_1", Cause: "spontaneous", Value: true, Quality: "good", Negative: true, Test: true}
	for name, c := range map[string]struct {
		v    any
		want []string
	}{
		"interrogation": {&InterrogateResult{Request: "interrogation", Group: 3, Received: 2, Count: 1, Points: []Point{p}}, []string{"Interrogation of group 3", "1 shown"}},
		"counters":      {&InterrogateResult{Request: "counter-interrogation", Group: 2, Freeze: "freeze"}, []string{"Counter interrogation, group 2 (freeze)"}},
		"read":          {&ReadResult{Requested: 2, Failed: 1, Rows: []ReadRow{{IOA: 2, Point: &p}, {IOA: 3, Error: fail}}}, []string{"1 of 2 answered", "ioa=3  error: no answer"}},
		"command":       {&CommandResult{Type: "C_SC_NA_1", Value: false, Steps: []CommandStep{{Step: "execute", Result: "confirmed"}}}, []string{"value off", "execute"}},
		"clock":         {&ConfirmedResult{Request: "clock-synchronization", Time: "t"}, []string{"set to t"}},
		"reset":         {&ConfirmedResult{Request: "reset-process", Qualifier: "events"}, []string{"Reset (events)"}},
		"delay":         {&DelayResult{MeasuredMs: &measured, SetMs: &set}, []string{"transmission delay 12 ms, delay 12 ms sent"}},
		"delay set":     {&DelayResult{SetMs: &set}, []string{"delay 12 ms sent"}},
		"ping":          {&PingSample{Seq: 1, Method: "command", Error: fail}, []string{"error: no answer"}},
		"scan":          {&ScanResult{Stations: []ScanStation{{CommonAddr: 1, Status: "found", Types: []TypeCount{{"M_SP_NA_1", 2}}}, {CommonAddr: 2, Status: "refused", Error: fail}, {CommonAddr: 3, Status: "other"}}}, []string{"M_SP_NA_1×2", "no answer", "ca=3"}},
		"link scan":     {&LinkScanResult{Stations: []LinkStation{{LinkAddress: 1, Status: "answered", ACD: true}, {LinkAddress: 2, Status: "refused", Error: fail}}}, []string{"acd=true", "refused"}},
		"types":         {&TypesResult{Types: []TypeInfo{{ID: 1, Name: "M_SP_NA_1", Decoded: false}}}, []string{"not decoded"}},
		"discover":      {&DiscoverResult{Devices: []DiscoveredDevice{{IP: "10.0.0.1", Port: 2401, Status: "iec101", LinkAddress: 1}}}, []string{"link=1"}},
		"event":         {&ServerEvent{Event: "command", Remote: "r", CommonAddr: 1, IOA: 2, Type: "C_SC_NA_1", Value: true, Detail: "d"}, []string{"ca=1 ioa=2", "on", "d"}},
		"point":         {p, []string{"spontaneous [negative,test]"}},
	} {
		text, err := c.v.(textual).MarshalTextOutput()
		if err != nil {
			t.Fatal(err)
		}
		for _, w := range c.want {
			if !strings.Contains(text, w) {
				t.Errorf("%s: %q lacks %q", name, text, w)
			}
		}
		tb := c.v.(tabular)
		for _, row := range tb.TableRows() {
			if len(row) != len(tb.TableHeaders()) {
				t.Errorf("%s: row %v does not match %v", name, row, tb.TableHeaders())
			}
		}
	}
	empty := (&ServerEvent{Event: "stopping"}).TableRows()[0]
	if empty[3] != "" || empty[6] != "" {
		t.Errorf("empty cells: %v", empty)
	}
	if (PingSummary{Sent: 2, Failed: 2}).String() == "" || ErrorMessage(nil) != "" {
		t.Error("summary")
	}
	q := Point{Timestamp: "t", TimestampInvalid: true}
	if q.qualityText() != "-" || q.timeText() != "t (invalid)" || q.flagsText() != "" {
		t.Errorf("point texts %q %q", q.qualityText(), q.timeText())
	}
	if (Point{Value: float32(1.5)}).ValueText() != "1.5" || (Point{Value: 3, Attributes: map[string]any{"x": false}}).ValueText() != "3" {
		t.Error("value texts")
	}
}
