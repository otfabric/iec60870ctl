// SPDX-License-Identifier: MIT

// Package client holds the commands of a controlling system: `iec103ctl client …`.
package client

import (
	"context"

	"github.com/otfabric/iec60870ctl/internal/cli"
	"github.com/otfabric/iec60870ctl/internal/format"
	"github.com/otfabric/iec60870ctl/internal/iec103ctl/config"
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
		Short: "Act as the controlling system: interrogate, command, read settings and fault records",
		Long: `Commands that poll one item of protection equipment as a controlling system does
(the primary station of unbalanced transmission, IEC 60870-5-103 clause 6.2.3): open the
line, initialize the link (request status of link, reset of the communication unit), run
one application function and close the line again.

The line is --url: tcp://host[:port] for a terminal server (port 2401 by default), or
serial:///dev/ttyUSB0 (serial://COM3 on Windows) with --baud, --data-bits, --parity and
--stop-bits (FT 1.2: 9600 baud, 8 data bits, even parity, 1 stop bit). --ip and --port
are the short form of a TCP line. --link-address is the device's link address and
--common-address the common address of ASDU (normally the same, the default).

The link start-up is not followed by the time synchronization and the general
interrogation of clause 7.4.1: every command does only what it is asked to.`,
	}
	cmd.AddCommand(
		newInterrogateCmd(),
		newCommandCmd(),
		newClockCmd(),
		newIdentifyCmd(),
		newGenericCmd(),
		newDisturbanceCmd(),
		newMonitorCmd(),
		newScanCmd(),
	)
	return cmd
}

// connDefaults are the defaults every client command starts from.
func connDefaults() config.ConnConfig {
	c := config.ConnConfig{}
	c.LinkAddress = 1
	c.Retries = 3
	c.Parity = "even"
	return c
}

// enum registers shell completion for a flag with a fixed set of values.
func enum(flag string, values ...string) func(*cobra.Command) {
	return func(cmd *cobra.Command) { _ = cli.RegisterEnumFlagCompletion(cmd, flag, values) }
}

// described registers shell completion for a flag whose values carry a description.
func described(flag string, values map[string]string) func(*cobra.Command) {
	return func(cmd *cobra.Command) { _ = cli.RegisterEnumFlagCompletionWithDescriptions(cmd, flag, values) }
}

// connCompletion completes the flags every client command has.
func connCompletion(cmd *cobra.Command) {
	_ = cli.RegisterEnumFlagCompletion(cmd, "timezone", []string{"utc", "local"})
	_ = cli.RegisterEnumFlagCompletion(cmd, "parity", line.ParityValues())
	_ = cli.RegisterEnumFlagCompletion(cmd, "baud", []string{"9600", "19200"})
	_ = cli.RegisterEnumFlagCompletion(cmd, "stop-bits", []string{"1", "2"})
}

// wire loads cfg from the environment, binds its flags to cmd and registers completions.
func wire(cmd *cobra.Command, cfg any, hooks ...func(*cobra.Command)) {
	runner.RegisterCommandCfg(cmd, cfg, append([]func(*cobra.Command){connCompletion}, hooks...)...)
}

// formatted runs a command that writes one result.
func formatted[T any](cmd *cobra.Command, outputFormat string, debug *bool, validate func() error, run func(context.Context) (*T, error), opts ...runner.FormatRunOption) error {
	*debug = cli.Debug(cmd)
	return runner.RunFormattedChecked(cmd, outputFormat, validate,
		func(ctx context.Context) (any, error) { return run(ctx) }, opts...)
}

// stdoutFormat is the default of --format.
var stdoutFormat = string(format.FormatText)
