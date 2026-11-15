// SPDX-License-Identifier: MIT

package cmd

import (
	"strings"
	"testing"

	"github.com/otfabric/iec60870ctl/internal/iec104ctl/stationtest"
	"github.com/otfabric/iec60870ctl/internal/iec104ctl/types"
)

// stationLogged reports whether st logged an event of kind whose detail holds detail.
func stationLogged(st *stationtest.Station, kind, detail string) bool {
	for _, e := range st.Events() {
		if e.Event == kind && strings.Contains(e.Detail, detail) {
			return true
		}
	}
	return false
}

// A parameter that decides what a request changes at a station is taken only from the
// command line. Its environment variable is not used: the flag keeps its built-in default,
// and one line on stderr, before anything is sent, names the variable and the flag. A flag
// on the command line, a variable that leaves the request a read, and a common address
// other than the broadcast address are used as before, without a line.
func TestStateChangesFromTheEnvironment(t *testing.T) {
	notice := func(env, flag string) string {
		return "notice: " + env + " is not used: --" + flag + " is taken only from the command line"
	}
	for _, c := range []struct {
		name    string
		env     map[string]string
		args    []string
		notices []string
		// has is what stdout shows, hasNot what it does not: the value that was used.
		has, hasNot []string
		// station checks the one event the station logs for the request.
		station func(*stationtest.Station) bool
	}{
		{"freeze", map[string]string{"IEC104CTL_FREEZE": "reset"}, []string{"counters", "--format", "json"},
			[]string{notice("IEC104CTL_FREEZE=reset", "freeze")}, []string{`"freeze": "read"`}, nil,
			func(st *stationtest.Station) bool {
				return stationLogged(st, "counter-interrogation", "freeze 0") && !stationLogged(st, "counter-interrogation", "freeze 3")
			}},
		// The freeze is not used, so the request is a read, and a read may go to every station.
		{"freeze-reset with a broadcast address", map[string]string{"IEC104CTL_FREEZE": "freeze-reset", "IEC104CTL_CA": "65535"}, []string{"counters", "--format", "json"},
			[]string{notice("IEC104CTL_FREEZE=freeze-reset", "freeze")}, []string{`"freeze": "read"`, `"ca": 65535`}, nil,
			func(st *stationtest.Station) bool { return stationLogged(st, "counter-interrogation", "freeze 0") }},
		{"freeze on the command line, broadcast address", map[string]string{"IEC104CTL_FREEZE": "reset", "IEC104CTL_CA": "65535"}, []string{"counters", "--freeze", "freeze", "--format", "json"},
			[]string{notice("IEC104CTL_CA=65535", "ca")}, []string{`"freeze": "freeze"`, `"ca": 1`}, nil,
			func(st *stationtest.Station) bool { return stationLogged(st, "counter-interrogation", "freeze 1") }},
		{"qualifier", map[string]string{"IEC104CTL_QUALIFIER": "long"}, []string{"command", "--type", "single", "--ioa", "5000", "--value", "on", "--format", "json"},
			[]string{notice("IEC104CTL_QUALIFIER=long", "qualifier")}, []string{`"qualifier": "none"`, `"mode": "execute"`}, nil, nil},
		{"time", map[string]string{"IEC104CTL_TIME": "2026-01-31T12:00:00Z"}, []string{"clock", "--format", "json"},
			[]string{notice("IEC104CTL_TIME=2026-01-31T12:00:00Z", "time")}, []string{`"request": "clock-synchronization"`}, []string{`"time": "2026-01-31T12:00:00`},
			func(st *stationtest.Station) bool {
				return stationLogged(st, "clock", "set to ") && !stationLogged(st, "clock", "set to 2026-01-31T12:00:00")
			}},
		{"clock to the broadcast address", map[string]string{"IEC104CTL_CA": "65535"}, []string{"clock", "--format", "json"},
			[]string{notice("IEC104CTL_CA=65535", "ca")}, []string{`"ca": 1`}, nil, nil},
		{"reset to the broadcast address", map[string]string{"IEC104CTL_CA": "65535"}, []string{"reset", "--qualifier", "events", "--format", "json"},
			[]string{notice("IEC104CTL_CA=65535", "ca")}, []string{`"ca": 1`}, nil, nil},

		{"freeze read", map[string]string{"IEC104CTL_FREEZE": "read", "IEC104CTL_CA": "65535"}, []string{"counters", "--format", "json"}, nil,
			[]string{`"freeze": "read"`, `"ca": 65535`}, nil, nil},
		{"freeze on the command line", map[string]string{"IEC104CTL_FREEZE": "freeze"}, []string{"counters", "--freeze", "reset", "--format", "json"}, nil,
			[]string{`"freeze": "reset"`}, nil,
			func(st *stationtest.Station) bool { return stationLogged(st, "counter-interrogation", "freeze 3") }},
		{"qualifier and mode on the command line", map[string]string{"IEC104CTL_QUALIFIER": "long", "IEC104CTL_COMMAND_MODE": "sbo"},
			[]string{"command", "--type", "single", "--ioa", "5000", "--value", "on", "--qualifier", "short", "--mode", "select", "--format", "json"}, nil,
			[]string{`"qualifier": "short"`, `"mode": "select"`}, nil,
			func(st *stationtest.Station) bool {
				return stationLogged(st, "command", "selected") && !stationLogged(st, "command", "executed")
			}},
		{"time now", map[string]string{"IEC104CTL_TIME": "now"}, []string{"clock", "--format", "json"}, nil,
			[]string{`"request": "clock-synchronization"`}, nil, nil},
		{"one station", map[string]string{"IEC104CTL_CA": "1"}, []string{"reset", "--qualifier", "events", "--format", "json"}, nil,
			[]string{`"ca": 1`}, nil, nil},
		{"time and address on the command line", map[string]string{"IEC104CTL_TIME": "now", "IEC104CTL_CA": "1"},
			[]string{"clock", "--time", "2026-01-31T12:00:00Z", "--ca", "65535", "--format", "json"}, nil,
			[]string{`"time": "2026-01-31T12:00:00.000Z"`, `"ca": 65535`}, nil,
			func(st *stationtest.Station) bool { return stationLogged(st, "clock", "set to 2026-01-31T12:00:00Z") }},
	} {
		t.Run(c.name, func(t *testing.T) {
			st := stationtest.Start(t, stationtest.Model)
			for k, v := range c.env {
				t.Setenv(k, v)
			}
			r := cli(t, st, append(c.args, "--debug")...).want(t, 0).has(t, c.has...)
			for _, s := range c.hasNot {
				if strings.Contains(r.stdout, s) {
					t.Errorf("stdout holds %q:\n%s", s, r.stdout)
				}
			}
			got := 0
			for line := range strings.SplitSeq(r.stderr, "\n") {
				if strings.HasPrefix(line, "notice: ") {
					got++
				}
			}
			if got != len(c.notices) {
				t.Errorf("%d notices, want %d:\n%s", got, len(c.notices), r.stderr)
			}
			sent := strings.Index(r.stderr, "debug: ")
			if sent < 0 {
				t.Fatalf("no frame trace:\n%s", r.stderr)
			}
			for _, n := range c.notices {
				at := strings.Index(r.stderr, n+"\n")
				switch {
				case at < 0:
					t.Errorf("stderr does not hold %q:\n%s", n, r.stderr)
				case at > sent:
					t.Errorf("%q comes after the first frame:\n%s", n, r.stderr)
				}
			}
			if c.station != nil {
				// The station logs a request after it has answered it.
				eventually(t, "the station to log the request as wanted", func() bool { return c.station(st) })
			}
		})
	}
}

// The procedure of `client command` is taken only from the command line, and without
// --mode it would be execute: with IEC104CTL_COMMAND_MODE set and no --mode the command is
// refused (exit 3) before it connects, and the station receives no command. With --mode on
// the command line the command runs.
func TestCommandModeFromTheEnvironment(t *testing.T) {
	for _, mode := range []string{"select", "sbo", "cancel", "execute"} {
		t.Run(mode, func(t *testing.T) {
			st := stationtest.Start(t, stationtest.Model)
			t.Setenv("IEC104CTL_COMMAND_MODE", mode)
			want := "IEC104CTL_COMMAND_MODE=" + mode + " is not used: --mode is taken only from the command line; give --mode " + mode + " to run it so"
			cli(t, st, "command", "--type", "double", "--ioa", "5001", "--value", "off").want(t, 3).errHas(t, want)
			r := cli(t, st, "command", "--type", "double", "--ioa", "5001", "--value", "off", "--format", "json").want(t, 3)
			if kind, code := r.errorCode(t); kind != "invalid_input" || code != "invalid_flag_combination" {
				t.Errorf("error %s/%s, want invalid_input/invalid_flag_combination", kind, code)
			}

			// The same command with --mode cancel on the command line runs. Once the station
			// has logged it, its connection is the only one the station saw, and its select
			// and cancel the only command events.
			var res types.CommandResult
			cli(t, st, "command", "--type", "double", "--ioa", "5001", "--value", "off", "--mode", "cancel", "--format", "json").want(t, 0).decode(t, &res)
			eventually(t, "the station to log the cancel", func() bool { return stationLogged(st, "command", "selection cancelled") })
			connections, commands := 0, 0
			for _, e := range st.Events() {
				switch e.Event {
				case "connected":
					connections++
				case "command":
					commands++
				}
			}
			if res.Mode != "cancel" || connections != 1 || commands != 2 {
				t.Errorf("mode %q, %d connections, %d commands; the station logged %+v", res.Mode, connections, commands, st.Events())
			}
		})
	}
}
