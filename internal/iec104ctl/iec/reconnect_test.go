// SPDX-License-Identifier: MIT

package iec_test

import (
	"bytes"
	"context"
	"io"
	"net"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/otfabric/go-iec60870-5/iec104/client"
	"github.com/otfabric/iec60870ctl/internal/iec104ctl/config"
	"github.com/otfabric/iec60870ctl/internal/iec104ctl/iec"
	"github.com/otfabric/iec60870ctl/internal/iec104ctl/stationtest"
	"github.com/otfabric/iec60870ctl/internal/iec104ctl/types"
)

// giObjects is how many values the general interrogation of stationtest.Model answers with:
// every point but the two integrated totals.
const giObjects = 8

// proxy passes TCP connections through to a station; cut closes the connections passed so
// far, as a station that closes its session does.
type proxy struct {
	ln     net.Listener
	target string
	wg     sync.WaitGroup
	mu     sync.Mutex
	conns  []net.Conn
}

func startProxy(t *testing.T, target string) *proxy {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	p := &proxy{ln: ln, target: target}
	p.wg.Go(func() {
		for {
			down, err := ln.Accept()
			if err != nil {
				return
			}
			up, err := net.Dial("tcp", target)
			if err != nil {
				_ = down.Close()
				continue
			}
			p.mu.Lock()
			p.conns = append(p.conns, down, up)
			p.mu.Unlock()
			p.wg.Go(func() { _, _ = io.Copy(up, down); _ = up.Close(); _ = down.Close() })
			p.wg.Go(func() { _, _ = io.Copy(down, up); _ = up.Close(); _ = down.Close() })
		}
	})
	t.Cleanup(p.close)
	return p
}

func (p *proxy) cut() {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, c := range p.conns {
		_ = c.Close()
	}
	p.conns = nil
}

func (p *proxy) close() {
	_ = p.ln.Close()
	p.cut()
	p.wg.Wait()
}

func (p *proxy) conn() config.ConnConfig {
	_, port, _ := net.SplitHostPort(p.ln.Addr().String())
	n, _ := strconv.ParseUint(port, 10, 16)
	return config.ConnConfig{IP: "127.0.0.1", Port: uint16(n), Timeout: 2000, LinkConfig: config.LinkConfig{T1: 2, T2: 1}}
}

// lines is a status writer that hands every line to a channel as well.
type lines struct {
	mu  sync.Mutex
	buf bytes.Buffer
	ch  chan string
}

func (l *lines) Write(b []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.buf.Write(b)
	for {
		line, err := l.buf.ReadString('\n')
		if err != nil {
			l.buf.WriteString(line)
			return len(b), nil
		}
		select {
		case l.ch <- strings.TrimSpace(line):
		default:
		}
	}
}

// waitFor receives from ch until match reports true, for at most bound.
func waitFor[T any](t *testing.T, ch <-chan T, bound time.Duration, what string, match func(T) bool) {
	t.Helper()
	deadline := time.After(bound)
	for {
		select {
		case v := <-ch:
			if match(v) {
				return
			}
		case <-deadline:
			t.Fatalf("timed out waiting for %s", what)
		}
	}
}

// goroutineIDs returns the IDs of the goroutines that are running.
func goroutineIDs() map[string]string {
	buf := make([]byte, 1<<20)
	buf = buf[:runtime.Stack(buf, true)]
	out := map[string]string{}
	for _, g := range strings.Split(string(buf), "\n\n") {
		if m := goroutineHeader.FindStringSubmatch(g); m != nil {
			out[m[1]] = g
		}
	}
	return out
}

var goroutineHeader = regexp.MustCompile(`^goroutine (\d+) \[`)

// noGoroutinesLeft fails t when a goroutine that was not running at before is still running
// once everything has had five seconds to end. There is no tolerance: every goroutine the
// code under test started must have ended.
func noGoroutinesLeft(t *testing.T, before map[string]string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	poll := time.NewTicker(10 * time.Millisecond)
	defer poll.Stop()
	for {
		var left []string
		for id, stack := range goroutineIDs() {
			if _, ok := before[id]; !ok && !strings.Contains(stack, "noGoroutinesLeft") {
				left = append(left, stack)
			}
		}
		if len(left) == 0 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("%d goroutines started by the test are still running:\n%s", len(left), strings.Join(left, "\n\n"))
		}
		<-poll.C
	}
}

// When the station closes the session, client monitor connects again: the summary counts
// one reconnect, and with --interrogate the station is interrogated again on the new
// connection, so the stream has the values of both interrogations.
func TestMonitorReconnectsAfterTheStationCloses(t *testing.T) {
	defer iec.SetMonitorReconnect(client.Reconnect{MinDelay: 10 * time.Millisecond, MaxDelay: 10 * time.Millisecond})()
	st := stationtest.Start(t, stationtest.Model)
	p := startProxy(t, st.Addr())

	values := make(chan *types.Point, 4*giObjects)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cfg := &config.MonitorConfig{ConnConfig: p.conn(), Interrogate: true}
	type out struct {
		sum iec.MonitorSummary
		err error
	}
	done := make(chan out, 1)
	go func() {
		sum, err := iec.Monitor(ctx, cfg, io.Discard, func(v *types.Point) error { values <- v; return nil })
		done <- out{sum, err}
	}()
	n := 0
	waitFor(t, values, 10*time.Second, "the values of the first interrogation", func(*types.Point) bool { n++; return n == giObjects })
	p.cut()
	waitFor(t, values, 10*time.Second, "the values of the second interrogation", func(*types.Point) bool { n++; return n == 2*giObjects })
	cancel()
	var o out
	select {
	case o = <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("monitor did not end after the cancel")
	}
	if o.err != nil {
		t.Fatal(o.err)
	}
	if o.sum.Reconnects != 1 || o.sum.Values != 2*giObjects {
		t.Errorf("summary %+v, want 1 reconnect and %d values", o.sum, 2*giObjects)
	}
	interrogations := func() (n int) {
		for _, e := range st.Events() {
			if e.Event == "interrogation" {
				n++
			}
		}
		return n
	}
	deadline := time.Now().Add(5 * time.Second)
	for interrogations() < 2 && time.Now().Before(deadline) {
		runtime.Gosched()
	}
	if got := interrogations(); got != 2 {
		t.Errorf("the station logged %d interrogations, want 2", got)
	}
}

// A cancel while the monitor waits to connect again ends it at once, not after the pause,
// and leaves no goroutine running.
func TestMonitorCancelDuringTheReconnectPause(t *testing.T) {
	defer iec.SetMonitorReconnect(client.Reconnect{MinDelay: time.Hour, MaxDelay: time.Hour})()
	st := stationtest.Start(t, stationtest.Model)
	p := startProxy(t, st.Addr())
	before := goroutineIDs()

	values := make(chan *types.Point, 2*giObjects)
	status := &lines{ch: make(chan string, 64)}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cfg := &config.MonitorConfig{ConnConfig: p.conn(), Interrogate: true}
	done := make(chan error, 1)
	go func() {
		_, err := iec.Monitor(ctx, cfg, status, func(v *types.Point) error { values <- v; return nil })
		done <- err
	}()
	n := 0
	waitFor(t, values, 10*time.Second, "the values of the interrogation", func(*types.Point) bool { n++; return n == giObjects })
	p.cut()
	waitFor(t, status.ch, 10*time.Second, "the lost connection", func(l string) bool {
		return strings.HasPrefix(l, "monitor: connecting: ") && strings.Contains(l, "connection lost")
	})
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("monitor did not end within 5 s of the cancel: it waits for the reconnect pause of an hour")
	}
	p.close()
	noGoroutinesLeft(t, before)
}
