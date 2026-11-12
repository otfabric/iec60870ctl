// SPDX-License-Identifier: MIT

package iec

import (
	"strings"
	"testing"
	"time"

	"github.com/otfabric/go-iec104/asdu"
	"github.com/otfabric/iec104ctl/internal/config"
)

// The value of a bitstring command is 32 bits: the largest is taken as it is, anything
// above is refused before a command is built, never truncated.
func TestBitstringValueLimit(t *testing.T) {
	cfg := func(v string) *config.CommandConfig {
		return &config.CommandConfig{Type: "bitstring", IOA: 5006, Value: v, Mode: ModeExecute}
	}
	for v, want := range map[string]uint32{"4294967295": 1<<32 - 1, "0xFFFFFFFF": 1<<32 - 1, "0": 0, "0x10": 16} {
		spec, err := parseCommand(cfg(v))
		if err != nil {
			t.Fatalf("--value %s: %v", v, err)
		}
		if spec.value != want {
			t.Errorf("--value %s: shown as %v", v, spec.value)
		}
		if o := spec.object(false).(asdu.BitstringCommand); o.Value != want {
			t.Errorf("--value %s: sent as %d", v, o.Value)
		}
	}
	for _, v := range []string{"4294967296", "0x100000000", "18446744073709551615", "99999999999999999999", "-1", "1.5"} {
		if _, err := parseCommand(cfg(v)); err == nil || !strings.Contains(err.Error(), "0 to 4294967295") {
			t.Errorf("--value %s: %v, want it refused", v, err)
		}
	}
}

// A range on the last octet ends at 255 at the most and does not run backwards. 255 is
// reached without wrapping to 0.
func TestHostRangeLimits(t *testing.T) {
	hosts, err := ExpandHosts([]string{"10.0.0.250-255"}, 0)
	if err != nil || len(hosts) != 6 || hosts[0] != "10.0.0.250" || hosts[5] != "10.0.0.255" {
		t.Fatalf("10.0.0.250-255: %v, %v", hosts, err)
	}
	if hosts, err := ExpandHosts([]string{"10.0.0.0-255"}, 0); err != nil || len(hosts) != 256 || hosts[255] != "10.0.0.255" {
		t.Fatalf("10.0.0.0-255: %d hosts, %v", len(hosts), err)
	}
	if hosts, err := ExpandHosts([]string{"10.0.0.255-255"}, 0); err != nil || len(hosts) != 1 || hosts[0] != "10.0.0.255" {
		t.Fatalf("10.0.0.255-255: %v, %v", hosts, err)
	}
	for _, spec := range []string{"10.0.0.0-256", "10.0.0.5-300", "10.0.0.5-65536", "10.0.0.5-4", "10.0.0.255-0", "10.0.0.5--1", "10.0.0.5-", "10.0.0.5-0x10"} {
		if hosts, err := ExpandHosts([]string{spec}, 0); err == nil {
			t.Errorf("%s: %d hosts, want it refused", spec, len(hosts))
		}
	}
}

// The time of a clock synchronization must fit the two-digit year of CP56Time2a, in the
// time zone on the wire.
func TestClockTimeWindow(t *testing.T) {
	brussels, err := time.LoadLocation("Europe/Brussels")
	if err != nil {
		t.Skipf("no time zone database: %v", err)
	}
	now := time.Date(2026, 1, 31, 12, 0, 0, 0, time.UTC)
	for _, c := range []struct {
		s   string
		now time.Time
		loc *time.Location
		ok  bool
	}{
		{"now", now, nil, true},
		{"", now, time.UTC, true},
		{"1970-01-01T00:00:00Z", now, time.UTC, true},
		{"2069-12-31T23:59:59.999Z", now, time.UTC, true},
		{"1969-12-31T23:59:59.999Z", now, time.UTC, false},
		{"2070-01-01T00:00:00Z", now, time.UTC, false},
		{"2126-01-31T12:00:00Z", now, time.UTC, false},
		{"0001-01-01T00:00:00Z", now, time.UTC, false},
		{"9999-12-31T23:59:59Z", now, time.UTC, false},
		{"2069-12-31T23:30:00Z", now, brussels, false}, // 2070 in Brussels
		{"1970-01-01T00:30:00+01:00", now, time.UTC, false},
		{"1970-01-01T00:30:00+01:00", now, brussels, true},
		{"now", time.Date(2070, 1, 1, 0, 0, 0, 0, time.UTC), time.UTC, false},
		{"yesterday", now, time.UTC, false},
	} {
		_, err := parseClockTime(c.s, c.now, c.loc)
		if (err == nil) != c.ok {
			t.Errorf("parseClockTime(%q, %s): %v, want ok %v", c.s, c.loc, err, c.ok)
		}
	}
	// CheckClock applies the window in the zone of --timezone.
	cfg := &config.ClockConfig{StationConfig: config.StationConfig{ConnConfig: config.ConnConfig{IP: "127.0.0.1",
		LinkConfig: config.LinkConfig{TimeZone: "Europe/Brussels"}}, CommonAddr: 1}, Time: "2069-12-31T23:30:00Z"}
	if err := CheckClock(cfg); err == nil {
		t.Error("CheckClock took a time that is 2070 on the wire")
	}
	cfg.TimeZone = "Mars/Olympus"
	if err := CheckClock(cfg); err == nil {
		t.Error("CheckClock took an unknown time zone")
	}
}
