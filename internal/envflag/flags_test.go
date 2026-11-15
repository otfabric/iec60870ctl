// SPDX-License-Identifier: MIT

package envflag

import (
	"math"
	"strconv"
	"strings"
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

// CommandLineOnly gives a flag whose value came from its environment variable its built-in
// default back and names the variable and the flag, once; a flag on the command line, a
// variable that is not set and a flag that is not listed keep their value.
func TestCommandLineOnly(t *testing.T) {
	type cfg struct {
		Freeze    string `env:"ENVFLAG_TEST_FREEZE" flag:"freeze"`
		Set       string `env:"ENVFLAG_TEST_SET" flag:"set"`
		CA        uint16 `env:"ENVFLAG_TEST_CA" flag:"ca"`
		Broadcast bool   `env:"ENVFLAG_TEST_BROADCAST" flag:"broadcast"`
		Mode      string `env:"ENVFLAG_TEST_MODE" flag:"mode"`
		Format    string `env:"ENVFLAG_TEST_FORMAT" flag:"format"`
	}
	t.Setenv("ENVFLAG_TEST_FREEZE", "reset")
	t.Setenv("ENVFLAG_TEST_SET", "1234")
	t.Setenv("ENVFLAG_TEST_CA", "255")
	t.Setenv("ENVFLAG_TEST_BROADCAST", "true")
	t.Setenv("ENVFLAG_TEST_FORMAT", "json")
	c := cfg{Freeze: "read", CA: 1, Mode: "execute", Format: "text"}
	var stderr strings.Builder
	cmd := &cobra.Command{Use: "x", RunE: func(cmd *cobra.Command, _ []string) error {
		CommandLineOnly(cmd, "freeze", "set", "ca", "broadcast", "mode", "missing")
		return nil
	}}
	cmd.SetErr(&stderr)
	LoadAndRegister(cmd, &c)
	if f := cmd.Flags().Lookup("freeze"); f.DefValue != "reset" {
		t.Errorf("--freeze defaults to %q in the help, want the value of its variable", f.DefValue)
	}
	cmd.SetArgs([]string{"--set", "5"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	want := "notice: ENVFLAG_TEST_FREEZE=reset is not used: --freeze is taken only from the command line\n" +
		"notice: ENVFLAG_TEST_CA=255 is not used: --ca is taken only from the command line\n" +
		"notice: ENVFLAG_TEST_BROADCAST=true is not used: --broadcast is taken only from the command line\n"
	if stderr.String() != want {
		t.Errorf("stderr:\n%s\nwant:\n%s", stderr.String(), want)
	}
	if want := (cfg{Freeze: "read", Set: "5", CA: 1, Mode: "execute", Format: "json"}); c != want {
		t.Errorf("values: %+v, want %+v", c, want)
	}
	for _, name := range []string{"freeze", "ca", "broadcast"} {
		if cmd.Flags().Lookup(name).Changed {
			t.Errorf("--%s counts as given on the command line", name)
		}
	}
}

// A flag that was not registered by LoadAndRegister has no built-in default to go back to:
// CommandLineOnly refuses to go on with the value of the variable.
func TestCommandLineOnlyNeedsTheBuiltInDefault(t *testing.T) {
	type cfg struct {
		Freeze string `env:"ENVFLAG_TEST_FREEZE" flag:"freeze"`
	}
	t.Setenv("ENVFLAG_TEST_FREEZE", "reset")
	var c cfg
	MustLoadFromEnv(&c)
	cmd := &cobra.Command{Use: "x"}
	RegisterFlags(cmd, &c)
	defer func() {
		if recover() == nil {
			t.Error("no panic for a flag without a built-in default")
		}
	}()
	CommandLineOnly(cmd, "freeze")
}

// NotFromEnvironment names the first flag whose value came from its variable; a flag on the
// command line, or one whose variable is not set, gives no error.
func TestNotFromEnvironment(t *testing.T) {
	type cfg struct {
		Mode   string `env:"ENVFLAG_TEST_MODE" flag:"mode"`
		Format string `env:"ENVFLAG_TEST_FORMAT" flag:"format"`
	}
	t.Setenv("ENVFLAG_TEST_MODE", "select")
	run := func(args ...string) error {
		c := cfg{Mode: "execute"}
		var got error
		cmd := &cobra.Command{Use: "x", RunE: func(cmd *cobra.Command, _ []string) error {
			got = NotFromEnvironment(cmd, "format", "mode")
			return nil
		}}
		LoadAndRegister(cmd, &c)
		cmd.SetArgs(args)
		if err := cmd.Execute(); err != nil {
			t.Fatal(err)
		}
		return got
	}
	want := "ENVFLAG_TEST_MODE=select is not used: --mode is taken only from the command line; give --mode select to run it so"
	if err := run(); err == nil || err.Error() != want {
		t.Errorf("from the variable: %v, want %q", err, want)
	}
	if err := run("--mode", "select"); err != nil {
		t.Errorf("on the command line: %v", err)
	}
}
