// SPDX-License-Identifier: MIT

package main

import (
	"io"
	"os"
	"strings"
	"testing"
)

// runMain runs main with args and returns its exit code and stdout.
func runMain(t *testing.T, args ...string) (int, string) {
	t.Helper()
	oldArgs, oldExit, oldStdout := os.Args, exit, os.Stdout
	t.Cleanup(func() { os.Args, exit, os.Stdout = oldArgs, oldExit, oldStdout })
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	code := -1
	os.Args = append([]string{"iec104ctl"}, args...)
	exit = func(c int) { code = c }
	os.Stdout = w
	main()
	os.Stdout = oldStdout
	_ = w.Close()
	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	return code, string(out)
}

func TestMainHandsBuildMetaToTheCommands(t *testing.T) {
	old := version
	t.Cleanup(func() { version = old })
	version = "9.9.9-test"
	code, out := runMain(t, "version")
	if code != 0 || !strings.HasPrefix(out, "iec104ctl 9.9.9-test (") {
		t.Fatalf("exit %d, stdout %q", code, out)
	}
}

func TestMainExitsWithTheCodeOfTheCommand(t *testing.T) {
	if code, _ := runMain(t, "--no-such-flag"); code != 2 {
		t.Fatalf("exit %d, want 2 (usage)", code)
	}
}
