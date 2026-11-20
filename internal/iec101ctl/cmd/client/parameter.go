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

func newParameterCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "parameter",
		Aliases: []string{"param"},
		Short:   "Load a parameter of a measured value, or activate parameters (P_ME_NA/NB/NC_1, P_AC_NA_1)",
		Long: `Load a parameter of measured value (a threshold, smoothing factor or transmission limit
of the measured value at --ioa) or send a parameter activation, and wait for the activation
confirmation of the station (IEC 60870-5-101:2003 7.4.9, 7.4.10).

  --type normalized  P_ME_NA_1  --value -1 … <1
  --type scaled      P_ME_NB_1  --value -32768 … 32767
  --type float       P_ME_NC_1  --value any number
  --type activation  P_AC_NA_1  no --value

--qualifier is the kind of parameter of measured value:

  threshold   threshold value
  smoothing   smoothing factor (filter time constant)
  low-limit   low limit for the transmission of measured values
  high-limit  high limit for the transmission of measured values

or, for an activation, what is activated:

  loaded  the previously loaded parameters (--ioa 0)
  object  the parameter of the object at --ioa
  cyclic  persistent cyclic or periodic transmission of the object at --ioa

THIS CHANGES THE STATION: a threshold or a limit decides which changes of a measured
value it reports. Everything is validated before anything is sent, and the parameter is
sent exactly once: it is never repeated, also not when its confirmation does not arrive.
--type, --ioa, --value and --qualifier are taken only from the command line.

A parameter the station refuses (negative confirmation, or "unknown type identification"
or "unknown information object address") ends with exit code 6.`,
		Example: `
  # Threshold of the measured value at IOA 3001: report changes of 0.5 and more
  iec101ctl client parameter --url tcp://10.0.0.5:2401 --type float --ioa 3001 --value 0.5 --qualifier threshold

  # High limit of a scaled measured value, result as JSON
  iec101ctl client parameter --url tcp://10.0.0.5:2401 --type scaled --ioa 3002 --value 1200 --qualifier high-limit --format json

  # Activate the parameter of the object at IOA 3001
  iec101ctl client parameter --url tcp://10.0.0.5:2401 --type activation --ioa 3001 --qualifier object
`,
	}
	cfg := config.ParameterConfig{StationConfig: stationDefaults(), OutputFormat: string(format.FormatText)}
	wire(cmd, &cfg,
		described("type", iec.ParameterTypes()),
		described("qualifier", iec.ParameterQualifiers()))
	for _, f := range []string{"type", "ioa", "qualifier"} {
		cli.MustMarkFlagRequired(cmd, f)
	}
	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		cfg.Debug = cli.Debug(cmd)
		return runner.RunFormattedChecked(cmd, cfg.OutputFormat,
			func() error { return iec.CheckParameter(&cfg) },
			func(ctx context.Context) (any, error) {
				return iec.Parameter(ctx, &cfg, cmd.ErrOrStderr())
			})
	}
	return cmd
}
