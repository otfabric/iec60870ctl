// SPDX-License-Identifier: MIT

// Package client holds the commands of a controlling station: `iec101ctl client …`.
package client

import (
	"sort"

	"github.com/otfabric/iec60870ctl/internal/cli"
	"github.com/otfabric/iec60870ctl/internal/iec101ctl/config"
	"github.com/otfabric/iec60870ctl/internal/iec101ctl/iec"
	"github.com/otfabric/iec60870ctl/internal/line"
	"github.com/otfabric/iec60870ctl/internal/runner"
	"github.com/spf13/cobra"
)

// NewCmd returns the `client` command with all its subcommands.
func NewCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "client",
		Args:  cobra.NoArgs,
		RunE:  func(cmd *cobra.Command, _ []string) error { return cmd.Help() },
		Short: "Act as a controlling station: interrogate, monitor and command a station",
		Long: `Commands that talk to a controlled station (RTU, gateway, IED) as a controlling
station does: open the line, bring up the link (request status of link, reset of remote
link), run one application function and close the line again.

The line is --url tcp://host[:port] (a terminal server, port 2401 by default) or
serial:///dev/ttyUSB0 with --baud, --data-bits, --parity and --stop-bits, or --ip with
--port for a terminal server. The station is addressed by its link address
(--link-address) and its common address (--ca).

IEC 60870-5-101 leaves the system parameters to the system: the link mode (--link-mode
unbalanced, where this station polls, or balanced), the size of the link address field
(--link-address-size), of the cause of transmission (--cot-size), of the common address
(--ca-size) and of the information object address (--ioa-size). Nothing on the wire says
which a station uses: they must match the station. The defaults are unbalanced, 1, 1, 1
and 2 octets.`,
	}
	cmd.AddCommand(
		newInterrogateCmd(),
		newCountersCmd(),
		newReadCmd(),
		newMonitorCmd(),
		newCommandCmd(),
		newClockCmd(),
		newResetCmd(),
		newDelayCmd(),
		newPingCmd(),
		newScanCmd(),
		newTypesCmd(),
	)
	return cmd
}

// connDefaults are the defaults every client command starts from.
func connDefaults() config.ConnConfig {
	return config.ConnConfig{LinkConfig: config.DefaultLink()}
}

func stationDefaults() config.StationConfig {
	return config.StationConfig{ConnConfig: connDefaults(), CommonAddr: 1}
}

// enum registers shell completion for a flag with a fixed set of values.
func enum(flag string, values ...string) func(*cobra.Command) {
	return func(cmd *cobra.Command) { _ = cli.RegisterEnumFlagCompletion(cmd, flag, values) }
}

// described registers shell completion for a flag whose values carry a description.
func described(flag string, values map[string]string) func(*cobra.Command) {
	return func(cmd *cobra.Command) { _ = cli.RegisterEnumFlagCompletionWithDescriptions(cmd, flag, values) }
}

// LinkCompletion completes the line and link flags a command has.
func LinkCompletion(cmd *cobra.Command) {
	for flag, values := range map[string][]string{
		"timezone":          {"utc", "local"},
		"link-mode":         {config.ModeUnbalanced, config.ModeBalanced},
		"link-address-size": {"0", "1", "2"},
		"cot-size":          {"1", "2"},
		"ca-size":           {"1", "2"},
		"ioa-size":          {"1", "2", "3"},
		"parity":            line.ParityValues(),
		"stop-bits":         {"1", "2"},
		"data-bits":         {"7", "8"},
	} {
		if cmd.Flags().Lookup(flag) != nil {
			_ = cli.RegisterEnumFlagCompletion(cmd, flag, values)
		}
	}
}

// typeCompletion completes --type with the names of the type identifications.
func typeCompletion(cmd *cobra.Command) {
	names := iec.TypeNames()
	sort.Strings(names)
	_ = cli.RegisterEnumFlagCompletion(cmd, "type", names)
}

// wire loads cfg from the environment, binds its flags to cmd and registers completions.
func wire(cmd *cobra.Command, cfg any, hooks ...func(*cobra.Command)) {
	runner.RegisterCommandCfg(cmd, cfg, append([]func(*cobra.Command){LinkCompletion}, hooks...)...)
}
