// SPDX-License-Identifier: MIT

package iec

import (
	"strings"
	"testing"
	"time"

	"github.com/otfabric/go-iec60870-5/asdu"
	"github.com/otfabric/go-iec60870-5/iec101"
	"github.com/otfabric/iec60870ctl/internal/iec101ctl/config"
	"github.com/otfabric/iec60870ctl/internal/iec101ctl/types"
	"github.com/otfabric/iec60870ctl/internal/line"
)

func FuzzParseRanges(f *testing.F) {
	for _, s := range []string{"", "100,200-203", " 5, 0x10-18 ,,7 ", "9-5", "0xffffffff", "4294967296", "-", "1-", "a", "0b11", "0o7", "1_0"} {
		f.Add(s, uint32(MaxIOA))
	}
	f.Add("1-5", uint32(3))
	f.Fuzz(func(t *testing.T, s string, max uint32) {
		r, err := ParseRanges(s, "number", max)
		if err != nil {
			return
		}
		for _, x := range r {
			if x.Lo > x.Hi || x.Hi > max || !r.Contains(x.Lo) || !r.Contains(x.Hi) {
				t.Fatalf("ParseRanges(%q, %d): range %+v", s, max, x)
			}
		}
	})
}

// --ioa is bounded by the field size and by a list length.
func FuzzParseIOAs(f *testing.F) {
	for _, s := range []string{"", "1-200000", "1,2,3", "0-65535", "65536", "255,256"} {
		f.Add(s, uint8(2))
	}
	f.Fuzz(func(t *testing.T, s string, size uint8) {
		max := MaxIOAFor(int(size%3) + 1)
		r, err := ParseIOAs(s, max)
		if err != nil {
			return
		}
		if n := r.Count(); n > maxListed {
			t.Fatalf("ParseIOAs(%q): %d addresses", s, n)
		}
		r.Each(func(n uint32) bool {
			if n > max {
				t.Fatalf("ParseIOAs(%q, %d): address %d", s, max, n)
			}
			return true
		})
	})
}

// --type of interrogate and monitor: what parses is a number 1-255 or a known name.
func FuzzParseTypes(f *testing.F) {
	for _, s := range []string{"", "M_SP_NA_1", "m_sp_na_1,C_SC_NA_1", "1", "0", "255", "256", "nope", ",,", " 1 , 2 "} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		set, err := ParseTypes(s)
		if err != nil {
			return
		}
		if set[0] {
			t.Fatalf("ParseTypes(%q) holds type 0", s)
		}
	})
}

// --type and --ioa together: a filter that parses selects only what it names.
func FuzzNewFilter(f *testing.F) {
	f.Add("M_SP_NA_1,13", "100,200-203", uint8(1), uint32(100))
	f.Add("", "", uint8(13), uint32(0))
	f.Add("nope", "1-", uint8(0), uint32(1))
	f.Fuzz(func(t *testing.T, typeList, ioaList string, typ uint8, ioa uint32) {
		flt, err := NewFilter(typeList, ioaList, MaxIOA)
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
// object of an IEC 101 command type at the address given, which encodes with the field
// sizes it was checked against.
func FuzzParseCommand(f *testing.F) {
	for _, c := range [][3]string{
		{"single", "on", "short"}, {"double", "off", "persistent"}, {"step", "higher", "31"}, {"C_RC_TA_1", "lower", ""},
		{"setpoint-normalized", "-1", "127"}, {"setpoint-normalized", "0.99999", ""}, {"setpoint-scaled", "-32768", "0"},
		{"setpoint-float", "3.4e38", "none"}, {"setpoint-float", "NaN", ""}, {"bitstring", "0xFFFFFFFF", ""},
		{"bitstring", "4294967296", ""}, {"C_SC_NA_1", "1", "1"}, {"single", "maybe", ""}, {"step", "up", "32"},
	} {
		f.Add(c[0], c[1], c[2], uint32(5000), uint8(2))
	}
	f.Fuzz(func(t *testing.T, typ, value, qualifier string, ioa uint32, size uint8) {
		ioaSize := int(size%3) + 1
		max := MaxIOAFor(ioaSize)
		spec, err := parseCommand(&config.CommandConfig{Type: typ, Value: value, Qualifier: qualifier, IOA: ioa}, max)
		if err != nil {
			return
		}
		if ioa == 0 || ioa > max {
			t.Fatalf("command at IOA %d accepted with a %d-octet address", ioa, ioaSize)
		}
		p := iec101.DefaultParams().ASDU
		p.IOASize = ioaSize
		for _, sel := range []bool{false, true} {
			obj := spec.object(sel)
			a := asdu.New(asdu.CauseActivation, 1, obj)
			if obj.Address() != asdu.IOA(ioa) || a.Type != spec.base || !iec101.Defined101(a.Type) {
				t.Fatalf("%s %q: %s at %d", typ, value, a.Type, obj.Address())
			}
			if _, err := a.Encode(p); err != nil {
				t.Fatalf("%s %q %q: the command does not encode: %v", typ, value, qualifier, err)
			}
		}
	})
}

// --ca of monitor: a station address within the field, or none.
func FuzzMonitorCA(f *testing.F) {
	for _, s := range []string{"", "1", "254", "255", "65534", "65535", "0", "-1", " 7 ", "0x10", "99999"} {
		f.Add(s, uint8(1))
	}
	f.Fuzz(func(t *testing.T, s string, size uint8) {
		sz := int(size%2) + 1
		bc := asdu.CommonAddr(BroadcastCA(sz))
		ca, set, err := monitorCA(s, sz)
		if err != nil {
			return
		}
		if set && (ca == 0 || ca >= bc) || !set && ca != bc {
			t.Fatalf("monitorCA(%q, %d) = %d, %v", s, sz, ca, set)
		}
	})
}

// --time of `client clock`: what parses fits a CP56Time2a and decodes back to the same
// instant without the invalid flag.
func FuzzParseClockTime(f *testing.F) {
	for _, s := range []string{"now", "", "2026-01-31T12:00:00Z", "2069-12-31T23:59:59.999+01:00", "1970-01-01T00:00:00Z", "2126-01-31T12:00:00Z", "yesterday", "2026-02-30T00:00:00Z"} {
		f.Add(s)
	}
	now := time.Date(2026, 1, 31, 12, 0, 0, 0, time.UTC)
	p := iec101.DefaultParams().ASDU
	f.Fuzz(func(t *testing.T, s string) {
		at, err := parseClockTime(s, now, time.UTC)
		if err != nil {
			return
		}
		raw, err := asdu.New(asdu.CauseActivation, 1, asdu.ClockSync{Time: asdu.At(at)}).Encode(p)
		if err != nil {
			t.Fatalf("--time %q: %v", s, err)
		}
		back, err := asdu.Decode(raw, p)
		if err != nil {
			t.Fatalf("--time %q: %v", s, err)
		}
		got := back.First().(asdu.ClockSync).Time
		if got.Invalid || !got.Equal(at.Truncate(time.Millisecond)) {
			t.Fatalf("--time %q: sent %s, the station reads %s", s, at, got.Time)
		}
	})
}

// --set of `client delay`: auto, nothing, or a delay C_CD_NA_1 carries.
func FuzzParseSetDelay(f *testing.F) {
	for _, s := range []string{"", "auto", "AUTO", "0", "59999", "60000", "-1", "0x10", " 12 ", "1e3"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		measure, send, d, err := ParseSetDelay(s)
		if err != nil {
			return
		}
		if !measure && !send || d > MaxDelay || (!measure && strings.TrimSpace(s) == "") {
			t.Fatalf("ParseSetDelay(%q) = %v %v %d", s, measure, send, d)
		}
	})
}

// --remote-link-address: an address that fits the field and is not its broadcast address.
func FuzzParseLinkAddr(f *testing.F) {
	for _, s := range []string{"", "0", "1", "254", "255", "65534", "65535", "65536", "0xff", "-1", "x"} {
		f.Add(s, uint8(1))
	}
	f.Fuzz(func(t *testing.T, s string, size uint8) {
		sz := int(size % 3)
		a, err := ParseLinkAddr(s, sz, 9)
		if err != nil {
			return
		}
		if sz == 1 && a >= 0xFF || sz == 2 && a == 0xFFFF {
			t.Fatalf("ParseLinkAddr(%q, %d) = %d", s, sz, a)
		}
	})
}

// The link flags together: what LinkParams accepts, go-iec60870-5 accepts too.
func FuzzLinkParams(f *testing.F) {
	f.Add("unbalanced", uint16(1), "", uint8(1), uint8(1), uint8(1), uint8(2))
	f.Add("balanced", uint16(0), "7", uint8(0), uint8(2), uint8(2), uint8(3))
	f.Add("half", uint16(300), "65535", uint8(2), uint8(0), uint8(3), uint8(4))
	f.Fuzz(func(t *testing.T, mode string, addr uint16, remote string, as, cot, ca, ioa uint8) {
		l, err := LinkParams(config.LinkConfig{Mode: mode, LinkAddr: addr, RemoteLinkAddr: remote, LinkAddrSize: as, CauseSize: cot, CASize: ca, IOASize: ioa})
		if err != nil {
			return
		}
		if _, err := l.Params.Normalize(); err != nil {
			t.Fatalf("accepted parameters the library refuses: %v", err)
		}
		if max := l.Params.Link.FT12.MaxAddr(); l.Addr > max || l.Remote > max {
			t.Fatalf("addresses %d/%d do not fit %d", l.Addr, l.Remote, max)
		}
	})
}

func FuzzExpandHosts(f *testing.F) {
	for _, s := range []string{"192.168.1.0/30", "10.0.0.250-255", "10.0.0.0-256", "10.0.0.5-4", "host", "::1", "10.0.0.0/8", "1.2.3.4/32", "1.2.3.4/31", ", ,", "a-b.c.d.e"} {
		f.Add(s, 16)
	}
	f.Fuzz(func(t *testing.T, spec string, limit int) {
		limit %= 4097
		if limit < 0 {
			limit = -limit
		}
		if limit == 0 {
			limit = 4096
		}
		hosts, err := ExpandHosts(strings.Split(spec, ","), limit)
		if err != nil {
			return
		}
		seen := map[string]bool{}
		for _, h := range hosts {
			if h == "" || seen[h] {
				t.Fatalf("ExpandHosts(%q): host %q empty or twice", spec, h)
			}
			seen[h] = true
		}
		if len(hosts) > limit {
			t.Fatalf("ExpandHosts(%q, %d): %d hosts", spec, limit, len(hosts))
		}
	})
}

func FuzzParsePorts(f *testing.F) {
	for _, s := range []string{"2401", "2401,4001", "0", "65536", "-1", " ,2401, ", "0x10", ""} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		ports, err := parsePorts(strings.Split(s, ","))
		if err != nil {
			return
		}
		for _, p := range ports {
			if p == 0 {
				t.Fatalf("parsePorts(%q): port 0", s)
			}
		}
	})
}

// --url and the serial line flags: a target that resolves is a TCP address with a port, or
// a serial device; settings that check give a go-serial configuration.
func FuzzResolveLine(f *testing.F) {
	for _, s := range []string{"tcp://10.0.0.1:2401", "serial:///dev/ttyUSB0", "serial://COM3", "tcp://h", "udp://h", "", "serial://", "tcp://h:0"} {
		f.Add(s, uint32(9600), uint8(8), "even", uint8(1))
	}
	f.Fuzz(func(t *testing.T, url string, baud uint32, bits uint8, parity string, stop uint8) {
		cfg := config.ConnConfig{LinkConfig: config.DefaultLink(), URL: url, LineConfig: config.LineConfig{Baud: baud, DataBits: bits, Parity: parity, StopBits: stop}}
		if err := Validate(&cfg); err != nil {
			return
		}
		tg, err := line.Resolve(url, "", 0, "station")
		if err != nil || (!tg.Serial() && tg.Port == 0) {
			t.Fatalf("Validate accepted %q: %v", url, err)
		}
		if tg.Serial() {
			if _, err := line.SerialConfig(tg.Device, Settings(cfg.LineConfig)); err != nil {
				t.Fatalf("Validate accepted settings go-serial refuses: %v", err)
			}
		}
	})
}
