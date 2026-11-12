// SPDX-License-Identifier: MIT

package cmd

import (
	"regexp"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// valueSet matches a flag usage that lists the values the flag takes: "text|json",
// "one of …", or "Output format: text, json, or table".
var valueSet = regexp.MustCompile(`(?i)\b[\w.-]+\|[\w.-]+\b|\bone of\b|: [\w.-]+, [\w.-]+,? or [\w.-]+`)

// Every flag that takes a value completes: to its values, to file names, or to nothing
// (but not to the file names a shell offers by default); a flag whose usage lists its
// values completes to them; and every command completes its arguments.
func TestEveryFlagCompletes(t *testing.T) {
	var walk func(c *cobra.Command)
	walk = func(c *cobra.Command) {
		if c.Name() == "help" || strings.HasPrefix(c.Name(), "__") {
			return
		}
		if c.ValidArgsFunction == nil && len(c.ValidArgs) == 0 {
			t.Errorf("%s: the arguments do not complete", c.CommandPath())
		}
		c.LocalFlags().VisitAll(func(f *pflag.Flag) {
			if typ := f.Value.Type(); typ == "bool" || typ == "count" {
				return
			}
			_, fn := c.GetFlagCompletionFunc(f.Name)
			_, files := f.Annotations[cobra.BashCompFilenameExt]
			if !fn && !files {
				t.Errorf("%s --%s does not complete", c.CommandPath(), f.Name)
				return
			}
			if !valueSet.MatchString(f.Usage) {
				return
			}
			args := append(strings.Fields(c.CommandPath())[1:], "--"+f.Name, "")
			r := run(t, append([]string{cobra.ShellCompNoDescRequestCmd}, args...)...)
			if lines := strings.Split(strings.TrimSpace(r.stdout), "\n"); len(lines) < 2 {
				t.Errorf("%s --%s lists values (%q) but completes none", c.CommandPath(), f.Name, f.Usage)
			}
		})
		for _, sub := range c.Commands() {
			walk(sub)
		}
	}
	walk(NewRootCmd())
}

// The completion command offers the four shells, and a free value completes no file names.
func TestCompletionShellsAndFreeValues(t *testing.T) {
	r := run(t, cobra.ShellCompNoDescRequestCmd, "completion", "")
	for _, sh := range []string{"bash", "zsh", "fish", "powershell"} {
		if !strings.Contains("\n"+r.stdout, "\n"+sh+"\n") {
			t.Errorf("completion offers no %s:\n%s", sh, r.stdout)
		}
	}
	r = run(t, cobra.ShellCompNoDescRequestCmd, "version", "")
	if !strings.HasSuffix(strings.TrimSpace(r.stdout), ":4") {
		t.Errorf("version arguments complete file names:\n%s", r.stdout)
	}
}
