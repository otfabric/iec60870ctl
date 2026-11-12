// SPDX-License-Identifier: MIT

package iec

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/otfabric/go-iec60870-5/ft12"
	"github.com/otfabric/go-iec60870-5/iec101"
	"github.com/otfabric/go-iec60870-5/link"
	"github.com/otfabric/iec60870ctl/internal/iec101ctl/config"
	"github.com/otfabric/iec60870ctl/internal/line"
)

// DefaultTimeout bounds bringing up the link and each request when --timeout is 0.
const DefaultTimeout = 10 * time.Second

// Timeout returns the request timeout of a --timeout value in milliseconds.
func Timeout(ms uint32) time.Duration {
	if ms == 0 {
		return DefaultTimeout
	}
	return time.Duration(ms) * time.Millisecond
}

// Link holds the resolved link parameters of a command: the system parameters both
// stations agree on and the two link addresses.
type Link struct {
	Params iec101.Params
	// Addr is the link address of the controlled station.
	Addr uint16
	// Remote is the link address of the controlling station in balanced mode.
	Remote uint16
}

// ParseMode resolves --link-mode.
func ParseMode(s string) (iec101.Mode, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", config.ModeUnbalanced:
		return iec101.ModeUnbalanced, nil
	case config.ModeBalanced:
		return iec101.ModeBalanced, nil
	}
	return 0, fmt.Errorf("invalid --link-mode %q (expected: unbalanced, balanced)", s)
}

// ParseLinkAddr parses a link address given as text (--remote-link-address): a number,
// decimal or 0x hex, that fits the address field of size octets and is not its broadcast
// address. An empty string is def.
func ParseLinkAddr(s string, size int, def uint16) (uint16, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return def, nil
	}
	n, err := strconv.ParseUint(s, 0, 16)
	if err != nil {
		return 0, fmt.Errorf("invalid link address %q: not a number 0-65534", s)
	}
	if err := checkLinkAddr(uint16(n), size); err != nil {
		return 0, err
	}
	return uint16(n), nil
}

// checkLinkAddr validates a link address against the address field: it must fit, and the
// broadcast address (all ones) is not a station's address.
func checkLinkAddr(a uint16, size int) error {
	switch size {
	case 0:
		return nil
	case 1:
		if a >= 0xFF {
			return fmt.Errorf("invalid link address %d: a one-octet address field holds 0-254 (255 is the broadcast address)", a)
		}
	default:
		if a == 0xFFFF {
			return fmt.Errorf("invalid link address 65535: it is the broadcast address of a two-octet field")
		}
	}
	return nil
}

// LinkParams resolves the link flags of cfg into the parameters of go-iec60870-5.
func LinkParams(cfg config.LinkConfig) (Link, error) {
	var l Link
	mode, err := ParseMode(cfg.Mode)
	if err != nil {
		return l, err
	}
	p := iec101.DefaultParams()
	p.Mode = mode
	switch {
	case cfg.LinkAddrSize > 2:
		return l, fmt.Errorf("invalid --link-address-size %d: use 1 or 2, or 0 in balanced mode", cfg.LinkAddrSize)
	case cfg.LinkAddrSize == 0 && mode == iec101.ModeUnbalanced:
		return l, fmt.Errorf("invalid --link-address-size 0: unbalanced transmission needs a link address (use 1 or 2)")
	case cfg.CauseSize < 1 || cfg.CauseSize > 2:
		return l, fmt.Errorf("invalid --cot-size %d: use 1 or 2", cfg.CauseSize)
	case cfg.CASize < 1 || cfg.CASize > 2:
		return l, fmt.Errorf("invalid --ca-size %d: use 1 or 2", cfg.CASize)
	case cfg.IOASize < 1 || cfg.IOASize > 3:
		return l, fmt.Errorf("invalid --ioa-size %d: use 1, 2 or 3", cfg.IOASize)
	}
	p.Link.FT12 = ft12.Params{AddrSize: int(cfg.LinkAddrSize)}
	p.ASDU.CauseSize, p.ASDU.CommonAddrSize, p.ASDU.IOASize = int(cfg.CauseSize), int(cfg.CASize), int(cfg.IOASize)
	p.Link.Timeout = link.DefaultTimeout
	if cfg.LinkTimeout != 0 {
		p.Link.Timeout = time.Duration(cfg.LinkTimeout) * time.Millisecond
	}
	if cfg.LinkRetries != 0 {
		p.Link.Retries = int(cfg.LinkRetries)
	}
	p.Strict = cfg.Strict
	loc, err := Location(cfg.TimeZone)
	if err != nil {
		return l, err
	}
	p.ASDU = p.ASDU.In(loc)
	if l.Params, err = p.Normalize(); err != nil {
		return l, fmt.Errorf("link parameters: %w", err)
	}
	size := p.Link.FT12.AddrSize
	addr := cfg.LinkAddr
	if size == 0 {
		addr = 0
	}
	if size == 1 && cfg.LinkAddr > 0xFF {
		return l, fmt.Errorf("invalid --link-address %d: a one-octet address field holds 0-254", cfg.LinkAddr)
	}
	if err := checkLinkAddr(addr, size); err != nil {
		return l, fmt.Errorf("--link-address: %w", err)
	}
	remote, err := ParseLinkAddr(cfg.RemoteLinkAddr, size, addr)
	if err != nil {
		return l, fmt.Errorf("--remote-link-address: %w", err)
	}
	if mode == iec101.ModeUnbalanced && strings.TrimSpace(cfg.RemoteLinkAddr) != "" {
		return l, fmt.Errorf("--remote-link-address is for balanced mode only")
	}
	if size == 0 {
		remote = 0
	}
	l.Addr, l.Remote = addr, remote
	return l, nil
}

// Location resolves --timezone: utc (default), local or an IANA zone name.
func Location(name string) (*time.Location, error) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "", "utc":
		return time.UTC, nil
	case "local":
		return time.Local, nil
	}
	loc, err := time.LoadLocation(strings.TrimSpace(name))
	if err != nil {
		return nil, fmt.Errorf("time zone %q: use utc, local or an IANA name such as Europe/Brussels", name)
	}
	return loc, nil
}

// Settings returns the serial line settings of cfg.
func Settings(cfg config.LineConfig) line.Settings {
	return line.Settings{Baud: cfg.Baud, DataBits: cfg.DataBits, Parity: cfg.Parity, StopBits: cfg.StopBits}
}

// BroadcastCA returns the broadcast common address of a common address field of size
// octets: 255 or 65535.
func BroadcastCA(size int) uint16 {
	if size == 1 {
		return 0xFF
	}
	return 0xFFFF
}

// MaxIOAFor returns the largest information object address of a field of size octets.
func MaxIOAFor(size int) uint32 {
	switch size {
	case 1:
		return 0xFF
	case 2:
		return 0xFFFF
	}
	return MaxIOA
}
