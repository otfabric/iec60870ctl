// SPDX-License-Identifier: MIT

package cmd

import "testing"

// An error Cobra or the environment raises before a command runs is one JSON object on
// stdout when the command line asks for JSON (docs/json-output.md), and text otherwise.
func TestEarlyErrorsAsJSON(t *testing.T) {
	r := run(t, "client", "types", "--no-such-flag", "--format", "json").want(t, 2)
	if code := r.errorCode(t); code == "" {
		t.Errorf("unknown flag: no error code\n%s", r.stdout)
	}
	t.Setenv("IEC102CTL_FORMAT", "json")
	r = run(t, "client", "types", "--no-such-flag").want(t, 2)
	if code := r.errorCode(t); code == "" {
		t.Errorf("unknown flag, JSON from IEC102CTL_FORMAT: no error code\n%s", r.stdout)
	}
	t.Setenv("IEC102CTL_FORMAT", "")
	run(t, "client", "types", "--no-such-flag").want(t, 2).errHas(t, "error: unknown flag: --no-such-flag")
}
