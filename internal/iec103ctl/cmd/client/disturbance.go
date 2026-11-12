// SPDX-License-Identifier: MIT

package client

import (
	"context"

	"github.com/otfabric/iec60870ctl/internal/cli"
	"github.com/otfabric/iec60870ctl/internal/iec103ctl/config"
	"github.com/otfabric/iec60870ctl/internal/iec103ctl/iec"
	"github.com/otfabric/iec60870ctl/internal/iec103ctl/types"
	"github.com/spf13/cobra"
)

func newDisturbanceCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "disturbance",
		Aliases: []string{"fault"},
		Args:    cobra.NoArgs,
		RunE:    func(cmd *cobra.Command, _ []string) error { return cmd.Help() },
		Short:   "Disturbance data: list the recorded faults and transfer one",
		Long: `The transmission of disturbance data of clause 7.4.7: a protection device records the
analogue channels (currents, voltages) and the binary signals (tags) around a fault.

  list  the list of recorded disturbances (ASDU 23): fault number (FAN), status and time
  get   the transfer of one record: its description, tags and channels with every value

The orders carry the function type --fun; by default the one the device identifies with
(ASDU 5 after the reset of the link).`,
	}
	cmd.AddCommand(newDisturbanceListCmd(), newDisturbanceGetCmd())
	return cmd
}

func newDisturbanceListCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List the recorded disturbances (ASDU 24 TOO 24, answered by ASDU 23)",
		Long: `Request the list of recorded disturbances (ASDU 24 with TOO 24) and show the faults of
the ASDU 23 the device answers with: at most eight, each with its fault number (FAN), its
status (TP with trip, TM being transmitted, TEST recorded in test mode, OTEV started by
another event than a pick-up) and its time.`,
		Example: "\n  iec103ctl client disturbance list --url tcp://10.0.0.20\n",
	}
	cfg := config.DisturbanceListConfig{ConnConfig: connDefaults(), OutputFormat: stdoutFormat}
	wire(cmd, &cfg)
	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		return formatted(cmd, cfg.OutputFormat, &cfg.Debug,
			func() error { return iec.CheckDisturbanceList(&cfg) },
			func(ctx context.Context) (*types.DisturbanceListResult, error) {
				return iec.DisturbanceList(ctx, &cfg, cmd.ErrOrStderr())
			})
	}
	return cmd
}

func newDisturbanceGetCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "get",
		Short: "Transfer one recorded disturbance with its tags and channels",
		Long: `Transfer the recorded disturbance --fan with the procedure of clause 7.4.7 (figure 10):
selection of the fault, request of the disturbance data, the tags, every channel the device
offers, and the acknowledgement of each part.

The result has the description (NOF, the number of channels and of values, the interval
between values in microseconds, the time of the first value), the tags (the state of each
binary signal from a value number on) and every channel with its rated values and all its
values (SDV, the signed integers as transmitted). The text and table forms summarize the
channels; --format json carries every value. --output writes the record as JSON to a file,
--csv the values as CSV: one row per value number with the time from the first value in
microseconds and one column per channel. No other format (COMTRADE among them) is written.

A transfer takes a while on a serial line: --transfer-timeout bounds it (2 minutes by
default). The final positive acknowledgement allows the device to delete the record.`,
		Example: `
  iec103ctl client disturbance get --url tcp://10.0.0.20 --fan 7
  iec103ctl client disturbance get --url serial:///dev/ttyUSB0 --fan 7 --output fault7.json --csv fault7.csv
`,
	}
	cfg := config.DisturbanceGetConfig{ConnConfig: connDefaults(), OutputFormat: stdoutFormat}
	wire(cmd, &cfg)
	cli.MustMarkFlagRequired(cmd, "fan")
	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		return formatted(cmd, cfg.OutputFormat, &cfg.Debug,
			func() error { return iec.CheckDisturbanceGet(&cfg) },
			func(ctx context.Context) (*types.DisturbanceResult, error) {
				return iec.DisturbanceGet(ctx, &cfg, cmd.ErrOrStderr())
			})
	}
	return cmd
}
