// SPDX-License-Identifier: MIT

package iec

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/otfabric/go-iec60870-5/iec103"
	"github.com/otfabric/iec60870ctl/internal/iec103ctl/config"
	"github.com/otfabric/iec60870ctl/internal/iec103ctl/types"
)

// OctetSet is a set of one-octet values (type identifications, FUN, INF, causes); a nil
// set filters nothing.
type OctetSet *[256]bool

// parseOctets parses a comma-separated list of values and ranges LOW-HIGH of 0..255.
// name, when not nil, resolves an element that is not a number. An empty list is a nil set.
func parseOctets(s, what string, name func(string) (uint8, bool)) (OctetSet, error) {
	var set [256]bool
	any := false
	for part := range strings.SplitSeq(s, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		lo, hi, isRange := strings.Cut(part, "-")
		if isRange && name != nil {
			// Names carry hyphens: a range is two numbers.
			if _, err := strconv.ParseUint(strings.TrimSpace(lo), 10, 8); err != nil {
				isRange = false
			}
		}
		if !isRange {
			v, err := octet(part, what, name)
			if err != nil {
				return nil, err
			}
			set[v], any = true, true
			continue
		}
		a, err := octet(lo, what, nil)
		if err != nil {
			return nil, err
		}
		b, err := octet(hi, what, nil)
		if err != nil {
			return nil, err
		}
		if a > b {
			return nil, fmt.Errorf("invalid %s range %q: the first value is above the last", what, part)
		}
		for v := int(a); v <= int(b); v++ {
			set[v] = true
		}
		any = true
	}
	if !any {
		return nil, nil
	}
	return &set, nil
}

// octet parses one value 0..255, or a name that name resolves.
func octet(s, what string, name func(string) (uint8, bool)) (uint8, error) {
	s = strings.TrimSpace(s)
	if n, err := strconv.ParseUint(s, 10, 8); err == nil {
		return uint8(n), nil
	}
	if name != nil {
		if v, ok := name(s); ok {
			return v, nil
		}
		return 0, fmt.Errorf("invalid %s %q: use a name or a number 0-255", what, s)
	}
	return 0, fmt.Errorf("invalid %s %q: use a number 0-255", what, s)
}

// typeByName resolves a type identification by the name the output writes.
func typeByName(s string) (uint8, bool) {
	s = strings.ToLower(s)
	for t, n := range typeNames {
		if n == s {
			return uint8(t), true
		}
	}
	return 0, false
}

// causeKey is the name of a cause as --cause takes it: its name in lower case, with a
// hyphen for every run of other characters ("reset-frame-count-bit-fcb").
func causeKey(name string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(name) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			if dash && b.Len() > 0 {
				b.WriteByte('-')
			}
			b.WriteRune(r)
			dash = false
			continue
		}
		dash = true
	}
	return b.String()
}

// CauseNames returns the names --cause takes, by number: the causes of clause 7.2.3.
func CauseNames() []string {
	var out []string
	for c := 1; c <= 63; c++ {
		if k := causeKey(CauseName(iec103.Cause(c))); k != "" && k != "reserved" {
			out = append(out, k)
		}
	}
	return out
}

// causeByName resolves a cause of transmission by the name of CauseNames.
func causeByName(s string) (uint8, bool) {
	s = causeKey(s)
	for c := 1; c <= 63; c++ {
		if k := causeKey(CauseName(iec103.Cause(c))); k == s && k != "reserved" {
			return uint8(c), true
		}
	}
	return 0, false
}

// MonitorFilter is what `client monitor` shows: a nil set lets every value pass.
type MonitorFilter struct {
	Types, Funs, Infs, Causes OctetSet
}

// ParseMonitorFilter parses --type, --fun, --inf and --cause of `client monitor`.
func ParseMonitorFilter(cfg *config.MonitorConfig) (MonitorFilter, error) {
	var f MonitorFilter
	var err error
	if f.Types, err = parseOctets(cfg.Types, "--type", typeByName); err != nil {
		return f, err
	}
	if f.Funs, err = parseOctets(cfg.Funs, "--fun", nil); err != nil {
		return f, err
	}
	if f.Infs, err = parseOctets(cfg.Infs, "--inf", nil); err != nil {
		return f, err
	}
	if f.Causes, err = parseOctets(cfg.Causes, "--cause", causeByName); err != nil {
		return f, err
	}
	return f, nil
}

func pass(set OctetSet, v uint8) bool { return set == nil || set[v] }

// Match reports whether m passes every filter.
func (f MonitorFilter) Match(m *types.Message) bool {
	return pass(f.Types, m.TypeID) && pass(f.Funs, m.Fun) && pass(f.Infs, m.Inf) && pass(f.Causes, m.Cause)
}

// typeDirections are the types of the control direction and of both directions (clause
// 7.2.1, tables 3 and 4); every other type of the tables is of the monitor direction.
var typeDirections = map[iec103.TypeID]string{
	iec103.TypeTimeSync: "both", iec103.TypeGenericData: "both",
	iec103.TypeGIInit: "control", iec103.TypeGeneralCommand: "control", iec103.TypeGenericCommand: "control",
	iec103.TypeDisturbanceOrder: "control", iec103.TypeDisturbanceAck: "control",
}

// typeDirection is the direction of a type identification: "monitor", "control" or "both".
func typeDirection(t iec103.TypeID) string {
	if d, ok := typeDirections[t]; ok {
		return d
	}
	return "monitor"
}

// TypeNames returns the names of the type identifications, sorted by number.
func TypeNames() []string {
	ids := make([]int, 0, len(typeNames))
	for t := range typeNames {
		ids = append(ids, int(t))
	}
	slices.Sort(ids)
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		out = append(out, typeNames[iec103.TypeID(id)])
	}
	return out
}

// CheckTypes validates `client types`.
func CheckTypes(cfg *config.TypesConfig) error {
	switch cfg.Direction {
	case "", "monitor", "control":
		return nil
	}
	return fmt.Errorf("invalid --direction %q: use monitor or control", cfg.Direction)
}

// Types lists the type identifications of the compatible range that tables 3 and 4 of
// clause 7.2.1 define.
func Types(cfg *config.TypesConfig) *types.TypesResult {
	res := &types.TypesResult{Types: []types.TypeInfo{}}
	for id := 1; id <= 255; id++ {
		t := iec103.TypeID(id)
		if !t.Defined() {
			continue
		}
		dir := typeDirection(t)
		if cfg.Direction != "" && dir != "both" && dir != cfg.Direction {
			continue
		}
		res.Types = append(res.Types, types.TypeInfo{ID: uint8(t), Name: TypeName(t), Description: stripNumber(t.String()), Direction: dir})
	}
	res.Count = len(res.Types)
	return res
}
