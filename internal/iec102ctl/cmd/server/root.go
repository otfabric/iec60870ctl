// SPDX-License-Identifier: MIT

// Package server holds the commands of an integrated total station: `iec102ctl server …`.
package server

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/otfabric/iec60870ctl/internal/cli"
	"github.com/otfabric/iec60870ctl/internal/errs"
	"github.com/otfabric/iec60870ctl/internal/format"
	"github.com/otfabric/iec60870ctl/internal/iec102ctl/config"
	"github.com/otfabric/iec60870ctl/internal/iec102ctl/station"
	"github.com/otfabric/iec60870ctl/internal/iec102ctl/types"
	"github.com/otfabric/iec60870ctl/internal/line"
	"github.com/otfabric/iec60870ctl/internal/runner"
	"github.com/spf13/cobra"
)

// NewCmd returns the `server` command with all its subcommands.
func NewCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "server",
		Args:  cobra.NoArgs,
		RunE:  func(cmd *cobra.Command, _ []string) error { return cmd.Help() },
		Short: "Act as an integrated total station: simulate a meter from a model of records",
		Long: `Commands that run a simulated integrated total station: a stand-in for an electricity
meter or data concentrator that a controlling station (a metering front end, a data
collector or "iec102ctl client") reads.

The meter is described by a model file (YAML or JSON): its addresses, clock and product
specification, its integrated totals per record and integration period, its events and,
for the Spanish REE profile, its access key, contracted powers and tariff information.
"server generate-config" writes a commented example to start from.`,
	}
	cmd.AddCommand(newStartCmd(), newGenerateConfigCmd())
	return cmd
}

func newStartCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "start",
		Short: "Serve a simulated meter",
		Long: `Serve the meter the model --config describes, on a TCP port (a line carried by a
terminal server, as the interop images of otfabric/iec60870-5-interop do) or on a serial
port (--serial). Without --config a built-in demo meter is served.

The meter is the secondary station of unbalanced transmission: it answers the link
procedures and serves C_RD_NA_2 (product specification), C_TI_NA_2 (clock), the integrated
totals reads C_CI_NA_2 .. C_CI_NU_2 and the events reads C_SP_NA_2 and C_SP_NB_2 from the
model, with confirmation, data and termination, and refuses what it cannot serve with the
causes 14, 16, 17 and 18. With an "ree" section it runs the REE session (C_AC_NA_2 with the
access key, C_FS_NA_2), sets its clock (C_CS_TA_2) and answers C_TA_VC_2, C_TA_VM_2,
C_PC_NA_2 and C_DF_NA_2. Its reads do not depend on an open session. All answers are class
2 data; IEC 102 has no spontaneous data.

A line has one controlling station: a TCP connection that arrives while one is served is
closed at once ("busy"). Every connection starts a fresh link and session; the clock (and
a time set on it) lives as long as the process.

Every event (connections, the link, requests, refusals, the session) is written to stdout
as it happens: one line with --format text, one JSON object per line with --format json, or
one table row with --format table. With --debug every frame is traced to stderr.

The server runs until interrupted (Ctrl-C), or for --duration seconds. It listens on all
interfaces by default: use --listen to restrict it.`,
		Example: `
  # Serve the built-in demo meter on port 2401
  iec102ctl server start

  # Your own model, on localhost only, events as JSON Lines
  iec102ctl server start --config meter.yaml --listen 127.0.0.1 --port 12401 --format json

  # On a serial port, link address 5, the meter's clock in Spanish time
  iec102ctl server start --config meter.yaml --serial /dev/ttyUSB0 --baud 9600 --link-address 5 --timezone Europe/Madrid

  # In a test pipeline: serve for 60 seconds, then exit
  iec102ctl server start --config meter.yaml --duration 60
`,
	}
	cfg := config.ServerDefaults()
	runner.RegisterCommandCfg(cmd, &cfg, func(c *cobra.Command) {
		_ = c.MarkFlagFilename("config", "yaml", "yml", "json")
		_ = cli.RegisterEnumFlagCompletion(c, "timezone", []string{"utc", "local"})
		_ = cli.RegisterEnumFlagCompletion(c, "parity", line.ParityValues())
		_ = cli.RegisterEnumFlagCompletion(c, "link-address-size", []string{"0", "1", "2"})
		_ = cli.RegisterEnumFlagCompletion(c, "ca-size", []string{"1", "2"})
	})
	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		cfg.Debug = cli.Debug(cmd)
		return runner.RunStream(cmd, cfg.OutputFormat,
			func() error { return station.Check(&cfg) },
			func(ctx context.Context, out *format.StreamWriter) (int, error) {
				var writeErr error
				log := func(e types.ServerEvent) {
					if err := out.Write(&e); err != nil && writeErr == nil {
						writeErr = err
					}
				}
				if err := station.Run(ctx, &cfg, log, cmd.ErrOrStderr(), nil); err != nil {
					return 0, err
				}
				if writeErr != nil {
					return 0, errs.Output(errs.CodeOutputWriteFailed, writeErr)
				}
				return errs.ExitOK, nil
			})
	}
	return cmd
}

func newGenerateConfigCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "generate-config",
		Short: "Write an example station model to start from",
		Long: `Write the model of the built-in demo meter: an REE meter with two days of hourly
load profile (absolute values and increments), a day of quarter hours, events, contracted
powers and tariff information. Edit it and serve it with
"iec102ctl server start --config <file>".

The YAML form carries comments that explain every section; --format json writes the same
model as JSON. An existing file is not overwritten without --force.`,
		Example: `
  iec102ctl server generate-config --output meter.yaml
  iec102ctl server generate-config --format json --output meter.json
  iec102ctl server generate-config            # to stdout
`,
	}
	cfg := config.GenerateConfigConfig{Output: "-", Format: "yaml"}
	// Not runner.RegisterCommandCfg: --format is the format of the model here, not of stdout.
	config.MustLoadFromEnv(&cfg)
	config.RegisterFlags(cmd, &cfg)
	_ = cli.RegisterEnumFlagCompletion(cmd, "format", []string{"yaml", "json"})
	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		data, err := generate(cfg)
		if err != nil {
			return err
		}
		if cfg.Output == "-" || cfg.Output == "" {
			if _, err := cmd.OutOrStdout().Write(data); err != nil {
				return errs.Output(errs.CodeOutputWriteFailed, err)
			}
			return nil
		}
		flags := os.O_WRONLY | os.O_CREATE | os.O_EXCL
		if cfg.Force {
			flags = os.O_WRONLY | os.O_CREATE | os.O_TRUNC
		}
		f, err := os.OpenFile(filepath.Clean(cfg.Output), flags, 0o600)
		if err != nil {
			if errors.Is(err, os.ErrExist) {
				return errs.InvalidInput(errs.CodeOutputFileCreateFailed, fmt.Sprintf("%s exists: pass --force to overwrite it", cfg.Output), err)
			}
			return errs.Output(errs.CodeOutputFileCreateFailed, err)
		}
		if _, err := f.Write(data); err != nil {
			_ = f.Close()
			return errs.Output(errs.CodeOutputFileWriteFailed, err)
		}
		if err := f.Close(); err != nil {
			return errs.Output(errs.CodeOutputFileWriteFailed, err)
		}
		_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "wrote %s\n", cfg.Output)
		return nil
	}
	return cmd
}

func generate(cfg config.GenerateConfigConfig) ([]byte, error) {
	switch strings.ToLower(strings.TrimSpace(cfg.Format)) {
	case "", "yaml", "yml":
		return []byte(station.DemoYAML), nil
	case "json":
		data, err := station.Demo().Encode(true)
		if err != nil {
			return nil, errs.Output(errs.CodeJSONEncodeFailed, err)
		}
		return data, nil
	}
	return nil, errs.InvalidInput(errs.CodeInvalidFormat, fmt.Sprintf("invalid --format %q (expected: yaml, json)", cfg.Format), nil)
}
