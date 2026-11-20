// SPDX-License-Identifier: MIT

package iec

import (
	"context"
	"testing"

	"github.com/otfabric/iec60870ctl/internal/errs"
	"github.com/otfabric/iec60870ctl/internal/iec104ctl/config"
)

// Validate and Open refuse a TLS flag with a tcp:// URL as a usage error, and report any
// other URL that is not valid as invalid_url (Open) or invalid input (Validate).
func TestTLSFlagWithATCPURLRefused(t *testing.T) {
	refused := &config.ConnConfig{URL: "tcp://127.0.0.1:1", TLSConfig: config.TLSConfig{CACert: "ca.pem"}}
	for name, err := range map[string]error{"Validate": Validate(refused), "Open": openErr(refused)} {
		e, ok := errs.As(err)
		if !ok || e.Kind != errs.KindUsage || e.Code != errs.CodeInvalidFlagCombination {
			t.Errorf("%s: %v, want a usage error invalid_flag_combination", name, err)
		}
	}
	bad := &config.ConnConfig{URL: "http://127.0.0.1"}
	if e, ok := errs.As(openErr(bad)); !ok || e.Code != errs.CodeInvalidURL {
		t.Errorf("Open of %s: %v, want invalid_url", bad.URL, e)
	}
	if _, ok := errs.As(Validate(bad)); ok {
		t.Errorf("Validate of %s gave a typed error; it is wrapped as invalid input by the caller", bad.URL)
	}
}

func openErr(cfg *config.ConnConfig) error {
	s, err := Open(cfg, nil)
	if s != nil {
		_ = s.Close()
	}
	return err
}

// A standby URL that is not valid is invalid_url from Open, before anything connects; a
// session without --standby-url has no group and one target, its own.
func TestSessionTargets(t *testing.T) {
	bad := &config.ConnConfig{URL: "tcp://127.0.0.1:1", Standby: []string{"tls://127.0.0.1:2"}}
	if e, ok := errs.As(openErr(bad)); !ok || e.Code != errs.CodeInvalidURL {
		t.Errorf("Open with a standby of another scheme: %v, want invalid_url", e)
	}
	s, err := Open(&config.ConnConfig{URL: "tcp://127.0.0.1:1"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()
	if s.Group() != nil {
		t.Error("a group without --standby-url")
	}
	if ts := s.Targets(); len(ts) != 1 || ts[0] != s.Target {
		t.Errorf("targets %v, want [%v]", ts, s.Target)
	}
	if _, ok := s.TargetOf(nil); ok {
		t.Error("a target for no connection")
	}

	g, err := Open(&config.ConnConfig{URL: "tcp://127.0.0.1:1", Standby: []string{"tcp://127.0.0.1:2"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = g.Close() }()
	if g.Group() == nil || len(g.Targets()) != 2 {
		t.Errorf("group %v, targets %v", g.Group(), g.Targets())
	}
	if tgt, ok := g.TargetOf(g.Group().Clients()[1]); !ok || tgt.Addr() != "127.0.0.1:2" {
		t.Errorf("target of the standby: %v %v", tgt, ok)
	}
}

// Parameter refuses what it cannot send, and reports a station that cannot be reached,
// without sending anything.
func TestParameterErrors(t *testing.T) {
	ctx := context.Background()
	cfg := &config.ParameterConfig{StationConfig: config.StationConfig{ConnConfig: config.ConnConfig{URL: "tcp://127.0.0.1:1", Timeout: 500}, CommonAddr: 1},
		Type: "float", IOA: 0, Value: "1", Qualifier: "threshold"}
	if _, err := Parameter(ctx, cfg, nil); err == nil {
		t.Error("a parameter of measured value at IOA 0 was accepted")
	}
	cfg.IOA = 1
	if _, err := Parameter(ctx, cfg, nil); err == nil {
		t.Error("a station that does not listen answered")
	}
	cfg.URL = "http://x"
	if err := CheckParameter(cfg); err == nil {
		t.Error("CheckParameter accepted an http URL")
	}
	cfg.URL, cfg.CommonAddr = "tcp://127.0.0.1:1", 0
	if err := CheckParameter(cfg); err == nil {
		t.Error("CheckParameter accepted common address 0")
	}
}
