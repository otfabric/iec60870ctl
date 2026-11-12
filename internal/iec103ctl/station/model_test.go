// SPDX-License-Identifier: MIT

package station

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/otfabric/go-iec60870-5/iec103"
)

const minimal = `{"station": {"commonAddress": 1, "identification": {"fun": 160, "col": 2, "ascii": "OTFAB103", "software": "01020304"}}}`

func TestDemoIsValidAndRoundTrips(t *testing.T) {
	m := Demo()
	if m.LinkAddr() != 1 || len(m.Generic) == 0 || len(m.Disturbances) != 1 {
		t.Fatalf("%+v", m)
	}
	for _, asJSON := range []bool{true, false} {
		b, err := m.Encode(asJSON)
		if err != nil {
			t.Fatal(err)
		}
		back, err := Parse(b, asJSON)
		if err != nil {
			t.Fatalf("json=%t: %v\n%s", asJSON, err, b)
		}
		if len(back.Indications) != len(m.Indications) || back.Station.Identification != m.Station.Identification {
			t.Fatalf("json=%t: %+v", asJSON, back)
		}
	}
}

func TestLoad(t *testing.T) {
	dir := t.TempDir()
	j := filepath.Join(dir, "m.json")
	y := filepath.Join(dir, "m.yaml")
	if err := os.WriteFile(j, []byte(minimal), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(y, []byte(DemoYAML), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{j, y} {
		if _, err := Load(p); err != nil {
			t.Fatalf("%s: %v", p, err)
		}
	}
	if _, err := Load(filepath.Join(dir, "none.yaml")); err == nil {
		t.Fatal("missing file loaded")
	}
	big := filepath.Join(dir, "big.yaml")
	if err := os.WriteFile(big, []byte(strings.Repeat("#", MaxModelSize+10)), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(big); err == nil || !strings.Contains(err.Error(), "larger") {
		t.Fatal(err)
	}
	if _, err := Load(dir); err == nil {
		t.Fatal("directory loaded")
	}
}

func TestParseRefusesUnknownFields(t *testing.T) {
	if _, err := Parse([]byte(`{"station": {}, "points": []}`), true); err == nil {
		t.Fatal("unknown JSON field accepted")
	}
	if _, err := Parse([]byte("station: {}\npoints: []\n"), false); err == nil {
		t.Fatal("unknown YAML field accepted")
	}
}

// edit applies f to the minimal model and returns its validation error.
func edit(t *testing.T, f func(m *Model)) error {
	t.Helper()
	m, err := Parse([]byte(minimal), true)
	if err != nil {
		t.Fatal(err)
	}
	f(m)
	return m.Validate()
}

func TestValidate(t *testing.T) {
	five := 255
	good := Record{FAN: 1, Time: "2026-01-02T03:04:05.678Z", Start: "03:04:05.000", Interval: 1000,
		Tags:     []TagDef{{Pos: 0, Fun: 160, Inf: 84, DPI: 1}},
		Channels: []ChannelDef{{ACC: 1, Values: []int{1, 2}}, {ACC: 2, Values: []int{3, 4}}}}
	if err := edit(t, func(m *Model) { m.Disturbances = []Record{good} }); err != nil {
		t.Fatal(err)
	}
	for name, f := range map[string]func(m *Model){
		"link":      func(m *Model) { m.Station.LinkAddress = &five },
		"ca":        func(m *Model) { m.Station.CommonAddress = 255 },
		"fun":       func(m *Model) { m.Station.Identification.Fun = 0 },
		"col":       func(m *Model) { m.Station.Identification.Col = 300 },
		"ascii":     func(m *Model) { m.Station.Identification.ASCII = "TOO LONG NAME" },
		"software":  func(m *Model) { m.Station.Identification.Software = "0102" },
		"start":     func(m *Model) { m.Station.StartCause = "reboot" },
		"ind range": func(m *Model) { m.Indications = []Indication{{Fun: 255, Inf: 1, DPI: 1}} },
		"dpi":       func(m *Model) { m.Indications = []Indication{{Fun: 1, Inf: 1, DPI: 4}} },
		"simulate":  func(m *Model) { m.Indications = []Indication{{Fun: 1, Inf: 1, DPI: 1, Simulate: 5}} },
		"ind twice": func(m *Model) { m.Indications = []Indication{{Fun: 1, Inf: 1}, {Fun: 1, Inf: 1}} },
		"mea range": func(m *Model) { m.Measurands = []MeasurandIO{{Fun: 255, Inf: 1, Type: 3, Values: []MeaIO{{}}}} },
		"mea reuse": func(m *Model) {
			m.Indications = []Indication{{Fun: 1, Inf: 1}}
			m.Measurands = []MeasurandIO{{Fun: 1, Inf: 1, Type: 3, Values: []MeaIO{{}}}}
		},
		"mea type":  func(m *Model) { m.Measurands = []MeasurandIO{{Fun: 1, Inf: 1, Type: 4, Values: []MeaIO{{}}}} },
		"mea count": func(m *Model) { m.Measurands = []MeasurandIO{{Fun: 1, Inf: 1, Type: 3, Values: make([]MeaIO, 5)}} },
		"mea value": func(m *Model) {
			m.Measurands = []MeasurandIO{{Fun: 1, Inf: 1, Type: 9, Values: []MeaIO{{Value: 5000}}}}
		},
		"cmd target": func(m *Model) { m.Commands = []CommandDef{{Fun: 1, Inf: 1}} },
		"cmd twice": func(m *Model) {
			m.Indications = []Indication{{Fun: 1, Inf: 1}}
			m.Commands = []CommandDef{{1, 1}, {1, 1}}
		},
		"group":      func(m *Model) { m.Groups = []GroupDef{{Group: 1, Description: "A"}, {Group: 1, Description: "B"}} },
		"group text": func(m *Model) { m.Groups = []GroupDef{{Group: 1, Description: ""}} },
		"gin": func(m *Model) {
			m.Generic = []GenericDef{{GIN: GINDef{0, 1}, Description: "X", Type: 3, Size: 1, Value: 1}}
		},
		"gin twice": func(m *Model) {
			g := GenericDef{GIN: GINDef{1, 1}, Description: "X", Type: 3, Size: 1, Value: 1}
			m.Generic = []GenericDef{g, g}
		},
		"gen desc": func(m *Model) {
			m.Generic = []GenericDef{{GIN: GINDef{1, 1}, Description: "\x01", Type: 3, Size: 1, Value: 1}}
		},
		"gen value": func(m *Model) {
			m.Generic = []GenericDef{{GIN: GINDef{1, 1}, Description: "X", Type: 3, Size: 1, Value: 256}}
		},
		"many faults": func(m *Model) { m.Disturbances = make([]Record, 9) },
		"fan twice":   func(m *Model) { m.Disturbances = []Record{good, good} },
		"fan range":   func(m *Model) { r := good; r.FAN = 70000; m.Disturbances = []Record{r} },
		"fault time":  func(m *Model) { r := good; r.Time = "yesterday"; m.Disturbances = []Record{r} },
		"start time":  func(m *Model) { r := good; r.Start = "noon"; m.Disturbances = []Record{r} },
		"tag order":   func(m *Model) { r := good; r.Tags = []TagDef{{Pos: 5}, {Pos: 1}}; m.Disturbances = []Record{r} },
		"tag dpi":     func(m *Model) { r := good; r.Tags = []TagDef{{DPI: 7}}; m.Disturbances = []Record{r} },
		"acc order":   func(m *Model) { r := good; r.Channels = []ChannelDef{{ACC: 2}, {ACC: 1}}; m.Disturbances = []Record{r} },
		"noe": func(m *Model) {
			r := good
			r.Channels = []ChannelDef{{ACC: 1, Values: []int{1}}, {ACC: 2}}
			m.Disturbances = []Record{r}
		},
		"sdv": func(m *Model) {
			r := good
			r.Channels = []ChannelDef{{ACC: 1, Values: []int{40000}}}
			m.Disturbances = []Record{r}
		},
		"rpv": func(m *Model) {
			r := good
			r.Channels = []ChannelDef{{ACC: 1, RPV: 1e300}}
			m.Disturbances = []Record{r}
		},
		"channels": func(m *Model) { r := good; r.Channels = make([]ChannelDef, 256); m.Disturbances = []Record{r} },
	} {
		if err := edit(t, f); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestActual(t *testing.T) {
	for _, g := range []GenericDef{
		{Type: 1, Size: 3, Value: "abc"},
		{Type: 3, Size: 4, Value: json.Number("4294967295")},
		{Type: 4, Size: 1, Value: -128},
		{Type: 4, Size: 2, Value: int64(-20)},
		{Type: 3, Size: 2, Value: uint64(5)},
		{Type: 3, Size: 2, Value: 5.0},
		{Type: 7, Size: 4, Value: json.Number("1.5")},
		{Type: 8, Size: 8, Value: 1e300},
	} {
		if _, err := g.Actual(); err != nil {
			t.Errorf("%+v: %v", g, err)
		}
	}
	for _, g := range []GenericDef{
		{Type: 1, Size: 2, Value: "abc"}, {Type: 1, Size: 1, Value: 1}, {Type: 3, Size: 3, Value: 1},
		{Type: 3, Size: 1, Value: -1}, {Type: 4, Size: 1, Value: 128}, {Type: 3, Size: 2, Value: 1.5},
		{Type: 3, Size: 2, Value: "x"}, {Type: 3, Size: 2, Value: json.Number("x")}, {Type: 7, Size: 2, Value: 1},
		{Type: 7, Size: 4, Value: 1e300}, {Type: 8, Size: 4, Value: 1}, {Type: 8, Size: 8, Value: "x"}, {Type: 22, Size: 1, Value: 1},
	} {
		if _, err := g.Actual(); err == nil {
			t.Errorf("%+v accepted", g)
		}
	}
}

func TestSmallHelpers(t *testing.T) {
	if clamp16(70000) != 32767 || clamp16(-70000) != -32768 || clamp16(5) != 5 {
		t.Fatal("clamp")
	}
	if n, err := parseAddress("", "x", -1); err != nil || n != -1 {
		t.Fatal(n, err)
	}
	if n, err := parseAddress(" 7 ", "x", -1); err != nil || n != 7 {
		t.Fatal(n, err)
	}
	for _, s := range []string{"255", "-1", "x"} {
		if _, err := parseAddress(s, "x", -1); err == nil {
			t.Errorf("%q accepted", s)
		}
	}
	r := Record{SOF: SOFDef{TP: true, TM: true, Test: true, OTEV: true}}
	if r.sof() != iec103.SOFTrip|iec103.SOFTM|iec103.SOFTest|iec103.SOFOTEV {
		t.Fatal("sof")
	}
}
