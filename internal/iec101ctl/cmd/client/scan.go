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

func newScanCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "scan",
		Short: "Find the stations on a line: common addresses or link addresses",
		Long: `One line can lead to several stations: a multidrop line in unbalanced transmission
has a link address per station, and one station (a gateway or data concentrator) can
answer for several common addresses. scan finds out which exist.

  --method broadcast  sends one general interrogation to the broadcast common address
                      (255, or 65535 with --ca-size 2) and lists every common address
                      that answers, with the number of objects and the types it reported.
                      It waits until the stations have been quiet for 1.5 s, or --timeout.
                      This is the default.
  --method ca         sends a general interrogation to each common address from --start
                      to --end. Addresses answered with "unknown common address", or not
                      at all, are counted but not listed.
  --method link       sends request status of link (function 9) to each link address from
                      --start to --end and lists the ones that answer, with the ACD and
                      DFC bits. Unbalanced transmission only. It changes nothing in the
                      stations: no link is reset and no data is asked for.

A station that stays silent costs a whole --timeout per address (ca) or --link-timeout
(link): lower them. The broadcast and ca methods make every station found send its whole
process image. Progress of a sweep goes to stderr; the result goes to stdout.`,
		Example: `
  # Which common addresses answer behind this terminal server?
  iec101ctl client scan --url tcp://10.0.0.5:2401

  # Which link addresses answer on a multidrop serial line, 300 ms per address?
  iec101ctl client scan --url serial:///dev/ttyUSB0 --method link --start 1 --end 30 --link-timeout 300

  # Sweep common addresses 1-50, 1 s per address, as JSON
  iec101ctl client scan --url tcp://10.0.0.5:2401 --method ca --start 1 --end 50 --timeout 1000 --format json
`,
	}
	cfg := config.ScanConfig{ConnConfig: connDefaults(), Method: iec.ScanBroadcast, Start: 1, End: 254, OutputFormat: string(format.FormatText)}
	wire(cmd, &cfg, described("method", iec.ScanMethods()))
	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		cfg.Debug = cli.Debug(cmd)
		return runner.RunFormattedChecked(cmd, cfg.OutputFormat,
			func() error { return iec.CheckScan(&cfg) },
			func(ctx context.Context) (any, error) {
				return iec.Scan(ctx, &cfg, cmd.ErrOrStderr(), cmd.ErrOrStderr())
			})
	}
	return cmd
}

func newTypesCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "types",
		Short: "List the ASDU type identifications of IEC 60870-5-101",
		Long: `List the type identifications IEC 60870-5-101 defines with their number, name,
direction and description. The names are what --type takes in "client interrogate" and
"client monitor", and what the output of every command shows.

A type marked "not decoded" is recognized by name and passed through; its content is not
interpreted. No connection is made.`,
		Example: `
  iec101ctl client types
  iec101ctl client types --direction control --format table
`,
	}
	cfg := config.TypesConfig{OutputFormat: string(format.FormatText)}
	wire(cmd, &cfg, enum("direction", "monitor", "control"))
	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		return runner.RunFormattedChecked(cmd, cfg.OutputFormat,
			func() error { return iec.CheckTypes(&cfg) },
			func(context.Context) (any, error) { return iec.Types(&cfg), nil })
	}
	return cmd
}
