// SPDX-License-Identifier: MIT

package client

import (
	"context"
	"io"

	"github.com/otfabric/iec60870ctl/internal/format"
	"github.com/otfabric/iec60870ctl/internal/iec102ctl/config"
	"github.com/otfabric/iec60870ctl/internal/iec102ctl/meter"
	"github.com/otfabric/iec60870ctl/internal/iec102ctl/types"
	"github.com/spf13/cobra"
)

func newREECmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "ree",
		Args:  cobra.NoArgs,
		RunE:  func(cmd *cobra.Command, _ []string) error { return cmd.Help() },
		Short: "Requests of the Spanish REE profile: session, clock, tariffs, contracted powers",
		Long: `Requests of the profile of IEC 60870-5-102 that Spanish fiscal metering uses (Red
Eléctrica de España). None of them is part of IEC 60870-5-102; their layouts come from
gisce/iec870ree, daiiniel/IEC60870-5-102 and recorded meter traffic, not from an REE
document (see docs/iec102ctl/LIMITATIONS.md). Every command here decodes the REE types, and
runs inside a session when an access key is given (--access-key, --access-key-file or
IEC102CTL_ACCESS_KEY): most REE meters answer only within one.`,
	}
	cmd.AddCommand(newLoginCmd(), newSetTimeCmd(), newTariffsCmd(), newPowersCmd(), newSpecialDaysCmd())
	return cmd
}

func newLoginCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "login",
		Short: "Open and close an REE session: check the access key (C_AC_NA_2, C_FS_NA_2)",
		Long: `Start a session with the access key (C_AC_NA_2, confirmed with cause 7) and end it
again (C_FS_NA_2). Nothing else is sent. A meter that refuses the key (P/N bit) ends the
command with exit code 6 and the code login_refused; a logout that is not confirmed gives
exit code 7.`,
		Example: `
  iec102ctl client ree login --url tcp://10.0.0.20 --access-key 7
  IEC102CTL_ACCESS_KEY=7 iec102ctl client ree login --url serial:///dev/ttyUSB0
`,
	}
	cfg := config.SessionOnlyConfig{ConnConfig: config.Defaults(), OutputFormat: string(format.FormatText)}
	wire(cmd, &cfg)
	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		return run(cmd, &cfg.ConnConfig, cfg.OutputFormat, func() error { return meter.CheckLogin(&cfg) },
			func(ctx context.Context, w io.Writer) (*types.LoginResult, error) { return meter.Login(ctx, &cfg, w) })
	}
	return cmd
}

func newSetTimeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "set-time",
		Short: "Set the meter's clock (C_CS_TA_2)",
		Long: `Send C_CS_TA_2 with the time --time (time b) and wait for the confirmation (cause 7).
"now" (the default) is this computer's time when the link is up, in the zone --timezone
names: it must be the zone the meter keeps its clock in, which also sets the SU bit.

THIS CHANGES THE METER: the integration periods it records follow its clock. The request is
validated before anything is sent and sent once; it is never repeated, also not when its
confirmation does not arrive.`,
		Example: `
  # Synchronize a Spanish meter with this computer
  iec102ctl client ree set-time --url tcp://10.0.0.20 --access-key 7 --timezone Europe/Madrid

  # A given time b
  iec102ctl client ree set-time --url tcp://10.0.0.20 --access-key 7 --time 2026-02-03T04:05:06.789
`,
	}
	cfg := config.SetTimeConfig{ConnConfig: config.Defaults(), Time: "now", OutputFormat: string(format.FormatText)}
	wire(cmd, &cfg)
	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		return run(cmd, &cfg.ConnConfig, cfg.OutputFormat, func() error { return meter.CheckSetTime(&cfg) },
			func(ctx context.Context, w io.Writer) (*types.SetTimeResult, error) {
				return meter.SetTime(ctx, &cfg, w)
			})
	}
	return cmd
}

func newTariffsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "tariffs",
		Short: "Read the tariff information of a contract (C_TA_VC_2, C_TA_VM_2)",
		Long: `Read the billing information of the contract register --register (134-136: contracts
I-III, 137-139: the latent ones): the current values (C_TA_VC_2), or with --from and --to
the stored values of the billing periods that end in the range (C_TA_VM_2). Each record
has the absolute and incremental active and reactive energies, the maximum demand with its
time, the excess power and the billing period.`,
		Example: `
  iec102ctl client ree tariffs --url tcp://10.0.0.20 --access-key 7
  iec102ctl client ree tariffs --url tcp://10.0.0.20 --access-key 7 --from 2025-11-01 --to 2026-01-01 --format json
`,
	}
	cfg := config.TariffsConfig{ConnConfig: config.Defaults(), Register: 134, OutputFormat: string(format.FormatText)}
	wire(cmd, &cfg)
	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		return run(cmd, &cfg.ConnConfig, cfg.OutputFormat, func() error { return meter.CheckTariffs(&cfg) },
			func(ctx context.Context, w io.Writer) (*types.TariffsResult, error) {
				return meter.Tariffs(ctx, &cfg, w)
			})
	}
	return cmd
}

func newPowersCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "contracted-powers",
		Short: "Read the contracted powers (C_PC_NA_2)",
		Long: `Send C_PC_NA_2 and print the contracted power of each tariff period (kW) of the answer
M_PC_NA_2. The meter writes them; iec102ctl has no command to change them, because no
source we hold gives the layout of that request.`,
		Example: `
  iec102ctl client ree contracted-powers --url tcp://10.0.0.20 --access-key 7
`,
	}
	cfg := config.REEReadConfig{ConnConfig: config.Defaults(), OutputFormat: string(format.FormatText)}
	wire(cmd, &cfg)
	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		return run(cmd, &cfg.ConnConfig, cfg.OutputFormat, func() error { return meter.CheckREERead(&cfg) },
			func(ctx context.Context, w io.Writer) (*types.PowersResult, error) {
				return meter.ContractedPowers(ctx, &cfg, w)
			})
	}
	return cmd
}

func newSpecialDaysCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "special-days",
		Short: "Read the special days of the tariff calendar (C_DF_NA_2)",
		Long:  `Send C_DF_NA_2 and print the special days (holidays) of the answer M_DF_NA_2.`,
		Example: `
  iec102ctl client ree special-days --url tcp://10.0.0.20 --access-key 7 --format table
`,
	}
	cfg := config.REEReadConfig{ConnConfig: config.Defaults(), OutputFormat: string(format.FormatText)}
	wire(cmd, &cfg)
	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		return run(cmd, &cfg.ConnConfig, cfg.OutputFormat, func() error { return meter.CheckREERead(&cfg) },
			func(ctx context.Context, w io.Writer) (*types.SpecialDaysResult, error) {
				return meter.SpecialDays(ctx, &cfg, w)
			})
	}
	return cmd
}
