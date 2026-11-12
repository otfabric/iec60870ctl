// SPDX-License-Identifier: MIT

package client

import (
	"context"

	"github.com/otfabric/iec104ctl/internal/cli"
	"github.com/otfabric/iec104ctl/internal/config"
	"github.com/otfabric/iec104ctl/internal/errs"
	"github.com/otfabric/iec104ctl/internal/format"
	"github.com/otfabric/iec104ctl/internal/iec"
	"github.com/otfabric/iec104ctl/internal/runner"
	"github.com/otfabric/iec104ctl/internal/types"
	"github.com/spf13/cobra"
)

func newReadCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "read",
		Short: "Read the current value of information objects (C_RD_NA_1)",
		Long: `Send a read command for each address of --ioa and print the value the station
returns with cause "request".

An address the station does not know, or does not answer, is reported in its row and the
remaining addresses are still read; the exit code is then 7 (partial success). Not every
station implements the read command: a general interrogation ("client interrogate") is
the way that always works.`,
		Example: `
  # Read one object
  iec104ctl client read --ip 192.168.1.10 --ioa 1000

  # Read a list and a range, as a table
  iec104ctl client read --ip 192.168.1.10 --ioa 100,1000-1003 --format table
`,
	}
	cfg := config.ReadConfig{StationConfig: stationDefaults(), OutputFormat: string(format.FormatText)}
	wire(cmd, &cfg)
	cli.MustMarkFlagRequired(cmd, "ioa")
	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		cfg.Debug = cli.Debug(cmd)
		return runner.RunFormattedChecked(cmd, cfg.OutputFormat,
			func() error { return iec.CheckRead(&cfg) },
			func(ctx context.Context) (any, error) { return iec.Read(ctx, &cfg, cmd.ErrOrStderr()) },
			runner.WithSuccessExit(func(v any) int {
				if r, ok := v.(*types.ReadResult); ok && r.Failed > 0 {
					return errs.ExitPartial
				}
				return errs.ExitOK
			}))
	}
	return cmd
}
