// SPDX-License-Identifier: MIT

package meter

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/otfabric/go-iec60870-5/iec102"
	"github.com/otfabric/go-iec60870-5/iec102/client"
	"github.com/otfabric/go-iec60870-5/iec102/ree"
	"github.com/otfabric/iec60870ctl/internal/iec102ctl/config"
)

func TestLocation(t *testing.T) {
	for _, s := range []string{"", "UTC", "local", "Europe/Madrid"} {
		if _, err := Location(s); err != nil {
			t.Errorf("%q: %v", s, err)
		}
	}
	if _, err := Location("Mars/Base"); err == nil {
		t.Error("an unknown zone was accepted")
	}
}

func TestParseTimes(t *testing.T) {
	madrid, _ := time.LoadLocation("Europe/Madrid")
	for in, want := range map[string]string{
		"2026-01-31T13:05": "2026-01-31T13:05", "2026-01-31 13:05": "2026-01-31T13:05", "2026-01-31": "2026-01-31T00:00",
		"2026-01-31T13:05:00Z": "2026-01-31T13:05",
	} {
		a, err := ParseTimeA(in, time.UTC)
		if err != nil || TextA(a) != want || a.SummerTime {
			t.Errorf("ParseTimeA(%q) = %s, %v", in, TextA(a), err)
		}
	}
	a, err := ParseTimeA("2026-07-01T12:00", madrid)
	if err != nil || !a.SummerTime {
		t.Errorf("summer time in Madrid: %+v %v", a, err)
	}
	for _, in := range []string{"", "yesterday", "1999-12-31", "2100-01-01T00:00", "2026-13-01"} {
		if _, err := ParseTimeA(in, time.UTC); err == nil {
			t.Errorf("ParseTimeA(%q) was accepted", in)
		}
	}
	now := time.Date(2026, 2, 3, 4, 5, 6, 789e6, time.UTC)
	for in, want := range map[string]string{
		"now": "2026-02-03T04:05:06.789", "": "2026-02-03T04:05:06.789", "2026-02-03T04:05:06.789": "2026-02-03T04:05:06.789",
		"2026-02-03T04:05:06": "2026-02-03T04:05:06.000", "2026-02-03T04:05": "2026-02-03T04:05:00.000",
		"2026-02-03T05:05:06.789+01:00": "2026-02-03T04:05:06.789",
	} {
		b, err := ParseTimeB(in, time.UTC, now)
		if err != nil || TextB(b) != want {
			t.Errorf("ParseTimeB(%q) = %s, %v", in, TextB(b), err)
		}
	}
	for _, in := range []string{"later", "1999-01-01T00:00:00", "2026-02-30T00:00"} {
		if _, err := ParseTimeB(in, time.UTC, now); err == nil {
			t.Errorf("ParseTimeB(%q) was accepted", in)
		}
	}
	tb := iec102.TimeBOf(now, time.UTC)
	tb.Invalid, tb.SummerTime = true, true
	if got := TimeOfB(tb); got.Time != "2026-02-03T04:05:06.789" || !got.SU || !got.Invalid {
		t.Errorf("TimeOfB %+v", got)
	}
	if got := TimeOfA(tb.TimeA); got.Time != "2026-02-03T04:05" || !got.SU || !got.Invalid {
		t.Errorf("TimeOfA %+v", got)
	}
}

func TestParseTypeAndCause(t *testing.T) {
	for in, want := range map[string]iec102.TypeID{
		"C_RM_NA_2": ree.C_RM_NA_2, "c_ci_nt_2": iec102.C_CI_NT_2, "122": iec102.C_CI_NT_2, "0x7a": 122, "255": 255,
	} {
		if got, err := ParseTypeID(in); err != nil || got != want {
			t.Errorf("ParseTypeID(%q) = %d, %v", in, got, err)
		}
	}
	for _, in := range []string{"", "256", "C_XX", "-1"} {
		if _, err := ParseTypeID(in); err == nil {
			t.Errorf("ParseTypeID(%q) was accepted", in)
		}
	}
	if TypeName(ree.C_AC_NA_2) != "C_AC_NA_2" || TypeName(iec102.M_IT_TK_2) != "M_IT_TK_2" {
		t.Error("type names")
	}
	for in, want := range map[string]iec102.Cause{"": 6, "activation": 6, "act": 6, "request": 5, "req": 5, "deactivation": 8, "13": 13, "0x3f": 63} {
		if got, err := ParseCause(in); err != nil || got != want {
			t.Errorf("ParseCause(%q) = %d, %v", in, got, err)
		}
	}
	for _, in := range []string{"64", "x", "-1"} {
		if _, err := ParseCause(in); err == nil {
			t.Errorf("ParseCause(%q) was accepted", in)
		}
	}
	if CauseName(iec102.CauseNoPeriod|iec102.CauseNegative) != "integration period not available" || CauseName(60) != "unknown" {
		t.Errorf("cause names: %q %q", CauseName(iec102.CauseNoPeriod), CauseName(60))
	}
}

func TestParseHexKeyUntil(t *testing.T) {
	for in, want := range map[string]string{"": "", "01 02": "\x01\x02", "0x0a0B": "\x0a\x0b", "01:02-03,04": "\x01\x02\x03\x04"} {
		if got, err := ParseHex(in); err != nil || string(got) != want {
			t.Errorf("ParseHex(%q) = %x, %v", in, got, err)
		}
	}
	for _, in := range []string{"0", "zz", "0x1"} {
		if _, err := ParseHex(in); err == nil {
			t.Errorf("ParseHex(%q) was accepted", in)
		}
	}
	for in, want := range map[string]uint32{"7": 7, "0xFFFFFFFF": 0xFFFFFFFF, " 12 ": 12} {
		if got, err := ParseAccessKey(in); err != nil || got != want {
			t.Errorf("ParseAccessKey(%q) = %d, %v", in, got, err)
		}
	}
	for _, in := range []string{"", "-1", "4294967296", "key"} {
		if _, err := ParseAccessKey(in); err == nil {
			t.Errorf("ParseAccessKey(%q) was accepted", in)
		}
	}
	for _, c := range []struct {
		in    string
		cause iec102.Cause
		want  client.Until
		name  string
	}{
		{"", iec102.CauseActivation, client.UntilTermination, "termination"},
		{"auto", iec102.CauseRequest, client.UntilReply, "reply"},
		{"Confirmation", 5, client.UntilConfirmation, "confirmation"},
		{"reply", 6, client.UntilReply, "reply"},
		{"termination", 5, client.UntilTermination, "termination"},
	} {
		if got, name, err := ParseUntil(c.in, c.cause); err != nil || got != c.want || name != c.name {
			t.Errorf("ParseUntil(%q) = %d %s %v", c.in, got, name, err)
		}
	}
	if _, _, err := ParseUntil("never", 6); err == nil {
		t.Error("--until never was accepted")
	}
	if len(UntilValues()) != 4 || len(KindValues()) != 6 {
		t.Error("completion values")
	}
}

func TestQualityAndKind(t *testing.T) {
	for _, c := range []struct {
		q    iec102.Quality
		ree  bool
		want string
	}{
		{0, false, "good"}, {0x80, false, "IV"}, {0xE3, false, "IV|CA|CY|SEQ=3"}, {0xFF, true, "IV|CA|CY|VH|MP|IN|AL|RES"}, {0x10, true, "VH"},
	} {
		if got := QualityFlags(c.q, c.ree); got != c.want {
			t.Errorf("QualityFlags(%#x, %t) = %s", c.q, c.ree, got)
		}
	}
	for f, want := range map[iec102.Family]string{
		iec102.FamilyAccounting: "accounting", iec102.FamilyResetAccounting: "reset-accounting", iec102.FamilyOperational: "operational",
		iec102.FamilyResetOperational: "reset-operational", iec102.FamilyNone: "none",
	} {
		if KindName(f) != want {
			t.Errorf("KindName(%d)", f)
		}
	}
}

func TestTotalsRead(t *testing.T) {
	base := func() config.TotalsConfig {
		return config.TotalsConfig{ConnConfig: config.Defaults(), Record: 11, First: 1, Last: 8}
	}
	for _, c := range []struct {
		edit func(*config.TotalsConfig)
		want iec102.TypeID
	}{
		{func(c *config.TotalsConfig) { c.From, c.To = "2026-01-01", "2026-01-02" }, iec102.C_CI_NT_2},
		{func(c *config.TotalsConfig) { c.Kind, c.From, c.To = "incremental", "2026-01-01", "2026-01-02" }, iec102.C_CI_NU_2},
		{func(c *config.TotalsConfig) { c.Kind, c.Oldest = "accounting", true }, iec102.C_CI_NA_2},
		{func(c *config.TotalsConfig) { c.Kind, c.Oldest, c.AddressRange = "reset-accounting", true, true }, iec102.C_CI_NF_2},
		{func(c *config.TotalsConfig) { c.Kind, c.Period = "", "2026-01-01T01:00" }, iec102.C_CI_NL_2},
		{func(c *config.TotalsConfig) { c.Kind, c.Period, c.AddressRange = "absolute", "2026-01-01T01:00", true }, iec102.C_CI_NM_2},
		{func(c *config.TotalsConfig) { c.Type = "C_CI_NA_2" }, iec102.C_CI_NA_2},
		{func(c *config.TotalsConfig) { c.Type, c.Period = "110", "2026-01-01" }, iec102.C_CI_NG_2},
		{func(c *config.TotalsConfig) { c.Type, c.From, c.To = "C_CI_NR_2", "2026-01-01", "2026-01-01" }, iec102.C_CI_NR_2},
	} {
		cfg := base()
		c.edit(&cfg)
		q, err := TotalsRead(&cfg)
		if err != nil || q.T != c.want {
			t.Errorf("%+v: %s, %v", cfg, TypeName(q.T), err)
		}
	}
	for name, edit := range map[string]func(*config.TotalsConfig){
		"nothing selected": func(*config.TotalsConfig) {},
		"two selections":   func(c *config.TotalsConfig) { c.Oldest, c.Period = true, "2026-01-01" },
		"kind":             func(c *config.TotalsConfig) { c.Kind, c.Oldest = "energy", true },
		"first > last":     func(c *config.TotalsConfig) { c.First, c.Last, c.Oldest = 9, 1, true },
		"from only":        func(c *config.TotalsConfig) { c.From = "2026-01-01" },
		"bad from":         func(c *config.TotalsConfig) { c.From, c.To = "x", "2026-01-01" },
		"bad to":           func(c *config.TotalsConfig) { c.From, c.To = "2026-01-01", "x" },
		"from > to":        func(c *config.TotalsConfig) { c.From, c.To = "2026-01-02", "2026-01-01" },
		"bad period":       func(c *config.TotalsConfig) { c.Period = "x" },
		"type":             func(c *config.TotalsConfig) { c.Type = "C_XX" },
		"not a read":       func(c *config.TotalsConfig) { c.Type = "C_TI_NA_2" },
		"type no period":   func(c *config.TotalsConfig) { c.Type = "C_CI_NC_2" },
		"type no range":    func(c *config.TotalsConfig) { c.Type = "C_CI_NU_2" },
		"zone":             func(c *config.TotalsConfig) { c.TimeZone, c.Oldest = "Mars/Base", true },
	} {
		cfg := base()
		edit(&cfg)
		if _, err := TotalsRead(&cfg); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestResolveProfile(t *testing.T) {
	dir := t.TempDir()
	keyFile := filepath.Join(dir, "key")
	if err := os.WriteFile(keyFile, []byte(" 0x10\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	empty := filepath.Join(dir, "empty")
	_ = os.WriteFile(empty, nil, 0o600)
	big := filepath.Join(dir, "big")
	_ = os.WriteFile(big, []byte(strings.Repeat("1", 300)), 0o600)
	cfg := config.Defaults()
	cfg.AccessKeyFile = keyFile
	if p, err := ResolveProfile(&cfg); err != nil || p.Key != 16 || !p.HasKey || !p.REE {
		t.Errorf("key file: %+v %v", p, err)
	}
	cfg = config.Defaults()
	cfg.Profile = "REE"
	if p, err := ResolveProfile(&cfg); err != nil || !p.REE || p.HasKey {
		t.Errorf("ree: %+v %v", p, err)
	}
	for name, edit := range map[string]func(*config.ConnConfig){
		"profile":    func(c *config.ConnConfig) { c.Profile = "dlms" },
		"both":       func(c *config.ConnConfig) { c.AccessKey, c.AccessKeyFile = "1", keyFile },
		"missing":    func(c *config.ConnConfig) { c.AccessKeyFile = filepath.Join(dir, "none") },
		"empty file": func(c *config.ConnConfig) { c.AccessKeyFile = empty },
		"big file":   func(c *config.ConnConfig) { c.AccessKeyFile = big },
		"bad key":    func(c *config.ConnConfig) { c.AccessKey = "secret" },
	} {
		c := config.Defaults()
		edit(&c)
		if _, err := ResolveProfile(&c); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestValidate(t *testing.T) {
	for name, edit := range map[string]func(*config.ConnConfig){
		"no line":        func(*config.ConnConfig) {},
		"url and ip":     func(c *config.ConnConfig) { c.URL, c.IP = "tcp://h", "h" },
		"parity":         func(c *config.ConnConfig) { c.URL, c.Parity = "serial:///dev/x", "mark" },
		"link size":      func(c *config.ConnConfig) { c.IP, c.LinkAddressSize = "h", 3 },
		"link address 1": func(c *config.ConnConfig) { c.IP, c.LinkAddressSize, c.LinkAddress = "h", 1, 255 },
		"link address 2": func(c *config.ConnConfig) { c.IP, c.LinkAddress = "h", 65535 },
		"link address 0": func(c *config.ConnConfig) { c.IP, c.LinkAddressSize, c.LinkAddress = "h", 0, 1 },
		"ca size":        func(c *config.ConnConfig) { c.IP, c.CASize = "h", 3 },
		"ca":             func(c *config.ConnConfig) { c.IP, c.CASize, c.CommonAddress = "h", 1, 256 },
		"zone":           func(c *config.ConnConfig) { c.IP, c.TimeZone = "h", "Mars/Base" },
		"profile":        func(c *config.ConnConfig) { c.IP, c.Profile = "h", "x" },
	} {
		c := config.Defaults()
		edit(&c)
		if err := Validate(&c); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	c := config.Defaults()
	c.URL, c.LinkAddressSize, c.LinkAddress = "serial:///dev/ttyS0", 0, 0
	if err := Validate(&c); err != nil {
		t.Errorf("serial without link address: %v", err)
	}
	if Timeout(0) != DefaultTimeout || Timeout(5) != 5*time.Millisecond {
		t.Error("timeouts")
	}
}

// Times come from flags and model files: what ParseTimeA accepts is a time of 2000-2099 that
// writes back to the same minute.
func FuzzParseTimeA(f *testing.F) {
	for _, s := range []string{"2026-01-31T13:05", "2026-01-31", "2026-01-31 13:05", "2026-01-31T13:05:00+02:00", "1999-12-31", "2026-02-30", "", "x"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		a, err := ParseTimeA(s, time.UTC)
		if err != nil {
			return
		}
		tt, ok := a.Time(time.UTC)
		if !ok || tt.Year() < 2000 || tt.Year() > 2099 || a.Invalid {
			t.Fatalf("ParseTimeA(%q) = %+v", s, a)
		}
		b, err := ParseTimeA(TextA(a), time.UTC)
		if err != nil || TextA(b) != TextA(a) {
			t.Fatalf("ParseTimeA(%q) does not write back: %s, %v", s, TextA(a), err)
		}
	})
}

// What ParseTimeB accepts is a valid time b that writes back unchanged.
func FuzzParseTimeB(f *testing.F) {
	for _, s := range []string{"now", "2026-02-03T04:05:06.789", "2026-02-03T04:05:06", "2026-02-03 04:05:06.7", "2026-02-03T04:05:06.789Z", "x"} {
		f.Add(s)
	}
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	f.Fuzz(func(t *testing.T, s string) {
		b, err := ParseTimeB(s, time.UTC, now)
		if err != nil {
			return
		}
		if _, ok := b.Time(time.UTC); !ok || b.Invalid {
			t.Fatalf("ParseTimeB(%q) = %+v", s, b)
		}
		c, err := ParseTimeB(TextB(b), time.UTC, now)
		if err != nil || TextB(c) != TextB(b) {
			t.Fatalf("ParseTimeB(%q) does not write back: %s, %v", s, TextB(b), err)
		}
	})
}

// --type, --cause and --access-key take what an operator types.
func FuzzParseTypeCauseKey(f *testing.F) {
	for _, s := range []string{"C_RM_NA_2", "122", "0x7f", "256", "activation", "63", "64", "7", "0xffffffff", "-1", ""} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		if tid, err := ParseTypeID(s); err == nil {
			if back, err := ParseTypeID(TypeName(tid)); err != nil || back != tid {
				t.Fatalf("type %q: %d does not name back (%s)", s, tid, TypeName(tid))
			}
		}
		if c, err := ParseCause(s); err == nil && c > 63 {
			t.Fatalf("cause %q = %d", s, c)
		}
		_, _ = ParseAccessKey(s)
		_, _, _ = ParseUntil(s, 6)
	})
}

// --data takes hex as an operator writes it: what ParseHex accepts encodes back to itself.
func FuzzParseHex(f *testing.F) {
	for _, s := range []string{"01 02", "0x0a0b", "01:02", "zz", "0", ""} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		b, err := ParseHex(s)
		if err != nil {
			return
		}
		c, err := ParseHex(strings.ToUpper(string(hexOf(b))))
		if err != nil || string(c) != string(b) {
			t.Fatalf("ParseHex(%q) = %x, back %x %v", s, b, c, err)
		}
	})
}

func hexOf(b []byte) []byte {
	const digits = "0123456789abcdef"
	out := make([]byte, 0, 2*len(b))
	for _, x := range b {
		out = append(out, digits[x>>4], digits[x&15])
	}
	return out
}
