// SPDX-License-Identifier: MIT

package runner

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/otfabric/iec104ctl/internal/errs"
	"github.com/otfabric/iec104ctl/internal/format"
	"github.com/spf13/cobra"
)

type text string

func (t text) MarshalTextOutput() (string, error) { return string(t), nil }

type failing struct{}

func (failing) Write([]byte) (int, error) { return 0, errors.New("pipe closed") }

func command(t *testing.T) (*cobra.Command, *Invocation, *bytes.Buffer) {
	t.Helper()
	inv := &Invocation{}
	cmd := &cobra.Command{Use: "x"}
	cmd.SetContext(WithInvocation(context.Background(), inv))
	var out bytes.Buffer
	cmd.SetOut(&out)
	return cmd, inv, &out
}

func TestRunFormatted(t *testing.T) {
	cmd, inv, out := command(t)
	err := RunFormattedChecked(cmd, "text", func() error { return nil },
		func(context.Context) (any, error) { return text("done"), nil },
		WithSuccessExit(func(any) int { return errs.ExitPartial }))
	if err != nil || out.String() != "done\n" || !inv.SuccessExitSet || inv.SuccessExitCode != errs.ExitPartial || inv.OutputFormat != format.FormatText {
		t.Errorf("run: %v, %q, %+v", err, out.String(), inv)
	}

	// An unknown format, a failed validation and a failed collection write nothing.
	for name, run := range map[string]func(*cobra.Command) error{
		"format": func(c *cobra.Command) error {
			return RunFormattedChecked(c, "xml", func() error { return nil }, func(context.Context) (any, error) { return text("x"), nil })
		},
		"validation": func(c *cobra.Command) error {
			return RunFormattedChecked(c, "json", func() error { return errors.New("bad flag") }, func(context.Context) (any, error) { return text("x"), nil })
		},
		"collection": func(c *cobra.Command) error {
			return RunFormattedChecked(c, "json", func() error { return nil }, func(context.Context) (any, error) { return nil, errors.New("no answer") })
		},
	} {
		cmd, inv, out := command(t)
		if err := run(cmd); err == nil || out.Len() != 0 || inv.SuccessExitSet {
			t.Errorf("%s: err %v, stdout %q, exit set %v", name, err, out.String(), inv.SuccessExitSet)
		}
	}
	cmd, _, _ = command(t)
	err = RunFormattedChecked(cmd, "json", func() error { return errors.New("bad flag") }, nil)
	if e, ok := errs.As(err); !ok || e.Kind != errs.KindInvalidInput || e.Message != "invalid input: bad flag" {
		t.Errorf("validation error: %+v", err)
	}

	// A result that cannot be written is an output error.
	for f, code := range map[string]string{"table": errs.CodeTableRenderFailed, "text": errs.CodeJSONEncodeFailed} {
		cmd, _, _ := command(t)
		cmd.SetOut(failing{})
		err := RunFormattedChecked(cmd, f, func() error { return nil }, func(context.Context) (any, error) { return 1, nil })
		if e, ok := errs.As(err); !ok || e.Kind != errs.KindOutput || e.Code != code {
			t.Errorf("%s: %+v", f, err)
		}
	}
	// Without an invocation in the context a command still runs.
	bare := &cobra.Command{Use: "x"}
	bare.SetContext(context.Background())
	var out2 bytes.Buffer
	bare.SetOut(&out2)
	if err := RunFormattedChecked(bare, "", func() error { return nil }, func(context.Context) (any, error) { return text("ok"), nil }); err != nil || out2.String() != "ok\n" {
		t.Errorf("without invocation: %v, %q", err, out2.String())
	}
}

func TestRunStream(t *testing.T) {
	cmd, inv, out := command(t)
	err := RunStream(cmd, "json", func() error { return nil }, func(_ context.Context, w *format.StreamWriter) (int, error) {
		_ = w.Write(map[string]int{"n": 1})
		return errs.ExitPartial, nil
	})
	if err != nil || out.String() != "{\"n\":1}\n" || inv.SuccessExitCode != errs.ExitPartial {
		t.Errorf("stream: %v, %q, %+v", err, out.String(), inv)
	}
	cmd, _, _ = command(t)
	if err := RunStream(cmd, "xml", func() error { return nil }, nil); err == nil {
		t.Error("an unknown format was accepted")
	}
	if err := RunStream(cmd, "text", func() error { return errors.New("bad") }, nil); err == nil {
		t.Error("a failed validation was ignored")
	}
	cause := errs.New(errs.KindTransport, errs.CodeConnectionLost, "lost", nil)
	if err := RunStream(cmd, "text", func() error { return nil }, func(context.Context, *format.StreamWriter) (int, error) { return 0, cause }); err != cause {
		t.Errorf("the error of the stream was not returned: %v", err)
	}
}

func TestRenderFatal(t *testing.T) {
	cause := errors.New("dial tcp: refused")
	typed := errs.New(errs.KindTransport, errs.CodeTransportConnectFailed, "connection error", cause)

	var stdout, stderr bytes.Buffer
	if code := RenderFatal(&Invocation{}, typed, &stdout, &stderr, false); code != 4 || stdout.Len() != 0 || stderr.String() != "error: connection error\n" {
		t.Errorf("text: code %d, stdout %q, stderr %q", code, stdout.String(), stderr.String())
	}
	// With --debug the causes follow.
	stderr.Reset()
	RenderFatal(nil, typed, &stdout, &stderr, true)
	if stderr.String() != "error: connection error\ncause: dial tcp: refused\n" {
		t.Errorf("debug: %q", stderr.String())
	}

	// With --format json one error object goes to stdout and nothing else.
	inv := &Invocation{}
	inv.SetOutputFormat(format.FormatJSON)
	stdout.Reset()
	stderr.Reset()
	if code := RenderFatal(inv, typed, &stdout, &stderr, false); code != 4 || stderr.Len() != 0 {
		t.Errorf("json: code %d, stderr %q", code, stderr.String())
	}
	want := "{\n  \"error\": {\n    \"kind\": \"transport\",\n    \"code\": \"transport_connect_failed\",\n    \"message\": \"connection error\"\n  }\n}\n"
	if stdout.String() != want {
		t.Errorf("json error object:\n%s", stdout.String())
	}
	stdout.Reset()
	RenderFatal(inv, typed, &stdout, &stderr, true)
	if !strings.Contains(stderr.String(), "cause: dial tcp: refused") {
		t.Errorf("json with debug: stderr %q", stderr.String())
	}

	// An untyped error is an internal one; a typed one without message takes its cause's.
	stdout.Reset()
	if code := RenderFatal(inv, cause, &stdout, &stderr, false); code != 10 || !strings.Contains(stdout.String(), `"internal_unexpected"`) {
		t.Errorf("untyped: code %d, %s", code, stdout.String())
	}
	stdout.Reset()
	RenderFatal(inv, errs.New(errs.KindTimeout, errs.CodeRequestTimeout, "", cause), &stdout, &stderr, false)
	if !strings.Contains(stdout.String(), `"message": "dial tcp: refused"`) {
		t.Errorf("message from the cause: %s", stdout.String())
	}
	stdout.Reset()
	RenderFatal(inv, errs.New(errs.KindTimeout, errs.CodeRequestTimeout, "", nil), &stdout, &stderr, false)
	if !strings.Contains(stdout.String(), `"message": "timeout"`) {
		t.Errorf("message from the kind: %s", stdout.String())
	}
	// When stdout cannot be written, say so on stderr.
	stderr.Reset()
	if code := RenderFatal(inv, typed, failing{}, &stderr, false); code != 10 || !strings.Contains(stderr.String(), "pipe closed") {
		t.Errorf("stdout fails: code %d, stderr %q", code, stderr.String())
	}
}
