// SPDX-License-Identifier: MIT

package runner

import (
	"strings"

	"github.com/otfabric/iec60870ctl/internal/envflag"
	"github.com/otfabric/iec60870ctl/internal/format"
)

// FormatFallback returns inv, or a new Invocation when inv is nil, with the output format
// set from args or the variable env when no command has set one. An error that Cobra or the
// environment raises before a command runs (a required flag that is missing, a variable that
// does not parse, an unknown flag) is then rendered in the format the command line asked
// for: with --format json, one error object on stdout, as docs/json-output.md promises.
//
// It reads the last --format of args before a "--", in the forms "--format json" and
// "--format=json", and else env. A value that is not a format leaves inv as it is: the
// error is then rendered as text.
func FormatFallback(inv *Invocation, args []string, env string) *Invocation {
	if inv == nil {
		inv = &Invocation{}
	}
	if inv.HasOutputFormat {
		return inv
	}
	value, found := "", false
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			break
		}
		switch {
		case a == "--format" && i+1 < len(args):
			value, found = args[i+1], true
			i++
		case strings.HasPrefix(a, "--format="):
			value, found = strings.TrimPrefix(a, "--format="), true
		}
	}
	if !found {
		value = envflag.Getenv(env)
	}
	if strings.TrimSpace(value) == "" {
		return inv
	}
	if f, err := format.Parse(value); err == nil {
		inv.SetOutputFormat(f)
	}
	return inv
}
