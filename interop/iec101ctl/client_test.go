// SPDX-License-Identifier: MIT

//go:build interop

package interop

import (
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/otfabric/go-iec60870-5/asdu"
)

// point is a point of iec101ctl's JSON output.
type point struct {
	CommonAddr int            `json:"ca"`
	IOA        int            `json:"ioa"`
	Type       string         `json:"type"`
	Cause      string         `json:"cause"`
	Value      any            `json:"value"`
	Quality    string         `json:"quality"`
	Attributes map[string]any `json:"attributes"`
}

// want is the value of a fixture point as iec101ctl writes it, and the attribute that
// carries what the value does not (the raw NVA, the sequence number).
func want(t *testing.T, typ string, raw json.RawMessage, seq int) (value any, attr string, attrValue any) {
	t.Helper()
	if typ == "M_SP_NA_1" {
		var b bool
		if err := json.Unmarshal(raw, &b); err != nil {
			t.Fatal(err)
		}
		return b, "", nil
	}
	var n float64
	if err := json.Unmarshal(raw, &n); err != nil {
		t.Fatal(err)
	}
	switch typ {
	case "M_DP_NA_1":
		return asdu.DoubleState(int(n)).String(), "", nil
	case "M_ME_NA_1":
		return float64(asdu.Normalized(int16(n)).Float64()), "raw", n
	case "M_ME_NC_1":
		return float64(float32(n)), "", nil
	case "M_IT_NA_1":
		return n, "sequence", float64(seq)
	}
	return n, "", nil
}

// sameQuality compares iec101ctl's quality ("good" or flags joined with |) with the
// fixture's list of flags.
func sameQuality(got string, flags []string) bool {
	if len(flags) == 0 {
		return got == "good" || got == ""
	}
	g := strings.Split(got, "|")
	slices.Sort(g)
	w := slices.Clone(flags)
	slices.Sort(w)
	return slices.Equal(g, w)
}

// iec101ctl client against the server of every image, in every link mode it declares,
// with the default field sizes and every non-default set the image takes.
func TestClientAgainstReference(t *testing.T) {
	for _, a := range adapters(t) {
		f := loadFixture(t, a)
		for _, mode := range modes {
			if !slices.Contains(a.caps.LinkModes, mode) {
				continue
			}
			t.Run(a.name+"/"+mode+"/default", func(t *testing.T) { clientScenario(t, a, f, mode, defaultFields(), true) })
			for _, run := range fieldSizeRuns {
				if !a.takes(run.f, mode) {
					continue
				}
				t.Run(a.name+"/"+mode+"/"+run.name, func(t *testing.T) { clientScenario(t, a, f, mode, run.f, false) })
			}
		}
	}
}

func clientScenario(t *testing.T, a adapter, f *fixture, mode string, fs fields, full bool) {
	port := startImageServer(t, a, fs.flags(mode)...)
	ca := strconv.Itoa(f.Station.CommonAddress)
	base := append([]string{"--ip", "127.0.0.1", "--port", strconv.Itoa(port), "--link-timeout", "500", "--poll-interval", "20", "--timeout", "15000"}, fs.flags(mode)...)
	run := func(sub string, args ...string) (string, string, int) {
		t.Helper()
		all := append([]string{"client", sub}, base...)
		if sub != "scan" && sub != "monitor" {
			all = append(all, "--ca", ca)
		}
		return cli(t, append(all, args...)...)
	}

	out, errOut, code := run("interrogate", "--format", "json")
	if code != 0 {
		if bug := knownBug(t, a, "server", mode, []string{"interrogate"}); bug != "" {
			t.Skipf("known bug of %s %s (exit %d): %s", a.name, a.caps.Upstream.Version, code, bug)
		}
		t.Fatalf("interrogate: exit %d\n%s%s", code, out, errOut)
	}
	if bug := knownBug(t, a, "server", mode, []string{"interrogate"}); bug != "" {
		t.Errorf("known bug of %s %s did not reproduce: %s", a.name, a.caps.Upstream.Version, bug)
	}
	var gi struct {
		Received int     `json:"received"`
		Points   []point `json:"points"`
	}
	if err := json.Unmarshal([]byte(out), &gi); err != nil {
		t.Fatal(err)
	}
	got := map[int]point{}
	for _, p := range gi.Points {
		if p.Cause != "interrogated-station" || p.CommonAddr != f.Station.CommonAddress {
			t.Errorf("point %+v", p)
		}
		got[p.IOA] = p
	}
	checkPoint := func(t *testing.T, p point, fp struct {
		IOA      int             `json:"ioa"`
		Type     string          `json:"type"`
		Value    json.RawMessage `json:"value"`
		Quality  []string        `json:"quality"`
		Sequence int             `json:"sequence"`
	}) {
		t.Helper()
		v, attr, av := want(t, fp.Type, fp.Value, fp.Sequence)
		if p.Type != fp.Type || fmt.Sprint(p.Value) != fmt.Sprint(v) || !sameQuality(p.Quality, fp.Quality) {
			t.Errorf("IOA %d: %s %v [%s], want %s %v %v", fp.IOA, p.Type, p.Value, p.Quality, fp.Type, v, fp.Quality)
		}
		if attr != "" && fmt.Sprint(p.Attributes[attr]) != fmt.Sprint(av) {
			t.Errorf("IOA %d: %s %v, want %v", fp.IOA, attr, p.Attributes[attr], av)
		}
	}
	for _, fp := range f.Points {
		if fp.Type == "M_IT_NA_1" {
			if _, ok := got[fp.IOA]; ok {
				t.Errorf("an integrated total in the interrogation")
			}
			continue
		}
		checkPoint(t, got[fp.IOA], fp)
	}
	if gi.Received != len(f.Points)-1 {
		t.Errorf("%d objects, want %d", gi.Received, len(f.Points)-1)
	}

	// The broadcast common address of the field.
	bc := strconv.Itoa(1<<(8*fs.ca) - 1)
	if out, errOut, code := cli(t, append(append([]string{"client", "interrogate"}, base...), "--ca", bc, "--format", "json")...); code != 0 || !strings.Contains(out, `"received": `+strconv.Itoa(len(f.Points)-1)) {
		t.Errorf("broadcast interrogation: exit %d\n%s%s", code, out, errOut)
	}

	out, _, code = run("counters", "--format", "json")
	var ci struct {
		Points []point `json:"points"`
	}
	if code != 0 || json.Unmarshal([]byte(out), &ci) != nil || len(ci.Points) != 1 {
		t.Fatalf("counters: exit %d %s", code, out)
	}
	for _, fp := range f.Points {
		if fp.Type == "M_IT_NA_1" {
			checkPoint(t, ci.Points[0], fp)
		}
	}

	unknown := "999"
	if fs.ioa == 3 {
		unknown = "70000" // only a three-octet address carries it
	}
	out, _, code = run("read", "--ioa", "202,"+unknown, "--format", "json")
	var rd struct {
		Failed int `json:"failed"`
		Rows   []struct {
			Point *point `json:"point"`
			Error *struct {
				Code string `json:"code"`
			} `json:"error"`
		} `json:"rows"`
	}
	if code != 7 || json.Unmarshal([]byte(out), &rd) != nil || rd.Failed != 1 || rd.Rows[0].Point == nil || rd.Rows[1].Error == nil || rd.Rows[1].Error.Code != "unknown_object_address" {
		t.Fatalf("read: exit %d %s", code, out)
	}

	if out, errOut, code := run("command", "--type", "setpoint-float", "--ioa", "502", "--value", "49.5", "--wait-termination", "5000"); code != 0 || !strings.Contains(out, "received") {
		t.Fatalf("set point: exit %d %s%s", code, out, errOut)
	}
	if out, _, code := run("read", "--ioa", "202", "--format", "json"); code != 0 || !strings.Contains(out, `"value": 49.5`) {
		t.Errorf("read after the set point: exit %d %s", code, out)
	}
	if _, _, code := run("interrogate", "--ca", "7"); code != 6 {
		t.Errorf("unknown common address: exit %d", code)
	}
	if !full {
		return
	}

	if _, _, code := run("clock"); code != 0 {
		t.Errorf("clock: exit %d", code)
	}
	if out, errOut, code := run("ping", "--count", "2", "--interval", "50"); code != 0 {
		t.Errorf("ping: exit %d %s%s", code, out, errOut)
	}
	if out, errOut, code := run("ping", "--count", "1", "--method", "link-status"); code != 0 {
		t.Errorf("ping link-status: exit %d %s%s", code, out, errOut)
	}
	if out, errOut, code := run("command", "--type", "single", "--ioa", "510", "--value", "on"); code != 6 {
		t.Errorf("execute without select: exit %d %s%s", code, out, errOut)
	}
	for _, args := range [][]string{
		{"--type", "single", "--ioa", "510", "--value", "off", "--mode", "sbo", "--wait-termination", "5000"},
		{"--type", "single", "--ioa", "510", "--value", "on", "--mode", "cancel"},
		{"--type", "double", "--ioa", "501", "--value", "off", "--mode", "sbo"},
		{"--type", "step", "--ioa", "505", "--value", "higher"},
		{"--type", "setpoint-scaled", "--ioa", "504", "--value", "-7"},
		{"--type", "setpoint-normalized", "--ioa", "503", "--value", "-0.5"},
	} {
		if out, errOut, code := run("command", args...); code != 0 {
			t.Errorf("command %v: exit %d %s%s", args, code, out, errOut)
		}
	}
	if _, _, code := run("command", "--type", "double", "--ioa", "500", "--value", "on"); code != 6 {
		t.Errorf("a command of the wrong type: exit %d", code)
	}
	if out, _, code := run("read", "--ioa", "100,101,102,200,201", "--format", "json"); code != 0 ||
		!strings.Contains(out, `"value": false`) || !strings.Contains(out, `"value": "off"`) || !strings.Contains(out, `"value": 6`) || !strings.Contains(out, `"value": -7`) || !strings.Contains(out, `"value": -0.5`) {
		t.Errorf("read after the commands: exit %d %s", code, out)
	}
	if out, errOut, code := run("scan", "--method", "ca", "--start", "1", "--end", "2", "--timeout", "3000", "--format", "json"); code != 0 || !strings.Contains(out, `"found": 1`) {
		t.Errorf("scan: exit %d %s%s", code, out, errOut)
	}
	if mode == "unbalanced" {
		if out, errOut, code := run("scan", "--method", "link", "--start", "0", "--end", "2", "--link-timeout", "300", "--format", "json"); code != 0 || !strings.Contains(out, `"found": 1`) {
			t.Errorf("link scan: exit %d %s%s", code, out, errOut)
		}
	}
	if out, errOut, code := run("monitor", "--duration", "1", "--format", "json"); code != 0 {
		t.Errorf("monitor: exit %d %s%s", code, out, errOut)
	}
}
