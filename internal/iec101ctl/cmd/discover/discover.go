// SPDX-License-Identifier: MIT

// Package discover holds `iec101ctl discover`.
package discover

import (
	"context"

	"github.com/otfabric/iec60870ctl/internal/cli"
	"github.com/otfabric/iec60870ctl/internal/format"
	"github.com/otfabric/iec60870ctl/internal/iec101ctl/config"
	"github.com/otfabric/iec60870ctl/internal/iec101ctl/iec"
	"github.com/otfabric/iec60870ctl/internal/runner"
	"github.com/spf13/cobra"
)

// NewCmd returns the `discover` command.
func NewCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "discover",
		Short: "Find IEC 60870-5-101 stations behind terminal servers on subnets",
		Long: `Probe every address of --subnets on --ports and list the ones where an
IEC 60870-5-101 station answers over TCP: a terminal server (serial device server) that
carries the line of a station, or a station with a TCP port of its own for the FT 1.2
byte stream.

The probe opens a TCP connection and sends one request status of link (function 9) to
--link-address, with the link address field --link-address-size and the link mode
--link-mode, and waits --timeout for the status of link. A station answers it in any
state, before its link is reset: the probe does not bring up the link and asks for no
data. A port that accepts the connection but does not answer is listed as "open"; with
other link parameters it may still be a station.

--subnets takes CIDR prefixes (192.168.1.0/24), ranges on the last octet (192.168.1.10-20)
and single addresses or host names. More than 4096 hosts need --force-large-scan.

A terminal server carries one line, and the line has one controlling station: a probe can
be turned away while the line is in use, and it may disturb that use. Scan only networks
you are responsible for.`,
		Example: `
  # Sweep a subnet on the port of the interop images (2401)
  iec101ctl discover --subnets 192.168.1.0/24

  # Two ports, link address 3 with a two-octet address field, as JSON
  iec101ctl discover --subnets 10.0.1.0/24 --ports 2401,4001 --link-address 3 --link-address-size 2 --format json

  # A handful of addresses with a short timeout
  iec101ctl discover --subnets 192.168.1.10-20 --timeout 300
`,
	}
	cfg := config.DiscoverConfig{Ports: []string{"2401"}, Mode: config.ModeUnbalanced, LinkAddr: 1, LinkAddrSize: 1, Parallel: 32, OutputFormat: string(format.FormatText)}
	runner.RegisterCommandCfg(cmd, &cfg, enumHook("link-mode", config.ModeUnbalanced, config.ModeBalanced), enumHook("link-address-size", "0", "1", "2"))
	cli.MustMarkFlagRequired(cmd, "subnets")
	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		cfg.Debug = cli.Debug(cmd)
		return runner.RunFormattedChecked(cmd, cfg.OutputFormat,
			func() error { return iec.CheckDiscover(&cfg) },
			func(ctx context.Context) (any, error) { return iec.Discover(ctx, &cfg, cmd.ErrOrStderr()) })
	}
	return cmd
}

func enumHook(flag string, values ...string) func(*cobra.Command) {
	return func(cmd *cobra.Command) { _ = cli.RegisterEnumFlagCompletion(cmd, flag, values) }
}
