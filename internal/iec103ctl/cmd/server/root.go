// SPDX-License-Identifier: MIT

// Package server holds the commands of the simulated protection equipment:
// `iec103ctl server …`.
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
	"github.com/otfabric/iec60870ctl/internal/iec103ctl/config"
	"github.com/otfabric/iec60870ctl/internal/iec103ctl/station"
	"github.com/otfabric/iec60870ctl/internal/iec103ctl/types"
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
		Short: "Act as protection equipment: simulate a device from a model",
		Long: `Commands that run a simulated item of protection equipment: the secondary station of
IEC 60870-5-103 that a controlling system, a protocol gateway or "iec103ctl client" polls.

The device is described by a model file (YAML or JSON): its addresses and identification,
its indications, measurands and commands, its generic entries and its recorded
disturbances. "server generate-config" writes a commented example to start from. The
IEC 103 fixtures of otfabric/iec60870-5-interop are models as they are.`,
	}
	cmd.AddCommand(newStartCmd(), newGenerateConfigCmd())
	return cmd
}

func newStartCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "start",
		Short: "Serve a simulated protection device",
		Long: `Serve the device the model --config describes; without --config a built-in demo device.

The line is TCP, as a terminal server carries it (--listen, --port 2401), or a serial port
(--serial with --baud, --data-bits, --parity and --stop-bits). Over TCP one controlling
system is served at a time: a connection that arrives while one is served is closed at
once. Every connection is a line that has just come up (link state and queues start
afresh); the process state (indications, generic values) lives as long as the server.

The device answers the link start-up with its identification, a time synchronization with
the time it set, a general interrogation with its indications, the general commands of the
model (acknowledged with cause 20, the change reported with cause 12; any other refused
with cause 21), the generic services (headings, group, directory, entry, general
interrogation of generic data, write entry, write with confirmation, write with execution
and write abort) and the disturbance data (the list and the transfer of each record). Its
measurands are sent cyclically as class 2 data; indications with "simulate" change by
themselves and are reported spontaneously.

Every event (connections, link state, requests, commands, writes, transfers) is written to
stdout as it happens: one line with --format text, one JSON object per line with --format
json, or one table row with --format table. With --debug every frame is traced to stderr.

The server runs until interrupted (Ctrl-C), or for --duration seconds. It listens on all
interfaces by default and accepts any client: use --listen to restrict it.`,
		Example: `
  # Serve the built-in demo device on port 2401
  iec103ctl server start

  # Serve your own model at link address 5, on localhost only
  iec103ctl server start --config relay.yaml --link-address 5 --listen 127.0.0.1

  # On a serial port at 19200 baud
  iec103ctl server start --config relay.yaml --serial /dev/ttyUSB1 --baud 19200

  # In a test pipeline: serve for 60 seconds, events as JSON Lines
  iec103ctl server start --config relay.yaml --duration 60 --format json
`,
	}
	cfg := config.ServerConfig{OutputFormat: string(format.FormatText)}
	cfg.Parity = "even"
	runner.RegisterCommandCfg(cmd, &cfg, func(c *cobra.Command) {
		_ = c.MarkFlagFilename("config", "yaml", "yml", "json")
		_ = cli.RegisterEnumFlagCompletion(c, "timezone", []string{"utc", "local"})
		_ = cli.RegisterEnumFlagCompletion(c, "parity", line.ParityValues())
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
				if err := station.Run(ctx, &cfg, logSync(log), cmd.ErrOrStderr(), nil); err != nil {
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
		Short: "Write an example device model to start from",
		Long: `Write the model of the built-in demo device: an overcurrent protection with
indications, measurands, commands, generic settings and one recorded disturbance. Edit it
and serve it with "iec103ctl server start --config <file>".

The YAML form carries comments that explain every section; --format json writes the same
model as JSON. An existing file is not overwritten without --force.`,
		Example: `
  iec103ctl server generate-config --output relay.yaml
  iec103ctl server generate-config --format json --output relay.json
  iec103ctl server generate-config            # to stdout
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

// logSync serializes the events: they come from the goroutines of the line and the device.
func logSync(f func(types.ServerEvent)) func(types.ServerEvent) {
	var mu sync.Mutex
	return func(e types.ServerEvent) {
		mu.Lock()
		defer mu.Unlock()
		f(e)
	}
}
