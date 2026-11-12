// SPDX-License-Identifier: MIT

package iec

import (
	"strings"
	"testing"

	"github.com/otfabric/go-iec60870-5/iec103"
)

func TestParseDCO(t *testing.T) {
	for in, want := range map[string]iec103.DPI{"on": 2, "ON ": 2, "2": 2, "off": 1, "1": 1} {
		if got, err := ParseDCO(in); err != nil || got != want {
			t.Errorf("%q: %v %v", in, got, err)
		}
	}
	for _, in := range []string{"", "0", "3", "toggle"} {
		if _, err := ParseDCO(in); err == nil {
			t.Errorf("%q accepted", in)
		}
	}
}

func TestParseGINAndItems(t *testing.T) {
	if g, err := ParseGIN(" 1.255 "); err != nil || g != (iec103.GIN{Group: 1, Entry: 255}) {
		t.Fatal(g, err)
	}
	for _, in := range []string{"", "1", "1.", ".1", "256.1", "1.256", "a.b", "1.2.3"} {
		if _, err := ParseGIN(in); err == nil {
			t.Errorf("%q accepted", in)
		}
	}
	items, err := ParseItems("1.1, 1.2:10 ,,2.0:description")
	if err != nil || len(items) != 3 || items[0].KOD != 1 || items[1].KOD != 10 || items[2].KOD != 10 {
		t.Fatal(items, err)
	}
	if items, err := ParseItems(""); err != nil || items != nil {
		t.Fatal(items, err)
	}
	for _, in := range []string{"1.1:x", "x", strings.Repeat("1.1,", maxItems+1)} {
		if _, err := ParseItems(in); err == nil {
			t.Errorf("%q accepted", in)
		}
	}
}

func TestParseKOD(t *testing.T) {
	for in, want := range map[string]iec103.KOD{"": 3, "actual": 1, "Description": 10, "23": 23, "255": 255} {
		if got, err := ParseKOD(in, 3); err != nil || got != want {
			t.Errorf("%q: %v %v", in, got, err)
		}
	}
	for _, in := range []string{"256", "-1", "nope"} {
		if _, err := ParseKOD(in, 1); err == nil {
			t.Errorf("%q accepted", in)
		}
	}
	if len(KODNames()) != len(kodNames) {
		t.Fatal("names")
	}
}

func TestParseTime(t *testing.T) {
	if tm, err := ParseTime("now"); err != nil || !tm.IsZero() {
		t.Fatal(tm, err)
	}
	if tm, err := ParseTime("2026-01-31T12:00:00.5+01:00"); err != nil || tm.Hour() != 12 {
		t.Fatal(tm, err)
	}
	for _, in := range []string{"tomorrow", "1999-01-01T00:00:00Z", "2100-01-01T00:00:00Z"} {
		if _, err := ParseTime(in); err == nil {
			t.Errorf("%q accepted", in)
		}
	}
}

func TestParseValue(t *testing.T) {
	ok := []struct {
		typ, value string
		size       uint16
		want       iec103.DataType
		data       []byte
	}{
		{"ascii", "AB", 0, iec103.DataASCII, []byte("AB")},
		{"1", "AB", 2, iec103.DataASCII, []byte("AB")},
		{"uint", "0x1234", 2, iec103.DataUint, []byte{0x34, 0x12}},
		{"uint", "18446744073709551615", 8, iec103.DataUint, []byte{0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF}},
		{"int", "-2", 1, iec103.DataInt, []byte{0xFE}},
		{"int", "-9223372036854775808", 8, iec103.DataInt, []byte{0, 0, 0, 0, 0, 0, 0, 0x80}},
		{"float32", "1.5", 0, iec103.DataFloat32, []byte{0, 0, 0xC0, 0x3F}},
		{"float", "1.5", 4, iec103.DataFloat32, []byte{0, 0, 0xC0, 0x3F}},
		{"float64", "1", 8, iec103.DataFloat64, []byte{0, 0, 0, 0, 0, 0, 0xF0, 0x3F}},
		{"double", "1", 0, iec103.DataFloat64, []byte{0, 0, 0, 0, 0, 0, 0xF0, 0x3F}},
	}
	for _, c := range ok {
		dt, err := ParseDataType(c.typ)
		if err != nil {
			t.Fatal(err)
		}
		v, err := ParseValue(dt, c.size, c.value)
		if err != nil || v.Type != c.want || string(v.Data) != string(c.data) {
			t.Errorf("%s %q: %+v %v", c.typ, c.value, v, err)
		}
	}
	bad := []struct {
		t     iec103.DataType
		size  uint16
		value string
	}{
		{iec103.DataASCII, 3, "AB"}, {iec103.DataASCII, 0, ""}, {iec103.DataASCII, 0, "a\x01"}, {iec103.DataASCII, 0, strings.Repeat("a", 256)},
		{iec103.DataUint, 0, "1"}, {iec103.DataUint, 9, "1"}, {iec103.DataUint, 1, "256"}, {iec103.DataUint, 2, "-1"},
		{iec103.DataInt, 0, "1"}, {iec103.DataInt, 1, "128"}, {iec103.DataInt, 1, "-129"}, {iec103.DataInt, 2, "x"},
		{iec103.DataFloat32, 8, "1"}, {iec103.DataFloat32, 4, "1e40"}, {iec103.DataFloat32, 4, "NaN"},
		{iec103.DataFloat64, 4, "1"}, {iec103.DataFloat64, 8, "Inf"},
		{iec103.DataBitString, 1, "1"},
	}
	for _, c := range bad {
		if v, err := ParseValue(c.t, c.size, c.value); err == nil {
			t.Errorf("%d/%d %q accepted: %+v", c.t, c.size, c.value, v)
		}
	}
	if _, err := ParseDataType("bits"); err == nil {
		t.Fatal("bits accepted")
	}
	if len(WriteTypes()) != 5 || len(WriteModes()) != 2 {
		t.Fatal("lists")
	}
}

func TestParseOctet(t *testing.T) {
	if n, err := ParseOctet("254", "x", 254); err != nil || n != 254 {
		t.Fatal(n, err)
	}
	if _, err := ParseOctet("255", "x", 254); err == nil {
		t.Fatal("255 accepted")
	}
}
