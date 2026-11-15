// SPDX-License-Identifier: MIT

package cmd

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/otfabric/iec60870ctl/internal/iec103ctl/stationtest"
)

// The final acknowledgement of a transfer lets the device delete the record, so the files
// of disturbance get are created first: a file that cannot be created exits 3 before the
// device is even connected, and leaves no other file behind. A transfer that fails
// removes the files it created.
func TestDisturbanceGetCreatesTheFilesFirst(t *testing.T) {
	st := stationtest.Start(t, stationtest.Model)
	dir := t.TempDir()
	out, csv := filepath.Join(dir, "fault.json"), filepath.Join(dir, "fault.csv")
	missing := filepath.Join(dir, "missing")
	for _, args := range [][]string{
		{"--output", filepath.Join(missing, "fault.json")},
		{"--csv", filepath.Join(missing, "fault.csv")},
		{"--output", out, "--csv", filepath.Join(missing, "fault.csv")},
		{"--output", out, "--csv", filepath.Join(missing, "fault.csv"), "--force"},
	} {
		r := cli(t, st, append([]string{"disturbance", "get", "--fan", "7", "--format", "json"}, args...)...).want(t, 3)
		if kind, code := r.errorCode(t); kind != "invalid_input" || code != "output_file_create_failed" {
			t.Errorf("%v: %s %s", args, kind, code)
		}
		if _, err := os.Stat(out); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("%v: %s left behind: %v", args, out, err)
		}
	}
	for _, kind := range []string{"link", "disturbance-transmitted"} {
		if st.HasEvent(kind) {
			t.Errorf("a %s event: the command reached the device", kind)
		}
	}
	cli(t, st, "disturbance", "get", "--fan", "99", "--output", out, "--csv", csv).want(t, 6)
	for _, p := range []string{out, csv} {
		if _, err := os.Stat(p); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("%s left behind by a transfer that failed: %v", p, err)
		}
	}
}
