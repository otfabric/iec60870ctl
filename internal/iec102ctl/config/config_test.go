// SPDX-License-Identifier: MIT

package config

import (
	"math"
	"strconv"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

func TestDefaults(t *testing.T) {
	c := Defaults()
	if c.LinkAddress != 1 || c.LinkAddressSize != 2 || c.CommonAddress != 1 || c.CASize != 2 || c.Profile != ProfileStandard {
		t.Errorf("client defaults %+v", c)
	}
	s := ServerDefaults()
	if s.LinkAddress != -1 || s.CommonAddress != -1 || s.LinkAddressSize != 2 || s.CASize != 2 || s.OutputFormat != "text" {
		t.Errorf("server defaults %+v", s)
	}
}

// Every flag of every command is bound to an IEC102CTL_ variable, and the environment sets
// the field.
func TestEnvironmentReachesEveryCommand(t *testing.T) {
	t.Setenv("IEC102CTL_LINK_ADDRESS", "9")
	t.Setenv("IEC102CTL_ACCESS_KEY", "7")
	t.Setenv("IEC102CTL_RECORD", "12")
	for _, cfg := range []any{
		&InfoConfig{}, &TimeConfig{}, &TotalsConfig{}, &EventsConfig{}, &RequestConfig{}, &SessionOnlyConfig{},
		&SetTimeConfig{}, &TariffsConfig{}, &REEReadConfig{}, &TypesConfig{}, &ServerConfig{}, &GenerateConfigConfig{},
	} {
		if err := LoadFromEnv(cfg); err != nil {
			t.Fatal(err)
		}
		cmd := &cobra.Command{Use: "x"}
		RegisterFlags(cmd, cfg)
		cmd.Flags().VisitAll(func(f *pflag.Flag) {
			if !strings.Contains(f.Usage, "[env: IEC102CTL_") {
				t.Errorf("%T --%s has no environment variable", cfg, f.Name)
			}
		})
	}
	var tc TotalsConfig
	MustLoadFromEnv(&tc)
	if tc.LinkAddress != 9 || tc.AccessKey != "7" || tc.Record != 12 || Getenv("IEC102CTL_RECORD") != "12" {
		t.Errorf("loaded %+v", tc)
	}
	var sc ServerConfig
	MustLoadFromEnv(&sc)
	if sc.LinkAddress != 9 {
		t.Errorf("server %+v", sc)
	}
}

// The environment is operator input as much as the flags: a value LoadFromEnv takes is the
// number it says, never truncated to the field; one it refuses leaves the field as it was.
func FuzzLoadFromEnv(f *testing.F) {
	for _, s := range []string{"0", "255", "256", "-1", "65535", "65536", "4294967296", "-2147483649", "true", "0x10", " 1"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		if strings.ContainsRune(s, 0) {
			return // no environment holds a NUL
		}
		for _, name := range []string{"IEC102CTL_RECORD", "IEC102CTL_PORT", "IEC102CTL_TIMEOUT", "IEC102CTL_LINK_ADDRESS", "IEC102CTL_OBJECTS"} {
			t.Setenv(name, s)
		}
		cfg := RequestConfig{Record: 1, Objects: 4}
		cfg.Port, cfg.Timeout, cfg.LinkAddress = 2, 3, 5
		_ = LoadFromEnv(&cfg)
		u, uerr := strconv.ParseUint(s, 10, 64)
		i, ierr := strconv.ParseInt(s, 10, 64)
		check := func(name string, got, unchanged uint64, ok bool) {
			if ok && got != u || !ok && got != unchanged {
				t.Fatalf("%s=%q: field %d", name, s, got)
			}
		}
		check("IEC102CTL_RECORD", uint64(cfg.Record), 1, uerr == nil && u <= math.MaxUint8)
		check("IEC102CTL_PORT", uint64(cfg.Port), 2, uerr == nil && u <= math.MaxUint16)
		check("IEC102CTL_TIMEOUT", uint64(cfg.Timeout), 3, uerr == nil && u <= math.MaxUint32)
		check("IEC102CTL_LINK_ADDRESS", uint64(cfg.LinkAddress), 5, uerr == nil && u <= math.MaxUint16)
		if ok := ierr == nil && i >= math.MinInt && i <= math.MaxInt; ok && int64(cfg.Objects) != i || !ok && cfg.Objects != 4 {
			t.Fatalf("IEC102CTL_OBJECTS=%q: field %d", s, cfg.Objects)
		}
	})
}
