// SPDX-License-Identifier: MIT

//go:build e2e

package e2e

import (
	"bufio"
	"context"
	"encoding/json"
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
	iec104 "github.com/otfabric/go-iec60870-5/iec104"
	"github.com/otfabric/go-iec60870-5/iec104/client"
	"github.com/otfabric/go-iec60870-5/iec104/server"
)

// binary returns the iec104ctl binary of make e2e.
func binary(t *testing.T) string {
	t.Helper()
	dir := os.Getenv("E2E_BIN_DIR")
	if dir == "" {
		t.Fatal("E2E_BIN_DIR is not set; run make e2e")
	}
	return filepath.Join(dir, "iec104ctl")
}

// libStation is a controlled station built on go-iec60870-5's iec104/server alone: it
// answers a general interrogation with two points, a read of either, a clock
// synchronization, a test command and single commands on IOA 6001, which it records.
type libStation struct {
	srv  *server.Server
	addr string
	mu   sync.Mutex
	cmds []asdu.SingleCommand
}

func startLibStation(t *testing.T) *libStation {
	t.Helper()
	st := &libStation{}
	const ca asdu.CommonAddr = 7
	points := func(cause asdu.Cause) []*asdu.ASDU {
		return []*asdu.ASDU{
			asdu.New(cause, ca, asdu.SinglePoint{IOA: 1001, Value: true}),
			asdu.New(cause, ca, asdu.MeasuredFloat{IOA: 4001, Value: 49.98}),
		}
	}
	mux := server.NewMux()
	mux.HandleFunc(asdu.C_IC_NA_1, func(s *server.Session, req *asdu.ASDU) {
		_ = s.Confirm(req)
		for _, a := range points(req.First().(asdu.Interrogation).Qualifier.Cause()) {
			_ = s.Send(s.Context(), a)
		}
		_ = s.Terminate(req)
	})
	mux.HandleFunc(asdu.C_RD_NA_1, func(s *server.Session, req *asdu.ASDU) {
		for _, a := range points(asdu.CauseRequest) {
			if a.First().Address() == req.First().Address() {
				_ = s.Send(s.Context(), a)
				return
			}
		}
		_ = s.Reject(req, asdu.CauseUnknownIOA)
	})
	mux.HandleFunc(asdu.C_CS_NA_1, func(s *server.Session, req *asdu.ASDU) { _ = s.Confirm(req) })
	mux.HandleFunc(asdu.C_TS_TA_1, func(s *server.Session, req *asdu.ASDU) { _ = s.Confirm(req) })
	mux.HandleFunc(asdu.C_SC_NA_1, func(s *server.Session, req *asdu.ASDU) {
		cmd := req.First().(asdu.SingleCommand)
		if cmd.IOA != 6001 {
			_ = s.Reject(req, asdu.CauseUnknownIOA)
			return
		}
		st.mu.Lock()
		st.cmds = append(st.cmds, cmd)
		st.mu.Unlock()
		_ = s.Confirm(req)
		_ = s.Terminate(req)
	})
	srv, err := server.New(mux, server.WithCommonAddrs(ca))
	if err != nil {
		t.Fatal(err)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() { _ = srv.Close() })
	st.srv, st.addr = srv, ln.Addr().String()
	return st
}

func (st *libStation) commands() []asdu.SingleCommand {
	st.mu.Lock()
	defer st.mu.Unlock()
	return append([]asdu.SingleCommand(nil), st.cmds...)
}

// stdoutOf runs the binary and returns its stdout and exit code; stderr goes to the log.
func stdoutOf(t *testing.T, args ...string) (string, int) {
	t.Helper()
	var out, errb strings.Builder
	cmd := exec.Command(binary(t), args...)
	cmd.Stdout, cmd.Stderr = &out, &errb
	err := cmd.Run()
	if errb.Len() > 0 {
		t.Logf("stderr of %v:\n%s", args, errb.String())
	}
	if ee, ok := err.(*exec.ExitError); ok {
		return out.String(), ee.ExitCode()
	} else if err != nil {
		t.Fatal(err)
	}
	return out.String(), 0
}

func TestClientAgainstLibraryServer(t *testing.T) {
	st := startLibStation(t)
	url := "tcp://" + st.addr
	base := func(op string, extra ...string) []string {
		return append([]string{"client", op, "--url", url, "--ca", "7", "--format", "json", "--timeout", "5000"}, extra...)
	}

	out, code := stdoutOf(t, base("interrogate")...)
	if code != 0 {
		t.Fatalf("interrogate: exit %d\n%s", code, out)
	}
	var gi struct {
		Count  int `json:"count"`
		Points []struct {
			IOA   uint32 `json:"ioa"`
			Type  string `json:"type"`
			Value any    `json:"value"`
		} `json:"points"`
	}
	if err := json.Unmarshal([]byte(out), &gi); err != nil || gi.Count != 2 || gi.Points[0].Type != "M_SP_NA_1" || gi.Points[1].IOA != 4001 {
		t.Fatalf("interrogate: %v %+v\n%s", err, gi, out)
	}

	out, code = stdoutOf(t, base("read", "--ioa", "4001,9")...)
	if code != 7 || !strings.Contains(out, `"unknown_object_address"`) || !strings.Contains(out, "49.98") {
		t.Errorf("read: exit %d\n%s", code, out)
	}
	if out, code = stdoutOf(t, base("clock", "--time", "now")...); code != 0 {
		t.Errorf("clock: exit %d\n%s", code, out)
	}
	if out, code = stdoutOf(t, base("ping", "--method", "command", "--count", "2", "--interval", "10")...); code != 0 {
		t.Errorf("ping: exit %d\n%s", code, out)
	}
	if out, code = stdoutOf(t, base("command", "--type", "single", "--ioa", "6001", "--value", "on")...); code != 0 {
		t.Fatalf("command: exit %d\n%s", code, out)
	}
	if out, code = stdoutOf(t, base("command", "--type", "single", "--ioa", "6002", "--value", "on")...); code != 6 {
		t.Errorf("command to an unknown address: exit %d\n%s", code, out)
	}
	if cmds := st.commands(); len(cmds) != 1 || !cmds[0].Value {
		t.Errorf("the station saw %+v, want one single command on", cmds)
	}
}

func TestServerAgainstLibraryClient(t *testing.T) {
	port := freePort(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cmd := exec.CommandContext(ctx, binary(t), "server", "start", "--listen", "127.0.0.1", "--port", port, "--format", "json")
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	events := make(chan string, 64)
	go func() {
		sc := bufio.NewScanner(stdout)
		for sc.Scan() {
			events <- sc.Text()
		}
		close(events)
	}()
	waitFor(t, events, `"listening"`)

	c, err := client.New("127.0.0.1:"+port, client.WithRequestTimeout(5*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.Close() }()
	cctx, ccancel := context.WithTimeout(ctx, 10*time.Second)
	defer ccancel()
	if err := c.Connect(cctx); err != nil {
		t.Fatal(err)
	}
	if err := c.StartDT(cctx); err != nil && err != iec104.ErrBusy {
		t.Fatalf("STARTDT: %v", err)
	}
	data, err := c.Interrogate(cctx, 1, asdu.QOIStation)
	if err != nil {
		t.Fatalf("interrogation of the demo station: %v", err)
	}
	n := 0
	for _, a := range data {
		n += len(a.Objects)
	}
	if n == 0 {
		t.Fatal("the demo station reported no points")
	}
	if _, err := c.Read(cctx, 1, 1000); err != nil {
		t.Errorf("read of IOA 1000: %v", err)
	}
	if err := c.ClockSync(cctx, 1, time.Now()); err != nil {
		t.Errorf("clock synchronization: %v", err)
	}
	if _, err := c.Read(cctx, 1, 65000); err == nil {
		t.Error("a read of an unknown address succeeded")
	}
	waitFor(t, events, `"interrogation"`)
	cancel()
	_ = cmd.Wait()
}

func freePort(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = l.Close() }()
	return strconv.Itoa(l.Addr().(*net.TCPAddr).Port)
}

// waitFor reads events until one contains want, for up to ten seconds.
func waitFor(t *testing.T, events <-chan string, want string) {
	t.Helper()
	deadline := time.After(10 * time.Second)
	for {
		select {
		case e, ok := <-events:
			if !ok {
				t.Fatalf("server output ended before %s", want)
			}
			if strings.Contains(e, want) {
				return
			}
		case <-deadline:
			t.Fatalf("timed out waiting for %s", want)
		}
	}
}
