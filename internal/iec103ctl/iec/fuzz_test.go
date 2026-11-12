// SPDX-License-Identifier: MIT

package iec

import (
	"testing"

	"github.com/otfabric/go-iec60870-5/iec103"
	"github.com/otfabric/iec60870ctl/internal/iec103ctl/config"
)

// The parsers of user input never panic, and what they accept is in range.

func FuzzParseGIN(f *testing.F) {
	for _, s := range []string{"1.2", "0.0", "255.255", "256.1", "1.", "a.b", " 7 . 8 "} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		g, err := ParseGIN(s)
		if err != nil {
			return
		}
		if back, err := ParseGIN(GINText(g)); err != nil || back != g {
			t.Fatalf("%q -> %v does not round-trip", s, g)
		}
	})
}

func FuzzParseItems(f *testing.F) {
	for _, s := range []string{"1.1", "1.1:10,1.2", "1.1:description", ",,", "1.1:256", "9.9:actual,"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		items, err := ParseItems(s)
		if err == nil && len(items) > maxItems {
			t.Fatalf("%d items", len(items))
		}
	})
}

func FuzzParseKOD(f *testing.F) {
	for _, s := range []string{"", "1", "actual", "range", "300", "-1"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		k, err := ParseKOD(s, iec103.KODActualValue)
		if err == nil && KODName(k) == "" {
			t.Fatal("no name")
		}
	})
}

func FuzzParseValue(f *testing.F) {
	for _, c := range []struct {
		typ   string
		size  uint16
		value string
	}{{"ascii", 0, "FEEDER"}, {"uint", 2, "450"}, {"int", 1, "-128"}, {"float32", 4, "1.5"}, {"float64", 8, "-1e300"}, {"7", 0, "x"}} {
		f.Add(c.typ, c.size, c.value)
	}
	f.Fuzz(func(t *testing.T, typ string, size uint16, value string) {
		dt, err := ParseDataType(typ)
		if err != nil {
			return
		}
		v, err := ParseValue(dt, size, value)
		if err != nil {
			return
		}
		if len(v.Data) != v.DataLen() || v.Number != 1 {
			t.Fatalf("%s %d %q: %+v", typ, size, value, v)
		}
		// What is accepted encodes in a generic write.
		if _, err := iec103.PackGenericData(1, []iec103.DataSet{{GIN: iec103.GIN{Group: 1, Entry: 1}, KOD: 1, Value: v}}); err != nil {
			t.Fatalf("%s %d %q: %v", typ, size, value, err)
		}
	})
}

func FuzzParseTime(f *testing.F) {
	for _, s := range []string{"now", "", "2026-01-31T12:00:00Z", "2026-01-31T12:00:00.123456789+05:30", "1999-12-31T23:59:59Z"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		tm, err := ParseTime(s)
		if err == nil && !tm.IsZero() && (tm.Year() < 2000 || tm.Year() > 2099) {
			t.Fatalf("%q: year %d", s, tm.Year())
		}
	})
}

func FuzzParseDCO(f *testing.F) {
	for _, s := range []string{"on", "off", "1", "2", "3", ""} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		d, err := ParseDCO(s)
		if err == nil && d != iec103.DPIOn && d != iec103.DPIOff {
			t.Fatalf("%q: %d", s, d)
		}
	})
}

// The line flags: URL, addresses and serial settings, as CheckConn takes them.
func FuzzCheckConn(f *testing.F) {
	f.Add("tcp://127.0.0.1:2401", "", uint16(0), uint16(1), uint16(0), "even", uint32(9600))
	f.Add("serial:///dev/ttyUSB0", "", uint16(0), uint16(254), uint16(255), "none", uint32(19200))
	f.Add("", "10.0.0.1", uint16(2401), uint16(255), uint16(0), "odd", uint32(0))
	f.Add("tcp://[::1]:0", "", uint16(0), uint16(1), uint16(1), "mark", uint32(1))
	f.Fuzz(func(t *testing.T, url, ip string, port, la, ca uint16, parity string, baud uint32) {
		c := config.ConnConfig{URL: url, IP: ip, Port: port}
		c.LinkAddress, c.CommonAddress, c.Parity, c.Baud = la, ca, parity, baud
		tg, err := CheckConn(&c)
		if err != nil {
			return
		}
		if la > 254 || ca > 255 {
			t.Fatalf("addresses %d/%d accepted", la, ca)
		}
		if !tg.Serial() && tg.Port == 0 {
			t.Fatalf("port 0 accepted: %+v", tg)
		}
	})
}
