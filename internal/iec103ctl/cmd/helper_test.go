// SPDX-License-Identifier: MIT

package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/otfabric/iec60870ctl/internal/iec103ctl/stationtest"
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

// result is the outcome of one invocation of iec103ctl.
type result struct {
	stdout, stderr string
	code           int
}

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

// errorCode returns the kind and code of the JSON error object on stdout.
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

// cli runs `iec103ctl client <words…> --url <st> <flags…>`: words are the leading
// arguments that do not start with "-".
func cli(t testing.TB, st *stationtest.Station, args ...string) result {
	t.Helper()
	n := 0
	for n < len(args) && !strings.HasPrefix(args[n], "-") {
		n++
	}
	full := append([]string{"client"}, args[:n]...)
	full = append(full, "--url", st.URL(), "--link-timeout", "300", "--poll-interval", "10", "--timeout", "5000")
	return run(t, append(full, args[n:]...)...)
}
