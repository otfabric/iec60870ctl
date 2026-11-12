// SPDX-License-Identifier: MIT

//go:build e2e

package e2e

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/otfabric/go-iec60870-5/asdu"
	"github.com/otfabric/go-iec60870-5/iec101"
	"github.com/otfabric/go-iec60870-5/iec101/client"
	"github.com/otfabric/go-iec60870-5/iec101/server"
)

// bin returns the path of the built binary (make e2e sets E2E_BIN_DIR).
func bin(t *testing.T) string {
	t.Helper()
	dir := os.Getenv("E2E_BIN_DIR")
	if dir == "" {
		t.Fatal("E2E_BIN_DIR is not set; run make e2e")
	}
	return filepath.Join(dir, "iec101ctl")
}

// run executes the binary and returns stdout, stderr and the exit code.
func run(t *testing.T, args ...string) (string, string, int) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	var out, errb bytes.Buffer
	cmd := exec.CommandContext(ctx, bin(t), args...)
	cmd.Stdout, cmd.Stderr = &out, &errb
	err := cmd.Run()
	var exit *exec.ExitError
	switch {
	case err == nil:
		return out.String(), errb.String(), 0
	case errors.As(err, &exit):
		return out.String(), errb.String(), exit.ExitCode()
	}
	t.Fatalf("run %s: %v", bin(t), err)
	return "", "", -1
}

func TestVersion(t *testing.T) {
	out, _, code := run(t, "version")
	if code != 0 || !strings.HasPrefix(out, "iec101ctl ") {
		t.Fatalf("exit %d: %s", code, out)
	}
	out, _, code = run(t, "version", "--json")
	var v struct{ Name string }
	if code != 0 || json.Unmarshal([]byte(out), &v) != nil || v.Name != "iec101ctl" {
		t.Fatalf("exit %d: %s", code, out)
	}
}

// An unknown command is a usage error: exit code 2 in docs/exitcodes.md.
func TestUnknownCommandIsAUsageError(t *testing.T) {
	_, errOut, code := run(t, "nosuchcommand")
	if code != 2 || !strings.Contains(errOut, `unknown command "nosuchcommand"`) {
		t.Fatalf("exit %d: %s", code, errOut)
	}
}

const station asdu.CommonAddr = 7

// handler is a small controlled station: two points, a counter, a select-before-operate
// single command at 500 that drives point 100, clock, delay, test and reset.
type handler struct {
	mu       sync.Mutex
	point    bool
	selected bool
	commands int
}

func (h *handler) HandleASDU(s *server.Server, a *asdu.ASDU) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if a.CommonAddr != station && !s.Params().ASDU.IsBroadcast(a.CommonAddr) {
		_ = s.Reject(a, asdu.CauseUnknownCommonAddr)
		return
	}
	switch o := a.First().(type) {
	case asdu.Interrogation:
		r := *a
		r.CommonAddr = station
		_ = s.Confirm(&r)
		_ = s.Send(asdu.New(o.Qualifier.Cause(), station, asdu.SinglePoint{IOA: 100, Value: h.point}, asdu.SinglePoint{IOA: 101, Value: true}))
		_ = s.Send(asdu.New(o.Qualifier.Cause(), station, asdu.MeasuredFloat{IOA: 200, Value: 49.5}))
		_ = s.Terminate(&r)
	case asdu.CounterInterrogation:
		_ = s.Confirm(a)
		_ = s.Send(asdu.New(o.Cause(), station, asdu.IntegratedTotal{IOA: 300, Value: 123456, Sequence: 1}))
		_ = s.Terminate(a)
	case asdu.Read:
		if o.IOA != 200 {
			_ = s.Reject(a, asdu.CauseUnknownIOA)
			return
		}
		_ = s.Send(asdu.New(asdu.CauseRequest, station, asdu.MeasuredFloat{IOA: 200, Value: 49.5}))
	case asdu.SingleCommand:
		switch {
		case o.IOA != 500:
			_ = s.Reject(a, asdu.CauseUnknownIOA)
		case a.Cause == asdu.CauseDeactivation:
			h.selected = false
			_ = s.Confirm(a)
		case o.Select:
			h.selected = true
			_ = s.Confirm(a)
		case !h.selected:
			_ = s.Negative(a)
		default:
			h.selected, h.point = false, o.Value
			h.commands++
			_ = s.Confirm(a)
			_ = s.Send(asdu.New(asdu.CauseReturnRemote, station, asdu.SinglePoint{IOA: 100, Value: o.Value}))
			_ = s.Terminate(a)
		}
	case asdu.DelayAcquisition:
		if a.Cause == asdu.CauseActivation {
			r := a.Reply(asdu.CauseActivationCon, false)
			r.Objects = []asdu.InformationObject{asdu.DelayAcquisition{Delay: (o.Delay + 5) % 60000}}
			_ = s.Send(r)
		}
	case asdu.ClockSync, asdu.TestCommand, asdu.ResetProcess:
		_ = s.Confirm(a)
	default:
		_ = s.Reject(a, asdu.CauseUnknownType)
	}
}

// modes are the link flags of the runs: the defaults, and balanced with two-octet fields.
var modes = map[string][]string{
	"unbalanced": {"--link-address", "3"},
	"balanced": {"--link-mode", "balanced", "--link-address", "3", "--remote-link-address", "1",
		"--link-address-size", "2", "--cot-size", "2", "--ca-size", "2", "--ioa-size", "3"},
}

func params(mode string) iec101.Params {
	p := iec101.DefaultParams()
	p.Link.Timeout = 200 * time.Millisecond
	if mode == "balanced" {
		p.Mode = iec101.ModeBalanced
		p.Link.FT12.AddrSize, p.ASDU.CauseSize, p.ASDU.CommonAddrSize, p.ASDU.IOASize = 2, 2, 2, 3
	}
	return p
}

// serveLibrary runs go-iec60870-5's controlled station on a loopback port, one line after
// the other, and returns the port.
func serveLibrary(t *testing.T, mode string, h *handler) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			s, err := server.New(c, server.Config{Params: params(mode), LinkAddr: 3, Remote: 1, CommonAddr: station, Handler: h, RetryInterval: 50 * time.Millisecond})
			if err != nil {
				t.Error(err)
				_ = c.Close()
				continue
			}
			<-s.Done()
			_ = s.Close()
		}
	}()
	t.Cleanup(func() { _ = ln.Close(); <-done })
	return strconv.Itoa(ln.Addr().(*net.TCPAddr).Port)
}

func TestClientAgainstLibraryServer(t *testing.T) {
	for mode, link := range modes {
		t.Run(mode, func(t *testing.T) {
			h := &handler{}
			port := serveLibrary(t, mode, h)
			cli := func(sub string, args ...string) (string, string, int) {
				full := append([]string{"client", sub, "--ip", "127.0.0.1", "--port", port, "--link-timeout", "200", "--poll-interval", "10"}, link...)
				if sub != "scan" && sub != "monitor" {
					full = append(full, "--ca", "7")
				}
				return run(t, append(full, args...)...)
			}
			out, errOut, code := cli("interrogate", "--format", "json")
			var gi struct {
				Received int
				Points   []struct{ IOA uint32 }
			}
			if code != 0 || json.Unmarshal([]byte(out), &gi) != nil || gi.Received != 3 {
				t.Fatalf("interrogate: exit %d %s %s", code, out, errOut)
			}
			if out, _, code := cli("counters"); code != 0 || !strings.Contains(out, "123456") {
				t.Fatalf("counters: exit %d %s", code, out)
			}
			if out, _, code := cli("read", "--ioa", "200,201"); code != 7 || !strings.Contains(out, "49.5") {
				t.Fatalf("read: exit %d %s", code, out)
			}
			if _, errOut, code := cli("command", "--type", "single", "--ioa", "500", "--value", "on"); code != 6 {
				t.Fatalf("execute without select: exit %d %s", code, errOut)
			}
			if out, _, code := cli("command", "--type", "single", "--ioa", "500", "--value", "on", "--mode", "sbo", "--wait-termination", "3000"); code != 0 || !strings.Contains(out, "received") {
				t.Fatalf("sbo: exit %d %s", code, out)
			}
			h.mu.Lock()
			if !h.point || h.commands != 1 {
				t.Errorf("the station executed %d commands, point %v", h.commands, h.point)
			}
			h.mu.Unlock()
			for _, args := range [][]string{
				{"clock"}, {"reset", "--qualifier", "events"}, {"delay", "--set", "auto"},
				{"ping", "--count", "2", "--interval", "10"}, {"ping", "--count", "1", "--method", "link-status"},
				{"scan", "--method", "ca", "--start", "6", "--end", "8", "--timeout", "1500"},
			} {
				if out, errOut, code := cli(args[0], args[1:]...); code != 0 {
					t.Errorf("%v: exit %d %s %s", args, code, out, errOut)
				}
			}
			if mode == "balanced" {
				if _, errOut, code := cli("ping", "--count", "1", "--method", "link-test"); code != 0 {
					t.Errorf("link-test: exit %d %s", code, errOut)
				}
			}
			if out, _, code := cli("monitor", "--interrogate", "--count", "2", "--format", "json"); code != 0 || strings.Count(out, "\n") != 2 {
				t.Errorf("monitor: exit %d %s", code, out)
			}
		})
	}
}

// startBinaryServer runs `iec101ctl server start` and returns its port and a function that
// reads the events it logged.
func startBinaryServer(t *testing.T, link []string) (string, func() string) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := strconv.Itoa(ln.Addr().(*net.TCPAddr).Port)
	_ = ln.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, bin(t), append([]string{"server", "start", "--listen", "127.0.0.1", "--port", port, "--format", "json", "--link-timeout", "200"}, link...)...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var log strings.Builder
	listening := make(chan struct{})
	go func() {
		sc := bufio.NewScanner(stdout)
		once := sync.Once{}
		for sc.Scan() {
			mu.Lock()
			log.WriteString(sc.Text() + "\n")
			mu.Unlock()
			if strings.Contains(sc.Text(), `"listening"`) {
				once.Do(func() { close(listening) })
			}
		}
		_, _ = io.Copy(io.Discard, stdout)
	}()
	t.Cleanup(func() { cancel(); _ = cmd.Wait() })
	select {
	case <-listening:
	case <-time.After(10 * time.Second):
		t.Fatal("the server did not listen")
	}
	return port, func() string {
		mu.Lock()
		defer mu.Unlock()
		return log.String()
	}
}

func TestServerAgainstLibraryClient(t *testing.T) {
	for mode, link := range modes {
		t.Run(mode, func(t *testing.T) {
			port, events := startBinaryServer(t, link)
			conn, err := net.Dial("tcp", "127.0.0.1:"+port)
			if err != nil {
				t.Fatal(err)
			}
			got := make(chan *asdu.ASDU, 64)
			c, err := client.New(conn, client.Config{Params: params(mode), LinkAddr: 3, OwnAddr: 1, PollInterval: 10 * time.Millisecond,
				Handler: func(a *asdu.ASDU) { got <- a }})
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = c.Close() }()
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			if err := c.WaitUp(ctx); err != nil {
				t.Fatal(err)
			}
			// The demo station: ten points answer a general interrogation.
			data, err := c.Interrogate(ctx, 1, asdu.QOIStation)
			if err != nil {
				t.Fatal(err)
			}
			n := 0
			for _, a := range data {
				n += len(a.Objects)
			}
			if n != 10 {
				t.Errorf("interrogation: %d objects", n)
			}
			if totals, err := c.CounterInterrogate(ctx, 1, asdu.CounterGeneral, asdu.FreezeRead); err != nil || len(totals) != 1 {
				t.Errorf("counters: %v %v", totals, err)
			}
			if a, err := c.Read(ctx, 1, 1003); err != nil || a.Type != asdu.M_ME_NA_1 {
				t.Errorf("read: %v %v", a, err)
			}
			if err := c.Command(ctx, 1, asdu.DoubleCommand{IOA: 5001, Value: asdu.DoubleOff, Select: true}); err != nil {
				t.Fatal(err)
			}
			if err := c.Command(ctx, 1, asdu.DoubleCommand{IOA: 5001, Value: asdu.DoubleOff}); err != nil {
				t.Fatal(err)
			}
			for report := false; !report; {
				select {
				case a := <-got:
					report = a.Type == asdu.M_DP_TB_1 && a.Cause == asdu.CauseReturnRemote
				case <-ctx.Done():
					t.Fatal("no report of the command")
				}
			}
			if err := c.ClockSync(ctx, 1, time.Now()); err != nil {
				t.Error(err)
			}
			if _, err := c.MeasureDelay(ctx, 1); err != nil {
				t.Error(err)
			}
			if err := c.TestCommand(ctx, 1); err != nil {
				t.Error(err)
			}
			if err := c.ResetProcess(ctx, 1, asdu.ResetPendingEvents); err != nil {
				t.Error(err)
			}
			for _, e := range []string{`"link-up"`, `"interrogation"`, `"command"`, `"clock"`, `"delay"`, `"test"`, `"reset"`} {
				if !strings.Contains(events(), e) {
					t.Errorf("no %s event:\n%s", e, events())
				}
			}
		})
	}
}
