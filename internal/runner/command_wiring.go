// SPDX-License-Identifier: MIT

package runner

import (
	"github.com/otfabric/iec104ctl/internal/config"
	"github.com/spf13/cobra"
)

// RegisterCommandCfg runs [config.MustLoadFromEnv], [config.RegisterFlags] and
// [RegisterStdoutFormatCompletion] (when the command has --format), then the optional hooks
// (extra completions, required flags, etc.).
func RegisterCommandCfg(cmd *cobra.Command, cfg any, hooks ...func(*cobra.Command)) {
	config.MustLoadFromEnv(cfg)
	config.RegisterFlags(cmd, cfg)
	if cmd.Flags().Lookup("format") != nil {
		RegisterStdoutFormatCompletion(cmd)
	}
	for _, h := range hooks {
		if h != nil {
			h(cmd)
		}
	}
}
