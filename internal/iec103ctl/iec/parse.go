// SPDX-License-Identifier: MIT

package iec

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/otfabric/go-iec60870-5/iec103"
)

// ParseDCO parses the double command of --value: on (2) or off (1). The values the
// standard does not use (0 and 3, clause 7.2.6.4) are refused.
func ParseDCO(s string) (iec103.DPI, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "on", "2":
		return iec103.DPIOn, nil
	case "off", "1":
		return iec103.DPIOff, nil
	}
	return 0, fmt.Errorf("invalid --value %q: use on or off (DCO 2 or 1; 0 and 3 are not used, clause 7.2.6.4)", s)
}

// ParseOctet parses a number 0..max.
func ParseOctet(s, what string, maxVal uint64) (uint8, error) {
	n, err := strconv.ParseUint(strings.TrimSpace(s), 10, 8)
	if err != nil || n > maxVal {
		return 0, fmt.Errorf("invalid %s %q: use 0-%d", what, s, maxVal)
	}
	return uint8(n), nil
}

// ParseGIN parses a generic identification number GROUP.ENTRY, each 0..255.
func ParseGIN(s string) (iec103.GIN, error) {
	g, e, ok := strings.Cut(strings.TrimSpace(s), ".")
	if !ok {
		return iec103.GIN{}, fmt.Errorf("invalid GIN %q: use GROUP.ENTRY, e.g. 1.2", s)
	}
	group, err := ParseOctet(g, "GIN group", 255)
	if err != nil {
		return iec103.GIN{}, fmt.Errorf("invalid GIN %q: group and entry are 0-255", s)
	}
	entry, err := ParseOctet(e, "GIN entry", 255)
	if err != nil {
		return iec103.GIN{}, fmt.Errorf("invalid GIN %q: group and entry are 0-255", s)
	}
	return iec103.GIN{Group: group, Entry: entry}, nil
}

// ParseKOD parses a kind of description: a number 0-255 or a name; "" is def.
func ParseKOD(s string, def iec103.KOD) (iec103.KOD, error) {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" {
		return def, nil
	}
	for k, n := range kodNames {
		if n == s {
			return k, nil
		}
	}
	n, err := strconv.ParseUint(s, 10, 8)
	if err != nil {
		return 0, fmt.Errorf("invalid KOD %q: use a number 0-255 or a name (actual, description, ...)", s)
	}
	return iec103.KOD(n), nil
}

// maxItems bounds the generic identifiers of one request: NOG is one octet, and an ASDU 21
// of the largest frame takes (253 - 8) / 3 of them.
const maxItems = 81

// ParseItems parses --items: GROUP.ENTRY[:KOD], comma-separated.
func ParseItems(s string) ([]iec103.Item, error) {
	var items []iec103.Item
	for part := range strings.SplitSeq(s, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		g, k, _ := strings.Cut(part, ":")
		gin, err := ParseGIN(g)
		if err != nil {
			return nil, err
		}
		kod, err := ParseKOD(k, iec103.KODActualValue)
		if err != nil {
			return nil, err
		}
		items = append(items, iec103.Item{GIN: gin, KOD: kod})
		if len(items) > maxItems {
			return nil, fmt.Errorf("too many items: at most %d fit one request", maxItems)
		}
	}
	return items, nil
}

// ParseTime parses --time: "now" or "" (the moment of sending, zero here) or RFC 3339.
func ParseTime(s string) (time.Time, error) {
	s = strings.TrimSpace(s)
	if s == "" || strings.EqualFold(s, "now") {
		return time.Time{}, nil
	}
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid --time %q: use now or an RFC 3339 time such as 2026-01-31T12:00:00Z", s)
	}
	if t.Year() < 2000 || t.Year() > 2099 {
		return time.Time{}, fmt.Errorf("invalid --time %q: a CP56Time2a carries the years 2000-2099", s)
	}
	return t, nil
}

// ParseDataType parses --type of a generic write: a name or a number.
func ParseDataType(s string) (iec103.DataType, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "ascii", "1":
		return iec103.DataASCII, nil
	case "uint", "3":
		return iec103.DataUint, nil
	case "int", "4":
		return iec103.DataInt, nil
	case "float32", "float", "7":
		return iec103.DataFloat32, nil
	case "float64", "double", "8":
		return iec103.DataFloat64, nil
	}
	return 0, fmt.Errorf("invalid --type %q: use ascii (1), uint (3), int (4), float32 (7) or float64 (8)", s)
}

// WriteTypes lists the names --type of a generic write takes.
func WriteTypes() []string { return []string{"ascii", "uint", "int", "float32", "float64"} }

// ParseValue builds the GID of a generic write: value of type t, DATASIZE size (0: the
// text's length for ascii, the type's for the floats).
func ParseValue(t iec103.DataType, size uint16, value string) (iec103.Value, error) {
	switch t {
	case iec103.DataASCII:
		if size != 0 && int(size) != len(value) {
			return iec103.Value{}, fmt.Errorf("--size %d does not match the %d characters of --value", size, len(value))
		}
		if len(value) == 0 || len(value) > 255 {
			return iec103.Value{}, fmt.Errorf("an ascii --value has 1-255 characters")
		}
		for _, r := range value {
			if r < 0x20 || r > 0x7E {
				return iec103.Value{}, fmt.Errorf("an ascii --value has printable ASCII characters only")
			}
		}
		return iec103.ASCII(value), nil
	case iec103.DataUint:
		if size < 1 || size > 8 {
			return iec103.Value{}, fmt.Errorf("--size of a uint is 1-8 octets")
		}
		n, err := strconv.ParseUint(strings.TrimSpace(value), 0, 64)
		if err != nil || (size < 8 && n >= 1<<(8*size)) {
			return iec103.Value{}, fmt.Errorf("invalid --value %q: not an unsigned integer of %d octets", value, size)
		}
		return iec103.Uints(iec103.DataUint, int(size), n), nil
	case iec103.DataInt:
		if size < 1 || size > 8 {
			return iec103.Value{}, fmt.Errorf("--size of an int is 1-8 octets")
		}
		n, err := strconv.ParseInt(strings.TrimSpace(value), 0, 64)
		if err != nil || (size < 8 && (n < -(1<<(8*size-1)) || n >= 1<<(8*size-1))) {
			return iec103.Value{}, fmt.Errorf("invalid --value %q: not an integer of %d octets", value, size)
		}
		return iec103.Ints(int(size), n), nil
	case iec103.DataFloat32:
		if size != 0 && size != 4 {
			return iec103.Value{}, fmt.Errorf("--size of a float32 is 4")
		}
		f, err := strconv.ParseFloat(strings.TrimSpace(value), 32)
		if err != nil || math.IsNaN(f) || math.IsInf(f, 0) {
			return iec103.Value{}, fmt.Errorf("invalid --value %q: not a finite single precision number", value)
		}
		return iec103.Float32s(float32(f)), nil
	case iec103.DataFloat64:
		if size != 0 && size != 8 {
			return iec103.Value{}, fmt.Errorf("--size of a float64 is 8")
		}
		f, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
		if err != nil || math.IsNaN(f) || math.IsInf(f, 0) {
			return iec103.Value{}, fmt.Errorf("invalid --value %q: not a finite number", value)
		}
		return iec103.Float64s(f), nil
	default:
		return iec103.Value{}, fmt.Errorf("data type %d cannot be written", t)
	}
}
