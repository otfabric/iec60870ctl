// SPDX-License-Identifier: MIT

package client

import (
	"context"

	"github.com/otfabric/iec104ctl/internal/cli"
	"github.com/otfabric/iec104ctl/internal/config"
	"github.com/otfabric/iec104ctl/internal/format"
	"github.com/otfabric/iec104ctl/internal/iec"
	"github.com/otfabric/iec104ctl/internal/runner"
	"github.com/spf13/cobra"
)

func newScanCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "scan",
		Short: "Find the stations (common addresses) behind a connection",
		Long: `One connection can lead to several stations: a gateway or data concentrator answers
for every common address behind it. scan finds out which ones exist, and what each sends.

  --method broadcast  sends one general interrogation to the broadcast address 65535 and
                      lists every station that answers, with the number of objects and the
                      types it reported. One request, and the quickest way (default). It
                      waits until the stations have been quiet for 1.5 s, or --timeout.
  --method sweep      sends a general interrogation to each common address from --start to
                      --end. Use it when the station does not answer the broadcast address.
                      Addresses answered with "unknown common address", or not at all, are
                      counted but not listed. A station that stays silent costs a whole
                      --timeout per address: lower it (for example --timeout 1000).

Both methods make every station found send its whole process image. Progress of a sweep
goes to stderr; the result goes to stdout.`,
		Example: `
  # Which stations are behind this gateway?
  iec104ctl client scan --ip 192.168.1.10

  # Sweep common addresses 1-50, 1 s per address, 100 ms between requests
  iec104ctl client scan --ip 192.168.1.10 --method sweep --start 1 --end 50 --timeout 1000 --delay 100

  # As JSON
  iec104ctl client scan --ip 192.168.1.10 --format json
`,
	}
	cfg := config.ScanConfig{ConnConfig: connDefaults(), Method: iec.ScanBroadcast, Start: 1, End: 255, OutputFormat: string(format.FormatText)}
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
		Short: "List the ASDU type identifications iec104ctl knows",
		Long: `List the type identifications of IEC 60870-5-101/104 with their number, name,
direction and description. The names are what --type takes in "client interrogate" and
"client monitor", and what the output of every command shows.

A type marked "not decoded" is recognized by name and passed through; its content is not
interpreted. No connection is made.`,
		Example: `
  iec104ctl client types
  iec104ctl client types --direction control --format table
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
