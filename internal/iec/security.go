// SPDX-License-Identifier: MIT

package iec

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/otfabric/go-iec104/asdu"
	"github.com/otfabric/go-iec104/sec"
	"github.com/otfabric/iec104ctl/internal/config"
	"github.com/otfabric/iec104ctl/internal/errs"
)

// Security is the application-layer security of IEC 62351-5 (the A-Profile of
// IEC TS 60870-5-7) as the security flags configure it.
type Security struct {
	Identity *sec.Identity
	Trust    sec.Trust
	// ID is the station's half of the association ID: the AIM of a controlling station,
	// the AIS of a controlled one.
	ID uint16
	// MAC and DataProtection are the algorithms a controlling station selects.
	MAC, DataProtection uint8
	// Roles are the roles of every controlling station at a controlled station; nil
	// applies no access control.
	Roles []sec.AccessRole
}

// The names of the algorithms, by code: the names of package sec in lower case, and short
// forms (the MAC truncated to 16 octets is the one of TCP/IP).
var (
	macNames = map[string]uint8{}
	dpaNames = map[string]uint8{}
)

func init() {
	for c := range 256 {
		code := uint8(c)
		if m, ok := sec.MACByCode(code); ok {
			name := strings.ToLower(m.Name)
			macNames[name] = code
			if short, ok := strings.CutSuffix(name, "/16"); ok {
				macNames[short] = code
			}
		}
		if d, ok := sec.DataProtectionByCode(code); ok {
			name := strings.ToLower(d.String())
			dpaNames[name] = code
			if short, ok := strings.CutSuffix(name, "/16"); ok {
				dpaNames[short] = code
			}
			if short, ok := strings.CutPrefix(name, "aead-"); ok {
				dpaNames[short] = code
			}
		}
	}
}

// MACNames returns the names --mal accepts, for completion.
func MACNames() []string { return sortedKeys(macNames) }

// DPANames returns the names --dpa accepts, for completion.
func DPANames() []string { return sortedKeys(dpaNames) }

// RoleNames returns the names --roles accepts, in the order of their IDs.
func RoleNames() []string {
	out := make([]string, 0, len(sec.Roles))
	for _, r := range sec.Roles {
		out = append(out, strings.ToLower(r.Name))
	}
	return out
}

func sortedKeys(m map[string]uint8) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// parseAlgorithm reads an algorithm of names by its code or its name.
func parseAlgorithm(flag, s string, names map[string]uint8) (uint8, error) {
	s = strings.ToLower(strings.TrimSpace(s))
	if code, ok := names[s]; ok {
		return code, nil
	}
	if n, err := strconv.ParseUint(s, 10, 8); err == nil {
		for _, code := range names {
			if code == uint8(n) {
				return code, nil
			}
		}
		return 0, fmt.Errorf("%s: algorithm %d is not supported (supported: %s)", flag, n, strings.Join(sortedKeys(names), ", "))
	}
	return 0, fmt.Errorf("%s: unknown algorithm %q (supported: %s)", flag, s, strings.Join(sortedKeys(names), ", "))
}

// ParseMAC reads --mal: a MAC algorithm by code or name; empty is HMAC-SHA-256.
func ParseMAC(s string) (uint8, error) {
	if strings.TrimSpace(s) == "" {
		return sec.HMACSHA256.Code, nil
	}
	return parseAlgorithm("--mal", s, macNames)
}

// ParseDPA reads --dpa: a data protection algorithm by code or name; empty is mac.
func ParseDPA(s string, mac uint8) (uint8, error) {
	if strings.TrimSpace(s) == "" {
		return mac, nil
	}
	return parseAlgorithm("--dpa", s, dpaNames)
}

// ParseRoles reads --roles: role names, case-insensitive. None is nil, no access control.
func ParseRoles(list []string) ([]sec.AccessRole, error) {
	var out []sec.AccessRole
	for _, s := range list {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		found := false
		for _, r := range sec.Roles {
			if strings.EqualFold(r.Name, s) {
				out, found = append(out, r), true
				break
			}
		}
		if !found {
			return nil, fmt.Errorf("--roles: unknown role %q (known: %s)", s, strings.Join(RoleNames(), ", "))
		}
	}
	return out, nil
}

// ParseTrust reads --security-trust: every certificate of the PEM file. A CA certificate
// is an authority; any other must be self-signed and is pinned by its fingerprint.
func ParseTrust(path string) (sec.Trust, error) {
	var t sec.Trust
	data, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return t, fmt.Errorf("--security-trust: %w", err)
	}
	certs, err := sec.ParseCertificates(data)
	if err != nil {
		return t, fmt.Errorf("--security-trust: %s: %w", path, err)
	}
	if len(certs) == 0 {
		return t, fmt.Errorf("--security-trust: no certificates found in %s", path)
	}
	for _, c := range certs {
		if c.IsCA {
			t.Authorities = append(t.Authorities, c)
			continue
		}
		selfSigned := bytes.Equal(c.RawIssuer, c.RawSubject) &&
			c.CheckSignature(c.SignatureAlgorithm, c.RawTBSCertificate, c.Signature) == nil
		if !selfSigned {
			return t, fmt.Errorf("--security-trust: %s: the certificate of %q is neither a CA certificate nor self-signed: trust the CA that signed it",
				path, c.Subject.CommonName)
		}
		t.Fingerprints = append(t.Fingerprints, sec.Fingerprint(c))
	}
	return t, nil
}

// loadSecurity reads the flags client and server share. others reports whether a flag of
// the role is set. Without --secure it returns nil, or an error if any security flag is
// set: a run never goes without the security it asked for.
func loadSecurity(c config.SecurityConfig, id uint16, others bool) (*Security, error) {
	if !c.Secure {
		if c.SecurityCert != "" || c.SecurityKey != "" || c.SecurityTrust != "" || id > 1 || others {
			return nil, errs.InvalidInput(errs.CodeInvalidFlagCombination,
				"invalid input: the security flags (--security-cert, --security-key, --security-trust, --association-id, --mal, --dpa, --roles) need --secure", nil)
		}
		return nil, nil
	}
	if c.SecurityCert == "" || c.SecurityKey == "" || c.SecurityTrust == "" {
		return nil, errs.InvalidInput(errs.CodeInvalidFlagCombination,
			"invalid input: --secure needs --security-cert, --security-key and --security-trust", nil)
	}
	if id == 0 {
		return nil, fmt.Errorf("--association-id: 0 is not 1-65535")
	}
	idt, err := sec.LoadIdentity(c.SecurityCert, c.SecurityKey)
	if err != nil {
		return nil, fmt.Errorf("--security-cert, --security-key: %w", err)
	}
	trust, err := ParseTrust(c.SecurityTrust)
	if err != nil {
		return nil, err
	}
	return &Security{Identity: idt, Trust: trust, ID: id}, nil
}

// ClientSecurity reads the security flags of a client command; nil without --secure.
func ClientSecurity(c config.ClientSecurityConfig) (*Security, error) {
	s, err := loadSecurity(c.SecurityConfig, c.AssociationID, strings.TrimSpace(c.MAL) != "" || strings.TrimSpace(c.DPA) != "")
	if s == nil || err != nil {
		return nil, err
	}
	if s.MAC, err = ParseMAC(c.MAL); err != nil {
		return nil, err
	}
	if s.DataProtection, err = ParseDPA(c.DPA, s.MAC); err != nil {
		return nil, err
	}
	return s, nil
}

// ServerSecurity reads the security flags of `server start`; nil without --secure.
func ServerSecurity(c config.ServerSecurityConfig) (*Security, error) {
	s, err := loadSecurity(c.SecurityConfig, c.AssociationID, len(c.Roles) > 0)
	if s == nil || err != nil {
		return nil, err
	}
	if s.Roles, err = ParseRoles(c.Roles); err != nil {
		return nil, err
	}
	return s, nil
}

// clientEndpoint makes the endpoint of a controlling station for s.
func clientEndpoint(s *Security, p asdu.Params, ca uint16, originator uint8, onEvent func(sec.Event)) (*sec.Endpoint, error) {
	return sec.New(sec.Config{
		Role:           sec.Controlling,
		AssociationID:  s.ID,
		Identity:       s.Identity,
		Trust:          s.Trust,
		MAC:            s.MAC,
		DataProtection: s.DataProtection,
		Params:         p,
		CommonAddr:     asdu.CommonAddr(ca),
		Originator:     originator,
		OnEvent:        onEvent,
	})
}

// secEvents follows the security events of a client: the first one that carries an
// error, to explain a failed association (what follows it is usually its consequence),
// and the trace of --debug.
type secEvents struct {
	tracer *Tracer
	mu     sync.Mutex
	first  *sec.Event
}

func (r *secEvents) on(e sec.Event) {
	if e.Err != nil {
		r.mu.Lock()
		if r.first == nil {
			r.first = &e
		}
		r.mu.Unlock()
	}
	if r.tracer != nil {
		r.tracer.line("--", nil, "security: "+describeEvent(e))
	}
}

func (r *secEvents) firstErr() *sec.Event {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.first
}

// describeEvent renders a security event for a log line.
func describeEvent(e sec.Event) string {
	name := e.Mnemonic
	if name == "" {
		name = e.Stat.String()
	}
	s := fmt.Sprintf("%s (%d)", name, e.Count)
	if e.Severity != 0 {
		s = fmt.Sprintf("%s %s", e.Severity, s)
	}
	if e.Err != nil {
		s += ": " + e.Err.Error()
	}
	return s
}

// secErrors are the errors of package sec that say an association failed.
var secErrors = []error{sec.ErrCertificate, sec.ErrProcedure, sec.ErrAlgorithm, sec.ErrAuthentication,
	sec.ErrUnexpected, sec.ErrMessage, sec.ErrUnsecured, sec.ErrReplay, sec.ErrConfig, sec.ErrNotReady}

func isSecError(err error) bool {
	for _, e := range secErrors {
		if errors.Is(err, e) {
			return true
		}
	}
	return false
}

// securityFailed classifies a failure to connect with security. It is security_failed
// when package sec says so, or when the association had begun (security messages were
// sent after STARTDT) and did not complete, whatever ended it; nil otherwise.
func securityFailed(err error, ep *sec.Endpoint, ev *secEvents) error {
	if errors.Is(err, context.Canceled) {
		return nil
	}
	begun := ep.Statistics()[sec.StatMessagesSent] > 0
	if !isSecError(err) && !begun {
		return nil
	}
	msg := fmt.Sprintf("security error: no secure session: %v", err)
	if first := ev.firstErr(); first != nil && !strings.Contains(msg, first.Err.Error()) {
		msg += fmt.Sprintf(" (security event: %s)", describeEvent(*first))
	}
	return errs.New(errs.KindTransport, errs.CodeSecurityFailed, msg, err)
}
