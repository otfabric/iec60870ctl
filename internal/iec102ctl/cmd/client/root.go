// SPDX-License-Identifier: MIT

// Package client holds the commands of a controlling station: `iec102ctl client …`.
package client

import (
	"context"
	"io"

	"github.com/otfabric/iec60870ctl/internal/cli"
	"github.com/otfabric/iec60870ctl/internal/errs"
	"github.com/otfabric/iec60870ctl/internal/format"
	"github.com/otfabric/iec60870ctl/internal/iec102ctl/config"
	"github.com/otfabric/iec60870ctl/internal/iec102ctl/meter"
	"github.com/otfabric/iec60870ctl/internal/iec102ctl/types"
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
		Short: "Act as a controlling station: read a meter's totals, events, clock and identity",
		Long: `Commands that read an integrated total station (an electricity meter or a data
concentrator) as a controlling station does: open the line, start the link (request status
of link, reset of remote link), run one request, poll class 2 data until its answer is
complete, and close the line again.

The line is --url tcp://host[:port] (a terminal server or serial device server that passes
the FT 1.2 byte stream through; port 2401 by default) or serial:///dev/ttyUSB0 with --baud,
--data-bits, --parity and --stop-bits; or --ip with --port. The link address (--link-address,
--link-address-size) and the measuring point address (--common-address, --ca-size) must
match the meter: the defaults are 1, with two octets each.

With --access-key (or --access-key-file, or IEC102CTL_ACCESS_KEY) a command runs inside a
session of the Spanish REE profile: C_AC_NA_2 with the key before, C_FS_NA_2 after.
--profile ree decodes the REE types without a session.`,
	}
	cmd.AddCommand(
		newInfoCmd(),
		newTimeCmd(),
		newTotalsCmd(),
		newEventsCmd(),
		newRequestCmd(),
		newREECmd(),
		newTypesCmd(),
	)
	return cmd
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
	for flag, values := range map[string][]string{
		"timezone": {"utc", "local"}, "parity": line.ParityValues(), "link-address-size": {"0", "1", "2"},
		"ca-size": {"1", "2"}, "profile": {config.ProfileStandard, config.ProfileREE},
	} {
		if cmd.Flags().Lookup(flag) != nil {
			_ = cli.RegisterEnumFlagCompletion(cmd, flag, values)
		}
	}
}

// wire loads cfg from the environment, binds its flags to cmd and registers completions.
func wire(cmd *cobra.Command, cfg any, hooks ...func(*cobra.Command)) {
	runner.RegisterCommandCfg(cmd, cfg, append([]func(*cobra.Command){connCompletion}, hooks...)...)
}

// sessionExit is the exit code of a result: 7 (partial) when the operation succeeded but its
// REE session did not end with a confirmed logout.
func sessionExit(m func(any) types.Meter) runner.FormatRunOption {
	return runner.WithSuccessExit(func(v any) int {
		if meter.SessionPartial(m(v)) {
			return errs.ExitPartial
		}
		return errs.ExitOK
	})
}

// meterOf returns the meter of any result.
func meterOf(v any) types.Meter {
	switch r := v.(type) {
	case *types.InfoResult:
		return r.Meter
	case *types.TimeResult:
		return r.Meter
	case *types.TotalsResult:
		return r.Meter
	case *types.EventsResult:
		return r.Meter
	case *types.RequestResult:
		return r.Meter
	case *types.LoginResult:
		return r.Meter
	case *types.SetTimeResult:
		return r.Meter
	case *types.TariffsResult:
		return r.Meter
	case *types.PowersResult:
		return r.Meter
	case *types.SpecialDaysResult:
		return r.Meter
	}
	return types.Meter{}
}

// run is the run of a client command: validate, collect, write the result.
func run[R any](cmd *cobra.Command, conn *config.ConnConfig, outputFormat string, check func() error, collect func(context.Context, io.Writer) (R, error)) error {
	conn.Debug = cli.Debug(cmd)
	return runner.RunFormattedChecked(cmd, outputFormat, check,
		func(ctx context.Context) (any, error) { return collect(ctx, cmd.ErrOrStderr()) },
		sessionExit(meterOf))
}

func newInfoCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "info",
		Short: "Read the manufacturer and product specification (C_RD_NA_2)",
		Long: `Send C_RD_NA_2 and print the answer P_MP_NA_2: the first octet (the edition of the
standard), the manufacturer code and the four-octet product code. The layout is that of
gisce/iec870ree; no source we hold gives its meaning beyond the field names.`,
		Example: `
  # The meter behind a terminal server
  iec102ctl client info --url tcp://10.0.0.20:2401

  # On a serial line, link address 3
  iec102ctl client info --url serial:///dev/ttyUSB0 --baud 9600 --link-address 3 --format json
`,
	}
	cfg := config.InfoConfig{ConnConfig: config.Defaults(), OutputFormat: string(format.FormatText)}
	wire(cmd, &cfg)
	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		return run(cmd, &cfg.ConnConfig, cfg.OutputFormat, func() error { return meter.CheckInfo(&cfg) },
			func(ctx context.Context, w io.Writer) (*types.InfoResult, error) { return meter.Info(ctx, &cfg, w) })
	}
	return cmd
}

func newTimeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "time",
		Short: "Read the meter's clock (C_TI_NA_2)",
		Long: `Send C_TI_NA_2 and print the answer M_TI_TA_2: the meter's clock as time b (its local
time, with the SU and IV bits), and how far it is from this computer's clock when read in
the zone --timezone names.`,
		Example: `
  iec102ctl client time --url tcp://10.0.0.20 --timezone Europe/Madrid
`,
	}
	cfg := config.TimeConfig{ConnConfig: config.Defaults(), OutputFormat: string(format.FormatText)}
	wire(cmd, &cfg)
	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		return run(cmd, &cfg.ConnConfig, cfg.OutputFormat, func() error { return meter.CheckTime(&cfg) },
			func(ctx context.Context, w io.Writer) (*types.TimeResult, error) { return meter.Time(ctx, &cfg, w) })
	}
	return cmd
}

func newTotalsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "totals",
		Short: "Read integrated totals of a record (C_CI_NA_2 .. C_CI_NU_2)",
		Long: `Read the integrated totals (meter readings) of the record --record and print every
integration period the meter sends, with the end of the period (time a) and each total's
address, value and quality descriptor.

--kind chooses the totals: operational (absolute values, the default), reset-operational
(the increments of each period), accounting or reset-accounting. What is selected chooses
the read command:

  --from T --to T     the periods that end in the time range, totals --first..--last
                      (C_CI_NR_2, NS_2, NT_2, NU_2)
  --period T          the one past period that ends at T (C_CI_NC_2, NG_2, NL_2, NP_2),
                      with --first/--last an address range of it (ND_2, NH_2, NM_2, NQ_2)
  --oldest            the oldest period (C_CI_NA_2, NE_2, NI_2, NN_2), with --first/--last
                      an address range of it (NB_2, NF_2, NK_2, NO_2)

--type names the read command directly. Times are the meter's local time (2026-01-31T13:00,
2026-01-31) in the zone of --timezone, which also sets the SU bit.

The meter confirms the request (cause 7), sends the periods (cause 5) and terminates it
(cause 10). A refusal ends with exit code 6 and the cause: no_period (18), no_object (17),
unknown_record_address (16), unknown_type (14).`,
		Example: `
  # The hourly load profile of 31 January, absolute values
  iec102ctl client totals --url tcp://10.0.0.20 --record 11 --from 2026-01-31T01:00 --to 2026-02-01T00:00

  # Increments, active energy only, in an REE session
  iec102ctl client totals --url tcp://10.0.0.20 --kind incremental --first 1 --last 1 \
      --from 2026-01-31 --to 2026-02-01 --access-key 7 --format table

  # The oldest accounting period
  iec102ctl client totals --url tcp://10.0.0.20 --record 1 --kind accounting --oldest
`,
	}
	cfg := config.TotalsConfig{ConnConfig: config.Defaults(), Record: 11, Kind: "operational", First: 1, Last: 8, OutputFormat: string(format.FormatText)}
	wire(cmd, &cfg, described("kind", meter.KindValues()))
	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		cfg.AddressRange = cfg.AddressRange || cmd.Flags().Changed("first") || cmd.Flags().Changed("last")
		return run(cmd, &cfg.ConnConfig, cfg.OutputFormat, func() error { return meter.CheckTotals(&cfg) },
			func(ctx context.Context, w io.Writer) (*types.TotalsResult, error) { return meter.Totals(ctx, &cfg, w) })
	}
	return cmd
}

func newEventsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "events",
		Short: "Read single-point information with time tag (C_SP_NA_2, C_SP_NB_2)",
		Long: `Read the list of single-point information (events) of the record --record: all of it
with C_SP_NA_2, or the events of a time range with C_SP_NB_2 (--from and --to). Each event
has its address, state (on or off), qualifier and time b.`,
		Example: `
  iec102ctl client events --url tcp://10.0.0.20 --record 52
  iec102ctl client events --url tcp://10.0.0.20 --record 52 --from 2026-01-01 --to 2026-02-01 --format json
`,
	}
	cfg := config.EventsConfig{ConnConfig: config.Defaults(), Record: 51, OutputFormat: string(format.FormatText)}
	wire(cmd, &cfg)
	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		return run(cmd, &cfg.ConnConfig, cfg.OutputFormat, func() error { return meter.CheckEvents(&cfg) },
			func(ctx context.Context, w io.Writer) (*types.EventsResult, error) { return meter.Events(ctx, &cfg, w) })
	}
	return cmd
}

func newRequestCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "request",
		Short: "Send one ASDU as given and print the answers (a type without a command)",
		Long: `Send one ASDU built from --type, --cause, --record and --data (the information objects
as hex) and print every ASDU the meter answers until the request is complete:

  --until reply         the first answer that is not the mirror of the request
  --until confirmation  the activation confirmation (cause 7)
  --until termination   the activation termination (cause 10)
  --until auto          termination for cause activation, else reply (the default)

A mirror with a cause of refusal (13..18) or the P/N bit ends the request with exit code 6.
Answers are decoded with --profile; their information objects are shown as hex.

THIS MAY CHANGE THE METER: the type decides what the request does (C_MR_NA_2 changes the
configuration of an REE meter). It is validated before anything is sent and sent once:
iec102ctl never repeats it (the link layer repeats an unanswered frame with the same frame
count bit, which the meter takes as a repetition).`,
		Example: `
  # The configuration of an REE meter (C_RM_NA_2), answered by M_RM_NA_2
  iec102ctl client request --url tcp://10.0.0.20 --type C_RM_NA_2 --access-key 7

  # The clock read written out: C_TI_NA_2 with cause request, complete at the first reply
  iec102ctl client request --url tcp://10.0.0.20 --type 103 --cause request --format json
`,
	}
	cfg := config.RequestConfig{ConnConfig: config.Defaults(), Cause: "activation", Objects: -1, Until: meter.UntilAuto, OutputFormat: string(format.FormatText)}
	wire(cmd, &cfg, described("until", meter.UntilValues()), enum("cause", "activation", "request", "deactivation"))
	cli.MustMarkFlagRequired(cmd, "type")
	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		return run(cmd, &cfg.ConnConfig, cfg.OutputFormat, func() error { return meter.CheckRequest(&cfg) },
			func(ctx context.Context, w io.Writer) (*types.RequestResult, error) {
				return meter.Request(ctx, &cfg, w)
			})
	}
	return cmd
}

func newTypesCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "types",
		Short: "List the type identifications of IEC 102 and the REE profile",
		Long: `List the type identifications iec102ctl knows by name: those of IEC 60870-5-102 (as the
Beckhoff TS650x manual lists them) and those the Spanish REE profile assigns in the private
range. "decoded" says whether iec102ctl shows a type's objects as fields; the others are
shown as hex. Nothing is sent.`,
		Example: `
  iec102ctl client types --profile ree --direction control
`,
	}
	cfg := config.TypesConfig{OutputFormat: string(format.FormatText)}
	runner.RegisterCommandCfg(cmd, &cfg, enum("direction", "monitor", "control"), enum("profile", config.ProfileStandard, config.ProfileREE))
	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		return runner.RunFormattedChecked(cmd, cfg.OutputFormat, func() error { _, err := meter.Types(&cfg); return err },
			func(context.Context) (any, error) { return meter.Types(&cfg) })
	}
	return cmd
}
