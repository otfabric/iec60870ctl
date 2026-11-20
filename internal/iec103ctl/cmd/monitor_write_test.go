// SPDX-License-Identifier: MIT

package cmd

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/otfabric/iec60870ctl/internal/iec103ctl/stationtest"
	"github.com/otfabric/iec60870ctl/internal/iec103ctl/types"
)

func TestClientTypes(t *testing.T) {
	run(t, "client", "types").want(t, 0).has(t, "  1  time-tagged-message", "generic-data", "both", "disturbance-end")
	var all types.TypesResult
	run(t, "client", "types", "--format", "json").want(t, 0).decode(t, &all)
	if all.Count != 22 || all.Count != len(all.Types) || all.Types[0].ID != 1 || all.Types[0].Direction != "monitor" {
		t.Fatalf("%+v", all)
	}
	var control types.TypesResult
	run(t, "client", "types", "--direction", "control", "--format", "json").want(t, 0).decode(t, &control)
	for _, ty := range control.Types {
		if ty.Direction == "monitor" {
			t.Errorf("--direction control listed %+v", ty)
		}
	}
	if control.Count != 7 {
		t.Errorf("%d control types, want 5 and the 2 of both directions", control.Count)
	}
	run(t, "client", "types", "--format", "table").want(t, 0).has(t, "DIRECTION", "general-command")
	r := run(t, "client", "types", "--direction", "up", "--format", "json").want(t, 3)
	if kind, _ := r.errorCode(t); kind != "invalid_input" {
		t.Fatal(kind)
	}
	r = run(t, "__complete", "client", "monitor", "--type", "").want(t, 0)
	if !strings.Contains(r.stdout, "time-tagged-message") || !strings.Contains(r.stdout, "disturbance-list") {
		t.Fatal(r.stdout)
	}
	r = run(t, "__complete", "client", "monitor", "--cause", "").want(t, 0)
	if !strings.Contains(r.stdout, "spontaneous") || !strings.Contains(r.stdout, "general-interrogation") {
		t.Fatal(r.stdout)
	}
}

// messages decodes the JSON Lines of a monitor.
func messages(t *testing.T, r result) []types.Message {
	t.Helper()
	var out []types.Message
	for line := range strings.SplitSeq(strings.TrimSpace(r.stdout), "\n") {
		var m types.Message
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			t.Fatalf("%v: %s", err, line)
		}
		out = append(out, m)
	}
	return out
}

func TestMonitorFilters(t *testing.T) {
	st := stationtest.Start(t, stationtest.Model)
	// The general interrogation answers with time-tagged messages (cause 9) and ends with
	// its termination (type 8, cause 10).
	msgs := messages(t, cli(t, st, "monitor", "--interrogate", "--count", "3", "--cause", "general-interrogation", "--format", "json").want(t, 0))
	for _, m := range msgs {
		if m.Cause != 9 {
			t.Errorf("--cause general-interrogation showed %+v", m)
		}
	}
	msgs = messages(t, cli(t, st, "monitor", "--interrogate", "--count", "1", "--type", "general-interrogation-termination", "--format", "json").want(t, 0))
	if msgs[0].TypeID != 8 || msgs[0].Cause != 10 {
		t.Errorf("--type 8: %+v", msgs[0])
	}
	msgs = messages(t, cli(t, st, "monitor", "--interrogate", "--count", "2", "--type", "1-2", "--fun", "160", "--inf", "16-17,64", "--cause", "9", "--format", "json").want(t, 0))
	for _, m := range msgs {
		if m.TypeID > 2 || m.Fun != 160 || (m.Inf != 16 && m.Inf != 17 && m.Inf != 64) {
			t.Errorf("filtered monitor showed %+v", m)
		}
	}
	// received is RFC 3339 in UTC with milliseconds, as iec101ctl and iec104ctl write it.
	if r := msgs[0].Received; !strings.HasSuffix(r, "Z") || len(r) != len("2006-01-02T15:04:05.000Z") {
		t.Errorf("received %q", r)
	} else if _, err := time.Parse(time.RFC3339, r); err != nil {
		t.Error(err)
	}
	for _, f := range [][]string{
		{"--type", "no-such-type"}, {"--type", "300"}, {"--fun", "4-1"}, {"--inf", "x"},
		{"--cause", "sometimes"}, {"--fun", "1-"}, {"--cause", "1-300"},
	} {
		r := cli(t, st, append([]string{"monitor", "--count", "1", "--format", "json"}, f...)...).want(t, 3)
		if kind, _ := r.errorCode(t); kind != "invalid_input" {
			t.Errorf("%v: %s", f, kind)
		}
	}
}

func TestGenericWriteSeveralSets(t *testing.T) {
	st := stationtest.Start(t, stationtest.Model)
	var w types.GenericResult
	cli(t, st, "generic", "write", "--data", "1.2=float32:2.5", "--data", "1.3:actual=uint/2:600", "--format", "json").want(t, 0).decode(t, &w)
	if w.Cause != 40 || w.Mode != "execute" {
		t.Fatalf("cause %d, mode %s", w.Cause, w.Mode)
	}
	cli(t, st, "generic", "entry", "--gin", "1.2").want(t, 0).has(t, "2.5")
	cli(t, st, "generic", "entry", "--gin", "1.3").want(t, 0).has(t, "600")
	cli(t, st, "generic", "write", "--data", "1.2=float32:3", "--data", "1.3=uint/2:700", "--mode", "abort").want(t, 0).has(t, "abort")
	cli(t, st, "generic", "entry", "--gin", "1.3").want(t, 0).has(t, "600")
	// The device writes all or none: a read-only entry refuses the write, and 1.3 keeps 600.
	cli(t, st, "generic", "write", "--data", "1.3=uint/2:800", "--data", "1.1=uint/2:5").want(t, 6).errHas(t, "read-only")
	cli(t, st, "generic", "entry", "--gin", "1.3").want(t, 0).has(t, "600")

	// The values of --gin, --type and --value from the environment do not mix with --data.
	t.Setenv("IEC103CTL_GIN", "1.1")
	t.Setenv("IEC103CTL_VALUE", "9")
	cli(t, st, "generic", "write", "--data", "1.3=uint/2:650").want(t, 0)
	cli(t, st, "generic", "entry", "--gin", "1.3").want(t, 0).has(t, "650")
}

func TestGenericWriteDataInvalid(t *testing.T) {
	st := stationtest.Start(t, stationtest.Model)
	many := []string{"generic", "write"}
	for range 65 {
		many = append(many, "--data", "1.3=uint/2:1")
	}
	for _, args := range [][]string{
		{"generic", "write", "--data", "1.3"},
		{"generic", "write", "--data", "1.3=uint:5"},
		{"generic", "write", "--data", "1.3=uint/x:5"},
		{"generic", "write", "--data", "1.3=uint/2"},
		{"generic", "write", "--data", "1=uint/2:5"},
		{"generic", "write", "--data", "1.3:nokod=uint/2:5"},
		{"generic", "write", "--data", "1.3=decimal:5"},
		{"generic", "write", "--data", "1.3=uint/2:70000"},
		{"generic", "write", "--data", "1.3=uint/2:1", "--gin", "1.2"},
		{"generic", "write", "--data", "1.3=uint/2:1", "--value", "2"},
		many,
	} {
		r := cli(t, st, append(args, "--format", "json")...)
		if r.code != 3 {
			t.Errorf("%v: exit %d, want 3\n%s%s", args, r.code, r.stdout, r.stderr)
			continue
		}
		if kind, _ := r.errorCode(t); kind != "invalid_input" {
			t.Errorf("%v: kind %s", args, kind)
		}
	}
	// Without --data, --gin, --type and --value stay required on the command line.
	r := cli(t, st, "generic", "write", "--gin", "1.3", "--format", "json").want(t, 2)
	if kind, _ := r.errorCode(t); kind != "usage" {
		t.Fatal(kind)
	}
	cli(t, st, "generic", "write", "--type", "uint").want(t, 2).errHas(t, `"gin", "value"`)
	t.Setenv("IEC103CTL_GIN", "1.3")
	t.Setenv("IEC103CTL_DATA_TYPE", "uint")
	t.Setenv("IEC103CTL_VALUE", "1")
	cli(t, st, "generic", "write", "--size", "2").want(t, 2)
	cli(t, st, "generic", "entry", "--gin", "1.3").want(t, 0).has(t, "300")
}

func TestFlagAliasesAndRetries(t *testing.T) {
	st := stationtest.Start(t, stationtest.Model)
	// --ca and --link-retries are the names of iec101ctl and iec102ctl.
	var gi types.InterrogateResult
	cli(t, st, "interrogate", "--ca", "1", "--link-retries", "-1", "--format", "json").want(t, 0).decode(t, &gi)
	if gi.CA != 1 || gi.Count != 8 {
		t.Fatalf("%+v", gi)
	}
	cli(t, st, "interrogate", "--common-address", "1", "--retries", "0").want(t, 0)
	cli(t, st, "interrogate", "--ca", "9", "--timeout", "500").want(t, 5)
	for _, args := range [][]string{{"--retries", "-2"}, {"--link-retries", "11"}, {"--ca", "256"}} {
		r := cli(t, st, append([]string{"interrogate", "--format", "json"}, args...)...).want(t, 3)
		if kind, _ := r.errorCode(t); kind != "invalid_input" {
			t.Errorf("%v: %s", args, kind)
		}
	}
	// The help keeps the names of iec103ctl.
	run(t, "client", "interrogate", "--help").want(t, 0).has(t, "--common-address", "--retries")
}
