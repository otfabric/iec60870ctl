// SPDX-License-Identifier: MIT

package iec

import (
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"

	"github.com/otfabric/go-iec104/asdu"
	"github.com/otfabric/iec104ctl/internal/types"
)

// MaxIOA is the largest information object address of IEC 60870-5-104 (3 octets).
const MaxIOA = 1<<24 - 1

// maxListed bounds how many addresses a list such as --ioa may name.
const maxListed = 100000

// Range is an inclusive range of numbers.
type Range struct{ Lo, Hi uint32 }

// Ranges is a set of numbers given as a list of values and ranges.
type Ranges []Range

// Contains reports whether n is in the set. An empty set contains everything.
func (r Ranges) Contains(n uint32) bool {
	if len(r) == 0 {
		return true
	}
	for _, x := range r {
		if n >= x.Lo && n <= x.Hi {
			return true
		}
	}
	return false
}

// Count returns how many numbers the set names.
func (r Ranges) Count() int {
	n := 0
	for _, x := range r {
		n += int(x.Hi-x.Lo) + 1
	}
	return n
}

// Each calls f for every number of the set, in the order given.
func (r Ranges) Each(f func(n uint32) bool) {
	for _, x := range r {
		for n := x.Lo; ; n++ {
			if !f(n) {
				return
			}
			if n == x.Hi {
				break
			}
		}
	}
}

// ParseRanges parses a list such as "100,200-203": single numbers (decimal or 0x hex) and
// inclusive ranges, comma-separated, each at most max. what names the list in errors.
func ParseRanges(s, what string, max uint32) (Ranges, error) {
	var out Ranges
	for part := range strings.SplitSeq(s, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		lo, hi, isRange := strings.Cut(part, "-")
		a, err := parseNumber(lo, what, max)
		if err != nil {
			return nil, err
		}
		b := a
		if isRange {
			if b, err = parseNumber(hi, what, max); err != nil {
				return nil, err
			}
			if b < a {
				return nil, fmt.Errorf("invalid %s range %q: the end is below the start", what, part)
			}
		}
		out = append(out, Range{a, b})
	}
	return out, nil
}

func parseNumber(s, what string, max uint32) (uint32, error) {
	n, err := strconv.ParseUint(strings.TrimSpace(s), 0, 32)
	if err != nil {
		return 0, fmt.Errorf("invalid %s %q: not a number", what, strings.TrimSpace(s))
	}
	if uint32(n) > max {
		return 0, fmt.Errorf("invalid %s %d: the maximum is %d", what, n, max)
	}
	return uint32(n), nil
}

// ParseIOAs parses --ioa. An empty string is an empty set.
func ParseIOAs(s string) (Ranges, error) {
	r, err := ParseRanges(s, "information object address", MaxIOA)
	if err != nil {
		return nil, err
	}
	if r.Count() > maxListed {
		return nil, fmt.Errorf("too many information object addresses: %d, the maximum is %d", r.Count(), maxListed)
	}
	return r, nil
}

// typeNames maps the name of every type identification to its number.
var typeNames = func() map[string]asdu.TypeID {
	m := map[string]asdu.TypeID{}
	for id := 1; id < 256; id++ {
		t := asdu.TypeID(id)
		if name := t.String(); !strings.HasPrefix(name, "TypeID(") && !strings.HasPrefix(name, "type") {
			m[strings.ToUpper(name)] = t
		}
	}
	return m
}()

// TypeNames returns the names of the types go-iec104 knows, sorted by number.
func TypeNames() []string {
	ids := slices.Sorted(maps.Values(typeNames))
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		out = append(out, id.String())
	}
	return out
}

// ParseType parses a type identification given by name (M_SP_NA_1, case-insensitive) or
// number (1).
func ParseType(s string) (asdu.TypeID, error) {
	s = strings.TrimSpace(s)
	if n, err := strconv.ParseUint(s, 10, 8); err == nil && n > 0 {
		return asdu.TypeID(n), nil
	}
	if t, ok := typeNames[strings.ToUpper(s)]; ok {
		return t, nil
	}
	return 0, fmt.Errorf("unknown type %q: use a name such as M_SP_NA_1 or a number 1-255 (see 'iec104ctl client types')", s)
}

// ParseTypes parses --type: a comma-separated list of type names or numbers.
func ParseTypes(s string) (map[asdu.TypeID]bool, error) {
	out := map[asdu.TypeID]bool{}
	for part := range strings.SplitSeq(s, ",") {
		if strings.TrimSpace(part) == "" {
			continue
		}
		t, err := ParseType(part)
		if err != nil {
			return nil, err
		}
		out[t] = true
	}
	return out, nil
}

// Filter selects points by type, information object address and common address. The zero
// Filter selects everything.
type Filter struct {
	Types map[asdu.TypeID]bool
	IOAs  Ranges
	// CommonAddr, when HasCommonAddr is set, is the only station selected.
	CommonAddr    asdu.CommonAddr
	HasCommonAddr bool
}

// NewFilter parses the --type and --ioa values of a command.
func NewFilter(typeList, ioaList string) (Filter, error) {
	var f Filter
	var err error
	if f.Types, err = ParseTypes(typeList); err != nil {
		return f, err
	}
	if f.IOAs, err = ParseIOAs(ioaList); err != nil {
		return f, err
	}
	return f, nil
}

// Match reports whether p passes the filter.
func (f Filter) Match(p types.Point) bool {
	if len(f.Types) > 0 && !f.Types[asdu.TypeID(p.TypeID)] {
		return false
	}
	if f.HasCommonAddr && p.CommonAddr != uint16(f.CommonAddr) {
		return false
	}
	return f.IOAs.Contains(p.IOA)
}

// Apply returns the points that pass the filter.
func (f Filter) Apply(points []types.Point) []types.Point {
	out := make([]types.Point, 0, len(points))
	for _, p := range points {
		if f.Match(p) {
			out = append(out, p)
		}
	}
	return out
}

// CheckCommonAddr validates a station address for a request: 1..65534, or the broadcast
// address 65535 where the request allows it.
func CheckCommonAddr(ca uint16, broadcast bool) error {
	switch {
	case ca == 0:
		return fmt.Errorf("invalid common address 0: use 1-65534")
	case ca == uint16(asdu.Broadcast) && !broadcast:
		return fmt.Errorf("invalid common address 65535: the broadcast address cannot be used for this request")
	}
	return nil
}

// oneOf validates an enumerated flag value.
func oneOf(flagName, value string, allowed ...string) error {
	if slices.Contains(allowed, value) {
		return nil
	}
	return fmt.Errorf("invalid --%s %q (expected: %s)", flagName, value, strings.Join(allowed, ", "))
}
