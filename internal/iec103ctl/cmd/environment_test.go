// SPDX-License-Identifier: MIT

package cmd

import (
	"bytes"
	"strings"
	"testing"

	"github.com/otfabric/go-iec60870-5/iec103"
	"github.com/otfabric/iec60870ctl/internal/iec103ctl/stationtest"
	"github.com/otfabric/iec60870ctl/internal/iec103ctl/types"
)

// noNotice fails when r wrote a notice on stderr.
func noNotice(t *testing.T, r result, what string) {
	t.Helper()
	if strings.Contains(r.stderr, "notice:") {
		t.Errorf("%s: a notice:\n%s", what, r.stderr)
	}
}

// A setting that decides what the clock synchronization or a generic write changes is taken
// only from the command line. For the clock the variable is not used, the built-in default
// is, and one line on stderr names the variable and the flag; a write mode from its
// variable refuses the write. On the command line the flags work as before.
func TestCommandLineOnlySettings(t *testing.T) {
	st := stationtest.Start(t, stationtest.Model)
	t.Run("broadcast", func(t *testing.T) {
		t.Setenv("IEC103CTL_BROADCAST", "true")
		var res types.ClockResult
		cli(t, st, "clock", "--format", "json").want(t, 0).
			errHas(t, "notice: IEC103CTL_BROADCAST=true is not used: --broadcast is taken only from the command line\n").decode(t, &res)
		if res.Broadcast || res.DeviceTime == "" {
			t.Errorf("IEC103CTL_BROADCAST=true: broadcast=%t device_time=%q, want the one device synchronized and its answer", res.Broadcast, res.DeviceTime)
		}
		r := cli(t, st, "clock", "--broadcast", "--format", "json").want(t, 0)
		noNotice(t, r, "--broadcast on the command line")
		r.decode(t, &res)
		if !res.Broadcast {
			t.Errorf("--broadcast: broadcast=%t", res.Broadcast)
		}
	})
	t.Run("time", func(t *testing.T) {
		for _, v := range []string{"2026-01-31T12:00:00Z", "soon"} {
			t.Setenv("IEC103CTL_TIME", v)
			var res types.ClockResult
			cli(t, st, "clock", "--format", "json").want(t, 0).
				errHas(t, "notice: IEC103CTL_TIME="+v+" is not used: --time is taken only from the command line\n").decode(t, &res)
			if strings.HasPrefix(res.Sent, "2026-01-31T12:00:00") {
				t.Errorf("IEC103CTL_TIME=%s used: sent %s", v, res.Sent)
			}
		}
		var res types.ClockResult
		r := cli(t, st, "clock", "--time", "2026-01-31T12:00:00Z", "--format", "json").want(t, 0)
		noNotice(t, r, "--time on the command line")
		r.decode(t, &res)
		if !strings.HasPrefix(res.Sent, "2026-01-31T12:00:00") {
			t.Errorf("--time: sent %s", res.Sent)
		}
		t.Setenv("IEC103CTL_TIME", "now")
		noNotice(t, cli(t, st, "clock").want(t, 0), "IEC103CTL_TIME=now")
	})
	t.Run("write mode", func(t *testing.T) {
		// Its default, write with execution, does more than abort: a mode from the variable
		// refuses the command before anything is sent.
		dev := stationtest.Start(t, stationtest.Model)
		before, _ := dev.Generic(iec103.GIN{Group: 1, Entry: 2})
		write := []string{"generic", "write", "--gin", "1.2", "--type", "float32", "--value", "2.5", "--format", "json"}
		for _, v := range []string{"abort", "execute"} {
			t.Setenv("IEC103CTL_WRITE_MODE", v)
			r := cli(t, dev, write...).want(t, 3)
			if kind, code := r.errorCode(t); kind != "invalid_input" || code != "invalid_flag_combination" {
				t.Errorf("IEC103CTL_WRITE_MODE=%s: %s %s", v, kind, code)
			}
			r.has(t, "IEC103CTL_WRITE_MODE="+v+" is not used: --mode is taken only from the command line; give --mode "+v+" to run it so")
		}
		for _, kind := range []string{"link", "generic-write"} {
			if dev.HasEvent(kind) {
				t.Errorf("a %s event: the refused write reached the device", kind)
			}
		}
		if v, _ := dev.Generic(iec103.GIN{Group: 1, Entry: 2}); !bytes.Equal(v.Data, before.Data) {
			t.Errorf("entry 1.2 changed from %x to %x", before.Data, v.Data)
		}
		var res types.GenericResult
		r := cli(t, dev, append(write, "--mode", "abort")...).want(t, 0)
		noNotice(t, r, "--mode on the command line")
		r.decode(t, &res)
		if res.Mode != "abort" {
			t.Errorf("--mode abort: mode %q", res.Mode)
		}
		cli(t, dev, append(write, "--mode", "execute")...).want(t, 0)
		cli(t, dev, "generic", "entry", "--gin", "1.2").want(t, 0).has(t, "2.5")
	})
	noNotice(t, cli(t, st, "clock").want(t, 0), "a clock without variables")
}
