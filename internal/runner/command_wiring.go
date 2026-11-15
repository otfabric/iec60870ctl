// SPDX-License-Identifier: MIT

package runner

import (
	"github.com/otfabric/iec60870ctl/internal/envflag"
	"github.com/spf13/cobra"
)

// RegisterCommandCfg runs [envflag.LoadAndRegister] (the environment, then the flags) and
// [RegisterStdoutFormatCompletion] (when the command has --format), then the optional hooks
// (extra completions, required flags, etc.).
func RegisterCommandCfg(cmd *cobra.Command, cfg any, hooks ...func(*cobra.Command)) {
	envflag.LoadAndRegister(cmd, cfg)
	if cmd.Flags().Lookup("format") != nil {
		RegisterStdoutFormatCompletion(cmd)
	}
	for _, h := range hooks {
		if h != nil {
			h(cmd)
		}
	}
}
