// SPDX-License-Identifier: MIT

package cmd

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/otfabric/iec60870ctl/internal/iec103ctl/station"
	"github.com/otfabric/iec60870ctl/internal/iec103ctl/stationtest"
	"github.com/otfabric/iec60870ctl/internal/iec103ctl/types"
)

func TestVersion(t *testing.T) {
	t.Cleanup(func() { SetBuildMeta("dev", "", "", "") })
	SetBuildMeta("1.2.3", "iec103ctl/v1.2.3", "abc", "2026-01-31T12:00:00Z")
	r := run(t, "version").want(t, 0)
	if !strings.HasPrefix(r.stdout, "iec103ctl 1.2.3 (tag iec103ctl/v1.2.3, commit abc") || !strings.Contains(r.stdout, runtime.GOOS) {
		t.Fatalf("%q", r.stdout)
	}
	run(t, "version", "--json").want(t, 0).has(t, `"name":"iec103ctl"`, `"version":"1.2.3"`)
	t.Setenv("IEC103CTL_FORMAT", "json")
	run(t, "version").want(t, 0).has(t, `"name":"iec103ctl"`)
	run(t, "version", "extra").want(t, 2)
}

func TestUsageErrors(t *testing.T) {
	run(t, "nosuchcommand").want(t, 2).errHas(t, `unknown command "nosuchcommand"`)
	run(t, "client", "interrogate", "--no-such-flag").want(t, 2)
	run(t, "client", "command", "--url", "tcp://h").want(t, 2).errHas(t, "required")
	run(t, "client", "interrogate", "--link-address", "70000").want(t, 2)
	run(t, "client").want(t, 0).has(t, "controlling system")
	run(t, "client", "generic").want(t, 0).has(t, "headings")
	run(t, "client", "disturbance").want(t, 0).has(t, "list")
	run(t, "server").want(t, 0).has(t, "generate-config")
}

func TestInvalidInputBeforeIO(t *testing.T) {
	for _, args := range [][]string{
		{"client", "interrogate"},
		{"client", "interrogate", "--url", "udp://h"},
		{"client", "interrogate", "--url", "tcp://h", "--link-address", "255"},
		{"client", "interrogate", "--url", "tcp://h", "--parity", "mark"},
		{"client", "interrogate", "--url", "tcp://h", "--format", "xml"},
		{"client", "command", "--url", "tcp://h", "--fun", "160", "--inf", "19", "--value", "off"},
		{"client", "command", "--url", "tcp://h", "--fun", "160", "--inf", "16", "--value", "3"},
		{"client", "clock", "--url", "tcp://h", "--time", "noon"},
		{"client", "generic", "entry", "--url", "tcp://h", "--gin", "1"},
		{"client", "generic", "write", "--url", "tcp://h", "--gin", "1.1", "--type", "uint", "--value", "1"},
		{"client", "generic", "read", "--url", "tcp://h", "--inf", "250", "--items", "1.1"},
		{"client", "generic", "group", "--url", "tcp://h", "--group", "300"},
		{"client", "disturbance", "get", "--url", "tcp://h", "--fan", "70000"},
		{"client", "disturbance", "list", "--url", "tcp://h", "--fun", "256"},
		{"client", "scan", "--url", "tcp://h", "--start", "9", "--end", "2"},
		{"client", "monitor", "--url", "tcp://h", "--timezone", "Mars/Base"},
		{"server", "start", "--link-address", "255"},
		{"server", "start", "--serial", "/dev/x", "--port", "1"},
		{"server", "generate-config", "--format", "xml"},
	} {
		r := run(t, args...)
		if r.code != 3 {
			t.Errorf("%v: exit %d\n%s%s", args, r.code, r.stdout, r.stderr)
		}
	}
	// --format json renders the error as JSON on stdout.
	r := run(t, "client", "interrogate", "--url", "udp://h", "--format", "json").want(t, 3)
	if kind, code := r.errorCode(t); kind != "invalid_input" || code != "invalid_url" {
		t.Fatal(kind, code)
	}
}

func TestMalformedEnvironment(t *testing.T) {
	t.Setenv("IEC103CTL_TIMEOUT", "soon")
	run(t, "client", "interrogate").want(t, 3).errHas(t, "IEC103CTL_TIMEOUT")
}

func TestCompletion(t *testing.T) {
	for _, sh := range completionShells {
		if r := run(t, "completion", sh).want(t, 0); len(r.stdout) < 100 {
			t.Errorf("%s: %q", sh, r.stdout)
		}
	}
	run(t, "completion", "tcsh").want(t, 2)
	r := run(t, "__complete", "client", "generic", "write", "--type", "").want(t, 0)
	if !strings.Contains(r.stdout, "float32") {
		t.Fatal(r.stdout)
	}
	r = run(t, "__complete", "client", "generic", "entry", "--kod", "").want(t, 0)
	if !strings.Contains(r.stdout, "description") {
		t.Fatal(r.stdout)
	}
}

func TestClientCommands(t *testing.T) {
	st := stationtest.Start(t, stationtest.Model)
	cli(t, st, "interrogate").want(t, 0).has(t, "General interrogation", "fun=160 inf=16")
	var gi types.InterrogateResult
	cli(t, st, "interrogate", "--format", "json").want(t, 0).decode(t, &gi)
	if gi.Count != 8 {
		t.Fatalf("%+v", gi)
	}
	cli(t, st, "gi", "--format", "table").want(t, 0).has(t, "TYPE", "time-tagged-message")
	cli(t, st, "command", "--fun", "160", "--inf", "16", "--value", "off", "--wait-report", "1000").want(t, 0).has(t, "acknowledged", "change of state")
	cli(t, st, "command", "--fun", "160", "--inf", "16", "--value", "on", "--format", "table").want(t, 0).has(t, "ACKNOWLEDGED")
	r := cli(t, st, "command", "--fun", "160", "--inf", "18", "--value", "on", "--format", "json").want(t, 6)
	if _, code := r.errorCode(t); code != "negative_confirmation" {
		t.Fatal(code)
	}
	cli(t, st, "clock").want(t, 0).has(t, "Clock of device 1")
	cli(t, st, "clock", "--broadcast", "--format", "table").want(t, 0).has(t, "BROADCAST")
	var id types.IdentifyResult
	cli(t, st, "identify", "--format", "json").want(t, 0).decode(t, &id)
	if id.Identifications[0].Manufacturer != "OTFAB103" {
		t.Fatalf("%+v", id)
	}
	cli(t, st, "identify").want(t, 0).has(t, "OTFAB103")
	cli(t, st, "identify", "--format", "table").want(t, 0).has(t, "MANUFACTURER")
}

func TestGenericCommands(t *testing.T) {
	st := stationtest.Start(t, stationtest.Model)
	cli(t, st, "generic", "headings").want(t, 0).has(t, "SETTINGS")
	cli(t, st, "generic", "interrogate", "--format", "table").want(t, 0).has(t, "1.5")
	cli(t, st, "generic", "group", "--group", "1", "--kod", "description").want(t, 0).has(t, "PICKUP I>")
	cli(t, st, "generic", "directory", "--gin", "1.4").want(t, 0).has(t, "FEEDER 1", "directory of 1.4")
	cli(t, st, "generic", "entry", "--gin", "1.2").want(t, 0).has(t, "1.5")
	cli(t, st, "generic", "entry", "--gin", "2.1").want(t, 7).has(t, "invalid-gin")
	cli(t, st, "generic", "read", "--items", "1.1,1.4:10", "--format", "json").want(t, 0).has(t, `"FEEDER"`)
	var w types.GenericResult
	cli(t, st, "generic", "write", "--gin", "1.3", "--type", "uint", "--size", "2", "--value", "450", "--format", "json").want(t, 0).decode(t, &w)
	if w.Cause != 40 || w.Mode != "execute" {
		t.Fatalf("%+v", w)
	}
	cli(t, st, "generic", "entry", "--gin", "1.3").want(t, 0).has(t, "450")
	cli(t, st, "generic", "write", "--gin", "1.2", "--type", "float32", "--value", "3", "--mode", "abort").want(t, 0).has(t, "abort")
	cli(t, st, "generic", "write", "--gin", "1.1", "--type", "uint", "--size", "2", "--value", "5").want(t, 6).errHas(t, "read-only")
}

func TestDisturbanceCommands(t *testing.T) {
	st := stationtest.Start(t, stationtest.Model)
	cli(t, st, "disturbance", "list").want(t, 0).has(t, "FAN 7", "trip")
	cli(t, st, "disturbance", "list", "--format", "table").want(t, 0).has(t, "FAN")
	dir := t.TempDir()
	out := filepath.Join(dir, "f.json")
	csv := filepath.Join(dir, "f.csv")
	cli(t, st, "fault", "get", "--fan", "7", "--output", out, "--csv", csv).want(t, 0).has(t, "Disturbance FAN 7", "record written")
	for _, p := range []string{out, csv} {
		if _, err := os.Stat(p); err != nil {
			t.Fatal(err)
		}
	}
	cli(t, st, "disturbance", "get", "--fan", "7", "--output", out).want(t, 3).errHas(t, "--force")
	cli(t, st, "disturbance", "get", "--fan", "7", "--format", "table").want(t, 0).has(t, "IL1")
	var d types.DisturbanceResult
	cli(t, st, "disturbance", "get", "--fan", "7", "--format", "json").want(t, 0).decode(t, &d)
	if d.NOE != 40 || len(d.Channels[0].Values) != 40 {
		t.Fatalf("%+v", d)
	}
}

func TestMonitorAndScan(t *testing.T) {
	st := stationtest.Start(t, stationtest.Model)
	r := cli(t, st, "monitor", "--count", "5", "--interrogate", "--format", "json").want(t, 0)
	if n := strings.Count(strings.TrimSpace(r.stdout), "\n") + 1; n != 5 {
		t.Fatalf("%d lines:\n%s", n, r.stdout)
	}
	r.errHas(t, "monitor ", "5 messages")
	cli(t, st, "monitor", "--count", "2", "--format", "table").want(t, 0).has(t, "RECEIVED")
	cli(t, st, "monitor", "--count", "1").want(t, 0)
	cli(t, st, "monitor", "--count", "1", "--format", "xml").want(t, 3)
	cli(t, st, "scan", "--start", "0", "--end", "2", "--timeout", "300").want(t, 0).has(t, "1 of 3 answered", "OTFAB103")
	cli(t, st, "scan", "--start", "1", "--end", "1", "--format", "table").want(t, 0).has(t, "LINK")
}

func TestNoDevice(t *testing.T) {
	u := stationtest.Refused(t)
	run(t, "client", "interrogate", "--url", u).want(t, 4)
	silent := stationtest.Silent(t)
	run(t, "client", "interrogate", "--url", silent, "--timeout", "300").want(t, 5)
	run(t, "--debug", "client", "identify", "--url", silent, "--timeout", "300").want(t, 5)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	runCtx(t, ctx, "client", "interrogate", "--url", silent).want(t, 130)
}

func TestServerStartAndGenerateConfig(t *testing.T) {
	r := run(t, "server", "generate-config").want(t, 0)
	if r.stdout != station.DemoYAML {
		t.Fatal("generate-config is not the demo model")
	}
	dir := t.TempDir()
	p := filepath.Join(dir, "m.json")
	run(t, "server", "generate-config", "--format", "json", "--output", p).want(t, 0).errHas(t, "wrote")
	run(t, "server", "generate-config", "--format", "json", "--output", p).want(t, 3)
	run(t, "server", "generate-config", "--output", p, "--force", "--format", "json").want(t, 0)
	run(t, "server", "generate-config", "--output", filepath.Join(dir, "no", "x")).want(t, 10)
	start := time.Now()
	r = run(t, "server", "start", "--config", p, "--listen", "127.0.0.1", "--port", freePort(t), "--duration", "1", "--format", "json").want(t, 0)
	if time.Since(start) > 10*time.Second || !strings.Contains(r.stdout, `"event":"listening"`) {
		t.Fatalf("%v %s", time.Since(start), r.stdout)
	}
	run(t, "server", "start", "--config", filepath.Join(dir, "none.yaml")).want(t, 3)
	run(t, "server", "start", "--format", "xml").want(t, 3)
}

func freePort(t *testing.T) string {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ln.Close() }()
	return strconv.Itoa(ln.Addr().(*net.TCPAddr).Port)
}

// failing is a stdout that refuses every write.
type failing struct{}

func (failing) Write([]byte) (int, error) { return 0, errors.New("pipe closed") }

func TestServerStartFormatsAndOutputErrors(t *testing.T) {
	for _, f := range []string{"text", "table"} {
		r := run(t, "server", "start", "--listen", "127.0.0.1", "--port", freePort(t), "--duration", "1", "--format", f).want(t, 0)
		if !strings.Contains(r.stdout, "listening") {
			t.Fatalf("%s: %s", f, r.stdout)
		}
	}
	var stderr syncBuffer
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if code := Run(ctx, []string{"server", "start", "--listen", "127.0.0.1", "--port", freePort(t), "--duration", "1"}, failing{}, &stderr); code != 10 {
		t.Fatalf("exit %d: %s", code, stderr.String())
	}
	if code := Run(ctx, []string{"server", "generate-config"}, failing{}, &stderr); code != 10 {
		t.Fatalf("exit %d", code)
	}
	if code := Run(ctx, []string{"client", "monitor", "--url", stationtest.Start(t, stationtest.Model).URL(), "--count", "1"}, failing{}, &stderr); code != 10 {
		t.Fatalf("exit %d", code)
	}
}
