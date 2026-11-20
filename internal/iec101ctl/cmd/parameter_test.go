// SPDX-License-Identifier: MIT

package cmd

import (
	"testing"

	"github.com/otfabric/iec60870ctl/internal/iec101ctl/stationtest"
	"github.com/otfabric/iec60870ctl/internal/iec101ctl/types"
)

// client parameter loads the three parameters of measured value and the activations, and
// the station confirms them; what it refuses ends with exit 6.
func TestClientParameter(t *testing.T) {
	st := stationtest.Start(t, stationtest.Model, nil)

	var res, act types.ParameterResult
	cli(t, st, "parameter", "--type", "float", "--ioa", "1000", "--value", "0.5", "--qualifier", "threshold", "--format", "json").want(t, 0).decode(t, &res)
	if res.Type != "P_ME_NC_1" || res.IOA != 1000 || res.Qualifier != "threshold" || res.Value != 0.5 {
		t.Errorf("float threshold: %+v", res)
	}
	cli(t, st, "parameter", "--type", "scaled", "--ioa", "1002", "--value", "1200", "--qualifier", "high-limit").want(t, 0).
		has(t, "Parameter high-limit = 1200 (P_ME_NB_1) of IOA 1002")
	cli(t, st, "param", "--type", "P_ME_NA_1", "--ioa", "1003", "--value", "-0.25", "--qualifier", "low-limit", "--format", "table").want(t, 0).
		has(t, "P_ME_NA_1", "low-limit", "-0.25", "confirmed")
	cli(t, st, "parameter", "--type", "normalized", "--ioa", "1000", "--value", "0.1", "--qualifier", "smoothing").want(t, 0)
	cli(t, st, "parameter", "--type", "activation", "--ioa", "1000", "--qualifier", "object").want(t, 0).
		has(t, "Parameter activation (object) of IOA 1000")
	cli(t, st, "parameter", "--type", "activation", "--ioa", "1000", "--qualifier", "cyclic", "--format", "json").want(t, 0).decode(t, &act)
	if act.Value != nil || act.Qualifier != "cyclic" || act.Type != "P_AC_NA_1" {
		t.Errorf("cyclic activation: %+v", act)
	}
	cli(t, st, "parameter", "--type", "activation", "--ioa", "0", "--qualifier", "loaded", "--format", "table").want(t, 0).has(t, "P_AC_NA_1", "loaded")
	for _, d := range []string{"loaded: threshold", "loaded: high-limit", "loaded: low-limit", "loaded: smoothing",
		"activated: object parameter", "activated: cyclic transmission", "activated: loaded parameters"} {
		if !hasParameterEvent(st, d) {
			t.Errorf("no parameter event %q in %+v", d, st.Events())
		}
	}

	// Refusals: an object that is no measured value, an unknown address, an activation
	// of an unknown object.
	for _, c := range []struct {
		args []string
		code string
	}{
		{[]string{"--type", "float", "--ioa", "400", "--value", "1", "--qualifier", "threshold"}, "negative_confirmation"},
		{[]string{"--type", "float", "--ioa", "9999", "--value", "1", "--qualifier", "threshold"}, "unknown_object_address"},
		{[]string{"--type", "activation", "--ioa", "9999", "--qualifier", "object"}, "unknown_object_address"},
	} {
		r := cli(t, st, "parameter", append(c.args, "--format", "json")...).want(t, 6)
		if _, code := r.errorCode(t); code != c.code {
			t.Errorf("%v: code %s, want %s", c.args, code, c.code)
		}
	}
	if !hasParameterEvent(st, "refused: the object is M_BO_NA_1, not a measured value") {
		t.Errorf("no refusal of IOA 400 in %+v", st.Events())
	}
}

// Invalid parameters are refused with exit 3 before anything is sent; a missing required
// flag is a usage error.
func TestClientParameterInvalid(t *testing.T) {
	st := stationtest.Start(t, stationtest.Model, nil)
	for _, args := range [][]string{
		{"--type", "percent", "--ioa", "1000", "--value", "1", "--qualifier", "threshold"},
		{"--type", "float", "--ioa", "0", "--value", "1", "--qualifier", "threshold"},
		{"--type", "float", "--ioa", "16777216", "--value", "1", "--qualifier", "threshold"},
		{"--type", "float", "--ioa", "1000", "--qualifier", "threshold"},
		{"--type", "float", "--ioa", "1000", "--value", "NaN", "--qualifier", "threshold"},
		{"--type", "float", "--ioa", "1000", "--value", "1e39", "--qualifier", "threshold"},
		{"--type", "float", "--ioa", "1000", "--value", "x", "--qualifier", "threshold"},
		{"--type", "float", "--ioa", "1000", "--value", "1", "--qualifier", "object"},
		{"--type", "normalized", "--ioa", "1000", "--value", "1", "--qualifier", "threshold"},
		{"--type", "scaled", "--ioa", "1000", "--value", "1.5", "--qualifier", "threshold"},
		{"--type", "scaled", "--ioa", "1000", "--value", "40000", "--qualifier", "threshold"},
		{"--type", "activation", "--ioa", "1000", "--value", "1", "--qualifier", "object"},
		{"--type", "activation", "--ioa", "1000", "--qualifier", "threshold"},
		{"--type", "activation", "--ioa", "1000", "--qualifier", "loaded"},
		{"--type", "activation", "--ioa", "0", "--qualifier", "object"},
		{"--ca", "0", "--type", "float", "--ioa", "1000", "--value", "1", "--qualifier", "threshold"},
	} {
		r := cli(t, st, "parameter", append(args, "--format", "json")...)
		if r.code != 3 {
			t.Errorf("%v: exit %d, want 3\n%s%s", args, r.code, r.stdout, r.stderr)
			continue
		}
		if kind, _ := r.errorCode(t); kind != "invalid_input" {
			t.Errorf("%v: kind %s", args, kind)
		}
	}
	cli(t, st, "parameter", "--type", "float", "--value", "1", "--qualifier", "threshold").want(t, 2)
	if hasParameterEvent(st, "") {
		t.Errorf("an invalid parameter reached the station: %+v", st.Events())
	}
}

// hasParameterEvent reports whether st logged a parameter event with this detail, or any
// parameter event for "".
func hasParameterEvent(st *stationtest.Station, detail string) bool {
	for _, e := range st.Events() {
		if e.Event == "parameter" && (detail == "" || e.Detail == detail) {
			return true
		}
	}
	return false
}
