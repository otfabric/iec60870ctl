// SPDX-License-Identifier: MIT

package iec

import (
	"strconv"
	"strings"
	"testing"
)

// fullRanges is a list of n ranges that each name the whole 24-bit address space.
func fullRanges(n int) string {
	return strings.TrimSuffix(strings.Repeat("0-"+strconv.Itoa(MaxIOA)+",", n), ",")
}

// A list is counted the same on every platform: one that names more than maxListed
// addresses is refused, also when its count does not fit an int of 32 bits.
func TestParseIOAsCount(t *testing.T) {
	for _, c := range []struct {
		name  string
		list  string
		count int64
		ok    bool
	}{
		{"one", "7", 1, true},
		{"the maximum", "1-100000", maxListed, true},
		{"one more than the maximum", "1-100001", maxListed + 1, false},
		{"the maximum in two ranges", "1-50000,50001-100000", maxListed, true},
		{"one full range", fullRanges(1), 1 << 24, false},
		{"128 full ranges", fullRanges(128), 128 << 24, false},
		{"129 full ranges, above 2^31", fullRanges(129), 129 << 24, false},
		{"256 full ranges, 2^32", fullRanges(256), 256 << 24, false},
		{"257 full ranges", fullRanges(257), 257 << 24, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			r, err := ParseRanges(c.list, "information object address", MaxIOA)
			if err != nil {
				t.Fatal(err)
			}
			if got := r.Count(); got != c.count {
				t.Errorf("Count() = %d, want %d", got, c.count)
			}
			_, err = ParseIOAs(c.list, MaxIOA)
			if c.ok && err != nil {
				t.Errorf("ParseIOAs: %v", err)
			}
			if !c.ok && (err == nil || !strings.Contains(err.Error(), "too many")) {
				t.Errorf("ParseIOAs: %v, want too many", err)
			}
		})
	}
}
