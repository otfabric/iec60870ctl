//go:build interop

// SPDX-License-Identifier: MIT

package interop

import (
	"bufio"
	"context"
	"encoding/json"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"
)

// baselinePoints are the monitored points of the baseline fixture of iec60870-5-interop
// (fixtures/iec104/baseline/fixture.json) a general interrogation reports: every point
// but the integrated total, which a counter interrogation reports.
var baselinePoints = map[uint32]string{
	100: "M_SP_NA_1", 101: "M_DP_NA_1", 102: "M_ST_NA_1", 103: "M_BO_NA_1", 110: "M_SP_NA_1", 111: "M_SP_NA_1",
	200: "M_ME_NA_1", 201: "M_ME_NB_1", 202: "M_ME_NC_1", 210: "M_ME_NB_1", 211: "M_ME_NB_1",
}

type ctlPoint struct {
	IOA  uint32 `json:"ioa"`
	Type string `json:"type"`
}

// TestClientAgainstReferenceServers runs iec104ctl client against each reference server
// with the baseline fixture.
func TestClientAgainstReferenceServers(t *testing.T) {
	for _, a := range adapters(t) {
		t.Run(a.name, func(t *testing.T) {
			if !a.caps(t).Roles.Server {
				t.Skipf("%s has no server", a.name)
			}
			srv := startServer(t, a)
			target := []string{"--ip", "127.0.0.1", "--port", strconv.Itoa(srv.port), "--ca", "1", "--format", "json", "--timeout", "10000"}
			op := func(name string, extra ...string) (string, int) {
				return ctl(t, append(append([]string{"client", name}, target...), extra...)...)
			}

			t.Run("interrogate", func(t *testing.T) {
				out, code := op("interrogate")
				if code != 0 {
					t.Fatalf("exit %d\n%s", code, out)
				}
				var res struct {
					Points []ctlPoint `json:"points"`
				}
				if err := json.Unmarshal([]byte(out), &res); err != nil {
					t.Fatal(err)
				}
				got := map[uint32]string{}
				for _, p := range res.Points {
					got[p.IOA] = p.Type
				}
				for ioa, typ := range baselinePoints {
					if got[ioa] != typ {
						t.Errorf("IOA %d: %q, want %s", ioa, got[ioa], typ)
					}
				}
			})
			t.Run("counters", func(t *testing.T) {
				out, code := op("counters")
				if code != 0 || !strings.Contains(out, `"M_IT_NA_1"`) || !strings.Contains(out, `"ioa": 300`) {
					t.Errorf("exit %d\n%s", code, out)
				}
			})
			t.Run("read", func(t *testing.T) {
				if a.knownReadBug(t) {
					t.Skip("known bug: j60870 1.8.0 cannot decode C_RD_NA_1 (iec60870-5-interop known-bugs.tsv)")
				}
				out, code := op("read", "--ioa", "202")
				if code != 0 || !strings.Contains(out, `"M_ME_NC_1"`) {
					t.Errorf("exit %d\n%s", code, out)
				}
			})
			t.Run("clock", func(t *testing.T) {
				if out, code := op("clock", "--time", "now"); code != 0 {
					t.Errorf("exit %d\n%s", code, out)
				}
			})
			t.Run("ping", func(t *testing.T) {
				if out, code := op("ping", "--method", "testfr", "--count", "2", "--interval", "100"); code != 0 {
					t.Errorf("testfr: exit %d\n%s", code, out)
				}
				if out, code := op("ping", "--method", "command", "--count", "1"); code != 0 {
					t.Errorf("test command: exit %d\n%s", code, out)
				}
			})
			t.Run("command", func(t *testing.T) {
				if out, code := op("command", "--type", "single", "--ioa", "500", "--value", "on"); code != 0 {
					t.Errorf("single command: exit %d\n%s", code, out)
				}
				if out, code := op("command", "--type", "setpoint-float", "--ioa", "502", "--value", "12.5"); code != 0 {
					t.Errorf("set point: exit %d\n%s", code, out)
				}
				if out, code := op("command", "--type", "single", "--ioa", "510", "--value", "off", "--mode", "sbo"); code != 0 {
					t.Errorf("select before operate: exit %d\n%s", code, out)
				}
			})
		})
	}
}

// TestReferenceClientsAgainstServer runs each reference client against iec104ctl server
// start with its built-in demo station (common address 1; points 100.., counters 2000..,
// commands 5000..).
func TestReferenceClientsAgainstServer(t *testing.T) {
	port := strconv.Itoa(freePort(t))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	srv := exec.CommandContext(ctx, binary(t), "server", "start", "--listen", "0.0.0.0", "--port", port, "--format", "json")
	stdout, err := srv.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := srv.Start(); err != nil {
		t.Fatal(err)
	}
	ready := make(chan struct{})
	go func() {
		sc := bufio.NewScanner(stdout)
		for sc.Scan() {
			if strings.Contains(sc.Text(), `"listening"`) {
				close(ready)
				for sc.Scan() {
				}
				return
			}
		}
	}()
	select {
	case <-ready:
	case <-time.After(10 * time.Second):
		t.Fatal("iec104ctl server did not start")
	}
	defer func() { cancel(); _ = srv.Wait() }()

	for _, a := range adapters(t) {
		t.Run(a.name, func(t *testing.T) {
			if !a.caps(t).Roles.Client {
				t.Skipf("%s has no client", a.name)
			}
			check := func(what string, r *result) {
				t.Helper()
				if !r.OK || r.exit != 0 {
					msg := ""
					if r.Error != nil {
						msg = r.Error.Code + ": " + r.Error.Message
					}
					t.Errorf("%s: ok=%v exit=%d %s", what, r.OK, r.exit, msg)
				}
			}
			common := []string{"--common-address", "1"}
			gi := runClient(t, a, port, append([]string{"interrogate"}, common...)...)
			check("interrogate", gi)
			if gi.objects("") < 10 {
				t.Errorf("interrogate: %d objects", gi.objects(""))
			}
			ci := runClient(t, a, port, append([]string{"counter-interrogate"}, common...)...)
			check("counter-interrogate", ci)
			if ci.objects("M_IT_NA_1") == 0 {
				t.Errorf("counter-interrogate: no integrated totals")
			}
			if !a.knownReadBug(t) {
				check("read", runClient(t, a, port, append([]string{"read", "--ioa", "1000"}, common...)...))
			}
			check("clock-sync", runClient(t, a, port, append([]string{"clock-sync"}, common...)...))
			check("test-command", runClient(t, a, port, append([]string{"test-command"}, common...)...))
			check("command", runClient(t, a, port, append([]string{"command", "--type", "C_SC_NA_1", "--ioa", "5000", "--value", "true"}, common...)...))
			check("command sbo", runClient(t, a, port, append([]string{"command", "--type", "C_DC_NA_1", "--ioa", "5001", "--value", "2", "--mode", "sbo"}, common...)...))
		})
	}
}
