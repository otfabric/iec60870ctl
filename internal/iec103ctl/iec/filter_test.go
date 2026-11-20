// SPDX-License-Identifier: MIT

package iec

import (
	"context"
	"io"
	"testing"

	"github.com/otfabric/go-iec60870-5/iec103"
	"github.com/otfabric/iec60870ctl/internal/iec103ctl/config"
	"github.com/otfabric/iec60870ctl/internal/iec103ctl/types"
)

func TestParseMonitorFilter(t *testing.T) {
	f, err := ParseMonitorFilter(&config.MonitorConfig{
		Types: "time-tagged-message, 9,20-21", Funs: "160", Infs: "16-18,64", Causes: "spontaneous,reset-fcb,12",
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		m    types.Message
		pass bool
	}{
		{types.Message{TypeID: 1, Fun: 160, Inf: 16, Cause: 1}, true},
		{types.Message{TypeID: 9, Fun: 160, Inf: 64, Cause: 3}, true},
		{types.Message{TypeID: 21, Fun: 160, Inf: 18, Cause: 12}, true},
		{types.Message{TypeID: 2, Fun: 160, Inf: 16, Cause: 1}, false},
		{types.Message{TypeID: 1, Fun: 128, Inf: 16, Cause: 1}, false},
		{types.Message{TypeID: 1, Fun: 160, Inf: 19, Cause: 1}, false},
		{types.Message{TypeID: 1, Fun: 160, Inf: 16, Cause: 9}, false},
	} {
		if got := f.Match(&c.m); got != c.pass {
			t.Errorf("%+v: %v", c.m, got)
		}
	}
	none, err := ParseMonitorFilter(&config.MonitorConfig{Types: " , ", Funs: ""})
	if err != nil || none.Types != nil || none.Funs != nil || !none.Match(&types.Message{TypeID: 200}) {
		t.Fatalf("an empty filter: %+v %v", none, err)
	}
	for _, cfg := range []config.MonitorConfig{
		{Types: "nothing"}, {Types: "1-x"}, {Types: "x-1"}, {Types: "256"}, {Funs: "a"}, {Funs: "9-3"},
		{Infs: "1-256"}, {Infs: "reserved"}, {Causes: "reserved"}, {Causes: "4-2"}, {Causes: "1-z"},
	} {
		if _, err := ParseMonitorFilter(&cfg); err == nil {
			t.Errorf("%+v accepted", cfg)
		}
	}
}

func TestCauseNames(t *testing.T) {
	names := CauseNames()
	if len(names) < 15 || names[0] != "spontaneous" {
		t.Fatalf("%v", names)
	}
	for _, n := range names {
		c, ok := causeByName(n)
		if !ok || causeKey(CauseName(iec103.Cause(c))) != n {
			t.Errorf("%s -> %d %v", n, c, ok)
		}
	}
	if c, ok := causeByName("Reset FCB"); !ok || c != 3 {
		t.Errorf("Reset FCB -> %d %v", c, ok)
	}
	if got := causeKey("  (x)  y--z "); got != "x-y-z" {
		t.Errorf("causeKey %q", got)
	}
}

func TestTypes(t *testing.T) {
	all := Types(&config.TypesConfig{})
	if all.Count != len(TypeNames()) || all.Count != 22 {
		t.Fatalf("%d types, %d names", all.Count, len(TypeNames()))
	}
	mon := Types(&config.TypesConfig{Direction: "monitor"})
	ctl := Types(&config.TypesConfig{Direction: "control"})
	// Types 6 and 10 are in both lists.
	if mon.Count+ctl.Count != all.Count+2 {
		t.Errorf("monitor %d + control %d != %d + 2", mon.Count, ctl.Count, all.Count)
	}
	if CheckTypes(&config.TypesConfig{Direction: "control"}) != nil || CheckTypes(&config.TypesConfig{Direction: "both"}) == nil {
		t.Error("CheckTypes")
	}
}

func TestParseDataSet(t *testing.T) {
	for in, want := range map[string]iec103.DataSet{
		"1.2=float32:1.25":          {GIN: iec103.GIN{Group: 1, Entry: 2}, KOD: iec103.KODActualValue},
		"1.3:actual=uint/2:450":     {GIN: iec103.GIN{Group: 1, Entry: 3}, KOD: iec103.KODActualValue},
		"2.1:10=ascii:LINE 1: A, B": {GIN: iec103.GIN{Group: 2, Entry: 1}, KOD: 10},
		"3.4=int/1:-5":              {GIN: iec103.GIN{Group: 3, Entry: 4}, KOD: iec103.KODActualValue},
		"3.5:default=float64:1e-3":  {GIN: iec103.GIN{Group: 3, Entry: 5}, KOD: iec103.KODDefaultValue},
		"4.6=ascii/5:ABCDE":         {GIN: iec103.GIN{Group: 4, Entry: 6}, KOD: iec103.KODActualValue},
	} {
		got, err := ParseDataSet(in)
		if err != nil || got.GIN != want.GIN || got.KOD != want.KOD {
			t.Errorf("%q: %+v %v", in, got, err)
		}
	}
	if d, _ := ParseDataSet("2.1:10=ascii:LINE 1: A, B"); string(d.Value.Data) != "LINE 1: A, B" {
		t.Errorf("ascii value %q", d.Value.Data)
	}
	for _, in := range []string{
		"", "1.2", "1.2=float32", "1=float32:1", "1.2:what=float32:1", "1.2=text:1",
		"1.2=uint:1", "1.2=uint/:1", "1.2=uint/9:1", "1.2=uint/2:x", "1.2=ascii/3:AB",
	} {
		if _, err := ParseDataSet(in); err == nil {
			t.Errorf("%q accepted", in)
		}
	}
}

func TestCheckWriteData(t *testing.T) {
	cfg := config.GenericWriteConfig{ConnConfig: conn("tcp://h"), Mode: WriteExecute, Data: []string{"1.2=float32:1", "1.3=uint/2:4"}}
	sets, err := checkWrite(&cfg)
	if err != nil || len(sets) != 2 {
		t.Fatalf("%v %v", sets, err)
	}
	for _, edit := range []func(*config.GenericWriteConfig){
		func(c *config.GenericWriteConfig) { c.GIN = "1.2" },
		func(c *config.GenericWriteConfig) { c.Type = "uint" },
		func(c *config.GenericWriteConfig) { c.Size = 2 },
		func(c *config.GenericWriteConfig) { c.Value = "1" },
		func(c *config.GenericWriteConfig) { c.Data = append(c.Data, "1.4") },
		func(c *config.GenericWriteConfig) {
			for len(c.Data) <= MaxDataSets {
				c.Data = append(c.Data, "1.3=uint/2:4")
			}
		},
	} {
		c := cfg
		c.Data = append([]string(nil), cfg.Data...)
		edit(&c)
		if _, err := checkWrite(&c); err == nil {
			t.Errorf("%+v accepted", c)
		}
	}
}

// A filter that does not parse ends Monitor before the line is opened.
func TestMonitorRefusesAnInvalidFilter(t *testing.T) {
	cfg := &config.MonitorConfig{Funs: "300"}
	cfg.URL = "tcp://127.0.0.1:1"
	if _, err := Monitor(context.Background(), cfg, io.Discard, func(*types.Message) error { return nil }); err == nil {
		t.Fatal("--fun 300 was accepted")
	}
}
