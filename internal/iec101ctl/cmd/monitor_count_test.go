// SPDX-License-Identifier: MIT

package cmd

import (
	"strconv"
	"testing"

	"github.com/otfabric/iec60870ctl/internal/iec101ctl/stationtest"
)

// A --count of monitor at or above 2^31 is a count of values like any other, on 32-bit
// platforms as well: the monitor writes every value of the interrogation and runs until
// --duration ends.
func TestMonitorLargeCount(t *testing.T) {
	st := stationtest.Start(t, stationtest.Model, nil)
	var gi struct {
		Points []map[string]any `json:"points"`
	}
	cli(t, st, "interrogate", "--format", "json").want(t, 0).decodeLoose(t, &gi)
	if len(gi.Points) < 2 {
		t.Fatalf("interrogation: %d points", len(gi.Points))
	}
	for _, count := range []uint64{1<<31 - 1, 1 << 31, 1<<32 - 1} {
		t.Run(strconv.FormatUint(count, 10), func(t *testing.T) {
			r := cli(t, st, "monitor", "--interrogate", "--count", strconv.FormatUint(count, 10), "--duration", "1", "--format", "json").want(t, 0)
			n := 0
			for _, v := range r.lines(t) {
				if v["cause"] == "interrogated-station" {
					n++
				}
			}
			if n != len(gi.Points) {
				t.Errorf("--count %d: %d values of the interrogation, want %d\n%s", count, n, len(gi.Points), r.stdout)
			}
		})
	}
}
