//go:build interop

// SPDX-License-Identifier: MIT

package interop

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"testing"
)

// ours runs `iec103ctl --debug client <op…>` against the reference server on port and
// returns its JSON result, the frame trace (stderr) and the exit code.
func ours(t *testing.T, port int, v any, op ...string) (trace string, code int) {
	t.Helper()
	n := 0
	for n < len(op) && !strings.HasPrefix(op[n], "-") {
		n++
	}
	args := append([]string{"--debug", "client"}, op[:n]...)
	args = append(args, "--url", "tcp://127.0.0.1:"+strconv.Itoa(port), "--link-address", "1",
		"--link-timeout", "500", "--poll-interval", "20", "--timeout", "5000", "--format", "json")
	args = append(args, op[n:]...)
	out, errOut, code := runBinary(t, args...)
	if code == 0 && v != nil {
		if err := json.Unmarshal([]byte(out), v); err != nil {
			t.Fatalf("%v: %v\n%s", op, err, out)
		}
	}
	return errOut, code
}

// evidence fails unless the frame trace holds the octets a known bug describes; then it
// ends the test as that known difference.
func evidence(t *testing.T, a adapter, op, trace string, hexPrefix string) {
	t.Helper()
	b, ok := knownBugOf(t, a, false, op)
	if !ok {
		t.Fatalf("%s: no known bug of %s %s explains this:\n%s", op, a.name, a.caps.Upstream.Version, tail(trace))
	}
	if !strings.Contains(trace, hexPrefix) {
		t.Fatalf("%s: not the recorded deviation of %s (no %q on the wire):\n%s", op, b.id, hexPrefix, tail(trace))
	}
	knownDifference(t, b, hexPrefix+"…")
}

func tail(s string) string {
	if len(s) > 4000 {
		return "…" + s[len(s)-4000:]
	}
	return s
}

// iec103ctl's controlling system against every reference server (vitusdong is the only
// one): link start-up, identification, clock synchronization, general interrogation,
// general commands, generic services and disturbance data. What the reference does as IEC
// 60870-5-103 says must pass; a deviation known-bugs.tsv records is checked on the wire
// (the --debug trace) and skipped with the bug id, never passed.
func TestClientAgainstReferenceServer(t *testing.T) {
	binary(t)
	var servers []adapter
	for _, a := range adapters(t) {
		if a.caps.Roles.Server {
			servers = append(servers, a)
		}
	}
	if len(servers) == 0 {
		skipOrFail(t, "no reference server selected")
	}
	for _, a := range servers {
		f := loadFixture(t, a)
		t.Run(a.name, func(t *testing.T) { clientScenario(t, a, f) })
	}
}

func clientScenario(t *testing.T, a adapter, f *fixture) {
	port := startReferenceServer(t, a)
	id := f.Station.Identification

	t.Run("link start-up", func(t *testing.T) {
		// Request status of link and reset CU are answered: the scan finds link address 1.
		var r struct {
			Found []struct {
				LinkAddress int `json:"link_address"`
			} `json:"found"`
		}
		if trace, code := ours(t, port, &r, "scan", "--start", "1", "--end", "1"); code != 0 || len(r.Found) != 1 {
			t.Fatalf("exit %d %+v\n%s", code, r, tail(trace))
		}
	})

	t.Run("identification", func(t *testing.T) {
		// Clause 7.4.1, table 8: ASDU 5, cause 4, INF 3, COL, eight characters, software.
		var r struct {
			Identifications []struct {
				Cause        int    `json:"cause"`
				Fun          int    `json:"fun"`
				Inf          int    `json:"inf"`
				COL          int    `json:"col"`
				Manufacturer string `json:"manufacturer"`
				Software     string `json:"software"`
			} `json:"identifications"`
		}
		trace, code := ours(t, port, &r, "identify", "--timeout", "3000")
		if code == 0 {
			got := r.Identifications[0]
			if got.Cause != 4 || got.Fun != id.Fun || got.Inf != 3 || got.COL != id.Col || got.Manufacturer != strings.TrimRight(id.ASCII, " ") || got.Software != id.Software {
				t.Fatalf("%+v, want %+v", got, id)
			}
			return
		}
		// vitusdong: FUN 255, INF 80, eight characters only.
		evidence(t, a, "identification", trace, "05 81 04 01 FF 50")
	})

	t.Run("clock synchronization", func(t *testing.T) {
		// Clause 7.4.2: the addressed ASDU 6 is answered with ASDU 6, cause 8.
		var r struct {
			DeviceTime string `json:"device_time"`
		}
		if trace, code := ours(t, port, &r, "clock"); code != 0 || r.DeviceTime == "" {
			t.Fatalf("exit %d %+v\n%s", code, r, tail(trace))
		}
	})

	var gi struct {
		Count    int `json:"count"`
		Messages []struct {
			Fun int `json:"fun"`
			Inf int `json:"inf"`
			DPI int `json:"dpi"`
		} `json:"messages"`
	}
	var giTrace string
	t.Run("general interrogation termination", func(t *testing.T) {
		// Clause 7.4.3: ASDU 8 with the scan number of the ASDU 7 ends it.
		var code int
		if giTrace, code = ours(t, port, &gi, "interrogate"); code != 0 {
			t.Fatalf("exit %d\n%s", code, tail(giTrace))
		}
	})

	t.Run("general interrogation answers", func(t *testing.T) {
		want := map[[2]int]int{}
		for _, ind := range f.Indications {
			if ind.GI == nil || *ind.GI {
				want[[2]int{ind.Fun, ind.Inf}] = ind.DPI
			}
		}
		got := map[[2]int]int{}
		for _, m := range gi.Messages {
			got[[2]int{m.Fun, m.Inf}] = m.DPI
		}
		if fmt.Sprint(got) == fmt.Sprint(want) {
			return
		}
		// vitusdong: ASDU 1 with cause 11 and the 12-octet elements, no SIN.
		evidence(t, a, "interrogate", giTrace, "01 81 0B 01")
	})

	t.Run("general command", func(t *testing.T) {
		// Clause 7.4.4: ASDU 1, cause 20, the command's FUN and INF, SIN the RII.
		trace, code := ours(t, port, nil, "command", "--fun", "160", "--inf", "17", "--value", "on", "--timeout", "3000")
		if code == 0 {
			return
		}
		if code != 5 {
			t.Fatalf("exit %d\n%s", code, tail(trace))
		}
		evidence(t, a, "command --fun 160 --inf 17", trace, "01 81 0B 01 A0 11")
	})

	t.Run("refused command", func(t *testing.T) {
		// Clause 7.4.4: 160/18 is no command of the fixture: cause 21, exit 6.
		trace, code := ours(t, port, nil, "command", "--fun", "160", "--inf", "18", "--value", "on", "--timeout", "3000")
		if code == 6 {
			return
		}
		if code != 5 {
			t.Fatalf("exit %d\n%s", code, tail(trace))
		}
		evidence(t, a, "command --fun 160 --inf 18", trace, "01 81 0B 01 A0 12")
	})

	t.Run("generic read", func(t *testing.T) {
		if !a.caps.Features["genericServices"] {
			t.Skip("server feature genericServices not declared")
		}
		// Clause 7.4.8.6.2: ASDU 21, INF 244, answered by ASDU 10, INF 244, cause 42.
		for _, g := range f.Generic {
			var r struct {
				Cause int `json:"cause"`
				Sets  []struct {
					DataType int   `json:"data_type"`
					Size     int   `json:"size"`
					Values   []any `json:"values"`
				} `json:"sets"`
			}
			gin := fmt.Sprintf("%d.%d", g.GIN.Group, g.GIN.Entry)
			trace, code := ours(t, port, &r, "generic", "entry", "--gin", gin, "--timeout", "3000")
			var want any
			_ = json.Unmarshal(g.Value, &want)
			if code == 0 && r.Cause == 42 && len(r.Sets) == 1 && r.Sets[0].DataType == g.Type && r.Sets[0].Size == g.Size &&
				fmt.Sprint(normalize(r.Sets[0].Values)) == fmt.Sprint(normalize([]any{want})) {
				continue
			}
			// vitusdong: the answer carries INF 0 instead of 244.
			evidence(t, a, "generic-read", trace, "0A 81 2A 01 FE 00")
		}
	})

	t.Run("generic write", func(t *testing.T) {
		if !a.caps.Features["genericServices"] {
			t.Skip("server feature genericServices not declared")
		}
		// Clause 7.4.8.6.3, figure 17: INF 250, answered by cause 40; 1.2 is writable.
		var r struct {
			Cause int `json:"cause"`
		}
		trace, code := ours(t, port, &r, "generic", "write", "--gin", "1.2", "--type", "float32", "--value", "2.25", "--timeout", "3000")
		if code == 0 && r.Cause == 40 {
			var back struct {
				Sets []struct {
					Values []float64 `json:"values"`
				} `json:"sets"`
			}
			if _, code := ours(t, port, &back, "generic", "entry", "--gin", "1.2"); code != 0 || len(back.Sets) != 1 || back.Sets[0].Values[0] != 2.25 {
				t.Fatalf("read after write: exit %d %+v", code, back)
			}
			return
		}
		evidence(t, a, "generic-write", trace, "0A 81 28 01 FE 00")
	})

	t.Run("disturbance list", func(t *testing.T) {
		if !a.caps.Features["disturbanceList"] {
			t.Skip("server feature disturbanceList not declared")
		}
		// Clause 7.3.1.11: ASDU 23, cause 31, the function type of the equipment, SQ = 0.
		var r struct {
			Faults []struct {
				FAN int `json:"fan"`
				SOF struct {
					TP   bool `json:"tp"`
					TM   bool `json:"tm"`
					Test bool `json:"test"`
					OTEV bool `json:"otev"`
				} `json:"sof"`
				Time string `json:"time"`
			} `json:"faults"`
		}
		trace, code := ours(t, port, &r, "disturbance", "list", "--fun", strconv.Itoa(id.Fun), "--timeout", "3000")
		want := []string{}
		for _, d := range f.Disturbances {
			want = append(want, fmt.Sprintf("%d %+v %s", d.FAN, d.SOF, d.Time))
		}
		got := []string{}
		for _, x := range r.Faults {
			got = append(got, fmt.Sprintf("%d %+v %s", x.FAN, x.SOF, x.Time))
		}
		if code == 0 && fmt.Sprint(got) == fmt.Sprint(want) {
			return
		}
		// vitusdong: VSQ 81, FUN 255, TP in bit 8.
		evidence(t, a, "disturbance-list", trace, "17 81 1F 01 FF")
	})

	t.Run("disturbance transfer", func(t *testing.T) {
		if !a.caps.Features["disturbanceDataServer"] {
			t.Skip("server feature disturbanceDataServer not declared")
		}
		d := f.Disturbances[0]
		var r struct {
			FAN        int    `json:"fan"`
			NOE        int    `json:"noe"`
			IntervalUs int    `json:"interval_us"`
			Start      string `json:"start"`
			Tags       []tag  `json:"tags"`
			Channels   []struct {
				ACC    int   `json:"acc"`
				Values []int `json:"values"`
			} `json:"channels"`
		}
		trace, code := ours(t, port, &r, "disturbance", "get", "--fan", strconv.Itoa(d.FAN), "--fun", strconv.Itoa(id.Fun), "--transfer-timeout", "20000")
		if code != 0 {
			t.Fatalf("exit %d\n%s", code, tail(trace))
		}
		if r.FAN != d.FAN || r.IntervalUs != d.Interval || r.Start != d.Start || len(r.Tags) != len(d.Tags) || len(r.Channels) != len(d.Channels) {
			t.Fatalf("%+v", r)
		}
		for i, c := range d.Channels {
			if fmt.Sprint(r.Channels[i].Values) != fmt.Sprint(c.Values) {
				t.Errorf("channel %d: %v, want %v", c.ACC, r.Channels[i].Values, c.Values)
			}
		}
	})
}
