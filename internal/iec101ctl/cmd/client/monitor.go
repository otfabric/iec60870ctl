// SPDX-License-Identifier: MIT

package client

import (
	"context"
	"fmt"

	"github.com/otfabric/iec60870ctl/internal/cli"
	"github.com/otfabric/iec60870ctl/internal/errs"
	"github.com/otfabric/iec60870ctl/internal/format"
	"github.com/otfabric/iec60870ctl/internal/iec101ctl/config"
	"github.com/otfabric/iec60870ctl/internal/iec101ctl/iec"
	"github.com/otfabric/iec60870ctl/internal/iec101ctl/types"
	"github.com/otfabric/iec60870ctl/internal/runner"
	"github.com/spf13/cobra"
)

func newMonitorCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "monitor",
		Short: "Stay on the line and print every value the station sends",
		Long: `Open the line, bring up the link and print each information object as it arrives:
spontaneous changes, periodic and background values, and the answers to interrogations.
In unbalanced transmission the station is polled for class 1 and class 2 data all the
time (--poll-interval); in balanced transmission it sends on its own.

Monitoring runs until interrupted (Ctrl-C), or until --duration seconds have passed or
--count values were printed. With --interrogate a general interrogation is run each time
the link comes up, so the stream starts with all current values. A link that stops
answering is initialized again; a lost line is opened again (turn that off with
--no-reconnect).

Each value goes to stdout as it arrives: one line with --format text, one JSON object per
line with --format json, or one table row with --format table. Link state changes and a
summary go to stderr.

A line has one controlling station: what this command receives, the station's regular
controlling station does not.`,
		Example: `
  # Watch everything station 192.168.1.10 sends, starting with a general interrogation
  iec101ctl client monitor --url tcp://10.0.0.5:2401 --interrogate

  # Only floating point measured values of station 1, as JSON Lines
  iec101ctl client monitor --url tcp://10.0.0.5:2401 --ca 1 --type M_ME_NC_1,M_ME_TF_1 --format json

  # Capture one minute into a file
  iec101ctl client monitor --url tcp://10.0.0.5:2401 --duration 60 --format json > events.jsonl

  # Wait for the next 10 changes of two objects
  iec101ctl client monitor --url tcp://10.0.0.5:2401 --ioa 100,200 --count 10
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
		Short: "Test a station and measure the round-trip time",
		Long: `Test a station at an interval, printing the round-trip time of every test.

  --method command      brings up the link and sends the test command C_TS_NA_1 (with the
                        fixed test bit pattern) to the common address --ca, and waits for
                        its confirmation. It goes through the application of the station.
                        This is the default, and it changes nothing.
  --method link-status  sends request status of link (function 9) to --link-address and
                        waits for the status of link. It is answered by the link layer of
                        the station, also before the link is reset; the link is not
                        brought up.
  --method link-test    balanced transmission only: brings up this station's direction of
                        the link, then sends the test function for link (function 2) and
                        waits for its acknowledgement.

The link methods ask once per test: the link layer does not repeat an unanswered frame.
Testing runs until --count tests were made, or until interrupted (Ctrl-C) when --count is
0. A test that fails is reported and testing goes on while the line is open. Exit code 7
(partial success) means at least one test failed.`,
		Example: `
  # Four test commands to station 1, one per second
  iec101ctl client ping --url tcp://10.0.0.5:2401 --count 4

  # Is a station at link address 3 on this serial line? Ask its link layer every 500 ms
  iec101ctl client ping --url serial:///dev/ttyUSB0 --method link-status --link-address 3 --interval 500

  # Test function for link on a balanced line
  iec101ctl client ping --url tcp://10.0.0.5:2401 --link-mode balanced --method link-test
`,
	}
	cfg := config.PingConfig{StationConfig: stationDefaults(), Method: iec.PingCommand, Count: 4, Interval: 1000, OutputFormat: string(format.FormatText)}
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
