// SPDX-License-Identifier: MIT

package cmd

import (
	"context"
	"net"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/otfabric/iec60870ctl/internal/iec101ctl/config"
	"github.com/otfabric/iec60870ctl/internal/iec101ctl/stationtest"
	"github.com/otfabric/iec60870ctl/internal/iec101ctl/types"
)

// links are the link configurations every client command is run with: the defaults,
// balanced with distinct addresses, and two-octet fields everywhere.
func links() map[string]*config.LinkConfig {
	bal := config.DefaultLink()
	bal.Mode, bal.LinkAddr, bal.RemoteLinkAddr = config.ModeBalanced, 3, "7"
	wide := config.DefaultLink()
	wide.LinkAddr, wide.LinkAddrSize, wide.CauseSize, wide.CASize, wide.IOASize = 300, 2, 2, 2, 3
	none := config.DefaultLink()
	none.Mode, none.LinkAddrSize = config.ModeBalanced, 0
	return map[string]*config.LinkConfig{"unbalanced": nil, "balanced": &bal, "fields-2223": &wide, "balanced-0112": &none}
}

func TestClientCommandsInEveryLinkMode(t *testing.T) {
	for name, lc := range links() {
		t.Run(name, func(t *testing.T) {
			st := stationtest.Start(t, stationtest.Model, lc)

			var gi types.InterrogateResult
			cli(t, st, "interrogate", "--format", "json").want(t, 0).decode(t, &gi)
			if gi.Received != 8 || gi.Count != 8 || gi.Request != "interrogation" {
				t.Fatalf("interrogation: %+v", gi)
			}
			cli(t, st, "interrogate", "--group", "2", "--format", "table").want(t, 0).has(t, "M_ST_NA_1", "M_BO_NA_1")
			cli(t, st, "gi", "--type", "M_ME_NC_1", "--ioa", "1000-1003").want(t, 0).has(t, "230.5", "1 shown")
			// A group without points is refused.
			cli(t, st, "interrogate", "--group", "9").want(t, 6)

			var ci types.InterrogateResult
			cli(t, st, "counters", "--format", "json").want(t, 0).decode(t, &ci)
			if ci.Received != 2 || ci.Points[0].Attributes["sequence"] != float64(1) {
				t.Fatalf("counters: %+v", ci)
			}
			cli(t, st, "counters", "--group", "2", "--freeze", "freeze").want(t, 0).has(t, "(freeze)")

			var rd types.ReadResult
			cli(t, st, "read", "--ioa", "100,999,2000", "--format", "json").want(t, 7).decode(t, &rd)
			if rd.Requested != 3 || rd.Failed != 1 || rd.Rows[1].Error.Code != "unknown_object_address" {
				t.Fatalf("read: %+v", rd)
			}
			cli(t, st, "read", "--ioa", "1002", "--format", "table").want(t, 0).has(t, "1250")

			var cmd types.CommandResult
			cli(t, st, "command", "--type", "double", "--ioa", "5001", "--value", "off", "--mode", "sbo", "--wait-termination", "3000", "--format", "json").want(t, 0).decode(t, &cmd)
			if len(cmd.Steps) != 3 || cmd.Steps[2].Result != "received" {
				t.Fatalf("command: %+v", cmd)
			}
			if v, _ := st.Value(200); v != "off" {
				t.Errorf("point 200 is %v", v)
			}
			cli(t, st, "cmd", "--type", "C_SE_NB_1", "--ioa", "5004", "--value", "-7").want(t, 0).has(t, "execute")
			cli(t, st, "command", "--type", "single", "--ioa", "5000", "--value", "on", "--mode", "cancel").want(t, 0).has(t, "cancel")
			cli(t, st, "command", "--type", "single", "--ioa", "5000", "--value", "on", "--mode", "select", "--format", "table").want(t, 0).has(t, "select")
			// Refusals: select required, an unknown object, a type mismatch.
			r := cli(t, st, "command", "--type", "double", "--ioa", "5001", "--value", "on", "--format", "json").want(t, 6)
			if _, code := r.errorCode(t); code != "negative_confirmation" {
				t.Errorf("select required: %s", code)
			}
			r = cli(t, st, "command", "--type", "single", "--ioa", "5999", "--value", "on", "--format", "json").want(t, 6)
			if _, code := r.errorCode(t); code != "unknown_object_address" {
				t.Errorf("unknown object: %s", code)
			}
			cli(t, st, "command", "--type", "double", "--ioa", "5000", "--value", "on").want(t, 6).errHas(t, "execute: ")

			cli(t, st, "clock", "--time", "2026-01-31T12:00:00Z").want(t, 0).has(t, "set to 2026-01-31T12:00:00.000Z")
			cli(t, st, "clock", "--timezone", "Europe/Brussels", "--format", "table").want(t, 0).has(t, "clock-synchronization")
			cli(t, st, "reset", "--qualifier", "events").want(t, 0).has(t, "Reset (events)")
			cli(t, st, "reset", "--qualifier", "process", "--format", "json").want(t, 0).has(t, `"reset-process"`)

			var d types.DelayResult
			cli(t, st, "delay", "--set", "auto", "--format", "json").want(t, 0).decode(t, &d)
			if d.MeasuredMs == nil || d.SetMs == nil {
				t.Fatalf("delay: %+v", d)
			}
			cli(t, st, "delay").want(t, 0).has(t, "transmission delay")
			cli(t, st, "delay", "--set", "120", "--format", "table").want(t, 0).has(t, "120")
			if !st.HasEvent("delay") {
				t.Error("no delay event")
			}

			cli(t, st, "ping", "--count", "2", "--interval", "10", "--format", "json").want(t, 0).errHas(t, "2 tests, 0 failed")
			cli(t, st, "ping", "--count", "1", "--method", "link-status", "--format", "table").want(t, 0).has(t, "link-status")
			if lc != nil && lc.Mode == config.ModeBalanced {
				cli(t, st, "ping", "--count", "2", "--interval", "10", "--method", "link-test").want(t, 0).has(t, "link-test")
			}
			// A test command to an unknown common address fails every test: partial success.
			cli(t, st, "ping", "--count", "1", "--ca", "9").want(t, 7).has(t, "error")

			var sc types.ScanResult
			cli(t, st, "scan", "--method", "ca", "--start", "1", "--end", "3", "--timeout", "2000", "--delay", "1", "--format", "json").want(t, 0).decode(t, &sc)
			if sc.Probed != 3 || sc.Found != 1 || sc.Stations[0].CommonAddr != 1 {
				t.Fatalf("scan: %+v", sc)
			}
		})
	}
}

func TestBroadcastAndLinkScan(t *testing.T) {
	old := setScanQuiet(100 * time.Millisecond)
	t.Cleanup(func() { setScanQuiet(old) })
	st := stationtest.Start(t, stationtest.Model, nil)
	var sc types.ScanResult
	cli(t, st, "scan", "--format", "json").want(t, 0).decode(t, &sc)
	if sc.Found != 1 || sc.Stations[0].Objects != 8 {
		t.Fatalf("broadcast scan: %+v", sc)
	}
	cli(t, st, "scan", "--format", "table").want(t, 0).has(t, "found")

	var ls types.LinkScanResult
	r := cli(t, st, "scan", "--method", "link", "--start", "0", "--end", "2", "--delay", "1", "--format", "json").want(t, 0)
	r.decode(t, &ls)
	if ls.Probed != 3 || ls.Found != 1 || ls.Stations[0].LinkAddress != 1 {
		t.Fatalf("link scan: %+v", ls)
	}
	r.errHas(t, "scan: link=1 answered")
	cli(t, st, "scan", "--method", "link", "--start", "1", "--end", "1", "--format", "table").want(t, 0).has(t, "answered")
	cli(t, st, "scan", "--method", "link", "--start", "1", "--end", "1").want(t, 0).has(t, "link=1")
}

func TestMonitor(t *testing.T) {
	st := stationtest.Start(t, stationtest.Model, nil)
	r := cli(t, st, "monitor", "--interrogate", "--count", "3", "--format", "json").want(t, 0)
	if lines := r.lines(t); len(lines) != 3 {
		t.Fatalf("monitor: %d lines\n%s", len(lines), r.stdout)
	}
	r.errHas(t, "monitor: link up", "3 values")

	// Spontaneous events, filtered by type and common address, as a table.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan result, 1)
	go func() {
		done <- runCtx(t, ctx, append(append([]string{"client", "monitor"}, st.Flags()...), "--ca", "1", "--type", "M_SP_TB_1", "--count", "1", "--format", "table")...)
	}()
	eventually(t, "the monitor's link", func() bool { return st.HasEvent("link-up") && len(st.Events()) > 4 })
	for i := 0; i < 50; i++ {
		select {
		case r := <-done:
			r.want(t, 0).has(t, "M_SP_TB_1", "spontaneous")
			return
		default:
		}
		st.Set(100, i%2 == 0)
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("no spontaneous event")
}

func TestMonitorDurationAndLostLine(t *testing.T) {
	st := stationtest.Start(t, stationtest.Model, nil)
	cli(t, st, "monitor", "--duration", "1", "--all").want(t, 0).errHas(t, "M_EI_NA_1×1")

	// A line that closes ends the monitor with --no-reconnect, and is opened again without.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := strconv.Itoa(ln.Addr().(*net.TCPAddr).Port)
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			_ = c.Close()
		}
	}()
	t.Cleanup(func() { _ = ln.Close() })
	run(t, "client", "monitor", "--ip", "127.0.0.1", "--port", port, "--no-reconnect", "--timeout", "2000").want(t, 4)
}

func TestClientInputErrors(t *testing.T) {
	st := stationtest.Start(t, stationtest.Model, nil)
	for _, args := range [][]string{
		{"interrogate", "--group", "17"},
		{"interrogate", "--ca", "0"},
		{"interrogate", "--ca", "256"},
		{"interrogate", "--ioa", "70000"},
		{"interrogate", "--type", "nope"},
		{"interrogate", "--link-mode", "half"},
		{"interrogate", "--link-address-size", "0"},
		{"interrogate", "--link-address-size", "3"},
		{"interrogate", "--cot-size", "3"},
		{"interrogate", "--ca-size", "0"},
		{"interrogate", "--ioa-size", "4"},
		{"interrogate", "--link-address", "255"},
		{"interrogate", "--remote-link-address", "4"},
		{"interrogate", "--timezone", "Mars/Olympus"},
		{"counters", "--group", "5"},
		{"counters", "--freeze", "melt"},
		{"read", "--ioa", ""},
		{"read", "--ca", "255", "--ioa", "1"},
		{"command", "--type", "C_SC_TA_1", "--ioa", "1", "--value", "on"},
		{"command", "--type", "single", "--ioa", "70000", "--value", "on"},
		{"command", "--type", "single", "--ioa", "1", "--value", "on", "--mode", "now"},
		{"command", "--type", "bitstring", "--ioa", "1", "--value", "1", "--mode", "sbo"},
		{"command", "--type", "single", "--ioa", "1", "--value", "on", "--mode", "select", "--wait-termination", "10"},
		{"clock", "--time", "yesterday"},
		{"clock", "--ca", "0"},
		{"clock", "--timezone", "Nowhere/Else"},
		{"reset", "--ca", "0", "--qualifier", "events"},
		{"delay", "--ca", "255"},
		{"counters", "--ca", "0"},
		{"command", "--ca", "0", "--type", "single", "--ioa", "1", "--value", "on"},
		{"reset", "--qualifier", "all"},
		{"delay", "--set", "60000"},
		{"ping", "--method", "testfr"},
		{"ping", "--method", "link-test"},
		{"ping", "--interval", "0"},
		{"ping", "--ca", "0"},
		{"scan", "--method", "sweep"},
		{"scan", "--method", "ca", "--start", "0"},
		{"scan", "--method", "link", "--end", "255"},
		{"monitor", "--ca", "x"},
		{"monitor", "--ioa", "1-"},
	} {
		r := cli(t, st, args[0], append(args[1:], "--format", "json")...)
		if r.code != 3 {
			t.Errorf("%v: exit %d, want 3\n%s%s", args, r.code, r.stdout, r.stderr)
			continue
		}
		if kind, _ := r.errorCode(t); kind != "invalid_input" {
			t.Errorf("%v: kind %s", args, kind)
		}
	}
	// The line itself.
	run(t, "client", "interrogate").want(t, 3).errHas(t, "no station given")
	run(t, "client", "interrogate", "--url", "tcp://h", "--ip", "h").want(t, 3)
	run(t, "client", "interrogate", "--url", "udp://h").want(t, 3)
	run(t, "client", "interrogate", "--url", "serial:///dev/x", "--parity", "mark").want(t, 3)
	run(t, "client", "scan", "--url", "tcp://h", "--method", "link", "--link-mode", "balanced").want(t, 3)
	run(t, "client", "types", "--format", "xml").want(t, 3)
	run(t, "client", "types", "--direction", "up").want(t, 3)
}

func TestClientLineErrors(t *testing.T) {
	// Nothing listens: the connection is refused.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := strconv.Itoa(ln.Addr().(*net.TCPAddr).Port)
	_ = ln.Close()
	r := run(t, "client", "interrogate", "--ip", "127.0.0.1", "--port", port, "--format", "json").want(t, 4)
	if _, code := r.errorCode(t); code != "transport_connect_failed" {
		t.Errorf("refused: %s", code)
	}
	for _, sub := range []string{"ping", "scan"} {
		args := []string{"client", sub, "--ip", "127.0.0.1", "--port", port, "--method", "link-status"}
		if sub == "scan" {
			args[len(args)-1] = "link"
		}
		run(t, args...).want(t, 4)
	}

	// Something listens and never answers: the link does not come up.
	silent, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = silent.Close() })
	go func() {
		for {
			c, err := silent.Accept()
			if err != nil {
				return
			}
			t.Cleanup(func() { _ = c.Close() })
		}
	}()
	port = strconv.Itoa(silent.Addr().(*net.TCPAddr).Port)
	r = run(t, "client", "read", "--ioa", "1", "--ip", "127.0.0.1", "--port", port, "--timeout", "500", "--link-timeout", "100", "--format", "json").want(t, 5)
	if _, code := r.errorCode(t); code != "link_init_failed" {
		t.Errorf("silent: %s", code)
	}
	run(t, "client", "ping", "--ip", "127.0.0.1", "--port", port, "--method", "link-status", "--count", "1", "--link-timeout", "100").want(t, 7)
	run(t, "client", "ping", "--ip", "127.0.0.1", "--port", port, "--method", "link-test", "--link-mode", "balanced", "--count", "1", "--link-timeout", "100", "--timeout", "300").want(t, 5)
	r = run(t, "client", "scan", "--ip", "127.0.0.1", "--port", port, "--method", "link", "--start", "1", "--end", "2", "--link-timeout", "50").want(t, 0)
	if !strings.Contains(r.stdout, "0 stations found, 2 link addresses probed") {
		t.Errorf("silent link scan: %s", r.stdout)
	}
}

func TestClientDebugTrace(t *testing.T) {
	st := stationtest.Start(t, stationtest.Model, nil)
	r := run(t, append(append([]string{"--debug", "client", "read"}, st.Flags()...), "--ioa", "100")...).want(t, 0)
	r.errHas(t, "debug: ", "-> PRM", "C_RD_NA_1", "<- ")
	r = run(t, append([]string{"--debug", "client", "ping", "--method", "link-status", "--count", "1"}, st.Flags()...)...).want(t, 0)
	r.errHas(t, "fn=9")
}

func TestTypes(t *testing.T) {
	var res types.TypesResult
	run(t, "client", "types", "--format", "json").want(t, 0).decode(t, &res)
	for _, ti := range res.Types {
		if ti.ID >= 58 && ti.ID <= 64 || ti.ID == 107 || ti.ID == 127 {
			t.Errorf("type %s is not of IEC 60870-5-101", ti.Name)
		}
	}
	run(t, "client", "types", "--direction", "control", "--format", "table").want(t, 0).has(t, "C_CD_NA_1").has(t, "C_TS_NA_1")
	run(t, "client", "types").want(t, 0).has(t, "M_EI_NA_1")
}
