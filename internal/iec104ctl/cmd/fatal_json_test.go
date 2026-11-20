// SPDX-License-Identifier: MIT

package cmd

import "testing"

// An error Cobra or the environment raises before a command runs is one JSON object on
// stdout when the command line asks for JSON (docs/json-output.md), and text otherwise.
func TestEarlyErrorsAsJSON(t *testing.T) {
	r := run(t, "client", "interrogate", "--no-such-flag", "--format", "json").want(t, 2)
	if kind, _ := r.errorCode(t); kind != "usage" {
		t.Errorf("unknown flag: kind %q\n%s", kind, r.stdout)
	}
	t.Setenv("IEC104CTL_FORMAT", "json")
	r = run(t, "client", "interrogate", "--no-such-flag").want(t, 2)
	if kind, _ := r.errorCode(t); kind != "usage" {
		t.Errorf("unknown flag, JSON from IEC104CTL_FORMAT: kind %q\n%s", kind, r.stdout)
	}
	t.Setenv("IEC104CTL_FORMAT", "")
	run(t, "client", "interrogate", "--no-such-flag").want(t, 2).errHas(t, "error: unknown flag: --no-such-flag")
}
