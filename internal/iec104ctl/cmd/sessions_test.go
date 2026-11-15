// SPDX-License-Identifier: MIT

package cmd

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/otfabric/iec60870ctl/internal/iec104ctl/config"
	"github.com/otfabric/iec60870ctl/internal/iec104ctl/stationtest"
)

// startServer runs `iec104ctl server start` on loopback with args until the test ends,
// and returns its port and its event log (JSON Lines) as it grows.
func startServer(t *testing.T, args ...string) (port string, log *syncBuffer) {
	t.Helper()
	port = strconv.Itoa(int(stationtest.FreePort(t)))
	ctx, stop := context.WithCancel(context.Background())
	var stdout, stderr syncBuffer
	done := make(chan int, 1)
	go func() {
		full := append([]string{"server", "start", "--listen", "127.0.0.1", "--port", port, "--format", "json"}, args...)
		done <- Run(ctx, full, &stdout, &stderr)
	}()
	t.Cleanup(func() {
		stop()
		if code := <-done; code != 0 {
			t.Errorf("server start: exit code %d\n%s", code, stderr.String())
		}
	})
	eventually(t, "the server to listen", func() bool { return strings.Contains(stdout.String(), `"event":"listening"`) })
	return port, &stdout
}

// connected counts the connections the server's event log reports as served.
func connected(log *syncBuffer) int { return strings.Count(log.String(), `"event":"connected"`) }

// dialRaw opens a TCP connection to the server and closes it when the test ends.
func dialRaw(t *testing.T, port string) net.Conn {
	t.Helper()
	c, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", port), 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c
}

// server start serves config.DefaultMaxSessions connections unless --max-sessions says
// otherwise, and closes a further one at once; --max-sessions 0 serves any number.
func TestServerStartSessionLimit(t *testing.T) {
	t.Run("default", func(t *testing.T) {
		port, log := startServer(t)
		for range config.DefaultMaxSessions {
			dialRaw(t, port)
		}
		eventually(t, "the allowed connections to be served", func() bool { return connected(log) == config.DefaultMaxSessions })
		c := dialRaw(t, port)
		_ = c.SetReadDeadline(time.Now().Add(5 * time.Second))
		n, err := c.Read(make([]byte, 1))
		var ne net.Error
		if err == nil || (errors.As(err, &ne) && ne.Timeout()) {
			t.Fatalf("connection %d beyond the default limit: read %d octets, %v; want it closed at once", config.DefaultMaxSessions+1, n, err)
		}
		if got := connected(log); got != config.DefaultMaxSessions {
			t.Errorf("%d connections served, want %d", got, config.DefaultMaxSessions)
		}
	})
	t.Run("no limit", func(t *testing.T) {
		port, log := startServer(t, "--max-sessions", "0")
		for range config.DefaultMaxSessions + 1 {
			dialRaw(t, port)
		}
		eventually(t, "every connection to be served", func() bool { return connected(log) == config.DefaultMaxSessions+1 })
	})
}

// The built-in demo station takes no files: a delivery is refused by the station.
func TestServerStartDemoTakesNoFiles(t *testing.T) {
	port, log := startServer(t)
	in := filepath.Join(t.TempDir(), "settings.bin")
	if err := os.WriteFile(in, []byte("iec104ctl demo upload\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	run(t, "client", "file", "put", "--ip", "127.0.0.1", "--port", port, "--t1", "2", "--t2", "1",
		"--ioa", "40000", "--input", in).want(t, 6)
	for _, line := range strings.Split(log.String(), "\n") {
		if strings.Contains(line, `"event":"file-received"`) && !strings.Contains(line, "refused") {
			t.Errorf("the demo station took a file: %s", line)
		}
	}
}
