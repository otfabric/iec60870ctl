// SPDX-License-Identifier: MIT

package errs

import (
	"errors"
	"fmt"
	"testing"
)

func TestErrors(t *testing.T) {
	cause := errors.New("root cause")
	e := New(KindTransport, CodeTransportConnectFailed, "cannot connect", cause)
	if e.Error() != "cannot connect" || !errors.Is(e, cause) {
		t.Errorf("error %q, unwraps to %v", e, errors.Unwrap(e))
	}
	// Without a message the cause speaks, and without both the kind.
	if got := New(KindTimeout, CodeRequestTimeout, "", cause).Error(); got != "root cause" {
		t.Errorf("message from the cause: %q", got)
	}
	if got := New(KindTimeout, CodeRequestTimeout, "", nil).Error(); got != "timeout" {
		t.Errorf("message from the kind: %q", got)
	}
	var none *Error
	if none.Error() != "" || none.Unwrap() != nil {
		t.Error("a nil *Error is not safe to use")
	}

	wrapped := fmt.Errorf("context: %w", e)
	if got, ok := As(wrapped); !ok || got != e {
		t.Error("As does not find a wrapped typed error")
	}
	if _, ok := As(cause); ok {
		t.Error("As took a plain error for a typed one")
	}

	if got := InvalidInput(CodeInvalidURL, "bad", cause); got.Kind != KindInvalidInput || got.Code != CodeInvalidURL {
		t.Errorf("InvalidInput: %+v", got)
	}
	if got := Output(CodeJSONEncodeFailed, cause); got.Kind != KindOutput || got.Message != "root cause" {
		t.Errorf("Output: %+v", got)
	}
	if got := Output(CodeJSONEncodeFailed, nil); got.Message != "output error" {
		t.Errorf("Output without cause: %+v", got)
	}
	if got := Internal(cause); got.Kind != KindInternal || got.Code != CodeInternalUnexpected || !errors.Is(got, cause) {
		t.Errorf("Internal: %+v", got)
	}
	if got := FormatInvalid(cause); got.Code != CodeInvalidFormat || got.Message != "invalid output format: root cause" {
		t.Errorf("FormatInvalid: %+v", got)
	}
	if got := WrapValidation(cause); got.Kind != KindInvalidInput || got.Message != "invalid input: root cause" {
		t.Errorf("WrapValidation: %+v", got)
	}
	if WrapValidation(nil) != nil || WrapValidation(wrapped) != e {
		t.Error("WrapValidation must pass nil and typed errors through")
	}
}

func TestExitCodes(t *testing.T) {
	for kind, want := range map[Kind]int{
		KindUsage:        2,
		KindInvalidInput: 3,
		KindTransport:    4,
		KindTimeout:      5,
		KindProtocol:     6,
		KindNegative:     6,
		KindOutput:       10,
		KindInternal:     10,
		Kind("new"):      10,
	} {
		if got := ExitCodeForKind(kind); got != want {
			t.Errorf("exit code of %q: %d, want %d", kind, got, want)
		}
	}
	if ExitOK != 0 || ExitPartial != 7 {
		t.Error("the exit codes for success and partial success are part of the contract")
	}
}
