// SPDX-License-Identifier: MIT

package config

import (
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

type sample struct {
	Inner
	Name    string   `env:"T_NAME" flag:"name" desc:"a name"`
	Port    uint16   `env:"T_PORT" flag:"port" desc:"a port"`
	Small   uint8    `env:"T_SMALL" flag:"small" desc:"small"`
	Wide    uint32   `env:"T_WIDE" flag:"wide" desc:"wide"`
	Huge    uint64   `env:"T_HUGE" flag:"huge" desc:"huge"`
	Plain   uint     `env:"T_PLAIN" flag:"plain" desc:"plain"`
	Signed  int      `env:"T_SIGNED" flag:"signed" desc:"signed"`
	S8      int8     `env:"T_S8" flag:"s8" desc:"s8"`
	S16     int16    `env:"T_S16" flag:"s16" desc:"s16"`
	S32     int32    `env:"T_S32" flag:"s32" desc:"s32"`
	S64     int64    `env:"T_S64" flag:"s64" desc:"s64"`
	On      bool     `env:"T_ON" flag:"on" desc:"on"`
	List    []string `env:"T_LIST" flag:"list" desc:"a list"`
	NoEnv   string   `flag:"no-env" desc:"without env"`
	Hidden  string   // neither flag nor env
	private string   `env:"T_PRIVATE"` //nolint:unused // proves unexported fields are skipped
}

type Inner struct {
	Deep string `env:"T_DEEP" flag:"deep" desc:"embedded"`
}

func TestEnvironmentAndFlags(t *testing.T) {
	for k, v := range map[string]string{
		"T_NAME": "rtu", "T_PORT": "2404", "T_SMALL": "7", "T_WIDE": "70000", "T_HUGE": "5000000000", "T_PLAIN": "9",
		"T_SIGNED": "-3", "T_S8": "-8", "T_S16": "-16", "T_S32": "-32", "T_S64": "-64", "T_ON": "true",
		"T_LIST": "a, b ,c", "T_DEEP": "embedded", "T_PRIVATE": "x",
	} {
		t.Setenv(k, v)
	}
	var cfg sample
	if err := LoadFromEnv(&cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.Name != "rtu" || cfg.Port != 2404 || cfg.Small != 7 || cfg.Wide != 70000 || cfg.Huge != 5000000000 || cfg.Plain != 9 ||
		cfg.Signed != -3 || cfg.S8 != -8 || cfg.S16 != -16 || cfg.S32 != -32 || cfg.S64 != -64 || !cfg.On ||
		strings.Join(cfg.List, "|") != "a|b|c" || cfg.Deep != "embedded" || cfg.private != "" {
		t.Fatalf("loaded %+v", cfg)
	}

	// Flags start from the environment, say which variable sets them, and win over it.
	cmd := &cobra.Command{Use: "x", RunE: func(*cobra.Command, []string) error { return nil }}
	RegisterFlags(cmd, &cfg)
	if f := cmd.Flags().Lookup("port"); f == nil || f.DefValue != "2404" || f.Usage != "a port [env: T_PORT]" {
		t.Errorf("flag port: %+v", f)
	}
	if f := cmd.Flags().Lookup("no-env"); f == nil || f.Usage != "without env" {
		t.Errorf("flag no-env: %+v", f)
	}
	if cmd.Flags().Lookup("deep") == nil || cmd.Flags().Lookup("hidden") != nil {
		t.Error("the flag of an embedded struct is missing, or an untagged field became a flag")
	}
	cmd.SetArgs([]string{"--port", "19998", "--list", "x,y", "--on=false", "--s8", "-1", "--small", "200", "--signed", "4", "--huge", "1",
		"--plain", "2", "--wide", "3", "--s16", "5", "--s32", "6", "--s64", "7", "--name", "n", "--deep", "d"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if cfg.Port != 19998 || strings.Join(cfg.List, "|") != "x|y" || cfg.On || cfg.S8 != -1 || cfg.Small != 200 || cfg.Deep != "d" {
		t.Errorf("after flags: %+v", cfg)
	}
}

func TestMalformedEnvironment(t *testing.T) {
	for k, v := range map[string]string{"T_PORT": "70000", "T_SMALL": "x", "T_SIGNED": "1.5", "T_S8": "500", "T_ON": "maybe"} {
		t.Setenv(k, v)
	}
	var cfg sample
	err := LoadFromEnv(&cfg)
	if err == nil {
		t.Fatal("malformed values were accepted")
	}
	for _, name := range []string{"T_PORT", "T_SMALL", "T_SIGNED", "T_S8", "T_ON"} {
		if !strings.Contains(err.Error(), name) {
			t.Errorf("the error does not name %s: %v", name, err)
		}
	}
	if LoadFromEnv(cfg) != nil || LoadFromEnv(nil) != nil {
		t.Error("a value that is no pointer to a struct is ignored, not an error")
	}
	defer func() {
		if r := recover(); r == nil || !strings.Contains(r.(string), "invalid environment") {
			t.Errorf("MustLoadFromEnv: %v", r)
		}
	}()
	MustLoadFromEnv(&cfg)
}

func TestUnsupportedFlagKindsFailAtStartup(t *testing.T) {
	for name, cfg := range map[string]any{
		"float": &struct {
			F float64 `flag:"f"`
		}{},
		"int slice": &struct {
			L []int `flag:"l"`
		}{},
	} {
		t.Run(name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Error("an unsupported field kind was registered as a flag")
				}
			}()
			RegisterFlags(&cobra.Command{}, cfg)
		})
	}
	// An env tag on an unsupported kind is skipped.
	t.Setenv("T_F", "1.5")
	t.Setenv("T_L", "1,2")
	var cfg struct {
		F float64 `env:"T_F"`
		L []int   `env:"T_L"`
	}
	if err := LoadFromEnv(&cfg); err != nil || cfg.F != 0 || cfg.L != nil {
		t.Errorf("unsupported kinds: %+v, %v", cfg, err)
	}
}

func TestTargets(t *testing.T) {
	for in, want := range map[string]string{
		"tcp://10.0.0.1":           "tcp://10.0.0.1:2404",
		"tcp://10.0.0.1:12404/":    "tcp://10.0.0.1:12404",
		" TLS://rtu.example.net ":  "tls://rtu.example.net:19998",
		"tls://[fd00::1]:20000":    "tls://[fd00::1]:20000",
		"tcp://rtu.example.net:80": "tcp://rtu.example.net:80",
	} {
		got, err := ParseURL(in)
		if err != nil || got.String() != want {
			t.Errorf("ParseURL(%q) = %v, %v; want %s", in, got, err, want)
		}
	}
	for _, in := range []string{"", "10.0.0.1", "10.0.0.1:2404", "http://x", "tcp://", "tcp://:2404", "tcp://h:0", "tcp://h:99999", "tcp://h/path",
		"tcp://h?x=1", "tcp://user@h", "tcp://h#frag", "tcp://h:port", "://"} {
		if got, err := ParseURL(in); err == nil {
			t.Errorf("ParseURL(%q) = %v, want an error", in, got)
		}
	}

	c := ConnConfig{IP: " rtu ", Port: 0}
	if got, err := c.ResolveTarget(); err != nil || got.Addr() != "rtu:2404" || got.TLS {
		t.Errorf("--ip without port: %v, %v", got, err)
	}
	c = ConnConfig{IP: "10.0.0.1", Port: 5000, TLSConfig: TLSConfig{Insecure: true}}
	if got, err := c.ResolveTarget(); err != nil || got.String() != "tls://10.0.0.1:5000" {
		t.Errorf("--ip with a TLS flag: %v, %v", got, err)
	}
	for _, tc := range []TLSConfig{{TLS: true}, {CACert: "x"}, {Cert: "x"}, {Key: "x"}, {ServerName: "x"}} {
		if c := (ConnConfig{TLSConfig: tc}); !c.wantsTLS() {
			t.Errorf("%+v does not ask for TLS", tc)
		}
	}
	if _, err := (&ConnConfig{}).ResolveTarget(); err == nil {
		t.Error("no station given was accepted")
	}
	if _, err := (&ConnConfig{URL: "tcp://a", IP: "b"}).ResolveTarget(); err == nil {
		t.Error("--url with --ip was accepted")
	}
}
