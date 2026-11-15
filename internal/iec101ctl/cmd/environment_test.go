// SPDX-License-Identifier: MIT

package cmd

import (
	"strings"
	"testing"

	"github.com/otfabric/iec60870ctl/internal/iec101ctl/stationtest"
	"github.com/otfabric/iec60870ctl/internal/iec101ctl/types"
)

// eventsOf returns the station's events of one kind after the first n events.
func eventsOf(st *stationtest.Station, n int, kind string) []types.ServerEvent {
	var out []types.ServerEvent
	for _, e := range st.Events()[n:] {
		if e.Event == kind {
			out = append(out, e)
		}
	}
	return out
}

// hasDetail reports whether one of events has a detail that contains s.
func hasDetail(events []types.ServerEvent, s string) bool {
	for _, e := range events {
		if strings.Contains(e.Detail, s) {
			return true
		}
	}
	return false
}

// unused is the line that names a variable whose value is not used.
func unused(env, value, flag string) string {
	return "notice: " + env + "=" + value + " is not used: --" + flag + " is taken only from the command line"
}

// envCase is one command run with environment variables set.
type envCase struct {
	name    string
	env     map[string]string
	args    []string
	notices []string
	// check fails the test unless the station and the result show what was sent; events
	// are the station's events of the command.
	check func(t *testing.T, r result, events func(kind string) []types.ServerEvent)
}

// runEnv runs the cases in order against st: each sets its variables, runs its command,
// which must succeed, and checks the lines that name variables and what was sent.
func runEnv(t *testing.T, st *stationtest.Station, cases []envCase) {
	t.Helper()
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			for k, v := range c.env {
				t.Setenv(k, v)
			}
			before := len(st.Events())
			r := cli(t, st, c.args[0], append(c.args[1:], "--format", "json")...).want(t, 0)
			r.errHas(t, c.notices...)
			if n := strings.Count(r.stderr, "notice:"); n != len(c.notices) {
				t.Errorf("%d notices, want %d:\n%s", n, len(c.notices), r.stderr)
			}
			c.check(t, r, func(kind string) []types.ServerEvent { return eventsOf(st, before, kind) })
		})
	}
}

// wantCA fails the test unless the JSON result names the common address ca.
func wantCA(t *testing.T, r result, ca float64) {
	t.Helper()
	var v struct {
		CA float64 `json:"ca"`
	}
	r.decodeLoose(t, &v)
	if v.CA != ca {
		t.Errorf("the request went to common address %v, want %v", v.CA, ca)
	}
}

// A parameter that decides what a request changes at a station, and a common address that
// sends such a request to every station of the line, are taken only from the command line.
// Set by its variable alone, it is not used: the command runs with the flag's built-in
// default, and one line on stderr names the variable and the flag. Given on the command
// line it is used, and read-only uses of the same variables are unchanged.
func TestStateChangesAreTakenOnlyFromTheCommandLine(t *testing.T) {
	st := stationtest.Start(t, stationtest.Model, nil)
	counter := func() any { v, _ := st.Value(2000); return v }
	start := counter()
	if start == int64(0) {
		t.Fatal("counter 2000 of the model is 0")
	}

	runEnv(t, st, []envCase{
		{
			name: "freeze", env: map[string]string{"IEC101CTL_FREEZE": "freeze-reset"},
			args:    []string{"counters", "--group", "1"},
			notices: []string{unused("IEC101CTL_FREEZE", "freeze-reset", "freeze")},
			check: func(t *testing.T, r result, events func(string) []types.ServerEvent) {
				if v := counter(); v != start {
					t.Errorf("counter 2000 is %v, want %v (a read leaves it as it is)", v, start)
				}
				if ev := events("counter-interrogation"); len(ev) != 1 || hasDetail(ev, "freeze") {
					t.Errorf("counter interrogations %+v, want one read", ev)
				}
			},
		},
		{
			name: "set delay", env: map[string]string{"IEC101CTL_SET_DELAY": "1234"},
			args:    []string{"delay"},
			notices: []string{unused("IEC101CTL_SET_DELAY", "1234", "set")},
			check: func(t *testing.T, r result, events func(string) []types.ServerEvent) {
				if hasDetail(events("delay"), "set to") || strings.Contains(r.stdout, "set_ms") {
					t.Errorf("a delay was sent, want the measurement only:\n%s", r.stdout)
				}
			},
		},
		{
			name: "command qualifier", env: map[string]string{"IEC101CTL_QUALIFIER": "long"},
			args:    []string{"command", "--type", "single", "--ioa", "5000", "--value", "off"},
			notices: []string{unused("IEC101CTL_QUALIFIER", "long", "qualifier")},
			check: func(t *testing.T, r result, events func(string) []types.ServerEvent) {
				var v struct {
					Mode      string `json:"mode"`
					Qualifier any    `json:"qualifier"`
				}
				r.decodeLoose(t, &v)
				if v.Mode != "execute" || v.Qualifier != "none" {
					t.Errorf("mode %q, qualifier %v; want execute and none", v.Mode, v.Qualifier)
				}
				if p, _ := st.Value(100); p != false || hasDetail(events("command"), "selected") {
					t.Errorf("point 100 is %v, events %+v: want the command executed", p, events("command"))
				}
			},
		},
		{
			name: "clock time", env: map[string]string{"IEC101CTL_TIME": "2026-01-31T12:00:00Z"},
			args:    []string{"clock"},
			notices: []string{unused("IEC101CTL_TIME", "2026-01-31T12:00:00Z", "time")},
			check: func(t *testing.T, r result, events func(string) []types.ServerEvent) {
				if ev := events("clock"); len(ev) != 1 || hasDetail(ev, "2026-01-31T12:00:") {
					t.Errorf("clock events %+v, want one, set to the current time", ev)
				}
			},
		},
		{
			name: "broadcast reset", env: map[string]string{"IEC101CTL_CA": "255"},
			args:    []string{"reset", "--qualifier", "events"},
			notices: []string{unused("IEC101CTL_CA", "255", "ca")},
			check:   func(t *testing.T, r result, _ func(string) []types.ServerEvent) { wantCA(t, r, 1) },
		},
		{
			name: "broadcast clock", env: map[string]string{"IEC101CTL_CA": "255"},
			args:    []string{"clock"},
			notices: []string{unused("IEC101CTL_CA", "255", "ca")},
			check:   func(t *testing.T, r result, _ func(string) []types.ServerEvent) { wantCA(t, r, 1) },
		},
		{
			name: "broadcast counter freeze", env: map[string]string{"IEC101CTL_CA": "255"},
			args:    []string{"counters", "--freeze", "freeze"},
			notices: []string{unused("IEC101CTL_CA", "255", "ca")},
			check: func(t *testing.T, r result, events func(string) []types.ServerEvent) {
				wantCA(t, r, 1)
				if !hasDetail(events("counter-interrogation"), "freeze 1,") {
					t.Errorf("the freeze of the command line was not sent: %+v", events("counter-interrogation"))
				}
			},
		},
		{
			// The freeze is not used, so the request is a read, which may go to every station.
			name: "freeze to every station", env: map[string]string{"IEC101CTL_CA": "255", "IEC101CTL_FREEZE": "freeze"},
			args:    []string{"counters"},
			notices: []string{unused("IEC101CTL_FREEZE", "freeze", "freeze")},
			check: func(t *testing.T, r result, events func(string) []types.ServerEvent) {
				wantCA(t, r, 255)
				if hasDetail(events("counter-interrogation"), "freeze") {
					t.Errorf("counters frozen: %+v", events("counter-interrogation"))
				}
			},
		},
	})

	// The default of --mode would execute where the variable asked to select or to cancel:
	// a mode from IEC101CTL_COMMAND_MODE refuses the command, and nothing is sent.
	t.Run("command mode", func(t *testing.T) {
		before := len(st.Events())
		value, _ := st.Value(100)
		for _, mode := range []string{"select", "cancel", "sbo", "execute"} {
			t.Setenv("IEC101CTL_COMMAND_MODE", mode)
			r := cli(t, st, "command", "--type", "single", "--ioa", "5000", "--value", "on", "--format", "json").want(t, 3)
			r.has(t, "IEC101CTL_COMMAND_MODE="+mode+" is not used: --mode is taken only from the command line; give --mode "+mode+" to run it so",
				`"invalid_flag_combination"`)
		}
		if ev := eventsOf(st, before, "command"); len(ev) != 0 {
			t.Errorf("the station received commands: %+v", ev)
		}
		if v, _ := st.Value(100); v != value {
			t.Errorf("point 100 is %v, want %v", v, value)
		}
	})

	// The same parameters on the command line are used, whatever the variables say.
	runEnv(t, st, []envCase{
		{
			name: "freeze on the command line", env: map[string]string{"IEC101CTL_FREEZE": "read"},
			args: []string{"counters", "--group", "1", "--freeze", "freeze-reset"},
			check: func(t *testing.T, _ result, _ func(string) []types.ServerEvent) {
				if v := counter(); v != int64(0) {
					t.Errorf("counter 2000 is %v after freeze-reset", v)
				}
			},
		},
		{
			name: "set on the command line", env: map[string]string{"IEC101CTL_SET_DELAY": "1"},
			args: []string{"delay", "--set", "120"},
			check: func(t *testing.T, _ result, events func(string) []types.ServerEvent) {
				if !hasDetail(events("delay"), "set to 120 ms") {
					t.Errorf("delay events %+v, want 120 ms set", events("delay"))
				}
			},
		},
		{
			name: "mode and qualifier on the command line", env: map[string]string{"IEC101CTL_COMMAND_MODE": "execute", "IEC101CTL_QUALIFIER": "none"},
			args: []string{"command", "--type", "single", "--ioa", "5000", "--value", "on", "--mode", "sbo", "--qualifier", "short"},
			check: func(t *testing.T, r result, events func(string) []types.ServerEvent) {
				if p, _ := st.Value(100); p != true || !hasDetail(events("command"), "selected") {
					t.Errorf("point 100 is %v, events %+v: want select, then execute", p, events("command"))
				}
			},
		},
		{
			name: "time on the command line", env: map[string]string{"IEC101CTL_TIME": "now"},
			args: []string{"clock", "--time", "2026-01-31T12:00:00Z"},
			check: func(t *testing.T, _ result, events func(string) []types.ServerEvent) {
				// The station adds the transmission delay set before.
				if !hasDetail(events("clock"), "set to 2026-01-31T12:00:") {
					t.Errorf("clock events %+v", events("clock"))
				}
			},
		},
		{
			name: "broadcast on the command line", env: map[string]string{"IEC101CTL_CA": "1"},
			args:  []string{"reset", "--qualifier", "events", "--ca", "255"},
			check: func(t *testing.T, r result, _ func(string) []types.ServerEvent) { wantCA(t, r, 255) },
		},
		// Read-only uses of the variables, and values that change nothing, are used as before.
		{
			name: "interrogation of every station", env: map[string]string{"IEC101CTL_CA": "255"},
			args:  []string{"interrogate"},
			check: func(t *testing.T, r result, _ func(string) []types.ServerEvent) { wantCA(t, r, 255) },
		},
		{
			name: "counter read of every station", env: map[string]string{"IEC101CTL_CA": "255", "IEC101CTL_FREEZE": "read"},
			args:  []string{"counters"},
			check: func(t *testing.T, r result, _ func(string) []types.ServerEvent) { wantCA(t, r, 255) },
		},
		{
			name: "one station", env: map[string]string{"IEC101CTL_CA": "1", "IEC101CTL_TIME": "now"},
			args:  []string{"clock"},
			check: func(t *testing.T, r result, _ func(string) []types.ServerEvent) { wantCA(t, r, 1) },
		},
	})
}

// IEC101CTL_IOA is the list of addresses of interrogate, read and monitor; the address of
// client command has its own variable, IEC101CTL_COMMAND_IOA. A list in IEC101CTL_IOA
// filters an interrogation, and every other command runs. --ioa of client command is a
// required flag: IEC101CTL_COMMAND_IOA does not replace it.
func TestIOAVariables(t *testing.T) {
	st := stationtest.Start(t, stationtest.Model, nil)
	t.Setenv("IEC101CTL_IOA", "100-199")

	var gi struct {
		Points []struct {
			IOA float64 `json:"ioa"`
		} `json:"points"`
	}
	cli(t, st, "interrogate", "--format", "json").want(t, 0).decodeLoose(t, &gi)
	if len(gi.Points) != 2 {
		t.Errorf("%d points, want 100 and 101: %+v", len(gi.Points), gi.Points)
	}
	for _, p := range gi.Points {
		if p.IOA < 100 || p.IOA > 199 {
			t.Errorf("point %v outside IEC101CTL_IOA", p.IOA)
		}
	}
	run(t, "version").want(t, 0)
	cli(t, st, "command", "--type", "single", "--ioa", "5000", "--value", "on").want(t, 0)

	t.Setenv("IEC101CTL_COMMAND_IOA", "5000")
	cli(t, st, "command", "--type", "single", "--value", "on").want(t, 2).errHas(t, `required flag(s) "ioa" not set`)
}
