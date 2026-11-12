// SPDX-License-Identifier: MIT

package station

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestModelValidation(t *testing.T) {
	m, err := Parse([]byte(`
station: {common_address: 65535, cyclic_interval: 10ms}
points:
  - {ioa: 0, type: M_SP_NA_1, value: true}
  - {ioa: 1, type: M_XX_NA_1, value: true}
  - {ioa: 2, type: M_SP_NA_1, value: maybe, quality: [XX]}
  - {ioa: 2, type: M_DP_NA_1, value: 9}
  - {ioa: 3, type: M_ME_NB_1, value: 1.5, group: 17}
  - {ioa: 4, type: M_IT_NA_1, value: 1, group: 5, sequence: 32}
  - {ioa: 5, type: M_SP_NA_1, value: true, sequence: 1, simulate: {mode: ramp, interval: 1s}}
  - {ioa: 6, type: M_ME_NC_1, value: 1, simulate: {mode: toggle, interval: 1s}}
  - {ioa: 12, type: M_SP_NA_1, value: true, simulate: {mode: toggle, interval: 10ms}}
  - {ioa: 7, type: M_ME_NC_1, value: 1, simulate: {mode: sine, interval: 1s, min: 2, max: 1}}
  - {ioa: 8, type: M_ME_NC_1, value: 1, simulate: {mode: walk, interval: 1s}}
  - {ioa: 9, type: M_DP_NA_1, value: "on", simulate: {mode: count, interval: 1s}}
  - {ioa: 10, type: M_ME_NC_1, value: 1, simulate: {mode: count, interval: 1s, step: -1}}
  - {ioa: 11, type: M_ME_NA_1, value: 2}
commands:
  - {ioa: 100, type: C_SC_TA_1}
  - {ioa: 101, type: C_BO_NA_1, select_required: true, target: 999}
  - {ioa: 102, type: C_SC_NA_1, target: 3, report_cause: 5}
`))
	if err != nil {
		t.Fatal(err)
	}
	err = m.Validate()
	if err == nil {
		t.Fatal("an invalid model validated")
	}
	for _, want := range []string{"common_address 65535", "cyclic_interval", "ioa 0", "M_XX_NA_1", "not a single point state", "quality \"XX\"",
		"already used", "group 17", "group 5", "sequence 32", "sequence 1", "mode ramp is for numeric", "mode toggle is for single",
		"needs max above min", "mode \"walk\"", "mode count is for numeric", "step cannot be negative", "out of range", "C_SC_TA_1",
		"select_required", "target 999", "report_cause 5", "which a C_SC_NA_1 cannot set"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the problems do not mention %q:\n%v", want, err)
		}
	}
	if _, err := Parse([]byte("")); err == nil {
		t.Error("an empty model parsed")
	}
	if _, err := Parse([]byte("station: {colour: red}")); err == nil {
		t.Error("an unknown field parsed")
	}
	if _, err := Parse([]byte("station: {cyclic_interval: 5}")); err == nil {
		t.Error("a duration as a number parsed")
	}
	if _, err := Parse([]byte("station: {cyclic_interval: soon}")); err == nil {
		t.Error("a duration that is no duration parsed")
	}
}

func TestLoadAndEncode(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "m.json")
	data, err := Demo().Encode(true)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	m, err := Load(path)
	if err != nil || m.Station.Name != "Demo bay" {
		t.Fatalf("load: %v", err)
	}
	if _, err := Load(filepath.Join(dir, "missing.yaml")); err == nil {
		t.Error("a missing file loaded")
	}
	bad := filepath.Join(dir, "bad.yaml")
	if err := os.WriteFile(bad, []byte("station: [\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(bad); err == nil {
		t.Error("a broken file loaded")
	}
	invalid := filepath.Join(dir, "invalid.yaml")
	if err := os.WriteFile(invalid, []byte("station: {common_address: 1}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(invalid); err == nil {
		t.Error("an invalid model loaded")
	}
}
