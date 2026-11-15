// SPDX-License-Identifier: MIT

package cmd

import (
	"path/filepath"
	"testing"

	"github.com/otfabric/iec60870ctl/internal/iec104ctl/stationtest"
	"github.com/otfabric/iec60870ctl/internal/iec104ctl/types"
)

// IEC104CTL_IOA is the list of `client interrogate`, `read` and `monitor`, with ranges. The
// single address of `client command` has IEC104CTL_COMMAND_IOA, the one of the file
// commands IEC104CTL_FILE_IOA: a list in IEC104CTL_IOA leaves every other command as it is.
func TestIOAVariables(t *testing.T) {
	st := stationtest.Start(t, stationtest.Model)
	t.Setenv("IEC104CTL_IOA", "100-199")

	run(t, "version").want(t, 0)

	var gi types.InterrogateResult
	cli(t, st, "interrogate", "--format", "json").want(t, 0).decode(t, &gi)
	if gi.Count != 2 || gi.Received != 8 || gi.Points[0].IOA != 100 || gi.Points[1].IOA != 101 {
		t.Errorf("interrogation filtered by IEC104CTL_IOA: %+v", gi)
	}
	cli(t, st, "read", "--ioa", "1000").want(t, 0).has(t, "1000")

	cli(t, st, "command", "--type", "single", "--ioa", "5000", "--value", "on").want(t, 0)
	cli(t, st, "file", "list").want(t, 0).has(t, "2 entries")
	cli(t, st, "file", "get", "--ioa", "30000", "--output", filepath.Join(t.TempDir(), "f.bin")).want(t, 0)

	t.Run("IEC104CTL_FILE_IOA", func(t *testing.T) {
		// The station has its directory at 0 and refuses one at 5, as with --ioa 5.
		t.Setenv("IEC104CTL_FILE_IOA", "5")
		cli(t, st, "file", "list").want(t, 6)
		var list types.FileListResult
		cli(t, st, "file", "list", "--ioa", "0", "--format", "json").want(t, 0).decode(t, &list)
		if list.IOA != 0 || list.Count != 2 {
			t.Errorf("--ioa 0 on the command line: %+v", list)
		}
	})
	t.Run("IEC104CTL_COMMAND_IOA", func(t *testing.T) {
		// --ioa of `client command` is required on the command line, as before.
		t.Setenv("IEC104CTL_COMMAND_IOA", "5000")
		cli(t, st, "command", "--type", "single", "--value", "on").want(t, 2).errHas(t, `"ioa" not set`)
	})
}
