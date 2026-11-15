// SPDX-License-Identifier: MIT

package cmd

import (
	"strings"
	"testing"
	"time"

	"github.com/otfabric/iec60870ctl/internal/iec102ctl/stationtest"
	"github.com/otfabric/iec60870ctl/internal/iec102ctl/types"
)

const keyWarning = "warning: --access-key is deprecated"

// --access-key on the command line is warned about on stderr and still used; the key from
// a file or from IEC102CTL_ACCESS_KEY is not warned about.
func TestAccessKeyFlagWarning(t *testing.T) {
	st := stationtest.Start(t)
	var r types.LoginResult
	res := cli(t, st, "ree", "login", "--access-key", "7", "--format", "json").want(t, 0)
	res.decode(t, &r)
	if !r.Session.Login || !strings.Contains(res.stderr, keyWarning) || strings.Count(res.stderr, keyWarning) != 1 {
		t.Errorf("--access-key: %+v, stderr %q", r.Session, res.stderr)
	}
	if res := cli(t, st, "ree", "login", "--access-key-file", keyFile(t)).want(t, 0); strings.Contains(res.stderr, "deprecated") {
		t.Errorf("--access-key-file: stderr %q", res.stderr)
	}
	t.Setenv("IEC102CTL_ACCESS_KEY", "7")
	if res := cli(t, st, "ree", "login").want(t, 0); strings.Contains(res.stderr, "deprecated") {
		t.Errorf("IEC102CTL_ACCESS_KEY: stderr %q", res.stderr)
	}
	// Every command with a session warns, a wrong key included.
	cli(t, st, "totals", "--oldest", "--access-key", "7").want(t, 0).errHas(t, keyWarning)
	cli(t, st, "ree", "login", "--access-key", "8").want(t, 6).errHas(t, keyWarning)
}

// The parameters of `client ree set-time` and `client request` that decide what is sent to
// the meter are taken only from the command line. A variable that is set for one of them is
// not used: the flag keeps its built-in default, and one notice on stderr names the variable
// and the flag. The flag on the command line is used as given, without a notice.
func TestStateChangingValuesOnlyFromTheCommandLine(t *testing.T) {
	st := stationtest.Start(t)
	t.Setenv("IEC102CTL_ACCESS_KEY", "7")
	t.Setenv("IEC102CTL_TIME", "2026-03-01T00:00:00.000")
	setTo := func() string {
		t.Helper()
		set := ""
		for _, e := range st.Events() {
			if e.Event == "set-time" {
				set = e.Detail
			}
		}
		return set
	}
	before := time.Now().UTC().Add(-time.Second)
	var r types.SetTimeResult
	res := cli(t, st, "ree", "set-time", "--format", "json").want(t, 0)
	res.decode(t, &r)
	after := time.Now().UTC().Add(time.Second)
	res.errHas(t, "notice: IEC102CTL_TIME=2026-03-01T00:00:00.000 is not used: --time is taken only from the command line\n")
	if strings.Count(res.stderr, "notice:") != 1 {
		t.Errorf("stderr %q, want one notice", res.stderr)
	}
	sent, err := time.ParseInLocation("2006-01-02T15:04:05.000", r.Time.Time, time.UTC)
	if err != nil || sent.Before(before) || sent.After(after) {
		t.Errorf("the meter's clock was set to %q (%v), want this computer's time (the default now)", r.Time.Time, err)
	}
	if set := setTo(); !strings.HasPrefix(set, r.Time.Time) {
		t.Errorf("the station's clock was set to %q, want %q", set, r.Time.Time)
	}
	res = cli(t, st, "ree", "set-time", "--time", "2026-03-02T00:00:00.000").want(t, 0)
	if strings.Contains(res.stderr, "notice:") {
		t.Errorf("--time on the command line: stderr %q", res.stderr)
	}
	if set := setTo(); !strings.HasPrefix(set, "2026-03-02T00:00:00.000") {
		t.Errorf("--time on the command line: the station's clock was set to %q", set)
	}

	t.Setenv("IEC102CTL_CAUSE", "request")
	t.Setenv("IEC102CTL_RECORD", "12")
	t.Setenv("IEC102CTL_DATA", "ff")
	t.Setenv("IEC102CTL_OBJECTS", "1")
	var rr types.RequestResult
	res = cli(t, st, "request", "--type", "C_TI_NA_2", "--until", "reply", "--format", "json").want(t, 0)
	res.decode(t, &rr)
	for _, want := range []string{
		"notice: IEC102CTL_CAUSE=request is not used: --cause is taken only from the command line\n",
		"notice: IEC102CTL_RECORD=12 is not used: --record is taken only from the command line\n",
		"notice: IEC102CTL_DATA=ff is not used: --data is taken only from the command line\n",
		"notice: IEC102CTL_OBJECTS=1 is not used: --objects is taken only from the command line\n",
	} {
		if !strings.Contains(res.stderr, want) {
			t.Errorf("stderr %q, want %q", res.stderr, want)
		}
	}
	if strings.Count(res.stderr, "notice:") != 4 {
		t.Errorf("stderr %q, want four notices", res.stderr)
	}
	// The built-in defaults: cause activation, record 0, no data, no objects.
	if q := rr.Request; q.Cot != 6 || q.Record != 0 || q.Objects != 0 {
		t.Errorf("request %+v, want the defaults (cause 6, record 0, no objects)", q)
	}
	res = cli(t, st, "request", "--type", "C_TI_NA_2", "--until", "reply", "--cause", "request", "--record", "12", "--objects", "0", "--format", "json").want(t, 0)
	res.decode(t, &rr)
	if strings.Contains(res.stderr, "notice: IEC102CTL_CAUSE") || strings.Contains(res.stderr, "notice: IEC102CTL_RECORD") || strings.Contains(res.stderr, "notice: IEC102CTL_OBJECTS") {
		t.Errorf("flags on the command line: stderr %q", res.stderr)
	}
	if q := rr.Request; q.Cot != 5 || q.Record != 12 {
		t.Errorf("flags on the command line: request %+v, want cause 5 and record 12", q)
	}
}
