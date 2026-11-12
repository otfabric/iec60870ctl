// SPDX-License-Identifier: MIT

package iec

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/otfabric/go-iec104/sec"
)

func TestAlgorithmsAndRoles(t *testing.T) {
	for in, want := range map[string]uint8{"": 4, "4": 4, "HMAC-SHA-256": 4, "hmac-sha-256/16": 4, "hmac-sha-256/8": 3, "3": 3, " hmac-sha3-256 ": 8, "7": 7} {
		if got, err := ParseMAC(in); err != nil || got != want {
			t.Errorf("ParseMAC(%q) = %d, %v; want %d", in, got, err, want)
		}
	}
	for _, in := range []string{"11", "aes-256-gcm", "md5", "0", "300"} {
		if _, err := ParseMAC(in); err == nil {
			t.Errorf("ParseMAC(%q) accepted", in)
		}
	}
	for in, want := range map[string]uint8{"": 8, "aes-256-gcm": 11, "AEAD-AES-256-GCM": 11, "11": 11, "hmac-sha-256": 4, "3": 3} {
		if got, err := ParseDPA(in, 8); err != nil || got != want {
			t.Errorf("ParseDPA(%q) = %d, %v; want %d", in, got, err, want)
		}
	}
	if _, err := ParseDPA("99", 4); err == nil || !strings.Contains(err.Error(), "aes-256-gcm") {
		t.Errorf("ParseDPA(99): %v", err)
	}
	if !slices.Contains(MACNames(), "hmac-sha-256") || !slices.Contains(DPANames(), "aes-256-gcm") {
		t.Errorf("names: %v %v", MACNames(), DPANames())
	}

	roles, err := ParseRoles([]string{"viewer", " OPERATOR", ""})
	if err != nil || len(roles) != 2 || roles[0].Name != "VIEWER" || roles[1].Name != "OPERATOR" {
		t.Errorf("ParseRoles: %v, %v", roles, err)
	}
	if roles, err := ParseRoles(nil); err != nil || roles != nil {
		t.Errorf("no roles: %v, %v", roles, err)
	}
	if _, err := ParseRoles([]string{"root"}); err == nil || !strings.Contains(err.Error(), "rbacmnt") {
		t.Errorf("unknown role: %v", err)
	}
	if got := RoleNames(); len(got) != len(sec.Roles) || got[0] != "viewer" {
		t.Errorf("RoleNames: %v", got)
	}
}

func TestDescribeEvent(t *testing.T) {
	e := sec.Event{Stat: sec.StatRemoteCertificateFailures, Mnemonic: "REM_CERT_FAIL", Severity: sec.Alarm, Count: 2, Err: errors.New("sec: bad")}
	if got := describeEvent(e); got != "alarm REM_CERT_FAIL (2): sec: bad" {
		t.Errorf("describeEvent: %q", got)
	}
	if got := describeEvent(sec.Event{Stat: sec.StatMessagesSent, Count: 1}); got != sec.StatMessagesSent.String()+" (1)" {
		t.Errorf("describeEvent without mnemonic: %q", got)
	}
}
