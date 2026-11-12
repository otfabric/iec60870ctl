// SPDX-License-Identifier: MIT

//go:build interop

package interop

import (
	"bufio"
	"context"
	"encoding/json"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
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
	f, err := os.Open("testdata/cases.tsv")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	var cs []tcase
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
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

// model writes the fixture as an iec101ctl station model: the same points, values and
// commands, and the behaviour docs/FIXTURES.md of the harness asks of a station where the
// model has a choice (a counter freeze is refused).
func model(t *testing.T, f *fixture) string {
	t.Helper()
	type pointSpec struct {
		IOA      int      `json:"ioa"`
		Type     string   `json:"type"`
		Value    any      `json:"value"`
		Quality  []string `json:"quality,omitempty"`
		Sequence int      `json:"sequence,omitempty"`
	}
	type commandSpec struct {
		IOA            int    `json:"ioa"`
		Type           string `json:"type"`
		Target         int    `json:"target,omitempty"`
		ReportCause    int    `json:"report_cause,omitempty"`
		SelectRequired bool   `json:"select_required,omitempty"`
	}
	m := struct {
		Station  map[string]any `json:"station"`
		Points   []pointSpec    `json:"points"`
		Commands []commandSpec  `json:"commands"`
	}{Station: map[string]any{"name": "iec60870-5-interop baseline", "common_address": f.Station.CommonAddress, "counter_freeze": false}}
	for _, p := range f.Points {
		var v any
		if err := json.Unmarshal(p.Value, &v); err != nil {
			t.Fatal(err)
		}
		if p.Type == "M_ME_NA_1" {
			v = v.(float64) / 32768 // the model takes the normalized value, the fixture the raw NVA
		}
		m.Points = append(m.Points, pointSpec{IOA: p.IOA, Type: p.Type, Value: v, Quality: p.Quality, Sequence: p.Sequence})
	}
	for _, c := range f.Commands {
		m.Commands = append(m.Commands, commandSpec{IOA: c.IOA, Type: c.Type, Target: c.Target, ReportCause: c.ReportCause, SelectRequired: c.SelectRequired})
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "baseline.json")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// binServer is a running `iec101ctl server start`.
type binServer struct {
	port   int
	cancel context.CancelFunc
	done   chan struct{}
	mu     sync.Mutex
	log    strings.Builder
}

// events returns how many events of a kind the server logged.
func (s *binServer) events(kind string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return strings.Count(s.log.String(), `"event":"`+kind+`"`)
}

func (s *binServer) stop() {
	s.cancel()
	<-s.done
}

// startServer runs `iec101ctl server start` with the model on all interfaces (the image
// reaches it as host.docker.internal), and the link flags of fs, without end of
// initialization: the harness's cases expect only the answers to their requests.
func startServer(t *testing.T, modelPath, mode string, fs fields) *binServer {
	t.Helper()
	ln, err := net.Listen("tcp", "0.0.0.0:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()
	ctx, cancel := context.WithCancel(context.Background())
	args := append([]string{"server", "start", "--config", modelPath, "--port", strconv.Itoa(port), "--format", "json", "--no-end-of-init", "--link-timeout", "500"}, fs.flags(mode)...)
	cmd := exec.CommandContext(ctx, bin(t), args...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	s := &binServer{port: port, cancel: cancel, done: make(chan struct{})}
	listening := make(chan struct{})
	go func() {
		defer close(s.done)
		sc := bufio.NewScanner(stdout)
		var once sync.Once
		for sc.Scan() {
			s.mu.Lock()
			s.log.WriteString(sc.Text() + "\n")
			s.mu.Unlock()
			if strings.Contains(sc.Text(), `"listening"`) {
				once.Do(func() { close(listening) })
			}
		}
		_ = cmd.Wait()
	}()
	t.Cleanup(func() {
		s.stop()
		if t.Failed() {
			s.mu.Lock()
			t.Logf("iec101ctl server log:\n%s", s.log.String())
			s.mu.Unlock()
		}
	})
	select {
	case <-listening:
	case <-time.After(10 * time.Second):
		t.Fatalf("iec101ctl server start did not listen")
	}
	return s
}

// The client of every image replays the harness's IEC 101 cases against
// `iec101ctl server start`, which serves the image's own fixture as a model. Each case must
// give the result the harness expects of any server.
func TestReferenceClientAgainstServer(t *testing.T) {
	for _, a := range adapters(t) {
		f := loadFixture(t, a)
		path := model(t, f)
		for _, mode := range modes {
			if !slices.Contains(a.caps.LinkModes, mode) {
				continue
			}
			t.Run(a.name+"/"+mode, func(t *testing.T) {
				// One server per set of link flags; a case that changes the station gets a
				// fresh one.
				servers := map[string]*binServer{}
				for _, c := range cases(t) {
					if c.needs != "-" && !satisfied(a, c.needs) {
						continue
					}
					fs, key := caseFields(c.op)
					if !a.takes(fs, mode) || !a.clientCanSend(c.op, fs, mode) {
						continue
					}
					srv := servers[key]
					if srv == nil || c.state == "rw" {
						if srv != nil {
							srv.stop()
						}
						srv = startServer(t, path, mode, fs)
						servers[key] = srv
					}
					t.Run(c.id, func(t *testing.T) {
						ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
						defer cancel()
						before := srv.events("disconnected")
						r, code, err := runImageClient(ctx, a, srv.port, mode, c.op)
						if err != nil {
							t.Fatal(err)
						}
						// A line has one controlling station: wait until the server has seen
						// this one go before the next case connects.
						deadline := time.Now().Add(10 * time.Second)
						for srv.events("connected") > srv.events("disconnected") || srv.events("disconnected") == before {
							if time.Now().After(deadline) {
								break
							}
							time.Sleep(20 * time.Millisecond)
						}
						if got := r.summary(); got != c.summary || code != c.exit {
							if bug := knownBug(t, a, "client", mode, c.op); bug != "" {
								t.Skipf("known bug of %s %s: %s", a.name, a.caps.Upstream.Version, bug)
							}
							t.Errorf("%s:\n got  exit %d %s\n want exit %d %s", strings.Join(c.op, " "), code, got, c.exit, c.summary)
						}
					})
				}
			})
		}
	}
}

// satisfied reports whether the image's client declares the client features a case needs;
// server features are the ones of the baseline station, which serves no groups.
func satisfied(a adapter, needs string) bool {
	for _, n := range strings.Split(needs, ",") {
		side, feat, _ := strings.Cut(n, ":")
		if side == "server" || !a.caps.Features[feat] {
			return false
		}
	}
	return true
}
