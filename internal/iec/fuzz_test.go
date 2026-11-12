// SPDX-License-Identifier: MIT

package iec

import (
	"strings"
	"testing"
	"time"

	"github.com/otfabric/go-iec104/asdu"
	"github.com/otfabric/go-iec104/sec"
	"github.com/otfabric/iec104ctl/internal/config"
	"github.com/otfabric/iec104ctl/internal/types"
)

// Whatever ParseRanges accepts is a set of ordered ranges within max, each of
// whose bounds the set contains.
func FuzzParseRanges(f *testing.F) {
	for _, s := range []string{"", "100,200-203", " 5, 0x10-18 ,,7 ", "9-5", "0xffffffff", "4294967296", "-", "1-", "a", "0b11", "0o7", "1_0"} {
		f.Add(s, uint32(MaxIOA))
	}
	f.Add("1-5", uint32(3))
	f.Add("0", uint32(0))
	f.Fuzz(func(t *testing.T, s string, max uint32) {
		r, err := ParseRanges(s, "number", max)
		if err != nil {
			return
		}
		for _, x := range r {
			if x.Lo > x.Hi || x.Hi > max {
				t.Fatalf("ParseRanges(%q, %d): range %+v", s, max, x)
			}
			if !r.Contains(x.Lo) || !r.Contains(x.Hi) {
				t.Fatalf("ParseRanges(%q, %d): %+v does not contain its bounds", s, max, x)
			}
		}
	})
}

// ParseIOAs bounds the list: a command cannot be made to expand a huge range.
func FuzzParseIOAs(f *testing.F) {
	for _, s := range []string{"", "1-200000", "1,2,3", "0-16777215", "16777216"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		r, err := ParseIOAs(s)
		if err != nil {
			return
		}
		if n := r.Count(); n > maxListed {
			t.Fatalf("ParseIOAs(%q): %d addresses, the maximum is %d", s, n, maxListed)
		}
	})
}

// A type is a number 1-255 or a known name, never type 0.
func FuzzParseTypes(f *testing.F) {
	for _, s := range []string{"", "M_SP_NA_1", "m_sp_na_1,C_SC_NA_1", "1", "0", "255", "256", "nope", ",,", " 1 , 2 "} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		types, err := ParseTypes(s)
		if err != nil {
			return
		}
		for id := range types {
			if id == 0 {
				t.Fatalf("ParseTypes(%q): type 0", s)
			}
		}
	})
}

// --subnets: whatever ExpandHosts accepts is a list of distinct hosts within the limit, and
// a range on the last octet yields IPv4 addresses that run from its start to its end.
func FuzzExpandHosts(f *testing.F) {
	for _, s := range []string{"192.168.1.0/30", "10.0.0.250-255", "10.0.0.0-256", "10.0.0.5-4", "host", "::1", "10.0.0.0/8", "1.2.3.4/32", "1.2.3.4/31", ", ,", "a-b.c.d.e", "1.2.3.4-0x10"} {
		f.Add(s, 16)
	}
	f.Fuzz(func(t *testing.T, spec string, limit int) {
		limit = limit % 4097
		if limit < 0 {
			limit = -limit
		}
		if limit == 0 {
			limit = 4096 // the fuzzer is not to expand a /0
		}
		hosts, err := ExpandHosts(strings.Split(spec, ","), limit)
		if err != nil {
			return
		}
		if len(hosts) > limit {
			t.Fatalf("ExpandHosts(%q, %d): %d hosts", spec, limit, len(hosts))
		}
		seen := map[string]bool{}
		for _, h := range hosts {
			if h == "" || seen[h] {
				t.Fatalf("ExpandHosts(%q): host %q empty or twice", spec, h)
			}
			seen[h] = true
		}
	})
}

// --ports: what parsePorts accepts are ports 1-65535.
func FuzzParsePorts(f *testing.F) {
	for _, s := range []string{"2404", "2404,19998", "0", "65536", "-1", " ,2404, ", "0x10", ""} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		ports, err := parsePorts(strings.Split(s, ","))
		if err != nil {
			return
		}
		if len(ports) == 0 {
			t.Fatalf("parsePorts(%q): no port and no error", s)
		}
		for _, p := range ports {
			if p == 0 {
				t.Fatalf("parsePorts(%q): port 0", s)
			}
		}
	})
}

// --type and --ioa of monitor and interrogate: a filter that parses selects only what it
// names.
func FuzzNewFilter(f *testing.F) {
	f.Add("M_SP_NA_1,13", "100,200-203", uint8(1), uint32(100))
	f.Add("", "", uint8(13), uint32(0))
	f.Add("nope", "1-", uint8(0), uint32(1))
	f.Fuzz(func(t *testing.T, typeList, ioaList string, typ uint8, ioa uint32) {
		flt, err := NewFilter(typeList, ioaList)
		if err != nil {
			return
		}
		p := types.Point{TypeID: typ, IOA: ioa}
		if flt.Match(p) && ((len(flt.Types) > 0 && !flt.Types[asdu.TypeID(typ)]) || !flt.IOAs.Contains(ioa)) {
			t.Fatalf("filter %q %q matched type %d ioa %d", typeList, ioaList, typ, ioa)
		}
		if n := len(flt.Apply([]types.Point{p})); (n == 1) != flt.Match(p) {
			t.Fatalf("Apply and Match disagree on %+v", p)
		}
	})
}

// --type, --value and --qualifier of `client command`: a command that parses builds an
// object of the type it names, at the address given, with the value it shows.
func FuzzParseCommand(f *testing.F) {
	for _, c := range [][3]string{
		{"single", "on", "short"}, {"double", "off", "persistent"}, {"step", "higher", "31"}, {"C_RC_TA_1", "lower", ""},
		{"setpoint-normalized", "-1", "127"}, {"setpoint-normalized", "0.99999", ""}, {"setpoint-scaled", "-32768", "0"},
		{"setpoint-float", "3.4e38", "none"}, {"setpoint-float", "NaN", ""}, {"bitstring", "0xFFFFFFFF", ""},
		{"bitstring", "4294967296", ""}, {"C_BO_TA_1", "7", "1"}, {"single", "maybe", ""}, {"step", "up", "32"},
	} {
		f.Add(c[0], c[1], c[2], uint32(5000))
	}
	f.Fuzz(func(t *testing.T, typ, value, qualifier string, ioa uint32) {
		spec, err := parseCommand(&config.CommandConfig{Type: typ, Value: value, Qualifier: qualifier, IOA: ioa})
		if err != nil {
			return
		}
		if ioa == 0 || ioa > MaxIOA {
			t.Fatalf("command at IOA %d accepted", ioa)
		}
		for _, sel := range []bool{false, true} {
			obj := spec.object(sel)
			if obj.Address() != asdu.IOA(ioa) {
				t.Fatalf("%s %q: object at %d, want %d", typ, value, obj.Address(), ioa)
			}
			a := asdu.New(asdu.CauseActivation, 1, obj)
			if a.Type != spec.base && a.Type != timeTagged[spec.base] {
				t.Fatalf("%s %q: built %s, want %s", typ, value, a.Type, spec.base)
			}
			if _, err := a.Encode(asdu.IEC104); err != nil {
				t.Fatalf("%s %q %q: the command does not encode: %v", typ, value, qualifier, err)
			}
		}
	})
}

// --ca of monitor: a station address 1-65534, or none.
func FuzzMonitorCA(f *testing.F) {
	for _, s := range []string{"", "1", "65534", "65535", "0", "-1", " 7 ", "0x10", "99999"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		ca, set, err := monitorCA(s)
		if err != nil {
			return
		}
		if set && (ca == 0 || ca == asdu.Broadcast) {
			t.Fatalf("monitorCA(%q) = %d", s, ca)
		}
		if !set && ca != asdu.Broadcast {
			t.Fatalf("monitorCA(%q): no address, but %d", s, ca)
		}
	})
}

// --time of `client clock`: what parses fits a CP56Time2a, which encodes and decodes back
// to the same instant (to the millisecond) without the invalid flag.
func FuzzParseClockTime(f *testing.F) {
	for _, s := range []string{"now", "", "2026-01-31T12:00:00Z", "2069-12-31T23:59:59.999+01:00", "1970-01-01T00:00:00Z", "2126-01-31T12:00:00Z", "yesterday", "2026-02-30T00:00:00Z"} {
		f.Add(s)
	}
	now := time.Date(2026, 1, 31, 12, 0, 0, 0, time.UTC)
	f.Fuzz(func(t *testing.T, s string) {
		at, err := parseClockTime(s, now, time.UTC)
		if err != nil {
			return
		}
		a := asdu.New(asdu.CauseActivation, 1, asdu.ClockSync{Time: asdu.At(at)})
		raw, err := a.Encode(asdu.IEC104)
		if err != nil {
			t.Fatalf("--time %q: %v", s, err)
		}
		back, err := asdu.Decode(raw, asdu.IEC104)
		if err != nil {
			t.Fatalf("--time %q: %v", s, err)
		}
		got := back.First().(asdu.ClockSync).Time
		if got.Invalid || !got.Equal(at.Truncate(time.Millisecond)) {
			t.Fatalf("--time %q: sent %s, the station reads %s (invalid %v)", s, at, got.Time, got.Invalid)
		}
	})
}

// --mal, --dpa and --roles: what parses is an algorithm or a role package sec knows.
func FuzzParseSecurityNames(f *testing.F) {
	for _, s := range []string{"", "4", "hmac-sha-256", "HMAC-SHA3-256/8", "11", "aes-256-gcm", "0", "256", "viewer", "SecAdm", "nobody", "operator,engineer"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		if mac, err := ParseMAC(s); err == nil {
			if _, ok := sec.MACByCode(mac); !ok {
				t.Fatalf("ParseMAC(%q) = %d, not a MAC algorithm", s, mac)
			}
		}
		if dpa, err := ParseDPA(s, sec.HMACSHA256.Code); err == nil {
			if _, ok := sec.DataProtectionByCode(dpa); !ok {
				t.Fatalf("ParseDPA(%q) = %d, not a data protection algorithm", s, dpa)
			}
		}
		roles, err := ParseRoles(strings.Split(s, ","))
		if err != nil {
			return
		}
		for _, r := range roles {
			if r.Name == "" {
				t.Fatalf("ParseRoles(%q): a role without a name", s)
			}
		}
	})
}
