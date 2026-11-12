// SPDX-License-Identifier: MIT

package config

import (
	"math"
	"strconv"
	"strings"
	"testing"
)

// A target comes from --url, a flag or a script. What ParseURL accepts must be
// usable: a host and a port.
func FuzzParseURL(f *testing.F) {
	for _, s := range []string{
		"tcp://10.0.0.1:2404", "tls://host", "tcp://[::1]:2404", "TCP://host:1", "tcp://", "tcp://host:0",
		"tcp://host:65536", "tcp://u@host", "tcp://host/path", "ftp://host", "", " tcp://host ", "%zz",
	} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, raw string) {
		tgt, err := ParseURL(raw)
		if err != nil {
			if tgt != (Target{}) {
				t.Fatalf("ParseURL(%q): %+v with error %v", raw, tgt, err)
			}
			return
		}
		if tgt.Host == "" || tgt.Port == 0 {
			t.Fatalf("ParseURL(%q): %+v", raw, tgt)
		}
	})
}

// The environment is operator input as much as the flags: a value LoadFromEnv takes is the
// number it says, never truncated to the field; one it refuses leaves the field as it was.
func FuzzLoadFromEnv(f *testing.F) {
	for _, s := range []string{"0", "255", "256", "-1", "65535", "65536", "4294967296", "18446744073709551616", "-129", "true", "0x10", "1e3", " 1", "a,b"} {
		f.Add(s)
	}
	type sample struct {
		U8  uint8    `env:"FUZZ_U8"`
		U16 uint16   `env:"FUZZ_U16"`
		U32 uint32   `env:"FUZZ_U32"`
		I8  int8     `env:"FUZZ_I8"`
		I64 int64    `env:"FUZZ_I64"`
		B   bool     `env:"FUZZ_B"`
		L   []string `env:"FUZZ_L"`
	}
	f.Fuzz(func(t *testing.T, s string) {
		if strings.ContainsRune(s, 0) {
			return // no environment holds a NUL
		}
		for _, name := range []string{"FUZZ_U8", "FUZZ_U16", "FUZZ_U32", "FUZZ_I8", "FUZZ_I64", "FUZZ_B", "FUZZ_L"} {
			t.Setenv(name, s)
		}
		cfg := sample{U8: 1, U16: 2, U32: 3, I8: 4, I64: 5}
		_ = LoadFromEnv(&cfg)
		u, uerr := strconv.ParseUint(s, 10, 64)
		i, ierr := strconv.ParseInt(s, 10, 64)
		check := func(name string, got, unchanged uint64, ok bool, want uint64) {
			if ok && got != want || !ok && got != unchanged {
				t.Fatalf("%s=%q: field %d", name, s, got)
			}
		}
		check("FUZZ_U8", uint64(cfg.U8), 1, uerr == nil && u <= math.MaxUint8, u)
		check("FUZZ_U16", uint64(cfg.U16), 2, uerr == nil && u <= math.MaxUint16, u)
		check("FUZZ_U32", uint64(cfg.U32), 3, uerr == nil && u <= math.MaxUint32, u)
		if ok := ierr == nil && i >= math.MinInt8 && i <= math.MaxInt8; ok && int64(cfg.I8) != i || !ok && cfg.I8 != 4 {
			t.Fatalf("FUZZ_I8=%q: field %d", s, cfg.I8)
		}
		if ierr == nil && cfg.I64 != i || ierr != nil && cfg.I64 != 5 {
			t.Fatalf("FUZZ_I64=%q: field %d", s, cfg.I64)
		}
	})
}
