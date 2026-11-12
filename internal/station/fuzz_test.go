// SPDX-License-Identifier: MIT

package station

import (
	"math"
	"strings"
	"testing"

	"github.com/otfabric/go-iec104/asdu"
)

// FuzzParse feeds model files: the operator's, or one handed to them. A model
// that parses and validates must survive its own encoding.
func FuzzParse(f *testing.F) {
	data, err := Demo().Encode(false)
	if err != nil {
		f.Fatal(err)
	}
	f.Add(data)
	if data, err = Demo().Encode(true); err != nil {
		f.Fatal(err)
	}
	f.Add(data)
	for _, s := range []string{"", "{}", "null", "- a\n- b\n", "station: {common_address: 1}\npoints:\n  - {ioa: 1, type: M_SP_NA_1, value: true}\n", "station: {common_address: 1, colour: red}", "points: [{}]\n", "\xff\xfe", "a: &a [*a]\n"} {
		f.Add([]byte(s))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		m, err := Parse(data)
		if err != nil {
			return
		}
		// Validate reads the files a model names: no input gets to pick them here.
		m.Files = nil
		if m.Validate() != nil {
			return
		}
		out, err := m.Encode(false)
		if err != nil {
			t.Fatalf("a valid model does not encode: %v\n%s", err, data)
		}
		back, err := Parse(out)
		if err != nil {
			t.Fatalf("a valid model does not parse back: %v\n%s", err, out)
		}
		if err := back.Validate(); err != nil {
			t.Fatalf("a valid model does not validate back: %v\n%s", err, out)
		}
	})
}

// The value of a point in a model, as YAML hands it over: a string, a whole number, a
// fraction or a boolean. What parses is within the range of the type, a whole number where
// the type needs one, and builds an object that encodes.
func FuzzParseValue(f *testing.F) {
	for _, s := range []string{"on", "off", "intermediate", "1", "0x10", "-64", "63", "64", "4294967295", "4294967296", "0.5", "1e400", "NaN", "-Inf", "1.5", " 7 "} {
		f.Add(s, int64(1), 0.5, true, uint8(0))
	}
	types := make([]asdu.TypeID, 0, len(monitorTypes))
	for _, t := range monitorTypes {
		types = append(types, t)
	}
	f.Fuzz(func(t *testing.T, s string, n int64, x float64, b bool, which uint8) {
		typ := types[int(which)%len(types)]
		for _, v := range []any{s, int(n), n, uint64(n), x, b} {
			val, err := parseValue(typ, v)
			if err != nil {
				continue
			}
			if typ != asdu.M_SP_NA_1 && typ != asdu.M_DP_NA_1 {
				lo, hi := limits(typ)
				if math.IsNaN(val.f) || val.f < lo || val.f > hi {
					t.Fatalf("%s %#v: %v outside %v..%v", typ, v, val.f, lo, hi)
				}
				if typ != asdu.M_ME_NA_1 && typ != asdu.M_ME_NC_1 && val.f != math.Trunc(val.f) {
					t.Fatalf("%s %#v: %v is not whole", typ, v, val.f)
				}
			}
			p := &point{spec: PointSpec{IOA: 1}, typ: typ, val: val}
			if _, err := asdu.New(asdu.CauseSpontaneous, 1, p.object(asdu.Timestamp{})).Encode(asdu.IEC104); err != nil {
				t.Fatalf("%s %#v: %v", typ, v, err)
			}
		}
	})
}

// --allow: what parses is a list of masked prefixes, one per entry given.
func FuzzParseAllow(f *testing.F) {
	for _, s := range []string{"127.0.0.1", "10.0.0.0/8", "10.1.2.3/8", "::1", "fe80::/10", "", " , ", "host", "1.2.3.4/33", "::ffff:1.2.3.4"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		list := strings.Split(s, ",")
		prefixes, err := parseAllow(list)
		if err != nil {
			return
		}
		given := 0
		for _, e := range list {
			if strings.TrimSpace(e) != "" {
				given++
			}
		}
		if len(prefixes) != given {
			t.Fatalf("parseAllow(%q): %d prefixes of %d entries", s, len(prefixes), given)
		}
		for _, p := range prefixes {
			if !p.IsValid() || p != p.Masked() {
				t.Fatalf("parseAllow(%q): prefix %v", s, p)
			}
			if !p.Contains(p.Addr()) {
				t.Fatalf("parseAllow(%q): %v does not contain its address", s, p)
			}
		}
	})
}
