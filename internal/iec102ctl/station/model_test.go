// SPDX-License-Identifier: MIT

package station

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestModelErrors(t *testing.T) {
	head := "station: {product: {standard: 1, manufacturer: 1, product: 1}}\n"
	for name, c := range map[string]struct{ model, want string }{
		"empty":            {"", "empty model"},
		"unknown field":    {"station: {colour: red}", "colour"},
		"duration":         {head + "totals: [{record: 1, kind: absolute, generate: {start: 2026-01-01, interval: [1], count: 1, totals: [{address: 1}]}}]", "duration"},
		"bad duration":     {head + "totals: [{record: 1, kind: absolute, generate: {start: 2026-01-01, interval: soon, count: 1, totals: [{address: 1}]}}]", "invalid duration"},
		"clock":            {"station: {clock: yesterday}", "station.clock"},
		"type and kind":    {head + "totals: [{record: 1, kind: absolute, type: M_IT_TA_2}]", "not both"},
		"kind":             {head + "totals: [{record: 1, kind: energy}]", "invalid kind"},
		"type":             {head + "totals: [{record: 1, type: M_SP_TA_2}]", "invalid type"},
		"no type":          {head + "totals: [{record: 1}]", "give the type"},
		"twice":            {head + "totals: [{record: 1, kind: absolute, periods: [{end: 2026-01-01, totals: [{address: 1}]}]}, {record: 1, type: M_IT_TG_2}]", "second list"},
		"no periods":       {head + "totals: [{record: 1, kind: absolute}]", "no periods"},
		"period end":       {head + "totals: [{record: 1, kind: absolute, periods: [{end: x, totals: [{address: 1}]}]}]", "periods[0].end"},
		"order":            {head + "totals: [{record: 1, kind: absolute, periods: [{end: 2026-01-02, totals: [{address: 1}]}, {end: 2026-01-01, totals: [{address: 1}]}]}]", "oldest first"},
		"address twice":    {head + "totals: [{record: 1, kind: absolute, periods: [{end: 2026-01-02, totals: [{address: 1}, {address: 1}]}]}]", "twice"},
		"width":            {head + "totals: [{record: 1, type: M_IT_TC_2, periods: [{end: 2026-01-02, totals: [{address: 1, value: 70000}]}]}]", "does not fit"},
		"width 3":          {head + "totals: [{record: 1, type: M_IT_TB_2, periods: [{end: 2026-01-02, totals: [{address: 1, value: 16777216}]}]}]", "does not fit"},
		"empty period":     {head + "totals: [{record: 1, kind: absolute, periods: [{end: 2026-01-02, totals: []}]}]", "no totals"},
		"count":            {head + "totals: [{record: 1, kind: absolute, generate: {start: 2026-01-01, interval: 1h, count: 0, totals: [{address: 1}]}}]", "generate.count"},
		"interval":         {head + "totals: [{record: 1, kind: absolute, generate: {start: 2026-01-01, interval: 1s, count: 1, totals: [{address: 1}]}}]", "generate.interval"},
		"gen totals":       {head + "totals: [{record: 1, kind: absolute, generate: {start: 2026-01-01, interval: 1h, count: 1}}]", "generate.totals"},
		"gen start":        {head + "totals: [{record: 1, kind: absolute, generate: {start: x, interval: 1h, count: 1, totals: [{address: 1}]}}]", "generate.start"},
		"gen 2099":         {head + "totals: [{record: 1, kind: absolute, generate: {start: 2099-12-31T23:00, interval: 1h, count: 2, totals: [{address: 1}]}}]", "after 2099"},
		"gen overflow":     {head + "totals: [{record: 1, kind: absolute, generate: {start: 2026-01-01, interval: 1h, count: 3, totals: [{address: 1, start: 4294967295, step: 1}]}}]", "32 bits"},
		"events twice":     {head + "events: [{record: 52, points: []}, {record: 52, points: []}]", "second list"},
		"qualifier":        {head + "events: [{record: 52, points: [{address: 1, qualifier: 200, time: 2026-01-01T00:00:00}]}]", "qualifier"},
		"event time":       {head + "events: [{record: 52, points: [{address: 1, time: x}]}]", "points[0].time"},
		"event no time":    {head + "events: [{record: 52, points: [{address: 1}]}]", "give the time"},
		"special day":      {head + "ree: {access_key: 1, special_days: [x]}", "special_days"},
		"tariff twice":     {head + "ree: {access_key: 1, tariffs: [{register: 134}, {register: 134}]}", "second entry"},
		"tariff time":      {head + "ree: {access_key: 1, tariffs: [{register: 134, current: [{address: 1, max_power_time: x}]}]}", "max_power_time"},
		"tariff stored":    {head + "ree: {access_key: 1, tariffs: [{register: 134, stored: [{address: 1, max_power_time: 2026-01-01, period_start: 2026-01-01, period_end: x}]}]}", "period_end"},
		"tariff order":     {head + "ree: {access_key: 1, tariffs: [{register: 134, current: [{address: 1, max_power_time: 2026-01-01, period_start: 2026-01-02, period_end: 2026-01-01}]}]}", "before period_start"},
		"too many powers":  {head + "ree: {access_key: 1, contracted_powers: [" + strings.Repeat("{address: 1, power_kw: 1},", 128) + "]}", "at most 127"},
		"too many records": {head + "ree: {access_key: 1, tariffs: [{register: 134, current: [" + strings.Repeat("{address: 1},", MaxEvents+1) + "]}]}", "records"},
	} {
		m, err := Parse([]byte(c.model))
		if err == nil {
			err = m.Validate(time.UTC)
		}
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: %v, want %q", name, err, c.want)
		}
	}
	if _, err := Parse(make([]byte, MaxModelSize+1)); err == nil {
		t.Error("an oversized model was parsed")
	}
}

func TestModelLimits(t *testing.T) {
	m := &Model{}
	m.Totals = make([]TotalsSpec, MaxRecords+1)
	if err := m.Validate(nil); err == nil || !strings.Contains(err.Error(), "lists") {
		t.Errorf("lists: %v", err)
	}
	m.Totals = []TotalsSpec{{Record: 1, Kind: "absolute", Periods: make([]PeriodSpec, MaxPeriods+1)}}
	if err := m.Validate(nil); err == nil {
		t.Error("periods of one list")
	}
	gen := &GenerateSpec{Start: "2026-01-01", Interval: Duration(time.Minute), Count: MaxPeriods, Totals: []GenTotalSpec{{Address: 1}}}
	m.Totals = []TotalsSpec{{Record: 1, Kind: "absolute", Generate: gen}, {Record: 2, Kind: "absolute", Generate: gen}}
	if err := m.Validate(nil); err == nil || !strings.Contains(err.Error(), "periods") {
		t.Errorf("periods: %v", err)
	}
	m.Totals = nil
	m.Events = []EventsSpec{{Record: 1, Points: make([]PointSpec, MaxEvents+1)}}
	if err := m.Validate(nil); err == nil || !strings.Contains(err.Error(), "points") {
		t.Errorf("events: %v", err)
	}
}

func TestLoad(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "m.json")
	if err := os.WriteFile(p, []byte(`{"station": {"common_address": 4, "product": {"standard": 1, "manufacturer": 2, "product": 3}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	m, err := Load(p)
	if err != nil || *m.Station.CommonAddress != 4 {
		t.Fatalf("%+v %v", m, err)
	}
	if _, err := Load(filepath.Join(dir, "none")); err == nil {
		t.Error("a missing file")
	}
	bad := filepath.Join(dir, "bad.yaml")
	_ = os.WriteFile(bad, []byte("station: ["), 0o600)
	if _, err := Load(bad); err == nil {
		t.Error("a broken file")
	}
	big := filepath.Join(dir, "big.yaml")
	_ = os.WriteFile(big, make([]byte, MaxModelSize+1), 0o600)
	if _, err := Load(big); err == nil || !strings.Contains(err.Error(), "larger") {
		t.Errorf("a big file: %v", err)
	}
	if _, err := Load(dir); err == nil {
		t.Error("a directory")
	}
}

// A model file is operator input: whatever Parse and Validate accept, a station is built
// from, and nothing panics.
func FuzzModel(f *testing.F) {
	f.Add([]byte(DemoYAML))
	f.Add([]byte(testModel))
	f.Add([]byte(`{"station": {"clock": "2026-01-01T00:00:00.000"}, "totals": [{"record": 1, "kind": "absolute", "periods": [{"end": "2026-01-01", "totals": [{"address": 1, "value": 1}]}]}]}`))
	f.Add([]byte("station: {}\nevents: [{record: 1, points: [{address: 1, on: true, time: 2026-01-01T00:00:00}]}]"))
	f.Add([]byte("totals: [{record: 1, kind: absolute, generate: {start: 2026-01-01, interval: 1h, count: 5, totals: [{address: 1, step: 2}]}}]"))
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 1<<16 {
			return
		}
		m, err := Parse(data)
		if err != nil {
			return
		}
		// Bound the work a fuzzed generator may ask for.
		for _, l := range m.Totals {
			if l.Generate != nil && l.Generate.Count > 1000 {
				return
			}
		}
		if err := m.Validate(time.UTC); err != nil {
			return
		}
		if _, err := New(m, Options{}); err != nil {
			if strings.Contains(err.Error(), "address") {
				return // a link or common address the field sizes cannot carry
			}
			t.Fatalf("a valid model makes no station: %v", err)
		}
	})
}
