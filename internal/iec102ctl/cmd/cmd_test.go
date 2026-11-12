// SPDX-License-Identifier: MIT

package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/otfabric/iec60870ctl/internal/iec102ctl/stationtest"
	"github.com/otfabric/iec60870ctl/internal/iec102ctl/types"
)

// syncBuffer is an output buffer that is safe for concurrent writers.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// result is the outcome of one invocation of iec102ctl.
type result struct {
	stdout, stderr string
	code           int
}

// run executes iec102ctl with args, as the process would.
func run(t testing.TB, args ...string) result {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	var stdout, stderr syncBuffer
	code := Run(ctx, args, &stdout, &stderr)
	return result{stdout.String(), stderr.String(), code}
}

func (r result) want(t testing.TB, code int) result {
	t.Helper()
	if r.code != code {
		t.Fatalf("exit code %d, want %d\nstdout:\n%s\nstderr:\n%s", r.code, code, r.stdout, r.stderr)
	}
	return r
}

func (r result) has(t testing.TB, subs ...string) result {
	t.Helper()
	for _, s := range subs {
		if !strings.Contains(r.stdout, s) {
			t.Errorf("stdout does not contain %q:\n%s", s, r.stdout)
		}
	}
	return r
}

func (r result) errHas(t testing.TB, subs ...string) result {
	t.Helper()
	for _, s := range subs {
		if !strings.Contains(r.stderr, s) {
			t.Errorf("stderr does not contain %q:\n%s", s, r.stderr)
		}
	}
	return r
}

// decode parses stdout as one JSON document into v, refusing unknown fields.
func (r result) decode(t testing.TB, v any) {
	t.Helper()
	dec := json.NewDecoder(strings.NewReader(r.stdout))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		t.Fatalf("stdout is not the expected JSON: %v\n%s", err, r.stdout)
	}
}

// errorCode returns the code of the JSON error object on stdout.
func (r result) errorCode(t testing.TB) string {
	t.Helper()
	var env types.ErrorEnvelope
	if err := json.Unmarshal([]byte(r.stdout), &env); err != nil || env.Error.Code == "" {
		t.Fatalf("stdout is not a JSON error object: %v\n%s", err, r.stdout)
	}
	return env.Error.Code
}

// cli runs `iec102ctl client <sub…>` against st with extra flags.
func cli(t testing.TB, st *stationtest.Station, args ...string) result {
	t.Helper()
	n := 1
	if args[0] == "ree" {
		n = 2
	}
	full := append([]string{"client"}, args[:n]...)
	full = append(full, "--url", st.URL(), "--timeout", "5000")
	return run(t, append(full, args[n:]...)...)
}

func TestClientCommands(t *testing.T) {
	st := stationtest.Start(t)
	cli(t, st, "info").want(t, 0).has(t, "standard 42, manufacturer 33, product 20261115")
	cli(t, st, "info", "--format", "table").want(t, 0).has(t, "MANUFACTURER")
	var info types.InfoResult
	cli(t, st, "info", "--format", "json").want(t, 0).decode(t, &info)
	if info.Product != 20261115 || info.LinkAddress != 1 {
		t.Errorf("info %+v", info)
	}
	cli(t, st, "time").want(t, 0).has(t, "2026-01-15T10:20:30.500")
	cli(t, st, "time", "--format", "table").want(t, 0).has(t, "DRIFT_MS")
	var tr types.TimeResult
	cli(t, st, "time", "--format", "json").want(t, 0).decode(t, &tr)

	cli(t, st, "totals", "--from", "2026-01-01", "--to", "2026-01-02").want(t, 0).has(t, "C_CI_NT_2 (operational) of record 11", "3 integration periods", "SEQ=16")
	cli(t, st, "totals", "--kind", "incremental", "--period", "2026-01-01T02:00", "--first", "1", "--last", "2", "--format", "table").
		want(t, 0).has(t, "M_IT_TK_2", "ADDRESS")
	var tot types.TotalsResult
	cli(t, st, "totals", "--from", "2026-01-01", "--to", "2026-01-02", "--access-key", "7", "--format", "json").want(t, 0).decode(t, &tot)
	if tot.Count != 3 || tot.Session == nil || !tot.Session.Logout || tot.Periods[1].Totals[0].Flags != "VH" {
		t.Errorf("totals %+v", tot)
	}
	r := cli(t, st, "totals", "--from", "2027-01-01", "--to", "2027-01-02", "--format", "json").want(t, 6)
	if r.errorCode(t) != "no_period" {
		t.Errorf("refusal %s", r.stdout)
	}
	cli(t, st, "totals", "--from", "2027-01-01", "--to", "2027-01-02").want(t, 6).errHas(t, "integration period not available (cause 18)")
	cli(t, st, "totals").want(t, 3).errHas(t, "--oldest")

	cli(t, st, "events", "--record", "52").want(t, 0).has(t, "2 events", "qualifier 3")
	cli(t, st, "events", "--record", "52", "--from", "2026-01-02", "--to", "2026-01-03", "--format", "table").want(t, 0).has(t, "STATE")
	var ev types.EventsResult
	cli(t, st, "events", "--record", "52", "--format", "json").want(t, 0).decode(t, &ev)

	cli(t, st, "request", "--type", "C_TI_NA_2", "--cause", "request").want(t, 0).has(t, "-> C_TI_NA_2", "<- M_TI_TA_2")
	cli(t, st, "request", "--type", "C_TI_NA_2", "--cause", "request", "--format", "table").want(t, 0).has(t, "DIR")
	var rq types.RequestResult
	cli(t, st, "request", "--type", "103", "--cause", "5", "--format", "json").want(t, 0).decode(t, &rq)
	cli(t, st, "request", "--type", "200", "--data", "01").want(t, 6)
	cli(t, st, "request").want(t, 2)

	cli(t, st, "ree", "login", "--access-key", "7").want(t, 0).has(t, "login confirmed logout confirmed")
	cli(t, st, "ree", "login", "--access-key", "7", "--format", "table").want(t, 0).has(t, "LOGOUT")
	var lr types.LoginResult
	cli(t, st, "ree", "login", "--access-key", "7", "--format", "json").want(t, 0).decode(t, &lr)
	r = cli(t, st, "ree", "login", "--access-key", "8", "--format", "json").want(t, 6)
	if r.errorCode(t) != "login_refused" {
		t.Errorf("login refused: %s", r.stdout)
	}
	cli(t, st, "ree", "login").want(t, 3)

	cli(t, st, "ree", "tariffs").want(t, 0).has(t, "C_TA_VC_2 of register 134", "max power 3100")
	cli(t, st, "ree", "tariffs", "--from", "2025-11-01", "--to", "2026-01-01", "--format", "table").want(t, 0).has(t, "ACTIVE_ABS")
	var tf types.TariffsResult
	cli(t, st, "ree", "tariffs", "--format", "json").want(t, 0).decode(t, &tf)

	cli(t, st, "ree", "contracted-powers").want(t, 0).has(t, "period 2  20 kW")
	cli(t, st, "ree", "contracted-powers", "--format", "table").want(t, 0).has(t, "POWER_KW")
	var pw types.PowersResult
	cli(t, st, "ree", "contracted-powers", "--format", "json").want(t, 0).decode(t, &pw)

	cli(t, st, "ree", "special-days").want(t, 0).has(t, "2026-01-01T00:00")
	cli(t, st, "ree", "special-days", "--format", "table").want(t, 0).has(t, "DAY")
	var sd types.SpecialDaysResult
	cli(t, st, "ree", "special-days", "--format", "json").want(t, 0).decode(t, &sd)

	cli(t, st, "ree", "set-time", "--access-key", "7", "--time", "2026-03-01T00:00:00.000").want(t, 0).has(t, "2026-03-01T00:00:00.000 confirmed")
	cli(t, st, "ree", "set-time", "--time", "2026-03-01T00:00:00.000", "--format", "table").want(t, 0).has(t, "RTT_MS")
	var stt types.SetTimeResult
	cli(t, st, "ree", "set-time", "--format", "json").want(t, 0).decode(t, &stt)
	cli(t, st, "ree", "set-time", "--time", "tomorrow").want(t, 3)
}

func TestTypesCommand(t *testing.T) {
	run(t, "client", "types").want(t, 0).has(t, "C_AC_NA_2", "M_IT_TK_2")
	run(t, "client", "types", "--profile", "ree", "--format", "table").want(t, 0).has(t, "DECODED")
	var r types.TypesResult
	run(t, "client", "types", "--direction", "control", "--format", "json").want(t, 0).decode(t, &r)
	run(t, "client", "types", "--direction", "sideways").want(t, 3)
}

func TestUsageAndInput(t *testing.T) {
	run(t, "client").want(t, 0).has(t, "Usage:")
	run(t, "client", "ree").want(t, 0).has(t, "set-time")
	run(t, "server").want(t, 0).has(t, "generate-config")
	run(t, "client", "info").want(t, 3).errHas(t, "no meter given")
	run(t, "client", "info", "--url", "udp://x").want(t, 3)
	run(t, "client", "info", "--ip", "127.0.0.1", "--format", "xml").want(t, 3)
	run(t, "client", "info", "--no-such-flag").want(t, 2)
	run(t, "client", "info", "--record", "x").want(t, 2)
	run(t, "nosuchcommand").want(t, 2)
	t.Setenv("IEC102CTL_RECORD", "abc")
	run(t, "client", "types").want(t, 3)
}

func TestConnectionErrorsExitCodes(t *testing.T) {
	run(t, "client", "info", "--url", "tcp://127.0.0.1:1", "--timeout", "2000").want(t, 4)
	st := stationtest.Start(t)
	run(t, "client", "info", "--url", st.URL(), "--link-address", "9", "--link-timeout", "100", "--link-retries", "1").want(t, 5).errHas(t, "link address 9")
	r := run(t, "client", "info", "--url", st.URL(), "--link-address", "9", "--link-timeout", "100", "--link-retries", "1", "--debug")
	r.want(t, 5).errHas(t, "debug:", "cause:")
}

func TestGenerateConfig(t *testing.T) {
	run(t, "server", "generate-config").want(t, 0).has(t, "access_key: 7")
	var m map[string]any
	r := run(t, "server", "generate-config", "--format", "json").want(t, 0)
	if err := json.Unmarshal([]byte(r.stdout), &m); err != nil || m["station"] == nil {
		t.Fatalf("json model: %v", err)
	}
	run(t, "server", "generate-config", "--format", "xml").want(t, 3)
	p := filepath.Join(t.TempDir(), "meter.yaml")
	run(t, "server", "generate-config", "--output", p).want(t, 0).errHas(t, "wrote")
	run(t, "server", "generate-config", "--output", p).want(t, 3).errHas(t, "--force")
	run(t, "server", "generate-config", "--output", p, "--force").want(t, 0)
	run(t, "server", "generate-config", "--output", filepath.Join(t.TempDir(), "no", "dir.yaml")).want(t, 10)
	// The generated model is served.
	run(t, "server", "start", "--config", p, "--listen", "127.0.0.1", "--port", freePort(t, "127.0.0.1"), "--duration", "1", "--format", "json").
		want(t, 0).has(t, `"event":"listening"`, `"event":"stopping"`)
}

func TestServerStart(t *testing.T) {
	run(t, "server", "start", "--listen", "127.0.0.1", "--port", freePort(t, "127.0.0.1"), "--duration", "1").want(t, 0).has(t, "listening", "Demo meter")
	run(t, "server", "start", "--listen", "127.0.0.1", "--port", freePort(t, "127.0.0.1"), "--duration", "1", "--format", "table").want(t, 0).has(t, "TIMESTAMP")
	run(t, "server", "start", "--config", "/nonexistent.yaml").want(t, 3)
	bad := filepath.Join(t.TempDir(), "bad.yaml")
	if err := os.WriteFile(bad, []byte("station: {clock: never}"), 0o600); err != nil {
		t.Fatal(err)
	}
	run(t, "server", "start", "--config", bad, "--format", "json").want(t, 3)
	run(t, "server", "start", "--format", "xml").want(t, 3)
	// A port that is taken.
	st := stationtest.Start(t)
	host, port, _ := strings.Cut(st.Addr, ":")
	run(t, "server", "start", "--listen", host, "--port", port, "--duration", "1").want(t, 4)
}

// The output of a failed write is an output error.
func TestServerWriteFails(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	code := Run(ctx, []string{"server", "start", "--listen", "127.0.0.1", "--port", freePort(t, "127.0.0.1"), "--duration", "1"}, failing{}, &bytes.Buffer{})
	if code != 10 {
		t.Errorf("exit %d", code)
	}
}

type failing struct{}

func (failing) Write([]byte) (int, error) { return 0, os.ErrClosed }

func TestExecute(t *testing.T) {
	old := os.Args
	t.Cleanup(func() { os.Args = old })
	os.Args = []string{"iec102ctl", "client", "types", "--format", "json"}
	r, w, _ := os.Pipe()
	stdout := os.Stdout
	os.Stdout = w
	code := Execute()
	os.Stdout = stdout
	_ = w.Close()
	var buf bytes.Buffer
	_, _ = buf.ReadFrom(r)
	if code != 0 || !strings.Contains(buf.String(), "C_AC_NA_2") {
		t.Errorf("exit %d", code)
	}
}

func TestCompletionScripts(t *testing.T) {
	for _, sh := range []string{"bash", "zsh", "fish", "powershell"} {
		if r := run(t, "completion", sh).want(t, 0); !strings.Contains(r.stdout, "iec102ctl") {
			t.Errorf("%s: no script", sh)
		}
	}
	run(t, "completion", "tcsh").want(t, 2)
}

// freePort returns a TCP port that is free on host now: --port 0 means 2401, the port of the
// interop images, which other tests of the repository use too.
func freePort(t testing.TB, host string) string {
	t.Helper()
	ln, err := net.Listen("tcp", net.JoinHostPort(host, "0"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ln.Close() }()
	return strconv.Itoa(ln.Addr().(*net.TCPAddr).Port)
}
