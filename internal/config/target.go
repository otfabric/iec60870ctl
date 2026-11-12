// SPDX-License-Identifier: MIT

package config

import (
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
)

// Default ports: 2404 is the IANA port of IEC 60870-5-104, 19998 the one IEC 62351-3
// assigns to IEC 60870-5-104 over TLS.
const (
	DefaultPort    = 2404
	DefaultTLSPort = 19998
)

// URL schemes accepted by --url.
const (
	SchemeTCP = "tcp"
	SchemeTLS = "tls"
)

// Target is a resolved station address.
type Target struct {
	Host string
	Port uint16
	TLS  bool
}

// Addr returns host:port for dialing or listening.
func (t Target) Addr() string {
	return net.JoinHostPort(t.Host, strconv.Itoa(int(t.Port)))
}

// String returns the target as a URL, the form used in output ("tcp://host:2404").
func (t Target) String() string {
	scheme := SchemeTCP
	if t.TLS {
		scheme = SchemeTLS
	}
	return scheme + "://" + t.Addr()
}

// ParseURL parses tcp://host[:port] or tls://host[:port]. A missing port is 2404 for tcp
// and 19998 for tls.
func ParseURL(raw string) (Target, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return Target{}, fmt.Errorf("invalid URL %q: %w", raw, err)
	}
	var t Target
	switch strings.ToLower(u.Scheme) {
	case SchemeTCP:
		t.Port = DefaultPort
	case SchemeTLS:
		t.Port = DefaultTLSPort
		t.TLS = true
	case "":
		return Target{}, fmt.Errorf("invalid URL %q: missing scheme (use tcp://host:port or tls://host:port)", raw)
	default:
		return Target{}, fmt.Errorf("invalid URL %q: unsupported scheme %q (use tcp or tls)", raw, u.Scheme)
	}
	if u.Path != "" && u.Path != "/" || u.RawQuery != "" || u.Fragment != "" || u.User != nil {
		return Target{}, fmt.Errorf("invalid URL %q: only scheme, host and port are allowed", raw)
	}
	t.Host = u.Hostname()
	if t.Host == "" {
		return Target{}, fmt.Errorf("invalid URL %q: missing host", raw)
	}
	if p := u.Port(); p != "" {
		n, err := strconv.ParseUint(p, 10, 16)
		if err != nil || n == 0 {
			return Target{}, fmt.Errorf("invalid URL %q: port must be 1-65535", raw)
		}
		t.Port = uint16(n)
	}
	return t, nil
}

// ResolveTarget returns the station a client command talks to: --url, or --ip with --port.
// The two styles are mutually exclusive, and one of them is required. With --ip, TLS is
// used when a TLS flag asks for it, on the port given (2404 unless --port is set).
func (c *ConnConfig) ResolveTarget() (Target, error) {
	url, ip := strings.TrimSpace(c.URL), strings.TrimSpace(c.IP)
	switch {
	case url != "" && ip != "":
		return Target{}, fmt.Errorf("--url and --ip are mutually exclusive")
	case url != "":
		return ParseURL(url)
	case ip != "":
		port := c.Port
		if port == 0 {
			port = DefaultPort
		}
		return Target{Host: ip, Port: port, TLS: c.wantsTLS()}, nil
	default:
		return Target{}, fmt.Errorf("no station given: set --url (tcp://host:port or tls://host:port) or --ip")
	}
}
