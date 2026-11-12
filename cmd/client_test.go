// SPDX-License-Identifier: MIT

package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/otfabric/go-iec104/asdu"
	"github.com/otfabric/iec104ctl/internal/station"
	"github.com/otfabric/iec104ctl/internal/stationtest"
	"github.com/otfabric/iec104ctl/internal/types"
)

func TestInterrogate(t *testing.T) {
	st := stationtest.Start(t, stationtest.Model)

	t.Run("text", func(t *testing.T) {
		cli(t, st, "interrogate").want(t, 0).has(t,
			"General interrogation of station 1 at tcp://127.0.0.1:", "8 objects",
			"M_SP_NA_1  ca=1   ioa=100      on  ", "[NT|IV]", "M_ME_NC_1", "230.5", "interrogated-station")
	})
	t.Run("json", func(t *testing.T) {
		var res types.InterrogateResult
		cli(t, st, "interrogate", "--format", "json").want(t, 0).decode(t, &res)
		if res.Count != 8 || res.Received != 8 || len(res.Points) != 8 || res.CommonAddr != 1 || res.Request != "interrogation" {
			t.Fatalf("result %+v", res)
		}
		byIOA := map[uint32]types.Point{}
		for _, p := range res.Points {
			byIOA[p.IOA] = p
		}
		// Counters are not part of an interrogation.
		if _, ok := byIOA[2000]; ok {
			t.Error("the interrogation returned a counter")
		}
		if p := byIOA[100]; p.Value != true || p.Quality != "good" || p.Type != "M_SP_NA_1" || p.CauseID != 20 {
			t.Errorf("point 100: %+v", p)
		}
		if p := byIOA[101]; p.Value != false || p.Quality != "NT|IV" {
			t.Errorf("point 101: %+v", p)
		}
		if p := byIOA[200]; p.Value != "on" {
			t.Errorf("point 200: %+v", p)
		}
		if p := byIOA[1000]; p.Value != 230.5 {
			t.Errorf("point 1000: %+v", p)
		}
		if p := byIOA[1003]; p.Value != 0.25 || p.Attributes["raw"] != float64(8192) {
			t.Errorf("point 1003: %+v", p)
		}
	})
	t.Run("table", func(t *testing.T) {
		r := cli(t, st, "interrogate", "--format", "table").want(t, 0).has(t, "CA  IOA   TYPE", "1   100   M_SP_NA_1  on")
		if n := len(strings.Split(strings.TrimSpace(r.stdout), "\n")); n != 10 {
			t.Errorf("%d table lines, want a header, a rule and 8 rows:\n%s", n, r.stdout)
		}
	})
	t.Run("group and filters", func(t *testing.T) {
		var res types.InterrogateResult
		cli(t, st, "interrogate", "--group", "3", "--format", "json").want(t, 0).decode(t, &res)
		if res.Count != 3 || res.Points[0].Cause != "interrogated-group-3" {
			t.Errorf("group 3: %+v", res)
		}
		cli(t, st, "gi", "--type", "M_SP_NA_1,13", "--ioa", "100,1000-1002", "--format", "json").want(t, 0).decode(t, &res)
		if res.Received != 8 || res.Count != 2 || res.Points[0].IOA != 100 || res.Points[1].IOA != 1000 {
			t.Errorf("filtered: %+v", res)
		}
		cli(t, st, "interrogate", "--type", "M_SP_NA_1").want(t, 0).has(t, "8 objects", "2 shown")
	})
	t.Run("refused", func(t *testing.T) {
		r := cli(t, st, "interrogate", "--ca", "9").want(t, 6).errHas(t, "error: the station refused C_IC_NA_1 to station 9: unknown common address")
		if r.stdout != "" {
			t.Errorf("stdout after a fatal error: %q", r.stdout)
		}
		kind, code := cli(t, st, "interrogate", "--ca", "9", "--format", "json").want(t, 6).errorCode(t)
		if kind != "negative_confirmation" || code != "unknown_common_address" {
			t.Errorf("error %s/%s", kind, code)
		}
	})
	t.Run("invalid input", func(t *testing.T) {
		for _, args := range [][]string{
			{"--group", "17"},
			{"--ca", "0"},
			{"--type", "M_NOPE"},
			{"--ioa", "5-1"},
			{"--ioa", "x"},
			{"--ioa", "99999999"},
			{"--format", "xml"},
			{"--k", "4", "--w", "9"},
			{"--ioa-size", "4"},
			{"--timezone", "Mars/Olympus"},
		} {
			cli(t, st, append([]string{"interrogate"}, args...)...).want(t, 3).errHas(t, "error: ")
		}
	})
}

func TestTargets(t *testing.T) {
	st := stationtest.Start(t, stationtest.Model)
	url := "tcp://" + st.Addr()

	run(t, "client", "interrogate", "--url", url).want(t, 0).has(t, "at "+url)
	run(t, "client", "interrogate").want(t, 3).errHas(t, "no station given")
	run(t, "client", "interrogate", "--url", url, "--ip", "127.0.0.1").want(t, 3).errHas(t, "mutually exclusive")
	run(t, "client", "interrogate", "--url", "udp://127.0.0.1").want(t, 3).errHas(t, "unsupported scheme")
	run(t, "client", "interrogate", "--url", "127.0.0.1").want(t, 3)

	t.Run("environment", func(t *testing.T) {
		t.Setenv("IEC104CTL_URL", url)
		t.Setenv("IEC104CTL_OUTPUT_FORMAT", "json")
		var res types.InterrogateResult
		run(t, "client", "interrogate").want(t, 0).decode(t, &res)
		if res.Target != url {
			t.Errorf("target %q", res.Target)
		}
		// A flag wins over the environment.
		run(t, "client", "interrogate", "--format", "text").want(t, 0).has(t, "General interrogation")
	})
	t.Run("nothing listening", func(t *testing.T) {
		port := strconv.Itoa(int(stationtest.FreePort(t)))
		run(t, "client", "interrogate", "--ip", "127.0.0.1", "--port", port, "--timeout", "500").want(t, 4).errHas(t, "error: connection error")
		kind, code := run(t, "client", "interrogate", "--ip", "127.0.0.1", "--port", port, "--format", "json").want(t, 4).errorCode(t)
		if kind != "transport" || code != "transport_connect_failed" {
			t.Errorf("error %s/%s", kind, code)
		}
	})
	t.Run("debug", func(t *testing.T) {
		r := run(t, "--debug", "client", "read", "--url", url, "--ioa", "100").want(t, 0)
		r.errHas(t, "U STARTDT act", "U STARTDT con", "C_RD_NA_1", "M_SP_NA_1", "connected", "disconnected")
	})
}

func TestCountersAndRead(t *testing.T) {
	st := stationtest.Start(t, stationtest.Model)

	var res types.InterrogateResult
	cli(t, st, "counters", "--format", "json").want(t, 0).decode(t, &res)
	if res.Count != 2 || res.Points[0].Value != float64(123456) || res.Points[0].Type != "M_IT_NA_1" || res.Request != "counter-interrogation" {
		t.Fatalf("counters: %+v", res)
	}
	cli(t, st, "counters", "--group", "2").want(t, 0).has(t, "Counter interrogation, group 2", "ioa=2001", "7 (sequence=0)")
	// Freeze with reset reports the values and then clears the counters.
	cli(t, st, "counters", "--freeze", "freeze-reset", "--format", "table").want(t, 0).has(t, "123456")
	cli(t, st, "counters", "--format", "json").want(t, 0).decode(t, &res)
	if res.Points[0].Value != float64(0) || res.Points[0].Attributes["sequence"] != float64(1) {
		t.Errorf("after freeze-reset: %+v", res.Points[0])
	}
	cli(t, st, "counters", "--freeze", "reset").want(t, 0).has(t, "0 objects")
	cli(t, st, "counters", "--freeze", "thaw").want(t, 3)
	cli(t, st, "counters", "--group", "5").want(t, 3)

	t.Run("read", func(t *testing.T) {
		cli(t, st, "read", "--ioa", "100,1000").want(t, 0).has(t, "2 of 2 answered", "ioa=1000", "230.5", "request")
		var rr types.ReadResult
		cli(t, st, "read", "--ioa", "100,9999,300", "--format", "json").want(t, 7).decode(t, &rr)
		if rr.Requested != 3 || rr.Failed != 1 || len(rr.Rows) != 3 {
			t.Fatalf("read: %+v", rr)
		}
		if rr.Rows[1].Error == nil || rr.Rows[1].Error.Code != "unknown_object_address" || rr.Rows[1].Point != nil {
			t.Errorf("row of the unknown address: %+v", rr.Rows[1])
		}
		if rr.Rows[2].Point == nil || rr.Rows[2].Point.Value != float64(3) {
			t.Errorf("row after the refused one: %+v", rr.Rows[2])
		}
		cli(t, st, "read", "--ioa", "9999", "--format", "table").want(t, 7).has(t, "ERROR", "unknown information object address")
		cli(t, st, "read", "--ioa", "9999").want(t, 7).has(t, "0 of 1 answered", "ioa=9999  error:")
		cli(t, st, "read").want(t, 2).errHas(t, `required flag(s) "ioa" not set`)
		cli(t, st, "read", "--ioa", ",").want(t, 3)
		cli(t, st, "read", "--ioa", "1", "--ca", "65535").want(t, 3)
	})
}

func TestCommand(t *testing.T) {
	st := stationtest.Start(t, stationtest.Model)
	value := func(ioa uint32) any {
		v, _ := st.Value(ioa)
		return v
	}

	t.Run("single, direct execute", func(t *testing.T) {
		cli(t, st, "command", "--type", "single", "--ioa", "5000", "--value", "off").want(t, 0).has(t,
			"Command C_SC_NA_1 to station 1", "ioa=5000, value off (execute)", "execute", "confirmed")
		if value(100) != false {
			t.Errorf("point 100 is %v after the command", value(100))
		}
	})
	t.Run("select required", func(t *testing.T) {
		r := cli(t, st, "command", "--type", "double", "--ioa", "5001", "--value", "off").want(t, 6)
		r.errHas(t, "error: execute: ", "negative confirmation")
		if value(200) != "on" {
			t.Errorf("point 200 changed to %v by a refused command", value(200))
		}
		var res types.CommandResult
		cli(t, st, "cmd", "--type", "C_DC_NA_1", "--ioa", "5001", "--value", "off", "--mode", "sbo", "--qualifier", "short",
			"--wait-termination", "3000", "--format", "json").want(t, 0).decode(t, &res)
		if len(res.Steps) != 3 || res.Steps[0].Step != "select" || res.Steps[1].Step != "execute" || res.Steps[2].Result != "received" {
			t.Fatalf("steps: %+v", res.Steps)
		}
		if res.Value != "off" || res.Qualifier != "short" || res.Mode != "sbo" || res.Type != "C_DC_NA_1" {
			t.Errorf("result: %+v", res)
		}
		if value(200) != "off" {
			t.Errorf("point 200 is %v after select before operate", value(200))
		}
	})
	t.Run("select and cancel operate nothing", func(t *testing.T) {
		before := value(100)
		open := func() (n int) {
			for _, e := range st.Events() {
				switch e.Event {
				case "connected":
					n++
				case "disconnected":
					n--
				}
			}
			return n
		}
		cli(t, st, "command", "--type", "single", "--ioa", "5000", "--value", "on", "--mode", "select").want(t, 0).has(t, "select", "confirmed")
		// The selection belongs to the session that made it, and the station
		// drops it when it handles that session's disconnect. Until then a
		// select from the next run is refused as made by another station.
		// Sessions of earlier subtests may still be closing, so a rising
		// disconnect count would not prove this one has gone.
		eventually(t, "the selecting session is gone", func() bool { return open() == 0 })
		cli(t, st, "command", "--type", "single", "--ioa", "5000", "--value", "on", "--mode", "cancel", "--format", "table").want(t, 0).has(t, "select", "cancel")
		if value(100) != before {
			t.Errorf("point 100 changed from %v to %v", before, value(100))
		}
	})
	t.Run("every type", func(t *testing.T) {
		for _, c := range []struct {
			args []string
			ioa  uint32
			want any
		}{
			{[]string{"--type", "step", "--ioa", "5002", "--value", "higher"}, 300, int64(4)},
			{[]string{"--type", "step", "--ioa", "5002", "--value", "lower", "--qualifier", "long"}, 300, int64(3)},
			{[]string{"--type", "setpoint-float", "--ioa", "5003", "--value", "231.5"}, 1000, 231.5},
			{[]string{"--type", "setpoint-scaled", "--ioa", "5004", "--value", "-42", "--qualifier", "5"}, 1002, int64(-42)},
			{[]string{"--type", "setpoint-normalized", "--ioa", "5005", "--value", "-0.5"}, 1003, -0.5},
			{[]string{"--type", "bitstring", "--ioa", "5006", "--value", "0xFF"}, 400, int64(255)},
			{[]string{"--type", "single", "--ioa", "5000", "--value", "on", "--time-tag", "--qualifier", "persistent"}, 100, true},
		} {
			cli(t, st, append([]string{"command"}, c.args...)...).want(t, 0)
			if got := value(c.ioa); got != c.want {
				t.Errorf("%v: point %d is %v (%T), want %v", c.args, c.ioa, got, got, c.want)
			}
		}
		cli(t, st, "command", "--type", "C_SC_TA_1", "--ioa", "5007", "--value", "on").want(t, 0).has(t, "Command C_SC_TA_1")
	})
	t.Run("refused by the station", func(t *testing.T) {
		kind, code := cli(t, st, "command", "--type", "single", "--ioa", "777", "--value", "on", "--format", "json").want(t, 6).errorCode(t)
		if kind != "negative_confirmation" || code != "unknown_object_address" {
			t.Errorf("error %s/%s", kind, code)
		}
		// The object takes another type.
		cli(t, st, "command", "--type", "double", "--ioa", "5000", "--value", "on").want(t, 6)
		// A command without a target is confirmed and terminated all the same.
		cli(t, st, "command", "--type", "single", "--ioa", "5007", "--value", "on", "--wait-termination", "100").want(t, 0).has(t, "termination", "received")
	})
	t.Run("nothing is sent when the input is wrong", func(t *testing.T) {
		commands := func() (n int) {
			for _, e := range st.Events() {
				if e.Event == "command" {
					n++
				}
			}
			return n
		}
		// The station logs a command when it has handled it, which can be
		// after the run that sent it has ended: count from a quiet station.
		before := commands()
		for quiet := time.Now(); time.Since(quiet) < 100*time.Millisecond; time.Sleep(5 * time.Millisecond) {
			if n := commands(); n != before {
				before, quiet = n, time.Now()
			}
		}
		for _, args := range [][]string{
			{"--type", "single", "--ioa", "5000", "--value", "maybe"},
			{"--type", "double", "--ioa", "5001", "--value", "2"},
			{"--type", "step", "--ioa", "5002", "--value", "sideways"},
			{"--type", "setpoint-normalized", "--ioa", "5005", "--value", "1"},
			{"--type", "setpoint-scaled", "--ioa", "5004", "--value", "1.5"},
			{"--type", "setpoint-scaled", "--ioa", "5004", "--value", "40000"},
			{"--type", "setpoint-float", "--ioa", "5003", "--value", "NaN"},
			{"--type", "setpoint-float", "--ioa", "5003", "--value", "1", "--qualifier", "200"},
			{"--type", "bitstring", "--ioa", "5006", "--value", "-1"},
			{"--type", "bitstring", "--ioa", "5006", "--value", "1", "--mode", "sbo"},
			{"--type", "bitstring", "--ioa", "5006", "--value", "1", "--qualifier", "short"},
			{"--type", "single", "--ioa", "5000", "--value", "on", "--qualifier", "forever"},
			{"--type", "single", "--ioa", "5000", "--value", "on", "--mode", "maybe"},
			{"--type", "single", "--ioa", "5000", "--value", "on", "--mode", "select", "--wait-termination", "10"},
			{"--type", "single", "--ioa", "0", "--value", "on"},
			{"--type", "single", "--ioa", "5000", "--value", " "},
			{"--type", "M_SP_NA_1", "--ioa", "5000", "--value", "on"},
			{"--type", "teleport", "--ioa", "5000", "--value", "on"},
		} {
			cli(t, st, append([]string{"command"}, args...)...).want(t, 3)
		}
		cli(t, st, "command", "--type", "single", "--value", "on").want(t, 2)
		if n := commands(); n != before {
			t.Errorf("the station received %d commands that were not valid", n-before)
		}
	})
}

func TestClockResetPing(t *testing.T) {
	st := stationtest.Start(t, stationtest.Model)

	cli(t, st, "clock").want(t, 0).has(t, "Clock of station 1", "confirmed in")
	var res types.ConfirmedResult
	cli(t, st, "clock", "--time", "2030-06-01T12:00:00+02:00", "--timezone", "Europe/Brussels", "--format", "json").want(t, 0).decode(t, &res)
	if res.Time != "2030-06-01T12:00:00.000+02:00" || res.Request != "clock-synchronization" {
		t.Errorf("clock: %+v", res)
	}
	if !st.HasEvent("clock") {
		t.Error("the station logged no clock synchronization")
	}
	cli(t, st, "clock", "--format", "table").want(t, 0).has(t, "clock-synchronization", "confirmed")
	cli(t, st, "clock", "--time", "yesterday").want(t, 3)

	// A process reset puts the values of the model back.
	cli(t, st, "command", "--type", "single", "--ioa", "5000", "--value", "off").want(t, 0)
	cli(t, st, "reset", "--qualifier", "events").want(t, 0).has(t, "Reset (events) of station 1")
	if v, _ := st.Value(100); v != false {
		t.Errorf("an event reset changed point 100 to %v", v)
	}
	cli(t, st, "reset", "--qualifier", "process", "--format", "json").want(t, 0).decode(t, &res)
	if res.Qualifier != "process" || res.Request != "reset-process" {
		t.Errorf("reset: %+v", res)
	}
	if v, _ := st.Value(100); v != true {
		t.Errorf("point 100 is %v after a process reset, want the value of the model", v)
	}
	cli(t, st, "reset").want(t, 2)
	cli(t, st, "reset", "--qualifier", "everything").want(t, 3)

	t.Run("ping", func(t *testing.T) {
		r := cli(t, st, "ping", "--count", "3", "--interval", "10").want(t, 0).has(t, "seq=1  testfr  rtt=", "seq=3").errHas(t, "3 tests, 0 failed, rtt min/avg/max")
		if n := strings.Count(r.stdout, "\n"); n != 3 {
			t.Errorf("%d samples, want 3", n)
		}
		lines := cli(t, st, "ping", "--count", "2", "--interval", "10", "--method", "command", "--format", "json").want(t, 0).lines(t)
		if len(lines) != 2 || lines[1]["seq"] != float64(2) || lines[0]["method"] != "command" || lines[0]["error"] != nil {
			t.Errorf("ping samples: %v", lines)
		}
		cli(t, st, "ping", "--count", "1", "--format", "table").want(t, 0).has(t, "TIMESTAMP", "testfr")
		// The test command to a station that is not there fails every time: partial.
		r = cli(t, st, "ping", "--count", "2", "--interval", "10", "--method", "command", "--ca", "5").want(t, 7)
		r.has(t, "error: the station refused C_TS_TA_1").errHas(t, "2 tests, 2 failed")
		cli(t, st, "ping", "--interval", "0").want(t, 3)
		cli(t, st, "ping", "--method", "icmp").want(t, 3)
	})
}

func TestScanAndTypes(t *testing.T) {
	st := stationtest.Start(t, stationtest.Model)

	var res types.ScanResult
	cli(t, st, "scan", "--format", "json").want(t, 0).decode(t, &res)
	if res.Method != "broadcast" || res.Found != 1 || len(res.Stations) != 1 || res.Stations[0].CommonAddr != 1 || res.Stations[0].Objects != 8 {
		t.Fatalf("broadcast scan: %+v", res)
	}
	cli(t, st, "scan", "--method", "sweep", "--start", "1", "--end", "4", "--delay", "1", "--format", "json").want(t, 0).errHas(t, "scan: ca=1 found").decode(t, &res)
	if res.Probed != 4 || res.Found != 1 || res.Stations[0].Status != "found" || len(res.Stations[0].Types) == 0 {
		t.Fatalf("sweep: %+v", res)
	}
	cli(t, st, "scan", "--method", "sweep", "--start", "1", "--end", "2").want(t, 0).has(t, "1 stations found, 2 common addresses probed", "M_SP_NA_1×2")
	cli(t, st, "scan", "--method", "sweep", "--start", "3", "--end", "4", "--format", "table").want(t, 0).has(t, "CA  STATUS")
	cli(t, st, "scan", "--method", "sweep", "--start", "9", "--end", "2").want(t, 3)
	cli(t, st, "scan", "--method", "guess").want(t, 3)

	var tr types.TypesResult
	run(t, "client", "types", "--format", "json").want(t, 0).decode(t, &tr)
	if tr.Count < 60 || tr.Types[0].Name != "M_SP_NA_1" || tr.Types[0].Direction != "monitor" || !tr.Types[0].Decoded {
		t.Errorf("types: %d, first %+v", tr.Count, tr.Types[0])
	}
	r := run(t, "client", "types", "--direction", "control").want(t, 0).has(t, "C_SC_NA_1", "Single command")
	if strings.Contains(r.stdout, "M_SP_NA_1") {
		t.Error("a monitor type in the list of control types")
	}
	run(t, "client", "types", "--format", "table").want(t, 0).has(t, "ID", "DESCRIPTION", "TIME_TAG")
	run(t, "client", "types", "--direction", "sideways").want(t, 3)
}

func TestFiles(t *testing.T) {
	st := stationtest.Start(t, stationtest.Model)
	dir := t.TempDir()

	var list types.FileListResult
	cli(t, st, "file", "list", "--format", "json").want(t, 0).decode(t, &list)
	if list.Count != 2 || list.Entries[0].IOA != 30000 || list.Entries[0].Length != 5000 || list.Entries[1].Name != 2 {
		t.Fatalf("directory: %+v", list)
	}
	cli(t, st, "file", "ls").want(t, 0).has(t, "Directory of station 1", "2 entries", "ioa=30000", "5000 octets")
	cli(t, st, "file", "list", "--format", "table").want(t, 0).has(t, "IOA    NAME  LENGTH")
	cli(t, st, "file", "list", "--ioa", "5").want(t, 6)

	out := filepath.Join(dir, "record.bin")
	var res types.FileTransferResult
	cli(t, st, "file", "get", "--ioa", "30000", "--output", out, "--format", "json").want(t, 0).decode(t, &res)
	want, _ := st.Uploaded(30000)
	got, err := os.ReadFile(out)
	if err != nil || !bytes.Equal(got, want) || res.Length != 5000 || res.Direction != "download" || res.Path != out || len(res.SHA256) != 64 {
		t.Fatalf("fetched %d octets (%v), result %+v", len(got), err, res)
	}
	// To stdout: the content on stdout, the result on stderr.
	r := cli(t, st, "file", "get", "--ioa", "30001", "--name", "2", "--output", "-").want(t, 0)
	if r.stdout != "hello from the station\n" {
		t.Errorf("content on stdout: %q", r.stdout)
	}
	r.errHas(t, "Fetched file ioa=30001 name=2", "sha256:")
	cli(t, st, "file", "get", "--ioa", "30001", "--name", "9", "--output", out).want(t, 6)
	cli(t, st, "file", "get", "--ioa", "30001", "--output", filepath.Join(dir, "missing", "x")).want(t, 6)
	cli(t, st, "file", "get", "--ioa", "30001", "--name", "2", "--output", filepath.Join(dir, "missing", "x")).want(t, 10)
	cli(t, st, "file", "get", "--ioa", "0", "--output", out).want(t, 3)
	cli(t, st, "file", "get", "--ioa", "30001").want(t, 2)

	in := filepath.Join(dir, "settings.bin")
	content := bytes.Repeat([]byte("iec104ctl "), 700)
	if err := os.WriteFile(in, content, 0o600); err != nil {
		t.Fatal(err)
	}
	cli(t, st, "file", "put", "--ioa", "40000", "--input", in, "--section-size", "3000", "--format", "json").want(t, 0).decode(t, &res)
	if res.Direction != "upload" || res.Sections != 3 || res.Length != len(content) {
		t.Errorf("upload: %+v", res)
	}
	if stored, ok := st.Uploaded(40000); !ok || !bytes.Equal(stored, content) {
		t.Errorf("the station holds %d octets, want what was delivered", len(stored))
	}
	cli(t, st, "file", "put", "--ioa", "40001", "--name", "3", "--input", in).want(t, 0).has(t, "Delivered file ioa=40001 name=3 to station 1")
	cli(t, st, "file", "put", "--ioa", "40002", "--input", in, "--format", "table").want(t, 0).has(t, "upload")
	// The delivered file is in the directory and can be fetched back.
	cli(t, st, "file", "list").want(t, 0).has(t, "5 entries", "ioa=40001")
	// An address that holds a point is refused by the station.
	cli(t, st, "file", "put", "--ioa", "100", "--input", in).want(t, 6)
	for _, args := range [][]string{
		{"--ioa", "40003", "--input", filepath.Join(dir, "nope")},
		{"--ioa", "40003", "--input", dir},
		{"--ioa", "0", "--input", in},
		{"--ioa", "40003", "--input", in, "--section-size", "1"},
	} {
		cli(t, st, append([]string{"file", "put"}, args...)...).want(t, 3)
	}
	empty := filepath.Join(dir, "empty")
	_ = os.WriteFile(empty, nil, 0o600)
	cli(t, st, "file", "put", "--ioa", "40003", "--input", empty).want(t, 3).errHas(t, "is empty")
	run(t, "client", "file").want(t, 0).has(t, "Available Commands", "get", "put")
	run(t, "client", "file", "delete").want(t, 2)
}

func TestMonitor(t *testing.T) {
	st := stationtest.Start(t, stationtest.Model)

	t.Run("interrogation first", func(t *testing.T) {
		lines := cli(t, st, "monitor", "--interrogate", "--count", "8", "--format", "json").want(t, 0).errHas(t, "data transfer started", "8 values").lines(t)
		if len(lines) != 8 || lines[0]["cause"] != "interrogated-station" || lines[0]["received_at"] == "" {
			t.Fatalf("monitor: %v", lines)
		}
	})
	t.Run("spontaneous changes", func(t *testing.T) {
		done := make(chan result, 1)
		before := len(st.Events())
		go func() {
			done <- cli(t, st, "monitor", "--ioa", "101,1000", "--ca", "1", "--count", "2", "--no-reconnect")
		}()
		eventually(t, "the monitor to start data transfer", func() bool {
			for _, e := range st.Events()[before:] {
				if e.Event == "started" {
					return true
				}
			}
			return false
		})
		st.Set(100, false) // filtered out
		st.Set(101, true)
		st.Set(1000, 228.25)
		r := (<-done).want(t, 0)
		r.has(t, "M_SP_TB_1", "ioa=101", "on", "spontaneous", "@ 20", "M_ME_TF_1", "228.25")
		if strings.Contains(r.stdout, "ioa=100 ") {
			t.Errorf("a filtered object was shown:\n%s", r.stdout)
		}
	})
	t.Run("duration and filters", func(t *testing.T) {
		r := cli(t, st, "monitor", "--interrogate", "--type", "M_DP_NA_1", "--duration", "1", "--format", "table").want(t, 0)
		r.has(t, "RECEIVED", "M_DP_NA_1").errHas(t, "1 values")
		// With --all the confirmation and termination of the interrogation are shown too.
		cli(t, st, "monitor", "--interrogate", "--all", "--count", "10").want(t, 0).has(t, "C_IC_NA_1", "activation-con")
	})
	t.Run("connection lost", func(t *testing.T) {
		own := stationtest.Start(t, stationtest.Model)
		done := make(chan result, 1)
		go func() { done <- cli(t, own, "monitor", "--no-reconnect") }()
		eventually(t, "the monitor to connect", func() bool { return own.HasEvent("started") })
		_ = own.Close()
		(<-done).want(t, 4).errHas(t, "error: connection error")
	})
	cli(t, st, "monitor", "--ca", "0").want(t, 3)
	cli(t, st, "monitor", "--ca", "x").want(t, 3)
	cli(t, st, "monitor", "--type", "nope").want(t, 3)
}

func TestTLSAndFieldSizes(t *testing.T) {
	certs := stationtest.NewCerts(t)
	st := stationtest.Start(t, stationtest.Model, func(o *station.Options) { o.TLS = certs.ServerTLS })
	url := "tls://" + st.Addr()

	run(t, "client", "interrogate", "--url", url, "--tls-ca", certs.CA, "--tls-cert", certs.ClientCert, "--tls-key", certs.ClientKey).
		want(t, 0).has(t, "at "+url, "8 objects")
	// --ip with a TLS flag uses TLS on the port given.
	run(t, "client", "read", "--ip", st.Host, "--port", strconv.Itoa(int(st.Port)), "--tls", "--tls-ca", certs.CA, "--tls-cert", certs.ClientCert,
		"--tls-key", certs.ClientKey, "--tls-server-name", "localhost", "--ioa", "100").want(t, 0)
	// Without the CA the station's certificate is not trusted.
	kind, code := run(t, "client", "interrogate", "--url", url, "--tls-cert", certs.ClientCert, "--tls-key", certs.ClientKey, "--format", "json").want(t, 4).errorCode(t)
	if kind != "transport" || code != "transport_tls_failed" {
		t.Errorf("error %s/%s", kind, code)
	}
	// Without a client certificate the station turns the client away.
	run(t, "client", "interrogate", "--url", url, "--tls-ca", certs.CA, "--timeout", "2000").want(t, 4)
	run(t, "client", "interrogate", "--url", url, "--tls-insecure", "--tls-cert", certs.ClientCert, "--tls-key", certs.ClientKey).want(t, 0)
	run(t, "client", "interrogate", "--url", url, "--tls-cert", certs.ClientCert).want(t, 3).errHas(t, "must be given together")
	run(t, "client", "interrogate", "--url", url, "--tls-ca", certs.ClientKey).want(t, 3).errHas(t, "no certificates found")
	run(t, "client", "interrogate", "--url", url, "--tls-ca", "/nonexistent.pem").want(t, 3)
	run(t, "client", "interrogate", "--url", url, "--tls-cert", certs.CA, "--tls-key", certs.ClientKey).want(t, 3)

	t.Run("IEC 101 field sizes", func(t *testing.T) {
		narrow := asdu.Params{CauseSize: 1, CommonAddrSize: 1, IOASize: 2}
		st := stationtest.Start(t, stationtest.Model, func(o *station.Options) { o.ASDU = narrow })
		sizes := []string{"--cot-size", "1", "--ca-size", "1", "--ioa-size", "2"}
		cli(t, st, append([]string{"interrogate"}, sizes...)...).want(t, 0).has(t, "8 objects")
		cli(t, st, append([]string{"command", "--type", "single", "--ioa", "5000", "--value", "off"}, sizes...)...).want(t, 0)
	})
}
