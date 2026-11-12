// SPDX-License-Identifier: MIT

// Package client holds the commands of a controlling station: `iec104ctl client …`.
package client

import (
	"sort"

	"github.com/otfabric/iec104ctl/internal/cli"
	"github.com/otfabric/iec104ctl/internal/config"
	"github.com/otfabric/iec104ctl/internal/iec"
	"github.com/otfabric/iec104ctl/internal/runner"
	"github.com/spf13/cobra"
)

// NewCmd returns the `client` command with all its subcommands.
func NewCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "client",
		Args:  cobra.NoArgs,
		RunE:  func(cmd *cobra.Command, _ []string) error { return cmd.Help() },
		Short: "Act as a controlling station: interrogate, monitor, command and transfer files",
		Long: `Commands that connect to a controlled station (RTU, gateway, IED) as a controlling
station does: open the connection, start data transfer (STARTDT), run one application
function and close the connection again.

Every command takes the station as --url (tcp://host[:port] or tls://host[:port]) or as
--ip with --port, and the common address of the station as --ca. Protocol parameters
(--k, --w, --t0 to --t3) and the ASDU field sizes (--cot-size, --ca-size, --ioa-size)
default to the values of IEC 60870-5-104 and must match the station.

With --secure, every command runs the application-layer security of IEC 62351-5 (the
A-Profile of IEC TS 60870-5-7) after STARTDT: a Station Association with --security-cert,
--security-key and --security-trust, then every ASDU as Secure Data.`,
	}
	cmd.AddCommand(
		newInterrogateCmd(),
		newCountersCmd(),
		newReadCmd(),
		newMonitorCmd(),
		newCommandCmd(),
		newClockCmd(),
		newResetCmd(),
		newPingCmd(),
		newScanCmd(),
		newFileCmd(),
		newTypesCmd(),
	)
	return cmd
}

// connDefaults are the defaults every client command starts from.
func connDefaults() config.ConnConfig {
	c := config.ConnConfig{Port: config.DefaultPort}
	c.AssociationID = 1
	return c
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

// connCompletion completes the flags every client command has.
func connCompletion(cmd *cobra.Command) {
	if cmd.Flags().Lookup("timezone") != nil {
		_ = cli.RegisterEnumFlagCompletion(cmd, "timezone", []string{"utc", "local"})
	}
	for _, f := range []string{"tls-ca", "tls-cert", "tls-key", "security-cert", "security-key", "security-trust"} {
		if cmd.Flags().Lookup(f) != nil {
			_ = cmd.MarkFlagFilename(f, "pem", "crt", "cer", "key")
		}
	}
	sizeCompletion(cmd)
	if cmd.Flags().Lookup("mal") != nil {
		_ = cli.RegisterEnumFlagCompletion(cmd, "mal", iec.MACNames())
		_ = cli.RegisterEnumFlagCompletion(cmd, "dpa", iec.DPANames())
	}
}

// sizeCompletion completes the octet counts of the ASDU fields: the sizes IEC 60870-5-101
// allows, of which IEC 104 uses the largest.
func sizeCompletion(cmd *cobra.Command) {
	_ = cli.RegisterEnumFlagCompletion(cmd, "ca-size", []string{"1", "2"})
	_ = cli.RegisterEnumFlagCompletion(cmd, "cot-size", []string{"1", "2"})
	_ = cli.RegisterEnumFlagCompletion(cmd, "ioa-size", []string{"1", "2", "3"})
}

// typeCompletion completes --type with the names of the type identifications.
func typeCompletion(cmd *cobra.Command) {
	names := iec.TypeNames()
	sort.Strings(names)
	_ = cli.RegisterEnumFlagCompletion(cmd, "type", names)
}

// wire loads cfg from the environment, binds its flags to cmd and registers completions.
func wire(cmd *cobra.Command, cfg any, hooks ...func(*cobra.Command)) {
	runner.RegisterCommandCfg(cmd, cfg, append([]func(*cobra.Command){connCompletion}, hooks...)...)
}
