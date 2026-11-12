// SPDX-License-Identifier: MIT

package client

import (
	"context"
	"fmt"

	"github.com/otfabric/iec104ctl/internal/cli"
	"github.com/otfabric/iec104ctl/internal/config"
	"github.com/otfabric/iec104ctl/internal/errs"
	"github.com/otfabric/iec104ctl/internal/format"
	"github.com/otfabric/iec104ctl/internal/iec"
	"github.com/otfabric/iec104ctl/internal/runner"
	"github.com/otfabric/iec104ctl/internal/types"
	"github.com/spf13/cobra"
)

func newMonitorCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "monitor",
		Short: "Stay connected and print every value the station sends",
		Long: `Connect, start data transfer and print each information object as it arrives:
spontaneous changes, periodic and background values, and the answers to interrogations.
This is what a controlling station sees between its requests.

Monitoring runs until interrupted (Ctrl-C), or until --duration seconds have passed or
--count values were printed. With --interrogate a general interrogation is run after
connecting, and again after every reconnect, so the stream starts with all current
values. A lost connection is established again (turn that off with --no-reconnect).

Each value goes to stdout as it arrives: one line with --format text, one JSON object per
line with --format json, or one table row with --format table. Connection state changes
and a summary go to stderr.

Only one connection of a station has data transfer started at a time: while you monitor,
the station's regular controlling station may not receive these events, unless the
station keeps a queue per connection.`,
		Example: `
  # Watch everything station 192.168.1.10 sends, starting with a general interrogation
  iec104ctl client monitor --ip 192.168.1.10 --interrogate

  # Only floating point measured values of station 1, as JSON Lines
  iec104ctl client monitor --ip 192.168.1.10 --ca 1 --type M_ME_NC_1,M_ME_TF_1 --format json

  # Capture one minute into a file
  iec104ctl client monitor --ip 192.168.1.10 --duration 60 --format json > events.jsonl

  # Wait for the next 10 changes of two objects
  iec104ctl client monitor --ip 192.168.1.10 --ioa 100,200 --count 10
`,
	}
	cfg := config.MonitorConfig{ConnConfig: connDefaults(), OutputFormat: string(format.FormatText)}
	wire(cmd, &cfg, typeCompletion)
	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		cfg.Debug = cli.Debug(cmd)
		return runner.RunStream(cmd, cfg.OutputFormat,
			func() error { return iec.CheckMonitor(&cfg) },
			func(ctx context.Context, out *format.StreamWriter) (int, error) {
				summary, err := iec.Monitor(ctx, &cfg, cmd.ErrOrStderr(), func(p *types.Point) error { return out.Write(p) })
				if summary.Target != "" {
					_, _ = fmt.Fprintln(cmd.ErrOrStderr(), summary)
				}
				if err != nil {
					return 0, err
				}
				return errs.ExitOK, nil
			})
	}
	return cmd
}

func newPingCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "ping",
		Short: "Test the connection to a station and measure the round-trip time",
		Long: `Connect to a station and test it at an interval, printing the round-trip time of
every test.

  --method testfr   sends the link test frame (TESTFR act) and waits for TESTFR con. It
                    is answered by the protocol machine of the station and proves that the
                    connection is alive. This is the default, and it changes nothing.
  --method command  sends the test command C_TS_TA_1 to the common address --ca. It goes
                    through the application of the station; not every station implements it.

Testing runs until --count tests were made, or until interrupted (Ctrl-C) when --count is
0. A test that fails is reported and testing goes on while the connection is up. Exit code
7 (partial success) means at least one test failed.`,
		Example: `
  # Four link tests, one per second
  iec104ctl client ping --ip 192.168.1.10 --count 4

  # Test the application of station 1 every 500 ms until Ctrl-C
  iec104ctl client ping --ip 192.168.1.10 --method command --ca 1 --interval 500
`,
	}
	cfg := config.PingConfig{StationConfig: stationDefaults(), Method: iec.PingTestFR, Count: 4, Interval: 1000, OutputFormat: string(format.FormatText)}
	wire(cmd, &cfg, described("method", iec.PingMethods()))
	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		cfg.Debug = cli.Debug(cmd)
		return runner.RunStream(cmd, cfg.OutputFormat,
			func() error { return iec.CheckPing(&cfg) },
			func(ctx context.Context, out *format.StreamWriter) (int, error) {
				emit := func(s *types.PingSample) error {
					if err := out.Write(s); err != nil {
						return errs.Output(errs.CodeOutputWriteFailed, err)
					}
					return nil
				}
				summary, err := iec.Ping(ctx, &cfg, cmd.ErrOrStderr(), emit)
				if summary.Sent > 0 {
					_, _ = fmt.Fprintln(cmd.ErrOrStderr(), summary)
				}
				if err != nil {
					return 0, err
				}
				if summary.Failed > 0 {
					return errs.ExitPartial, nil
				}
				return errs.ExitOK, nil
			})
	}
	return cmd
}
