// SPDX-License-Identifier: MIT

package client

import (
	"context"
	"fmt"

	"github.com/otfabric/iec60870ctl/internal/cli"
	"github.com/otfabric/iec60870ctl/internal/errs"
	"github.com/otfabric/iec60870ctl/internal/format"
	"github.com/otfabric/iec60870ctl/internal/iec103ctl/config"
	"github.com/otfabric/iec60870ctl/internal/iec103ctl/iec"
	"github.com/otfabric/iec60870ctl/internal/iec103ctl/types"
	"github.com/otfabric/iec60870ctl/internal/runner"
	"github.com/spf13/cobra"
)

func newMonitorCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "monitor",
		Short: "Poll the device and print every message it sends",
		Long: `Initialize the link and poll the device as a controlling system does, class 1 while it
has events and class 2 otherwise, printing every ASDU it sends as it arrives: spontaneous
events (cause 1), measurands (cause 2), changes after commands (cause 12), the list of
recorded disturbances it announces, and with --interrogate the answers of a general
interrogation run after the link start-up.

Monitoring runs until interrupted (Ctrl-C), or until --duration seconds have passed or
--count messages were printed. A lost line is opened again (turn that off with
--no-reconnect; the command then ends with exit code 4). Each message goes to stdout as it
arrives: one line with --format text, one JSON object per line with --format json, or one
table row with --format table. Link state changes and a summary go to stderr.

A line has one controlling system: while you monitor, the regular one cannot poll.`,
		Example: `
  iec103ctl client monitor --url tcp://10.0.0.20 --interrogate
  iec103ctl client monitor --url serial:///dev/ttyUSB0 --duration 60 --format json > events.jsonl
`,
	}
	cfg := config.MonitorConfig{ConnConfig: connDefaults(), OutputFormat: stdoutFormat}
	wire(cmd, &cfg)
	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		cfg.Debug = cli.Debug(cmd)
		return runner.RunStream(cmd, cfg.OutputFormat,
			func() error { return iec.CheckMonitor(&cfg) },
			func(ctx context.Context, out *format.StreamWriter) (int, error) {
				summary, err := iec.Monitor(ctx, &cfg, cmd.ErrOrStderr(), func(m *types.Message) error { return out.Write(m) })
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

func newScanCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "scan",
		Short: "Find the devices on a line by link address",
		Long: `Try every link address from --start to --end on one line: request the status of the
link and reset it. Every address that answers is listed with the identification the reset
makes the device send (ASDU 5): its common address, function type, manufacturer and
software. --timeout is the wait at each address (1000 ms by default).

The reset of the communication unit clears the device's transmit buffer (clause 7.4.1):
events it held for its regular controlling system are lost. Scan a line in service only
when that is acceptable.`,
		Example: `
  iec103ctl client scan --url serial:///dev/ttyUSB0 --start 1 --end 32
  iec103ctl client scan --url tcp://10.0.0.20 --end 10 --timeout 500 --format table
`,
	}
	cfg := config.ScanConfig{ConnConfig: connDefaults(), Start: 0, End: 254, OutputFormat: stdoutFormat}
	wire(cmd, &cfg)
	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		return formatted(cmd, cfg.OutputFormat, &cfg.Debug,
			func() error { return iec.CheckScan(&cfg) },
			func(ctx context.Context) (*types.ScanResult, error) { return iec.Scan(ctx, &cfg, cmd.ErrOrStderr()) })
	}
	return cmd
}
