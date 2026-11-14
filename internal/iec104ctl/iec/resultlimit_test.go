// SPDX-License-Identifier: MIT

package iec_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"github.com/otfabric/go-iec60870-5/telecontrol"
	"github.com/otfabric/iec60870ctl/internal/errs"
	"github.com/otfabric/iec60870ctl/internal/format"
	"github.com/otfabric/iec60870ctl/internal/iec104ctl/iec"
	"github.com/otfabric/iec60870ctl/internal/runner"
)

// A request that hits the library's result limit is a protocol error: exit 6 with kind
// protocol and code result_limit, in text and in the JSON error envelope.
func TestResultLimitExit(t *testing.T) {
	for _, c := range []struct {
		name string
		err  error
	}{
		{"bare", telecontrol.ErrResultLimit},
		{"wrapped", fmt.Errorf("%w: more than 65536 ASDUs answer the request", telecontrol.ErrResultLimit)},
		{"with context", fmt.Errorf("interrogate: %w", fmt.Errorf("%w: more than 16 queued", telecontrol.ErrResultLimit))},
	} {
		t.Run(c.name, func(t *testing.T) {
			wrapped := iec.WrapError(c.err)
			e, ok := errs.As(wrapped)
			if !ok || e.Kind != errs.KindProtocol || e.Code != errs.CodeResultLimit || !errors.Is(wrapped, telecontrol.ErrResultLimit) {
				t.Fatalf("classified as %+v, want protocol/result_limit wrapping the sentinel", e)
			}
			for _, f := range []format.OutputFormat{format.FormatText, format.FormatJSON} {
				inv := &runner.Invocation{}
				inv.SetOutputFormat(f)
				var stdout, stderr bytes.Buffer
				if code := runner.RenderFatal(inv, wrapped, &stdout, &stderr, false); code != 6 {
					t.Errorf("%s: exit %d, want 6", f, code)
				}
				if f != format.FormatJSON {
					if stderr.Len() == 0 {
						t.Errorf("%s: no error line on stderr", f)
					}
					continue
				}
				var env errs.Envelope
				if err := json.Unmarshal(stdout.Bytes(), &env); err != nil {
					t.Fatalf("JSON envelope: %v (%q)", err, stdout.String())
				}
				if env.Error.Kind != "protocol" || env.Error.Code != "result_limit" || env.Error.Message == "" {
					t.Errorf("JSON error %+v, want kind protocol, code result_limit", env.Error)
				}
			}
		})
	}
}
