// SPDX-License-Identifier: MIT

package runner

import (
	"github.com/otfabric/iec104ctl/internal/cli"
	"github.com/otfabric/iec104ctl/internal/format"
	"github.com/spf13/cobra"
)

// RegisterStdoutFormatCompletion registers shell completion for --format (text, json, table) on client stdout commands.
func RegisterStdoutFormatCompletion(cmd *cobra.Command) {
	_ = cli.RegisterEnumFlagCompletionWithDescriptions(cmd, "format", format.ValueDescriptions())
}
