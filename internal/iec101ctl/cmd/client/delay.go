// SPDX-License-Identifier: MIT

package client

import (
	"context"

	"github.com/otfabric/iec60870ctl/internal/cli"
	"github.com/otfabric/iec60870ctl/internal/format"
	"github.com/otfabric/iec60870ctl/internal/iec101ctl/config"
	"github.com/otfabric/iec60870ctl/internal/iec101ctl/iec"
	"github.com/otfabric/iec60870ctl/internal/runner"
	"github.com/spf13/cobra"
)

func newDelayCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "delay",
		Short: "Measure the transmission delay to a station, or set it (C_CD_NA_1)",
		Long: `Run the delay acquisition procedure of IEC 60870-5-101 (7.4.12), with which a
controlling station makes clock synchronization exact on a slow line.

Without --set the transmission delay is measured: C_CD_NA_1 with cause activation carries
the time of sending, the station's confirmation that time plus its reaction time, and the
one-way delay is half of the round trip less the reaction time. The formula is that of
IEC 60870-5-5 as go-iec60870-5 interprets it.

  --set auto   measures, then sends the measured delay to the station (C_CD_NA_1 with
               cause spontaneous)
  --set 120    sends 120 ms to the station without measuring (0-59999)

THIS CHANGES THE STATION when --set is given: it corrects every following clock
synchronization by the delay. Nothing is answered to a set delay: the command succeeds
when the link layer delivered it.`,
		Example: `
  # Measure the transmission delay to station 1
  iec101ctl client delay --url serial:///dev/ttyUSB0 --baud 1200

  # Measure it and give it to the station, then synchronize its clock
  iec101ctl client delay --url serial:///dev/ttyUSB0 --baud 1200 --set auto
  iec101ctl client clock --url serial:///dev/ttyUSB0 --baud 1200
`,
	}
	cfg := config.DelayConfig{StationConfig: stationDefaults(), OutputFormat: string(format.FormatText)}
	wire(cmd, &cfg, enum("set", "auto"))
	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		cfg.Debug = cli.Debug(cmd)
		return runner.RunFormattedChecked(cmd, cfg.OutputFormat,
			func() error { return iec.CheckDelay(&cfg) },
			func(ctx context.Context) (any, error) { return iec.Delay(ctx, &cfg, cmd.ErrOrStderr()) })
	}
	return cmd
}
