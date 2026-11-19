// SPDX-License-Identifier: MIT

package runner

import (
	"testing"

	"github.com/otfabric/iec60870ctl/internal/format"
)

func TestFormatFallback(t *testing.T) {
	const env = "RUNNERTEST_FORMAT"
	for _, c := range []struct {
		name string
		args []string
		env  string
		want format.OutputFormat // "" when no format is set
	}{
		{"separate value", []string{"client", "read", "--format", "json"}, "", format.FormatJSON},
		{"equals", []string{"client", "read", "--format=table"}, "", format.FormatTable},
		{"last wins", []string{"--format", "table", "--format", "JSON"}, "", format.FormatJSON},
		{"after --", []string{"client", "--", "--format", "json"}, "", ""},
		{"no value", []string{"client", "--format"}, "", ""},
		{"not a format", []string{"--format", "yaml"}, "json", ""},
		{"environment", []string{"client", "read"}, "json", format.FormatJSON},
		{"command line wins", []string{"--format", "text"}, "json", format.FormatText},
		{"nothing", []string{"client"}, "", ""},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Setenv(env, c.env)
			inv := FormatFallback(nil, c.args, env)
			if inv == nil {
				t.Fatal("nil Invocation")
			}
			if got := inv.OutputFormat; inv.HasOutputFormat != (c.want != "") || got != c.want {
				t.Errorf("format %q (set %v), want %q", got, inv.HasOutputFormat, c.want)
			}
		})
	}
	// A format a command has set is kept.
	inv := &Invocation{}
	inv.SetOutputFormat(format.FormatTable)
	if got := FormatFallback(inv, []string{"--format", "json"}, env); got != inv || got.OutputFormat != format.FormatTable {
		t.Errorf("the command's format was replaced: %+v", got)
	}
}
