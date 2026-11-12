// SPDX-License-Identifier: MIT

package client

import (
	"context"

	"github.com/otfabric/iec60870ctl/internal/cli"
	"github.com/otfabric/iec60870ctl/internal/iec103ctl/config"
	"github.com/otfabric/iec60870ctl/internal/iec103ctl/iec"
	"github.com/otfabric/iec60870ctl/internal/iec103ctl/types"
	"github.com/otfabric/iec60870ctl/internal/runner"
	"github.com/spf13/cobra"
)

// partial ends a read answered with invalid data (cause 43) with exit code 7.
var partial = runner.WithSuccessExit(iec.InvalidData)

func newGenericCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "generic",
		Args:  cobra.NoArgs,
		RunE:  func(cmd *cobra.Command, _ []string) error { return cmd.Help() },
		Short: "Generic services: read and write settings and their descriptions",
		Long: `The generic services of clause 7.4.8: the data of the device organised in groups of
entries, each entry with attributes (kinds of description, KOD): its actual value (1), its
description (10), its range (3), its dimension (9) and others. A generic identification
number (GIN) names an entry as GROUP.ENTRY; entry 0 is the group itself.

  headings     the headings of all groups (INF 240)
  group        one attribute of every entry of a group (INF 241)
  directory    every attribute of one entry (INF 243, answered with ASDU 11)
  entry        one attribute of one entry (INF 244)
  read         any generic read, with --inf and a list of identifiers
  interrogate  the general interrogation of generic data (INF 245)
  write        write the value of one entry (INF 250), or stage and abort it

A read the device answers with invalid data (cause 43) is shown, with the generic reply
code of each identifier it could not read, and ends with exit code 7.`,
	}
	cmd.AddCommand(newHeadingsCmd(), newGenericReadCmd(), newEntryCmd(), newGroupCmd(), newDirectoryCmd(), newGenericGICmd(), newGenericWriteCmd())
	return cmd
}

func kodCompletion(cmd *cobra.Command) {
	_ = cli.RegisterEnumFlagCompletion(cmd, "kod", iec.KODNames())
}

func newHeadingsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "headings",
		Short:   "Read the headings of all generic groups (INF 240)",
		Long:    `Read the headings of all groups the device defines (clause 7.4.8.5.1): one data set per group, GIN GROUP.0 with its description.`,
		Example: "\n  iec103ctl client generic headings --url tcp://10.0.0.20\n",
	}
	cfg := config.GenericConfig{ConnConfig: connDefaults(), OutputFormat: stdoutFormat}
	wire(cmd, &cfg)
	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		return formatted(cmd, cfg.OutputFormat, &cfg.Debug,
			func() error { return iec.CheckGeneric(&cfg) },
			func(ctx context.Context) (*types.GenericResult, error) {
				return iec.Headings(ctx, &cfg, cmd.ErrOrStderr())
			}, partial)
	}
	return cmd
}

func newGenericGICmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "interrogate",
		Short: "Run the general interrogation of generic data (INF 245)",
		Long: `Run the general interrogation of generic data (clause 7.4.8.7): the device sends the
data sets it includes in it (normally the actual values of its entries) and ends with INF 245
and cause 10.`,
		Example: "\n  iec103ctl client generic interrogate --url tcp://10.0.0.20 --format table\n",
	}
	cfg := config.GenericConfig{ConnConfig: connDefaults(), OutputFormat: stdoutFormat}
	wire(cmd, &cfg)
	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		return formatted(cmd, cfg.OutputFormat, &cfg.Debug,
			func() error { return iec.CheckGeneric(&cfg) },
			func(ctx context.Context) (*types.GenericResult, error) {
				return iec.GenericGI(ctx, &cfg, cmd.ErrOrStderr())
			})
	}
	return cmd
}

func newGenericReadCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "read",
		Short: "Send a generic read command with any information number and identifiers",
		Long: `Send a generic read command (ASDU 21, cause 42) with the information number --inf
(240 headings, 241 group, 243 directory, 244 entry; table 18) and the generic identifiers
--items, GROUP.ENTRY[:KOD] separated by commas (KOD 1, the actual value, by default), and
collect the answers until the last (CONT = 0). The other generic commands are short forms
of this one.`,
		Example: `
  # The values of two entries and the description of a third
  iec103ctl client generic read --url tcp://10.0.0.20 --inf 244 --items 1.2,1.3,1.4:description
`,
	}
	cfg := config.GenericReadConfig{ConnConfig: connDefaults(), Inf: 244, OutputFormat: stdoutFormat}
	wire(cmd, &cfg, enum("inf", "240", "241", "243", "244"))
	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		return formatted(cmd, cfg.OutputFormat, &cfg.Debug,
			func() error { return iec.CheckGenericRead(&cfg) },
			func(ctx context.Context) (*types.GenericResult, error) {
				return iec.GenericReadItems(ctx, &cfg, cmd.ErrOrStderr())
			}, partial)
	}
	return cmd
}

func newEntryCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "entry",
		Short: "Read one attribute of one entry (INF 244)",
		Long:  `Read the attribute --kod (actual value by default) of the entry --gin (clause 7.4.8.6.2).`,
		Example: `
  iec103ctl client generic entry --url tcp://10.0.0.20 --gin 1.2
  iec103ctl client generic entry --url tcp://10.0.0.20 --gin 1.2 --kod description
`,
	}
	cfg := config.GenericEntryConfig{ConnConfig: connDefaults(), KOD: "actual", OutputFormat: stdoutFormat}
	wire(cmd, &cfg, kodCompletion)
	cli.MustMarkFlagRequired(cmd, "gin")
	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		return formatted(cmd, cfg.OutputFormat, &cfg.Debug,
			func() error { return iec.CheckGenericEntry(&cfg) },
			func(ctx context.Context) (*types.GenericResult, error) {
				return iec.GenericReadEntry(ctx, &cfg, cmd.ErrOrStderr())
			}, partial)
	}
	return cmd
}

func newDirectoryCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "directory",
		Short:   "Read every attribute of one entry (INF 243, ASDU 11)",
		Long:    `Read the directory of the entry --gin (clause 7.4.8.6.1): every attribute the device holds for it, answered with generic identification (ASDU 11).`,
		Example: "\n  iec103ctl client generic directory --url tcp://10.0.0.20 --gin 1.2\n",
	}
	cfg := config.GenericEntryConfig{ConnConfig: connDefaults(), OutputFormat: stdoutFormat}
	wire(cmd, &cfg)
	cli.MustMarkFlagRequired(cmd, "gin")
	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		return formatted(cmd, cfg.OutputFormat, &cfg.Debug,
			func() error { return iec.CheckGenericEntry(&cfg) },
			func(ctx context.Context) (*types.GenericResult, error) {
				return iec.GenericReadDirectory(ctx, &cfg, cmd.ErrOrStderr())
			}, partial)
	}
	return cmd
}

func newGroupCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "group",
		Short:   "Read one attribute of every entry of a group (INF 241)",
		Long:    `Read the attribute --kod (actual value by default) of every entry of the group --group (clause 7.4.8.5.2).`,
		Example: "\n  iec103ctl client generic group --url tcp://10.0.0.20 --group 1 --kod description\n",
	}
	cfg := config.GenericGroupConfig{ConnConfig: connDefaults(), KOD: "actual", OutputFormat: stdoutFormat}
	wire(cmd, &cfg, kodCompletion)
	cli.MustMarkFlagRequired(cmd, "group")
	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		return formatted(cmd, cfg.OutputFormat, &cfg.Debug,
			func() error { return iec.CheckGenericGroup(&cfg) },
			func(ctx context.Context) (*types.GenericResult, error) {
				return iec.GenericReadGroup(ctx, &cfg, cmd.ErrOrStderr())
			}, partial)
	}
	return cmd
}

func newGenericWriteCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "write",
		Short: "Write the value of one entry (INF 250), or stage it and abort",
		Long: `Write one data set to the device (ASDU 10, cause 40, clause 7.4.8.6.3, figure 17): the
attribute --kod (the actual value by default) of the entry --gin, with the data type --type
(ascii, uint, int, float32 or float64), the size --size in octets (uint and int: 1-8) and
the value --value.

  --mode execute  write with execution (INF 250): the device checks the value and activates
                  it, answering with cause 40, or refuses it with cause 41 and a generic
                  reply code (exit code 6). This is the default.
  --mode abort    write entry (INF 248), then write abort (INF 251): the device takes the
                  value and drops it again; nothing is activated. It shows how the device
                  handles the two steps.

THIS CHANGES THE DEVICE: a setting written with execution is in force at once, on a
protection relay a pick-up value or a delay. Everything is validated before anything is
sent, and the write is sent exactly once.`,
		Example: `
  # Pick-up current 1.25 (an R32.23 setting)
  iec103ctl client generic write --url tcp://10.0.0.20 --gin 1.2 --type float32 --value 1.25

  # A delay of 450 as an unsigned integer of two octets
  iec103ctl client generic write --url tcp://10.0.0.20 --gin 1.3 --type uint --size 2 --value 450
`,
	}
	cfg := config.GenericWriteConfig{ConnConfig: connDefaults(), KOD: "actual", Mode: iec.WriteExecute, OutputFormat: stdoutFormat}
	wire(cmd, &cfg, kodCompletion, enum("type", iec.WriteTypes()...), described("mode", iec.WriteModes()))
	for _, f := range []string{"gin", "type", "value"} {
		cli.MustMarkFlagRequired(cmd, f)
	}
	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		return formatted(cmd, cfg.OutputFormat, &cfg.Debug,
			func() error { return iec.CheckGenericWrite(&cfg) },
			func(ctx context.Context) (*types.GenericResult, error) {
				return iec.GenericWriteSet(ctx, &cfg, cmd.ErrOrStderr())
			})
	}
	return cmd
}
