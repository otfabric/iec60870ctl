// SPDX-License-Identifier: MIT

package cli

import (
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// FlagAliases makes every flag of root and its subcommands also answer to another name:
// aliases maps the alias to the flag's own name. The tools of this repository name some
// flags differently (iec101ctl and iec104ctl --ca, iec102ctl and iec103ctl
// --common-address); an alias lets the same command line work with each of them. The
// help and the environment variables keep the flag's own name. Call it after the
// subcommands are added.
func FlagAliases(root *cobra.Command, aliases map[string]string) {
	root.SetGlobalNormalizationFunc(func(_ *pflag.FlagSet, name string) pflag.NormalizedName {
		if n, ok := aliases[name]; ok {
			return pflag.NormalizedName(n)
		}
		return pflag.NormalizedName(name)
	})
}
