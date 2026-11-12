// SPDX-License-Identifier: MIT

package station

import (
	"math/rand/v2"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/otfabric/go-iec104/asdu"
)

func TestDemoModel(t *testing.T) {
	m := Demo()
	if m.Station.CommonAddress != 1 || len(m.Points) < 10 || len(m.Commands) != 7 || len(m.Files) != 2 || !m.Uploads.Accept {
		t.Fatalf("demo model: %+v", m.Station)
	}
	// Both encodings of the model read back as the same model.
	for _, asJSON := range []bool{false, true} {
		data, err := m.Encode(asJSON)
		if err != nil {
			t.Fatal(err)
		}
		back, err := Parse(data)
		if err != nil || back.Validate() != nil {
			t.Fatalf("json=%v: the encoded model does not parse: %v\n%s", asJSON, err, data)
		}
		if len(back.Points) != len(m.Points) || time.Duration(back.Station.CyclicInterval) != 30*time.Second ||
			back.Points[1].Simulate == nil || time.Duration(back.Points[1].Simulate.Interval) != 15*time.Second {
			t.Errorf("json=%v: the model changed in a round trip", asJSON)
		}
	}
}

func TestModelValidation(t *testing.T) {
	base := "station: {common_address: 1}\npoints:\n  - {ioa: 1, type: M_SP_NA_1, value: true}\n"
	for name, c := range map[string]struct{ model, want string }{
		"empty":             {"", "the model is empty"},
		"unknown field":     {"station: {common_address: 1, colour: red}", "colour"},
		"not yaml":          {"station: [", ""},
		"bad duration":      {"station: {common_address: 1, cyclic_interval: soon}", "invalid duration"},
		"duration number":   {"station: {common_address: 1, cyclic_interval: [1]}", "duration"},
		"broadcast address": {"station: {common_address: 65535}\npoints: [{ioa: 1, type: M_SP_NA_1, value: true}]", "use 1-65534"},
		"fast cyclic":       {"station: {common_address: 1, cyclic_interval: 1ms}\npoints: [{ioa: 1, type: M_SP_NA_1, value: true}]", "at least 100ms"},
		"no points":         {"station: {common_address: 1}", "at least one point"},
		"point type":        {"station: {common_address: 1}\npoints: [{ioa: 1, type: M_XX, value: 1}]", "type \"M_XX\""},
		"ioa zero":          {"station: {common_address: 1}\npoints: [{ioa: 0, type: M_SP_NA_1, value: true}]", "ioa 0"},
		"ioa twice":         {base + "  - {ioa: 1, type: M_DP_NA_1, value: on}\n", "already used by points[0]"},
		"single value":      {"station: {common_address: 1}\npoints: [{ioa: 1, type: M_SP_NA_1, value: 7}]", "not a single point state"},
		"double value":      {"station: {common_address: 1}\npoints: [{ioa: 1, type: M_DP_NA_1, value: half}]", "not a double point state"},
		"not a number":      {"station: {common_address: 1}\npoints: [{ioa: 1, type: M_ME_NC_1, value: lots}]", "not a number"},
		"out of range":      {"station: {common_address: 1}\npoints: [{ioa: 1, type: M_ST_NA_1, value: 64}]", "out of range"},
		"fraction":          {"station: {common_address: 1}\npoints: [{ioa: 1, type: M_ME_NB_1, value: 1.5}]", "not a whole number"},
		"normalized range":  {"station: {common_address: 1}\npoints: [{ioa: 1, type: M_ME_NA_1, value: 1}]", "out of range"},
		"quality":           {"station: {common_address: 1}\npoints: [{ioa: 1, type: M_SP_NA_1, value: true, quality: [XX]}]", "quality \"XX\""},
		"group":             {"station: {common_address: 1}\npoints: [{ioa: 1, type: M_SP_NA_1, value: true, group: 17}]", "interrogation group is 1-16"},
		"counter group":     {"station: {common_address: 1}\npoints: [{ioa: 1, type: M_IT_NA_1, value: 1, group: 5}]", "counter group is 1-4"},
		"sim interval":      {"station: {common_address: 1}\npoints: [{ioa: 1, type: M_SP_NA_1, value: true, simulate: {mode: toggle, interval: 1ms}}]", "at least 50ms"},
		"sim toggle number": {"station: {common_address: 1}\npoints: [{ioa: 1, type: M_ME_NC_1, value: 1, simulate: {mode: toggle, interval: 1s}}]", "mode toggle is for single and double points"},
		"sim ramp state":    {"station: {common_address: 1}\npoints: [{ioa: 1, type: M_SP_NA_1, value: true, simulate: {mode: ramp, interval: 1s, max: 2}}]", "numeric points"},
		"sim count state":   {"station: {common_address: 1}\npoints: [{ioa: 1, type: M_DP_NA_1, value: on, simulate: {mode: count, interval: 1s}}]", "numeric points"},
		"sim range":         {"station: {common_address: 1}\npoints: [{ioa: 1, type: M_ME_NC_1, value: 1, simulate: {mode: sine, interval: 1s, min: 5, max: 5}}]", "max above min"},
		"sim mode":          {"station: {common_address: 1}\npoints: [{ioa: 1, type: M_ME_NC_1, value: 1, simulate: {mode: wobble, interval: 1s}}]", "mode \"wobble\""},
		"sim step":          {"station: {common_address: 1}\npoints: [{ioa: 1, type: M_IT_NA_1, value: 1, simulate: {mode: count, interval: 1s, step: -1}}]", "step cannot be negative"},
		"command type":      {base + "commands: [{ioa: 2, type: C_XX}]", "type \"C_XX\""},
		"command target":    {base + "commands: [{ioa: 2, type: C_SC_NA_1, target: 9}]", "target 9 is not a point"},
		"command mismatch":  {base + "commands: [{ioa: 2, type: C_SE_NC_1, target: 1}]", "cannot set"},
		"command ioa":       {base + "commands: [{ioa: 1, type: C_SC_NA_1}]", "already used"},
		"bitstring select":  {base + "commands: [{ioa: 2, type: C_BO_NA_1, select_required: true}]", "has no select"},
		"file sources":      {base + "files: [{ioa: 3, name: 1, size: 10, content: x}]", "exactly one of path, content and size"},
		"file none":         {base + "files: [{ioa: 3, name: 1}]", "exactly one of"},
		"file negative":     {base + "files: [{ioa: 3, name: 1, size: 10, section_size: -1}]", "cannot be negative"},
		"file missing":      {base + "files: [{ioa: 3, name: 1, path: /no/such/file}]", "no such file"},
		"file sections":     {base + "files: [{ioa: 3, name: 1, size: 1000, section_size: 1}]", "more than 254 sections"},
		"upload dir":        {base + "uploads: {dir: x}", "uploads.accept is not"},
	} {
		t.Run(name, func(t *testing.T) {
			m, err := Parse([]byte(c.model))
			if err == nil {
				err = m.Validate()
			}
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Errorf("error %v, want one that mentions %q", err, c.want)
			}
		})
	}

	// Everything that is wrong is reported at once.
	m, err := Parse([]byte("station: {common_address: 0}\npoints: [{ioa: 0, type: M_XX, value: 1}]\ncommands: [{ioa: 0, type: C_XX}]"))
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Count(m.Validate().Error(), "\n  - "); got != 5 {
		t.Errorf("%d problems reported, want 5:\n%v", got, m.Validate())
	}
	if _, err := New(m, Options{}); err == nil {
		t.Error("a station was built from an invalid model")
	}
}

func TestValuesOfAModel(t *testing.T) {
	for _, c := range []struct {
		typ  asdu.TypeID
		in   any
		want value
	}{
		{asdu.M_SP_NA_1, "on", value{b: true}},
		{asdu.M_SP_NA_1, "OFF", value{}},
		{asdu.M_SP_NA_1, 1, value{b: true}},
		{asdu.M_DP_NA_1, true, value{d: asdu.DoubleOn}},
		{asdu.M_DP_NA_1, false, value{d: asdu.DoubleOff}},
		{asdu.M_DP_NA_1, "intermediate", value{d: asdu.DoubleIntermediate}},
		{asdu.M_DP_NA_1, "indeterminate", value{d: asdu.DoubleIndeterminate}},
		{asdu.M_DP_NA_1, 2, value{d: asdu.DoubleOn}},
		{asdu.M_BO_NA_1, "0xFF", value{f: 255}},
		{asdu.M_ME_NC_1, "1.5", value{f: 1.5}},
		{asdu.M_IT_NA_1, int64(5), value{f: 5}},
		{asdu.M_IT_NA_1, uint64(6), value{f: 6}},
		{asdu.M_ST_NA_1, -64, value{f: -64}},
	} {
		got, err := parseValue(c.typ, c.in)
		if err != nil || got != c.want {
			t.Errorf("parseValue(%s, %v) = %+v, %v; want %+v", c.typ, c.in, got, err, c.want)
		}
	}
	if _, err := parseValue(asdu.M_ME_NC_1, []int{1}); err == nil {
		t.Error("a list was taken for a number")
	}
}

func TestLoadResolvesPathsBesideTheModel(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "data.bin"), []byte("from disk"), 0o600); err != nil {
		t.Fatal(err)
	}
	model := "station: {common_address: 3, name: On disk}\npoints: [{ioa: 1, type: M_SP_NA_1, value: true, time_tag: false}]\n" +
		"files: [{ioa: 9, name: 1, path: data.bin}]\nuploads: {accept: true, dir: uploads}\n"
	path := filepath.Join(dir, "station.yaml")
	if err := os.WriteFile(path, []byte(model), 0o600); err != nil {
		t.Fatal(err)
	}
	m, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	st, err := New(m, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if st.CommonAddr() != 3 || st.name() != "On disk" {
		t.Errorf("station %d %q", st.CommonAddr(), st.name())
	}
	if data, ok := st.Uploaded(9); !ok || string(data) != "from disk" {
		t.Errorf("file of the model: %q, %v", data, ok)
	}
	if info, err := os.Stat(filepath.Join(dir, "uploads")); err != nil || !info.IsDir() {
		t.Errorf("the upload directory was not created beside the model: %v", err)
	}
	// A delivered file is written there.
	file, _ := st.openFile(nil, 3, 9, 1)
	if err := st.StoreFile(nil, 3, 77, 4, file); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(filepath.Join(dir, "uploads", "ioa-77-name-4.bin")); err != nil || string(data) != "from disk" {
		t.Errorf("stored upload: %q, %v", data, err)
	}
	if st.AcceptFile(nil, 3, 1, 1, 10) || !st.AcceptFile(nil, 3, 78, 1, 10) || st.AcceptFile(nil, 4, 78, 1, 10) {
		t.Error("AcceptFile: a point's address or another station was accepted, or a free address refused")
	}
	if _, ok := st.openFile(nil, 3, 9, 2); ok {
		t.Error("a file was opened under another name")
	}
	if st.Set(99, 1) || st.Set(1, "maybe") {
		t.Error("Set accepted an unknown point or a value the point cannot hold")
	}
	if _, ok := st.Value(99); ok {
		t.Error("Value of an unknown point")
	}
	if _, ok := st.Uploaded(99); ok {
		t.Error("Uploaded of an unknown file")
	}

	if _, err := Load(filepath.Join(dir, "missing.yaml")); err == nil {
		t.Error("a missing model file was loaded")
	}
	_ = os.WriteFile(path, []byte("station: {common_address: 0}"), 0o600)
	if _, err := Load(path); err == nil || !strings.Contains(err.Error(), path) {
		t.Errorf("error of an invalid file does not name it: %v", err)
	}
	_ = os.WriteFile(path, []byte("station: ["), 0o600)
	if _, err := Load(path); err == nil {
		t.Error("a broken model file was loaded")
	}
}

func TestSimulationSteps(t *testing.T) {
	rng := rand.New(rand.NewPCG(1, 2))
	step := func(typ asdu.TypeID, v value, sim SimSpec, n int) value {
		p := &point{typ: typ, val: v, spec: PointSpec{Simulate: &sim}}
		for range n {
			p.step(rng)
		}
		return p.val
	}
	if got := step(asdu.M_SP_NA_1, value{}, SimSpec{Mode: "toggle"}, 3); !got.b {
		t.Errorf("single point after three toggles: %+v", got)
	}
	if got := step(asdu.M_DP_NA_1, value{d: asdu.DoubleOn}, SimSpec{Mode: "toggle"}, 1); got.d != asdu.DoubleOff {
		t.Errorf("double point after a toggle: %+v", got)
	}
	if got := step(asdu.M_DP_NA_1, value{d: asdu.DoubleIntermediate}, SimSpec{Mode: "toggle"}, 1); got.d != asdu.DoubleOn {
		t.Errorf("double point in an intermediate state after a toggle: %+v", got)
	}
	// A ramp climbs by its step and starts over at min.
	if got := step(asdu.M_ME_NB_1, value{f: 8}, SimSpec{Mode: "ramp", Min: 0, Max: 10, Step: 2}, 1); got.f != 10 {
		t.Errorf("ramp: %+v", got)
	}
	if got := step(asdu.M_ME_NB_1, value{f: 10}, SimSpec{Mode: "ramp", Min: 0, Max: 10}, 1); got.f != 0 {
		t.Errorf("ramp past max: %+v", got)
	}
	// A sine stays within its bounds and comes back after a period.
	if got := step(asdu.M_ME_NC_1, value{}, SimSpec{Mode: "sine", Min: 10, Max: 20, Step: 4}, 1); got.f != 20 {
		t.Errorf("sine at a quarter period: %+v", got)
	}
	if got := step(asdu.M_ME_NC_1, value{}, SimSpec{Mode: "sine", Min: 10, Max: 20}, 60); got.f < 14.99 || got.f > 15.01 {
		t.Errorf("sine after a whole default period: %+v", got)
	}
	for range 50 {
		if got := step(asdu.M_ME_NC_1, value{}, SimSpec{Mode: "random", Min: 49, Max: 51}, 1); got.f < 49 || got.f > 51 {
			t.Fatalf("random value out of bounds: %+v", got)
		}
	}
	// Whole-number types are rounded, and a counter overflows to zero.
	if got := step(asdu.M_ST_NA_1, value{}, SimSpec{Mode: "random", Min: 0.2, Max: 0.4}, 1); got.f != 0 {
		t.Errorf("step position from a random fraction: %+v", got)
	}
	if got := step(asdu.M_IT_NA_1, value{f: 5}, SimSpec{Mode: "count"}, 2); got.f != 7 {
		t.Errorf("counter: %+v", got)
	}
	if got := step(asdu.M_IT_NA_1, value{f: 2147483647}, SimSpec{Mode: "count", Step: 5}, 1); got.f != 0 {
		t.Errorf("counter at its maximum: %+v", got)
	}
}

func TestCommandsOnTargets(t *testing.T) {
	for _, c := range []struct {
		typ  asdu.TypeID
		from value
		cmd  asdu.InformationObject
		want value
	}{
		{asdu.M_ST_NA_1, value{f: 63}, asdu.StepCommand{Value: asdu.StepHigher}, value{f: 63}},
		{asdu.M_ST_NA_1, value{f: -64}, asdu.StepCommand{Value: asdu.StepLower}, value{f: -64}},
		{asdu.M_ME_NB_1, value{}, asdu.SetpointFloat{Value: 12.6}, value{f: 13}},
		{asdu.M_ME_NB_1, value{}, asdu.SetpointFloat{Value: 1e9}, value{f: 32767}},
		{asdu.M_ME_NA_1, value{}, asdu.SetpointScaled{Value: 5}, value{f: 32767.0 / 32768}},
		{asdu.M_ME_NC_1, value{}, asdu.SetpointNormalized{Value: 16384}, value{f: 0.5}},
		{asdu.M_BO_NA_1, value{}, asdu.BitstringCommand{Value: 0xFFFFFFFF}, value{f: 4294967295}},
	} {
		p := &point{typ: c.typ, val: c.from}
		p.apply(c.cmd)
		if p.val != c.want {
			t.Errorf("%T on %s: %+v, want %+v", c.cmd, c.typ, p.val, c.want)
		}
	}
	if valid(asdu.DoubleCommand{Value: asdu.DoubleIntermediate}) || valid(asdu.StepCommand{}) || !valid(asdu.SingleCommand{}) {
		t.Error("valid: a command the station cannot carry out was accepted")
	}
	if baseType(asdu.C_SE_TC_1) != asdu.C_SE_NC_1 || baseType(asdu.C_IC_NA_1) != asdu.C_IC_NA_1 {
		t.Error("baseType")
	}
	if v, sel, key := commanded(asdu.Read{}); v != nil || sel || key != "" {
		t.Error("commanded: a read command was taken for a process command")
	}
}
