// SPDX-License-Identifier: MIT

package iec

import (
	"math"
	"strconv"
	"strings"
	"testing"
)

// fullRanges is a list of n ranges over the whole 24-bit address space.
func fullRanges(n int) string {
	parts := make([]string, n)
	for i := range parts {
		parts[i] = "0-" + strconv.Itoa(MaxIOA)
	}
	return strings.Join(parts, ",")
}

// The number of addresses a --ioa list names is bounded on every platform: lists whose
// total exceeds 2^31 or 2^32 (a 32-bit int) are refused as too many, as on a 64-bit one.
func TestIOAListCountBounds(t *testing.T) {
	for _, c := range []struct {
		name  string
		list  string
		count int // 0 when the list is refused
	}{
		{"empty", "", 0},
		{"one", "7", 1},
		{"maximum - 1", "1-" + strconv.Itoa(maxListed-1), maxListed - 1},
		{"maximum", "1-" + strconv.Itoa(maxListed), maxListed},
		{"maximum + 1", "1-" + strconv.Itoa(maxListed+1), 0},
		{"maximum in two ranges", "1-50000,60001-110000", maxListed},
		{"maximum + 1 in two ranges", "1-50000,60001-110001", 0},
		{"one full range", fullRanges(1), 0},
		{"127 full ranges", fullRanges(127), 0},
		{"128 full ranges (2^31 addresses)", fullRanges(128), 0},
		{"129 full ranges", fullRanges(129), 0},
		{"256 full ranges (2^32 addresses)", fullRanges(256), 0},
		{"257 full ranges", fullRanges(257), 0},
		{"256 full ranges and one address", fullRanges(256) + ",1", 0},
	} {
		r, err := ParseIOAs(c.list)
		if c.count == 0 && c.list != "" {
			if err == nil || !strings.Contains(err.Error(), "too many") {
				t.Errorf("%s: %d addresses, %v; want too many", c.name, r.Count(), err)
			}
			continue
		}
		if err != nil || r.Count() != c.count {
			t.Errorf("%s: %d addresses, %v; want %d", c.name, r.Count(), err, c.count)
		}
	}
	// Count stops just above the bound, whatever the set names.
	if n := (Ranges{{0, math.MaxUint32}, {0, math.MaxUint32}}).Count(); n != maxListed+1 {
		t.Errorf("two ranges of 2^32 numbers count %d, want %d", n, maxListed+1)
	}
}

// --count of client monitor holds every value of its uint32 flag: a count of 2^31 or more
// is not reached by the first value on a platform with a 32-bit int.
func TestMonitorCountReached(t *testing.T) {
	for _, c := range []struct {
		values int
		count  uint32
		want   bool
	}{
		{0, 0, false},
		{1, 0, false},
		{math.MaxInt32, 0, false},
		{0, 1, false},
		{1, 1, true},
		{2, 1, true},
		{1, 2, false},
		{math.MaxInt32 - 1, math.MaxInt32, false},
		{math.MaxInt32, math.MaxInt32, true},
		{1, 1 << 31, false},
		{math.MaxInt32, 1 << 31, false},
		{1, math.MaxUint32, false},
		{math.MaxInt32, math.MaxUint32, false},
	} {
		if got := countReached(c.values, c.count); got != c.want {
			t.Errorf("countReached(%d, %d) = %v, want %v", c.values, c.count, got, c.want)
		}
	}
}
