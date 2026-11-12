// SPDX-License-Identifier: MIT

//go:build e2e

package e2e

import (
	"bytes"
	"errors"
	"os/exec"
	"strings"
	"testing"
)

// run executes the iec104ctl binary in E2E_BIN_DIR (set by make e2e).
func run(t *testing.T, args ...string) (output string, exitCode int) {
	t.Helper()
	bin := binary(t)
	var out bytes.Buffer
	cmd := exec.Command(bin, args...)
	cmd.Stdout = &out
	cmd.Stderr = &out
	err := cmd.Run()
	var exit *exec.ExitError
	switch {
	case err == nil:
		return out.String(), 0
	case errors.As(err, &exit):
		return out.String(), exit.ExitCode()
	default:
		t.Fatalf("run %s: %v", bin, err)
		return "", -1
	}
}

func TestVersionPrintsTheBuiltVersion(t *testing.T) {
	output, code := run(t, "version")
	if code != 0 {
		t.Fatalf("exit code %d, want 0\n%s", code, output)
	}
	if !strings.HasPrefix(output, "iec104ctl ") || !strings.Contains(output, " (") {
		t.Fatalf("output does not name iec104ctl and its build:\n%s", output)
	}
}

// An unknown command is a usage error: exit code 2 in docs/exitcodes.md.
func TestUnknownCommandIsAUsageError(t *testing.T) {
	output, code := run(t, "nosuchcommand")
	if code != 2 {
		t.Fatalf("exit code %d, want 2\n%s", code, output)
	}
	if !strings.Contains(output, `unknown command "nosuchcommand"`) {
		t.Fatalf("output does not name the unknown command:\n%s", output)
	}
}
