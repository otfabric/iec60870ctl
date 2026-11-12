// SPDX-License-Identifier: MIT

package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"

	serial "github.com/otfabric/go-serial"
	"github.com/otfabric/iec60870ctl/internal/iec101ctl/station"
	"github.com/otfabric/iec60870ctl/internal/iec101ctl/stationtest"
	"github.com/otfabric/iec60870ctl/internal/line"
)

// freePort returns a TCP port that nothing listens on.
func freePort(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ln.Close() }()
	return strconv.Itoa(ln.Addr().(*net.TCPAddr).Port)
}

// serve runs `iec101ctl server start` with args until the test ends and returns its output.
func serve(t *testing.T, args ...string) (*syncBuffer, func() result) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	var stdout, stderr syncBuffer
	done := make(chan int, 1)
	go func() { done <- Run(ctx, append([]string{"server", "start"}, args...), &stdout, &stderr) }()
	stop := func() result {
		cancel()
		code := <-done
		return result{stdout.String(), stderr.String(), code}
	}
	t.Cleanup(func() { cancel() })
	return &stdout, stop
}

func TestServerStartServesClients(t *testing.T) {
	for name, link := range map[string][]string{
		"unbalanced": nil,
		"balanced":   {"--link-mode", "balanced", "--link-address", "2", "--remote-link-address", "9", "--link-address-size", "2", "--cot-size", "2", "--ca-size", "2", "--ioa-size", "3"},
	} {
		t.Run(name, func(t *testing.T) {
			port := freePort(t)
			out, stop := serve(t, append([]string{"--listen", "127.0.0.1", "--port", port, "--format", "json", "--link-timeout", "200", "--single-char-ack"}, link...)...)
			eventually(t, "listening", func() bool { return strings.Contains(out.String(), `"listening"`) })
			client := append([]string{"--ip", "127.0.0.1", "--port", port, "--link-timeout", "200", "--poll-interval", "10"}, link...)
			run(t, append([]string{"client", "interrogate"}, client...)...).want(t, 0).has(t, "M_ME_NC_1")
			// A line has one controlling station: the next waits until the station saw the
			// first one go.
			eventually(t, "disconnected", func() bool { return strings.Contains(out.String(), `"disconnected"`) })
			run(t, append([]string{"client", "command", "--type", "single", "--ioa", "5000", "--value", "off"}, client...)...).want(t, 0)
			r := stop().want(t, 0)
			for _, e := range []string{`"connected"`, `"link-up"`, `"interrogation"`, `"command"`, `"disconnected"`, `"stopping"`} {
				if !strings.Contains(r.stdout, e) {
					t.Errorf("no %s event:\n%s", e, r.stdout)
				}
			}
		})
	}
}

func TestServerStartBusyAndDebug(t *testing.T) {
	port := freePort(t)
	out, stop := serve(t, "--listen", "127.0.0.1", "--port", port, "--link-timeout", "200", "--no-end-of-init")
	eventually(t, "listening", func() bool { return strings.Contains(out.String(), "listening") })
	first, err := net.Dial("tcp", "127.0.0.1:"+port)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = first.Close() }()
	eventually(t, "connected", func() bool { return strings.Contains(out.String(), "connected") })
	second, err := net.Dial("tcp", "127.0.0.1:"+port)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.ReadAll(second) // closed at once
	eventually(t, "busy", func() bool { return strings.Contains(out.String(), "busy") })
	stop().want(t, 0)

	// --debug traces the frames to stderr.
	port = freePort(t)
	var stderr syncBuffer
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan int, 1)
	var stdout syncBuffer
	go func() {
		done <- Run(ctx, []string{"--debug", "server", "start", "--listen", "127.0.0.1", "--port", port, "--format", "table"}, &stdout, &stderr)
	}()
	eventually(t, "listening", func() bool { return strings.Contains(stdout.String(), "listening") })
	run(t, "client", "ping", "--ip", "127.0.0.1", "--port", port, "--count", "1").want(t, 0)
	cancel()
	if code := <-done; code != 0 {
		t.Fatalf("exit %d", code)
	}
	if !strings.Contains(stderr.String(), "debug: ") || !strings.Contains(stdout.String(), "TIMESTAMP") {
		t.Errorf("stderr %q\nstdout %q", stderr.String(), stdout.String())
	}
}

func TestServerStartDuration(t *testing.T) {
	dir := t.TempDir()
	model := filepath.Join(dir, "station.yaml")
	if err := os.WriteFile(model, []byte(stationtest.Model), 0o600); err != nil {
		t.Fatal(err)
	}
	run(t, "server", "start", "--config", model, "--listen", "127.0.0.1", "--port", freePort(t), "--duration", "1").want(t, 0).has(t, "Test station", "stopping")
}

// pipeSerial makes line.OpenSerial hand out the two ends of one in-memory line: the first
// port opened gets one end, the next the other.
func pipeSerial(t *testing.T) {
	t.Helper()
	a, b := net.Pipe()
	var mu sync.Mutex
	ends := []io.ReadWriteCloser{a, b}
	old := line.OpenSerial
	line.OpenSerial = func(c *serial.Config) (io.ReadWriteCloser, error) {
		mu.Lock()
		defer mu.Unlock()
		if c.Address == "/dev/missing" || len(ends) == 0 {
			return nil, errors.New("no such device")
		}
		e := ends[0]
		ends = ends[1:]
		return e, nil
	}
	t.Cleanup(func() { line.OpenSerial = old; _ = a.Close(); _ = b.Close() })
}

func TestServerStartOnSerialPort(t *testing.T) {
	pipeSerial(t)
	out, stop := serve(t, "--serial", "/dev/ttyS9", "--baud", "19200", "--link-timeout", "200")
	eventually(t, "listening", func() bool { return strings.Contains(out.String(), "serial:///dev/ttyS9") })
	run(t, "client", "read", "--url", "serial:///dev/ttyS1", "--baud", "19200", "--ioa", "100", "--link-timeout", "200", "--poll-interval", "10").want(t, 0).has(t, "ioa=100")
	// The client closed its end: to the station the port is gone.
	eventually(t, "disconnected", func() bool { return strings.Contains(out.String(), "disconnected") })
	stop().want(t, 4).errHas(t, "serial:///dev/ttyS9")

	// A port that cannot be opened, and a port that closes under the station.
	run(t, "server", "start", "--serial", "/dev/missing").want(t, 4)
	pipeSerial(t)
	a, _ := line.OpenSerial(&serial.Config{Address: "x"})
	b, _ := line.OpenSerial(&serial.Config{Address: "y"})
	_ = b.Close()
	_ = a
	line.OpenSerial = func(*serial.Config) (io.ReadWriteCloser, error) { return a, nil }
	run(t, "server", "start", "--serial", "/dev/ttyS9").want(t, 4)
}

func TestServerStartErrors(t *testing.T) {
	dir := t.TempDir()
	bad := filepath.Join(dir, "bad.yaml")
	if err := os.WriteFile(bad, []byte("station: {common_address: 0}\npoints: []\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	big := filepath.Join(dir, "big.yaml")
	if err := os.WriteFile(big, []byte("station: {common_address: 1}\npoints: [{ioa: 70000, type: M_SP_NA_1, value: true}]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"--config", filepath.Join(dir, "missing.yaml")},
		{"--config", bad},
		{"--config", big},
		{"--ca-size", "3"},
		{"--link-mode", "balanced", "--link-address-size", "3"},
		{"--serial", "/dev/x", "--port", "1"},
		{"--serial", "/dev/x", "--parity", "mark"},
		{"--format", "xml"},
	} {
		r := run(t, append([]string{"server", "start"}, args...)...)
		if r.code != 3 {
			t.Errorf("%v: exit %d\n%s", args, r.code, r.stderr)
		}
	}
	// A port that is taken.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ln.Close() }()
	port := strconv.Itoa(ln.Addr().(*net.TCPAddr).Port)
	run(t, "server", "start", "--listen", "127.0.0.1", "--port", port, "--format", "json").want(t, 4).has(t, "listen_failed")
}

func TestGenerateConfig(t *testing.T) {
	r := run(t, "server", "generate-config").want(t, 0).has(t, "# iec101ctl station model", "commands:")
	if _, err := station.Parse([]byte(r.stdout)); err != nil {
		t.Fatal(err)
	}
	r = run(t, "server", "generate-config", "--format", "json").want(t, 0)
	var m map[string]any
	if err := json.Unmarshal([]byte(r.stdout), &m); err != nil {
		t.Fatalf("not JSON: %v", err)
	}
	dir := t.TempDir()
	out := filepath.Join(dir, "station.yaml")
	run(t, "server", "generate-config", "--output", out).want(t, 0).errHas(t, "wrote")
	run(t, "server", "generate-config", "--output", out).want(t, 3).errHas(t, "--force")
	run(t, "server", "generate-config", "--output", out, "--force").want(t, 0)
	run(t, "server", "generate-config", "--output", filepath.Join(dir, "no", "such", "dir.yaml")).want(t, 10)
	run(t, "server", "generate-config", "--format", "toml").want(t, 3)
	// The generated model is served.
	run(t, "server", "start", "--config", out, "--listen", "127.0.0.1", "--port", freePort(t), "--duration", "1").want(t, 0)
}

func TestDiscover(t *testing.T) {
	st := stationtest.Start(t, stationtest.Model, nil)
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
	ports := strconv.Itoa(int(st.Port)) + "," + strconv.Itoa(silent.Addr().(*net.TCPAddr).Port) + "," + freePort(t)
	r := run(t, "--debug", "discover", "--subnets", "127.0.0.1", "--ports", ports, "--timeout", "300", "--format", "json").want(t, 0)
	var res struct {
		Found   int
		Devices []struct {
			Status string
			Port   uint16
		}
	}
	if err := json.Unmarshal([]byte(r.stdout), &res); err != nil {
		t.Fatal(err)
	}
	if res.Found != 1 || len(res.Devices) != 2 || res.Devices[0].Status == res.Devices[1].Status {
		t.Fatalf("discover: %s", r.stdout)
	}
	r.errHas(t, "discover: 127.0.0.1 port")
	run(t, "discover", "--subnets", "127.0.0.1", "--ports", strconv.Itoa(int(st.Port)), "--format", "table").want(t, 0).has(t, "iec101")
	run(t, "discover", "--subnets", "127.0.0.1", "--ports", strconv.Itoa(int(st.Port))).want(t, 0).has(t, "1 IEC 60870-5-101 stations")

	for _, args := range [][]string{
		{"--subnets", ""},
		{"--subnets", "10.0.0.0/8"},
		{"--subnets", "10.0.0.1", "--ports", "0"},
		{"--subnets", "10.0.0.1", "--parallel", "0"},
		{"--subnets", "10.0.0.1", "--link-address-size", "0"},
		{"--subnets", "fe80::/64"},
		{"--subnets", "10.0.0.9-3"},
	} {
		if r := run(t, append([]string{"discover"}, args...)...); r.code != 3 {
			t.Errorf("%v: exit %d %s", args, r.code, r.stderr)
		}
	}
	run(t, "discover").want(t, 2)
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("stdout closed") }

// Output that cannot be written is an output error: exit code 10.
func TestServerOutputFails(t *testing.T) {
	var stderr syncBuffer
	if code := Run(context.Background(), []string{"server", "generate-config"}, failingWriter{}, &stderr); code != 10 {
		t.Errorf("generate-config: exit %d", code)
	}
	if code := Run(context.Background(), []string{"server", "start", "--listen", "127.0.0.1", "--port", freePort(t), "--duration", "1"}, failingWriter{}, &stderr); code != 10 {
		t.Errorf("start: exit %d\n%s", code, stderr.String())
	}
}
