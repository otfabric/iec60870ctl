// SPDX-License-Identifier: MIT

package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/otfabric/iec104ctl/internal/stationtest"
)

// private fails t unless path is readable by its owner only. The mode is checked against
// group and others, so that it holds under any umask.
func private(t *testing.T, path string) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm&0o077 != 0 {
		t.Errorf("%s has mode %v: group or others can access it", path, perm)
	}
}

// The files the tool writes for the operator are the operator's own.
func TestOutputFilesArePrivate(t *testing.T) {
	dir := t.TempDir()
	model := filepath.Join(dir, "model.yaml")
	run(t, "server", "generate-config", "--output", model).want(t, 0)
	private(t, model)
	// With --force an existing file is truncated; it keeps being private.
	run(t, "server", "generate-config", "--output", model, "--force", "--format", "json").want(t, 0)
	private(t, model)

	st := stationtest.Start(t, stationtest.Model)
	out := filepath.Join(dir, "file.bin")
	cli(t, st, "file", "get", "--ioa", "30001", "--name", "2", "--output", out).want(t, 0)
	private(t, out)
}

// A clock synchronization carries a two-digit year: a time outside 1970-2069 would go out in
// another century, flagged invalid. It is refused before connecting, with the exit code of
// invalid input, and the station receives nothing.
func TestClockOutsideTheCP56Window(t *testing.T) {
	st := stationtest.Start(t, stationtest.Model)
	for _, at := range []string{"2126-01-31T12:00:00Z", "1969-12-31T23:59:59Z", "2069-12-31T23:30:00-01:00"} {
		cli(t, st, "clock", "--time", at).want(t, 3).errHas(t, "1970 to 2069")
	}
	// The window is that of the time zone on the wire: 23:30 UTC on the last day of 2069 is
	// 2070 in Brussels.
	cli(t, st, "clock", "--time", "2069-12-31T23:30:00Z", "--timezone", "Europe/Brussels").want(t, 3)
	if st.HasEvent("clock") {
		t.Error("a refused clock synchronization reached the station")
	}
	cli(t, st, "clock", "--time", "2069-12-31T23:30:00Z").want(t, 0)
	cli(t, st, "clock", "--time", "1970-01-01T00:00:00Z").want(t, 0)
}
