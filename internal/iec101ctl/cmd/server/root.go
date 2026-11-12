// SPDX-License-Identifier: MIT

// Package server holds the commands of a controlled station: `iec101ctl server …`.
package server

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/otfabric/iec60870ctl/internal/cli"
	"github.com/otfabric/iec60870ctl/internal/errs"
	"github.com/otfabric/iec60870ctl/internal/format"
	"github.com/otfabric/iec60870ctl/internal/iec101ctl/cmd/client"
	"github.com/otfabric/iec60870ctl/internal/iec101ctl/config"
	"github.com/otfabric/iec60870ctl/internal/iec101ctl/station"
	"github.com/otfabric/iec60870ctl/internal/iec101ctl/types"
	"github.com/otfabric/iec60870ctl/internal/runner"
	"github.com/spf13/cobra"
)

// NewCmd returns the `server` command with all its subcommands.
func NewCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "server",
		Args:  cobra.NoArgs,
		RunE:  func(cmd *cobra.Command, _ []string) error { return cmd.Help() },
		Short: "Act as a controlled station: simulate an RTU from a model of points and commands",
		Long: `Commands that run a simulated controlled station: a stand-in for an RTU, gateway or
IED that a SCADA front end, a protocol gateway or "iec101ctl client" can talk to over a
serial line or a TCP connection that carries one.

The station is described by a model file (YAML or JSON): its common address and its points
with their initial values and commands. "server generate-config" writes a commented example
to start from.`,
	}
	cmd.AddCommand(newStartCmd(), newGenerateConfigCmd())
	return cmd
}

func newStartCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "start",
		Short: "Serve a simulated station",
		Long: `Serve the station the model --config describes, as the controlled station of
IEC 60870-5-101. Without --config a built-in demo station is served (see "server
generate-config").

The line is TCP by default: the station listens on --listen and --port (2401) and serves
one controlling station at a time, as a terminal server carries one line; a connection
that arrives while one is served is closed at once. Every connection is a line that has
just come up (the link starts afresh), while the process state of the station (values,
clock) lives on. With --serial the station serves a serial port instead, with --baud,
--data-bits, --parity and --stop-bits.

The link flags must match the controlling station: --link-mode (unbalanced: the station
answers polls with class 1 data, its events and confirmations, and class 2 data, its
periodic values; balanced: it sends on its own), --link-address (the station's), in
balanced mode --remote-link-address (the controlling station's), and the field sizes
--link-address-size, --cot-size, --ca-size and --ioa-size. Every address of the model must
fit them.

The station answers general and group interrogations, counter interrogations, read
commands, clock synchronization, delay acquisition, the test command and the reset process
command. It carries out process commands, direct or select-before-operate, on the points
the model names as their targets, and reports the new value between the confirmation and
the termination. Values with "simulate" in the model change by themselves and are reported
as spontaneous events; measured values can be sent periodically. After the first reset of
the link it sends end of initialization, unless --no-end-of-init.

Every event (lines, link state, requests, commands) is written to stdout as it happens:
one line with --format text, one JSON object per line with --format json, or one table row
with --format table. With --debug every frame is traced to stderr.

The server runs until interrupted (Ctrl-C), or for --duration seconds. It listens on all
interfaces by default: use --listen to restrict it.`,
		Example: `
  # Serve the built-in demo station on TCP port 2401, unbalanced, link address 1
  iec101ctl server start

  # Serve your own model on localhost, balanced, with two-octet addresses
  iec101ctl server start --config station.yaml --listen 127.0.0.1 --link-mode balanced --link-address-size 2 --ca-size 2 --ioa-size 3

  # On a serial port at 9600 baud, link address 3
  iec101ctl server start --config station.yaml --serial /dev/ttyUSB0 --baud 9600 --link-address 3

  # In a test pipeline: serve for 60 seconds, log events as JSON Lines
  iec101ctl server start --config station.yaml --duration 60 --format json
`,
	}
	cfg := config.ServerConfig{LinkConfig: config.DefaultLink(), OutputFormat: string(format.FormatText)}
	runner.RegisterCommandCfg(cmd, &cfg, func(c *cobra.Command) {
		_ = c.MarkFlagFilename("config", "yaml", "yml", "json")
	}, client.LinkCompletion)
	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		cfg.Debug = cli.Debug(cmd)
		return runner.RunStream(cmd, cfg.OutputFormat,
			func() error { return station.Check(&cfg) },
			func(ctx context.Context, out *format.StreamWriter) (int, error) {
				var (
					mu       sync.Mutex
					writeErr error
				)
				log := func(e types.ServerEvent) {
					mu.Lock()
					defer mu.Unlock()
					if err := out.Write(&e); err != nil && writeErr == nil {
						writeErr = err
					}
				}
				if err := station.Run(ctx, &cfg, log, cmd.ErrOrStderr(), nil); err != nil {
					return 0, err
				}
				mu.Lock()
				defer mu.Unlock()
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
		Long: `Write the model of the built-in demo station: a small substation bay with a point of
every type and a command for each. Edit it and serve it with
"iec101ctl server start --config <file>".

The YAML form carries comments that explain every section; --format json writes the same
model as JSON. An existing file is not overwritten without --force.`,
		Example: `
  iec101ctl server generate-config --output station.yaml
  iec101ctl server generate-config --format json --output station.json
  iec101ctl server generate-config            # to stdout
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
