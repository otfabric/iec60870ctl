// SPDX-License-Identifier: MIT

package config

import (
	"math"
	"strconv"
	"testing"

	"github.com/spf13/cobra"
)

// The value a field holds when its flag is registered is the default of the flag, for every
// integer kind, at both ends of its range: nothing is converted through a wider or narrower
// type on the way.
func TestFlagDefaultsOfEveryIntegerKind(t *testing.T) {
	type ints struct {
		U8  uint8  `flag:"u8"`
		U16 uint16 `flag:"u16"`
		U32 uint32 `flag:"u32"`
		U64 uint64 `flag:"u64"`
		U   uint   `flag:"u"`
		I   int    `flag:"i"`
		I8  int8   `flag:"i8"`
		I16 int16  `flag:"i16"`
		I32 int32  `flag:"i32"`
		I64 int64  `flag:"i64"`
	}
	for name, cfg := range map[string]ints{
		"largest":  {math.MaxUint8, math.MaxUint16, math.MaxUint32, math.MaxUint64, math.MaxUint, math.MaxInt, math.MaxInt8, math.MaxInt16, math.MaxInt32, math.MaxInt64},
		"smallest": {I: math.MinInt, I8: math.MinInt8, I16: math.MinInt16, I32: math.MinInt32, I64: math.MinInt64},
		"ordinary": {7, 2404, 70000, 5000000000, 9, -3, -8, -16, -32, -64},
	} {
		t.Run(name, func(t *testing.T) {
			want := map[string]string{
				"u8": strconv.FormatUint(uint64(cfg.U8), 10), "u16": strconv.FormatUint(uint64(cfg.U16), 10),
				"u32": strconv.FormatUint(uint64(cfg.U32), 10), "u64": strconv.FormatUint(cfg.U64, 10),
				"u": strconv.FormatUint(uint64(cfg.U), 10), "i": strconv.Itoa(cfg.I),
				"i8": strconv.Itoa(int(cfg.I8)), "i16": strconv.Itoa(int(cfg.I16)),
				"i32": strconv.Itoa(int(cfg.I32)), "i64": strconv.FormatInt(cfg.I64, 10),
			}
			before := cfg
			cmd := &cobra.Command{Use: "x", RunE: func(*cobra.Command, []string) error { return nil }}
			RegisterFlags(cmd, &cfg)
			for flag, def := range want {
				f := cmd.Flags().Lookup(flag)
				if f == nil {
					t.Fatalf("flag --%s not registered", flag)
				}
				if f.DefValue != def {
					t.Errorf("--%s defaults to %s, want %s", flag, f.DefValue, def)
				}
			}
			cmd.SetArgs(nil)
			if err := cmd.Execute(); err != nil {
				t.Fatal(err)
			}
			if cfg != before {
				t.Errorf("parsing no flags changed the fields: %+v, want %+v", cfg, before)
			}
		})
	}
}
