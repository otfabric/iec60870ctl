// SPDX-License-Identifier: MIT

// Package discover holds `iec104ctl discover`.
package discover

import (
	"context"

	"github.com/otfabric/iec104ctl/internal/cli"
	"github.com/otfabric/iec104ctl/internal/config"
	"github.com/otfabric/iec104ctl/internal/format"
	"github.com/otfabric/iec104ctl/internal/iec"
	"github.com/otfabric/iec104ctl/internal/runner"
	"github.com/spf13/cobra"
)

// NewCmd returns the `discover` command.
func NewCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "discover",
		Short: "Find IEC 60870-5-104 stations on subnets",
		Long: `Probe every address of --subnets on --ports and list the ones that answer as an
IEC 60870-5-104 station.

The probe opens a TCP connection, sends one link test frame (TESTFR act) and waits for
TESTFR con. A station answers that in any state: the probe does not start data transfer
and asks for no data. A port that accepts the connection but does not answer the test
frame is listed as "open".

--subnets takes CIDR prefixes (192.168.1.0/24), ranges on the last octet (192.168.1.10-20)
and single addresses or host names. More than 4096 hosts need --force-large-scan.

A station accepts a limited number of connections, often one per controlling station it
knows: a probe can be turned away by a station that is in service, and it may show up in
that station's logs. Scan only networks you are responsible for.`,
		Example: `
  # Sweep a subnet on the standard port
  iec104ctl discover --subnets 192.168.1.0/24

  # Two subnets, the standard and the TLS port, 64 probes at a time, as JSON
  iec104ctl discover --subnets 10.0.1.0/24,10.0.2.0/24 --ports 2404,19998 --parallel 64 --format json

  # A handful of addresses with a short timeout
  iec104ctl discover --subnets 192.168.1.10-20 --timeout 300
`,
	}
	cfg := config.DiscoverConfig{Ports: []string{"2404"}, Parallel: 32, OutputFormat: string(format.FormatText)}
	runner.RegisterCommandCfg(cmd, &cfg)
	cli.MustMarkFlagRequired(cmd, "subnets")
	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		cfg.Debug = cli.Debug(cmd)
		return runner.RunFormattedChecked(cmd, cfg.OutputFormat,
			func() error { return iec.CheckDiscover(&cfg) },
			func(ctx context.Context) (any, error) { return iec.Discover(ctx, &cfg, cmd.ErrOrStderr()) })
	}
	return cmd
}
