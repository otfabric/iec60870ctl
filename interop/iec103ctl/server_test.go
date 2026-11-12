//go:build interop

// SPDX-License-Identifier: MIT

package interop

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
)

type tcase struct {
	id, state, summary, needs string
	exit                      int
	op                        []string
}

func cases(t *testing.T) []tcase {
	t.Helper()
	data, err := os.ReadFile("testdata/cases.tsv")
	if err != nil {
		t.Fatal(err)
	}
	var cs []tcase
	for _, line := range strings.Split(string(data), "\n") {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		col := strings.Split(line, "\t")
		if len(col) != 6 {
			t.Fatalf("cases.tsv: %d columns in %q", len(col), line)
		}
		exit, _ := strconv.Atoi(col[2])
		cs = append(cs, tcase{id: col[0], state: col[1], exit: exit, summary: col[3], needs: col[4], op: strings.Fields(col[5])})
	}
	return cs
}

func clients(t *testing.T) []adapter {
	t.Helper()
	var cs []adapter
	for _, a := range adapters(t) {
		if a.caps.Roles.Client {
			cs = append(cs, a)
		}
	}
	if len(cs) == 0 {
		skipOrFail(t, "no reference client selected")
	}
	return cs
}

// satisfied reports whether the reference client declares the client features a case
// needs; the server features are iec103ctl's, which serves them all.
func satisfied(a adapter, needs string) bool {
	if needs == "-" {
		return true
	}
	for _, n := range strings.Split(needs, ",") {
		side, feat, _ := strings.Cut(n, ":")
		if side == "client" && !a.caps.Features[feat] {
			return false
		}
	}
	return true
}

// object, result and summary are the client result document of the contract (2.3).
type object struct {
	Fun      int     `json:"fun"`
	Inf      int     `json:"inf"`
	DPI      *int    `json:"dpi"`
	SIN      *int    `json:"sin"`
	Col      *int    `json:"col"`
	ASCII    *string `json:"ascii"`
	Software *string `json:"software"`
	Values   []value `json:"values"`
}

type setDoc struct {
	GIN    string `json:"gin"`
	KOD    int    `json:"kod"`
	Type   int    `json:"type"`
	Size   int    `json:"size"`
	Number int    `json:"number"`
	Values []any  `json:"values"`
	Data   string `json:"data,omitempty"`
}

type faultDoc struct {
	FAN int `json:"fan"`
	SOF struct {
		TP   bool `json:"tp"`
		TM   bool `json:"tm"`
		Test bool `json:"test"`
		OTEV bool `json:"otev"`
	} `json:"sof"`
	Time string `json:"time"`
}

type channelDoc struct {
	ACC    int     `json:"acc"`
	RPV    float64 `json:"rpv"`
	RSV    float64 `json:"rsv"`
	RFA    float64 `json:"rfa"`
	Values []int   `json:"values"`
}

type disturbanceDoc struct {
	FAN      int          `json:"fan"`
	TOV      *int         `json:"tov"`
	NOF      *int         `json:"nof"`
	NOC      int          `json:"noc"`
	NOE      int          `json:"noe"`
	Interval int          `json:"interval"`
	Start    string       `json:"start"`
	Tags     []tag        `json:"tags"`
	Channels []channelDoc `json:"channels"`
}

type result struct {
	Operation string `json:"operation"`
	OK        bool   `json:"ok"`
	Error     *struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
	Connected     bool `json:"connected"`
	Confirmations []struct {
		Cot      int  `json:"cot"`
		Negative bool `json:"negative"`
	} `json:"confirmations"`
	Terminated bool `json:"terminated"`
	Generic    *struct {
		Cot  int      `json:"cot"`
		Sets []setDoc `json:"sets"`
	} `json:"generic"`
	Disturbances []faultDoc      `json:"disturbances"`
	Disturbance  *disturbanceDoc `json:"disturbance"`
	ASDUs        []struct {
		Type    string   `json:"type"`
		TypeID  int      `json:"typeId"`
		Cot     int      `json:"cot"`
		Objects []object `json:"objects"`
	} `json:"asdus"`
}

// summary renders a result as cases.tsv states it (SUMMARY_103 of the harness).
func (r result) summary() string {
	code := "-"
	if r.Error != nil && r.Error.Code != "" {
		code = r.Error.Code
	}
	var conf, asdus []string
	for _, c := range r.Confirmations {
		s := strconv.Itoa(c.Cot)
		if c.Negative {
			s += "n"
		}
		conf = append(conf, s)
	}
	svc := strings.HasPrefix(r.Operation, "generic-") || strings.HasPrefix(r.Operation, "disturbance-")
	for _, a := range r.ASDUs {
		if a.TypeID == 5 || a.Cot == 2 || svc {
			continue
		}
		typ := a.Type
		if typ == "" {
			typ = strconv.Itoa(a.TypeID)
		}
		asdus = append(asdus, fmt.Sprintf("%s/%d", typ, a.Cot))
	}
	return fmt.Sprintf("ok=%t error=%s conf=%s term=%t asdus=%s", r.OK, code, strings.Join(conf, ","), r.Terminated, strings.Join(asdus, ","))
}

// host is `iec103ctl server start` serving the reference's fixture on all interfaces (the
// container reaches it as host.docker.internal); restart starts a fresh process.
type host struct {
	t      *testing.T
	f      *fixture
	port   int
	cancel func()
}

func (h *host) restart() {
	h.t.Helper()
	if h.cancel != nil {
		h.cancel()
	}
	path := filepath.Join(h.t.TempDir(), "baseline.json")
	if err := os.WriteFile(path, h.f.raw, 0o600); err != nil {
		h.t.Fatal(err)
	}
	ln, err := net.Listen("tcp", "0.0.0.0:0")
	if err != nil {
		h.t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()
	ctx, cancel := context.WithCancel(context.Background())
	args := []string{"server", "start", "--config", path, "--listen", "0.0.0.0", "--port", strconv.Itoa(port), "--format", "json"}
	cmd := exec.CommandContext(ctx, binary(h.t), args...)
	if os.Getenv("IEC103_INTEROP_TRACE") == "1" {
		cmd.Args = append([]string{cmd.Args[0], "--debug"}, cmd.Args[1:]...)
		cmd.Stderr = os.Stderr
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		h.t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		h.t.Fatal(err)
	}
	ready := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		sc := bufio.NewScanner(stdout)
		once := false
		for sc.Scan() {
			if !once && strings.Contains(sc.Text(), `"event":"listening"`) {
				once = true
				close(ready)
			}
		}
	}()
	h.cancel = func() { cancel(); _ = cmd.Wait(); <-done }
	h.t.Cleanup(h.cancel)
	select {
	case <-ready:
	case <-time.After(30 * time.Second):
		h.t.Fatal("iec103ctl server start did not listen")
	}
	h.port = port
}

// run runs one client operation of reference a against the host and returns its result
// and exit code.
func (h *host) run(t *testing.T, a adapter, op ...string) (result, int) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	args := []string{"run", "--rm", "--add-host=host.docker.internal:host-gateway", a.image, "client"}
	args = append(args, op...)
	args = append(args, "--host", "host.docker.internal", "--port", strconv.Itoa(h.port), "--link-mode", "unbalanced")
	var out, errb bytes.Buffer
	cmd := exec.CommandContext(ctx, "docker", args...)
	cmd.Stdout, cmd.Stderr = &out, &errb
	err := cmd.Run()
	code := 0
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		code = ee.ExitCode()
	} else if err != nil {
		t.Fatal(err)
	}
	var r result
	if err := json.Unmarshal(bytes.TrimSpace(out.Bytes()), &r); err != nil {
		t.Fatalf("result %q (stderr %q): %v", out.String(), errb.String(), err)
	}
	return r, code
}

// The reference client of every stack replays the harness's IEC 103 cases against
// `iec103ctl server start`, which serves the reference's own fixture. Each case must give
// the result the harness expects of any server.
func TestReferenceClientAgainstServer(t *testing.T) {
	binary(t)
	for _, a := range clients(t) {
		f := loadFixture(t, a)
		t.Run(a.name, func(t *testing.T) {
			h := &host{t: t, f: f}
			h.restart()
			for _, c := range cases(t) {
				if !satisfied(a, c.needs) {
					continue
				}
				if c.state == "rw" {
					h.restart()
				}
				t.Run(c.id, func(t *testing.T) {
					r, code := h.run(t, a, c.op...)
					got := r.summary()
					if got == c.summary && code == c.exit {
						return
					}
					op := strings.Join(c.op, " ")
					if b, ok := knownBugOf(t, a, true, op); ok {
						knownDifference(t, b, "exit "+strconv.Itoa(code)+" "+got)
					}
					msg := ""
					if r.Error != nil {
						msg = r.Error.Message
					}
					t.Errorf("%s:\n got  exit %d %s (%s)\n want exit %d %s", op, code, got, msg, c.exit, c.summary)
				})
			}
		})
	}
}

// valueCheck compares a value the harness checks as JSON; a known bug of the client
// explains a difference.
func valueCheck(t *testing.T, a adapter, label string, got, want any) {
	t.Helper()
	g, _ := json.Marshal(got)
	w, _ := json.Marshal(want)
	if string(g) == string(w) {
		return
	}
	if b, ok := knownBugOf(t, a, true, label); ok {
		knownDifference(t, b, string(g))
	}
	t.Errorf("%s:\n got  %s\n want %s", label, g, w)
}

func intOf(p *int) int {
	if p == nil {
		return -1
	}
	return *p
}

func strOf(p *string) string {
	if p == nil {
		return "<none>"
	}
	return *p
}

type funInf struct{ fun, inf int }

// The values and sessions of the harness's scripts/interop-iec103.sh, and the values
// checks of contract 2.3: interrogation, identification, command with its report and the
// new state, measurands, an idle line, generic read and write, the list of recorded
// disturbances and the transfer of one.
func TestReferenceClientValues(t *testing.T) {
	binary(t)
	for _, a := range clients(t) {
		f := loadFixture(t, a)
		t.Run(a.name, func(t *testing.T) {
			h := &host{t: t, f: f}
			h.restart()
			type indication struct{ Fun, Inf, DPI, SIN int }
			order := func(s []indication) {
				slices.SortFunc(s, func(a, b indication) int { return (a.Fun-b.Fun)*256 + a.Inf - b.Inf })
			}
			t.Run("values.interrogation", func(t *testing.T) {
				r, _ := h.run(t, a, "interrogate", "--scn", "5")
				got := []indication{}
				for _, x := range r.ASDUs {
					if (x.TypeID == 1 || x.TypeID == 2) && x.Cot == 9 && len(x.Objects) > 0 {
						o := x.Objects[0]
						got = append(got, indication{o.Fun, o.Inf, intOf(o.DPI), intOf(o.SIN)})
					}
				}
				want := []indication{}
				for _, ind := range f.Indications {
					if ind.GI == nil || *ind.GI {
						want = append(want, indication{ind.Fun, ind.Inf, ind.DPI, 5})
					}
				}
				order(got)
				order(want)
				valueCheck(t, a, "interrogate --scn 5", got, want)
			})
			t.Run("values.identification", func(t *testing.T) {
				if !a.caps.Features["identification"] {
					t.Skip("client feature identification not declared")
				}
				r, _ := h.run(t, a, "connect", "--hold-ms", "300")
				type ident struct {
					Fun, Inf, Col   int
					ASCII, Software string
				}
				got := []ident{}
				for _, x := range r.ASDUs {
					if x.TypeID == 5 && x.Cot == 4 && len(x.Objects) > 0 {
						o := x.Objects[0]
						got = append(got, ident{o.Fun, o.Inf, intOf(o.Col), strOf(o.ASCII), strOf(o.Software)})
					}
				}
				id := f.Station.Identification
				valueCheck(t, a, "identification", got, []ident{{id.Fun, 3, id.Col, id.ASCII, id.Software}})
			})
			t.Run("values.command", func(t *testing.T) {
				if !a.caps.Features["generalCommand"] {
					t.Skip("client feature generalCommand not declared")
				}
				h.restart()
				r, _ := h.run(t, a, "command", "--fun", "160", "--inf", "17", "--dco", "on", "--rii", "9", "--collect-ms", "500")
				type conf struct{ Cot, Fun, Inf, SIN int }
				type report struct{ Fun, Inf, DPI int }
				got, reports := []conf{}, []report{}
				for _, x := range r.ASDUs {
					if (x.TypeID != 1 && x.TypeID != 2) || len(x.Objects) == 0 {
						continue
					}
					o := x.Objects[0]
					switch x.Cot {
					case 20, 21:
						got = append(got, conf{x.Cot, o.Fun, o.Inf, intOf(o.SIN)})
					case 12:
						reports = append(reports, report{o.Fun, o.Inf, intOf(o.DPI)})
					}
				}
				valueCheck(t, a, "command --fun 160 --inf 17", got, []conf{{20, 160, 17, 9}})
				valueCheck(t, a, "command --fun 160 --inf 17", reports, []report{{160, 17, 2}})
				r, _ = h.run(t, a, "interrogate")
				after := []int{}
				for _, x := range r.ASDUs {
					if x.Cot == 9 && len(x.Objects) > 0 && x.Objects[0].Fun == 160 && x.Objects[0].Inf == 17 {
						after = append(after, intOf(x.Objects[0].DPI))
					}
				}
				valueCheck(t, a, "command --fun 160 --inf 17", after, []int{2})
			})
			t.Run("values.measurands", func(t *testing.T) {
				if !a.caps.Features["measurands"] {
					t.Skip("client feature measurands not declared")
				}
				r, _ := h.run(t, a, "monitor", "--duration-ms", "2000")
				type meas struct {
					Type, Fun, Inf int
					Values         []value
				}
				seen := map[funInf]bool{}
				got := []meas{}
				for _, x := range r.ASDUs {
					if x.Cot == 2 && (x.TypeID == 3 || x.TypeID == 9) && len(x.Objects) > 0 {
						o := x.Objects[0]
						if !seen[funInf{o.Fun, o.Inf}] {
							seen[funInf{o.Fun, o.Inf}] = true
							got = append(got, meas{x.TypeID, o.Fun, o.Inf, o.Values})
						}
					}
				}
				want := []meas{}
				for _, m := range f.Measurands {
					want = append(want, meas{m.Type, m.Fun, m.Inf, m.Values})
				}
				cmp := func(a, b meas) int { return (a.Fun-b.Fun)*256 + a.Inf - b.Inf }
				slices.SortFunc(got, cmp)
				slices.SortFunc(want, cmp)
				valueCheck(t, a, "monitor measurands", got, want)
			})
			t.Run("session.idle-hold", func(t *testing.T) {
				if r, _ := h.run(t, a, "connect", "--hold-ms", "3000"); !r.OK || !r.Connected {
					t.Errorf("idle line: %s", r.summary())
				}
			})
			t.Run("session.after-idle", func(t *testing.T) {
				if r, _ := h.run(t, a, "interrogate", "--scn", "1"); !r.OK || !r.Terminated {
					t.Errorf("interrogation after an idle connection: %s", r.summary())
				}
			})
			t.Run("values.generic-read", func(t *testing.T) {
				if !a.caps.Features["genericServices"] {
					t.Skip("client feature genericServices not declared")
				}
				var got, want []setDoc
				for _, g := range f.Generic {
					gin := fmt.Sprintf("%d.%d", g.GIN.Group, g.GIN.Entry)
					var v any
					_ = json.Unmarshal(g.Value, &v)
					want = append(want,
						setDoc{GIN: gin, KOD: 1, Type: g.Type, Size: g.Size, Number: 1, Values: []any{v}},
						setDoc{GIN: gin, KOD: 10, Type: 1, Size: len(g.Description), Number: 1, Values: []any{g.Description}})
					for _, kod := range []string{"1", "10"} {
						r, _ := h.run(t, a, "generic-read", "--gin", gin, "--kod", kod)
						if r.Generic != nil {
							got = append(got, r.Generic.Sets...)
						}
					}
				}
				valueCheck(t, a, "generic-read", normalize(got), normalize(want))
			})
			t.Run("values.generic-write", func(t *testing.T) {
				if !a.caps.Features["genericServices"] {
					t.Skip("client feature genericServices not declared")
				}
				h.restart()
				r, _ := h.run(t, a, "generic-write", "--gin", "1.2", "--type", "7", "--size", "4", "--value", "2.25")
				cots := []int{}
				for _, c := range r.Confirmations {
					cots = append(cots, c.Cot)
				}
				after, _ := h.run(t, a, "generic-read", "--gin", "1.2")
				var vals []any
				if after.Generic != nil {
					for _, s := range after.Generic.Sets {
						vals = append(vals, s.Values...)
					}
				}
				valueCheck(t, a, "generic-write", fmt.Sprint(cots, normalize(vals)), fmt.Sprint([]int{40}, normalize([]any{2.25})))
			})
			t.Run("values.disturbance-list", func(t *testing.T) {
				if !a.caps.Features["disturbanceList"] {
					t.Skip("client feature disturbanceList not declared")
				}
				r, _ := h.run(t, a, "disturbance-list")
				want := []faultDoc{}
				for _, d := range f.Disturbances {
					w := faultDoc{FAN: d.FAN, Time: d.Time}
					w.SOF.TP, w.SOF.TM, w.SOF.Test, w.SOF.OTEV = d.SOF.TP, d.SOF.TM, d.SOF.Test, d.SOF.OTEV
					want = append(want, w)
				}
				got := r.Disturbances
				if got == nil {
					got = []faultDoc{}
				}
				valueCheck(t, a, "disturbance-list", got, want)
			})
			t.Run("values.disturbance-transfer", func(t *testing.T) {
				if !a.caps.Features["disturbanceData"] {
					t.Skip("client feature disturbanceData not declared")
				}
				h.restart()
				d := f.Disturbances[0]
				r, code := h.run(t, a, "disturbance-transfer", "--fan", strconv.Itoa(d.FAN))
				want := disturbanceDoc{FAN: d.FAN, NOC: len(d.Channels), Interval: d.Interval, Start: d.Start, Tags: d.Tags}
				wantVals := [][]int{}
				for _, c := range d.Channels {
					want.NOE = len(c.Values)
					want.Channels = append(want.Channels, channelDoc{ACC: c.ACC, RPV: c.RPV, RSV: c.RSV, RFA: c.RFA})
					wantVals = append(wantVals, c.Values)
				}
				var got any = fmt.Sprintf("exit %d %s", code, r.summary())
				gotVals := [][]int{}
				if r.Disturbance != nil {
					g := *r.Disturbance
					if g.NOF != nil && *g.NOF != d.NOF || g.TOV != nil && *g.TOV != 1 {
						t.Errorf("NOF %v TOV %v, want %d 1", intOf(g.NOF), intOf(g.TOV), d.NOF)
					}
					g.NOF, g.TOV = nil, nil
					g.Channels = append([]channelDoc{}, g.Channels...)
					for i := range g.Channels {
						gotVals = append(gotVals, g.Channels[i].Values)
						g.Channels[i].Values = nil
					}
					got = g
				}
				t.Run("description", func(t *testing.T) { valueCheck(t, a, "disturbance-transfer", normalize(got), normalize(want)) })
				t.Run("values", func(t *testing.T) { valueCheck(t, a, "disturbance-transfer values", gotVals, wantVals) })
			})
		})
	}
}
