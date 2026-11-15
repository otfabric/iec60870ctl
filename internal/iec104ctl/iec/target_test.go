// SPDX-License-Identifier: MIT

package iec

import (
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
