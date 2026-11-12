// SPDX-License-Identifier: MIT

package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/otfabric/iec104ctl/internal/stationtest"
)

// syncBuffer is an output buffer that is safe for concurrent writers: streaming commands
// write from the goroutines of the connection.
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

// result is the outcome of one invocation of iec104ctl.
type result struct {
	stdout, stderr string
	code           int
}

// run executes iec104ctl with args, as the process would.
func run(t testing.TB, args ...string) result {
	t.Helper()
	return runCtx(t, context.Background(), args...)
}

func runCtx(t testing.TB, ctx context.Context, args ...string) result {
	t.Helper()
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	var stdout, stderr syncBuffer
	code := Run(ctx, args, &stdout, &stderr)
	return result{stdout.String(), stderr.String(), code}
}

// want fails the test unless the exit code is code.
func (r result) want(t testing.TB, code int) result {
	t.Helper()
	if r.code != code {
		t.Fatalf("exit code %d, want %d\nstdout:\n%s\nstderr:\n%s", r.code, code, r.stdout, r.stderr)
	}
	return r
}

// has fails the test unless stdout contains every one of subs.
func (r result) has(t testing.TB, subs ...string) result {
	t.Helper()
	for _, s := range subs {
		if !strings.Contains(r.stdout, s) {
			t.Errorf("stdout does not contain %q:\n%s", s, r.stdout)
		}
	}
	return r
}

// errHas fails the test unless stderr contains every one of subs.
func (r result) errHas(t testing.TB, subs ...string) result {
	t.Helper()
	for _, s := range subs {
		if !strings.Contains(r.stderr, s) {
			t.Errorf("stderr does not contain %q:\n%s", s, r.stderr)
		}
	}
	return r
}

// decode parses stdout as one JSON document into v.
func (r result) decode(t testing.TB, v any) {
	t.Helper()
	dec := json.NewDecoder(strings.NewReader(r.stdout))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		t.Fatalf("stdout is not the expected JSON: %v\n%s", err, r.stdout)
	}
}

// lines parses stdout as JSON Lines.
func (r result) lines(t testing.TB) []map[string]any {
	t.Helper()
	var out []map[string]any
	for line := range strings.SplitSeq(strings.TrimSpace(r.stdout), "\n") {
		if line == "" {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			t.Fatalf("stdout line is not JSON: %v\n%s", err, line)
		}
		out = append(out, m)
	}
	return out
}

// errorCode returns the code of the JSON error object on stdout.
func (r result) errorCode(t testing.TB) (kind, code string) {
	t.Helper()
	var env struct {
		Error struct{ Kind, Code, Message string }
	}
	if err := json.Unmarshal([]byte(r.stdout), &env); err != nil || env.Error.Code == "" {
		t.Fatalf("stdout is not a JSON error object: %v\n%s", err, r.stdout)
	}
	return env.Error.Kind, env.Error.Code
}

// target returns the flags that address st.
func target(st *stationtest.Station) []string {
	return []string{"--ip", st.Host, "--port", strconv.Itoa(int(st.Port)), "--t1", "2", "--t2", "1"}
}

// cli runs `iec104ctl client <sub…>` against st with extra flags.
func cli(t testing.TB, st *stationtest.Station, args ...string) result {
	t.Helper()
	n := 1
	if args[0] == "file" {
		n = 2
	}
	full := append([]string{"client"}, args[:n]...)
	full = append(full, target(st)...)
	return run(t, append(full, args[n:]...)...)
}

// eventually polls cond for up to five seconds.
func eventually(t testing.TB, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}
