// SPDX-License-Identifier: MIT

// Package server holds the commands of a controlled station: `iec104ctl server …`.
package server

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"

	"github.com/otfabric/iec104ctl/internal/cli"
	"github.com/otfabric/iec104ctl/internal/config"
	"github.com/otfabric/iec104ctl/internal/errs"
	"github.com/otfabric/iec104ctl/internal/format"
	"github.com/otfabric/iec104ctl/internal/iec"
	"github.com/otfabric/iec104ctl/internal/runner"
	"github.com/otfabric/iec104ctl/internal/station"
	"github.com/otfabric/iec104ctl/internal/types"
	"github.com/spf13/cobra"
)

// NewCmd returns the `server` command with all its subcommands.
func NewCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "server",
		Args:  cobra.NoArgs,
		RunE:  func(cmd *cobra.Command, _ []string) error { return cmd.Help() },
		Short: "Act as a controlled station: simulate an RTU from a model of points, commands and files",
		Long: `Commands that run a simulated controlled station: a stand-in for an RTU, gateway or
IED that a SCADA front end, a protocol gateway or "iec104ctl client" can connect to.

The station is described by a model file (YAML or JSON): its common address, its points
with their initial values, its commands and its files. "server generate-config" writes a
commented example to start from.`,
	}
	cmd.AddCommand(newStartCmd(), newGenerateConfigCmd())
	return cmd
}

func newStartCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "start",
		Short: "Serve a simulated station",
		Long: `Listen for controlling stations and serve the station the model --config describes.
Without --config a built-in demo station is served (see "server generate-config").

The station answers general, group and counter interrogations, read commands, clock
synchronization, the test command and the reset process command. It carries out process
commands, direct or select-before-operate, on the points the model names as their targets,
and reports the change as an event. Values with "simulate" in the model change by
themselves and are reported as spontaneous events; measured values can be sent
periodically. Files are served and, when the model accepts uploads, taken.

Events are queued while no controlling station has data transfer started and are delivered
to the next one, and the station serves any number of connections, of which one receives
the events (redundancy as in IEC 60870-5-104).

Every event (connections, requests, commands, file transfers) is written to stdout as it
happens: one line with --format text, one JSON object per line with --format json, or one
table row with --format table. With --debug every frame is traced to stderr.

With --secure the station runs the application-layer security of IEC 62351-5 (the
A-Profile of IEC TS 60870-5-7): every controlling station must associate with a
certificate --security-trust accepts, every ASDU travels as Secure Data, and with --roles
a request the roles do not permit is refused with cause 17 (not authorized).

The server runs until interrupted (Ctrl-C), or for --duration seconds. It listens on all
interfaces by default and accepts any client: use --listen and --allow to restrict it.`,
		Example: `
  # Serve the built-in demo station on port 2404
  iec104ctl server start

  # Serve your own model on another port, on localhost only
  iec104ctl server start --config station.yaml --listen 127.0.0.1 --port 12404

  # TLS, clients must present a certificate of this CA
  iec104ctl server start --config station.yaml --tls-cert server.pem --tls-key server.key --tls-client-ca ca.pem

  # IEC 62351-5 application-layer security; controlling stations may look, not operate
  iec104ctl server start --config station.yaml --secure --security-cert station.pem --security-key station.key --security-trust ca.pem --roles viewer

  # Only two front ends may connect; log events as JSON Lines
  iec104ctl server start --config station.yaml --allow 10.0.0.5,10.0.0.6 --format json

  # In a test pipeline: serve for 60 seconds, then exit
  iec104ctl server start --config station.yaml --duration 60
`,
	}
	cfg := config.ServerConfig{OutputFormat: string(format.FormatText)}
	cfg.AssociationID = 1
	runner.RegisterCommandCfg(cmd, &cfg, func(c *cobra.Command) {
		_ = c.MarkFlagFilename("config", "yaml", "yml", "json")
		_ = cli.RegisterEnumFlagCompletion(c, "timezone", []string{"utc", "local"})
		for _, f := range []string{"tls-cert", "tls-key", "tls-client-ca", "security-cert", "security-key", "security-trust"} {
			_ = c.MarkFlagFilename(f, "pem", "crt", "cer", "key")
		}
		_ = cli.RegisterEnumFlagCompletion(c, "roles", iec.RoleNames())
		_ = cli.RegisterEnumFlagCompletion(c, "ca-size", []string{"1", "2"})
		_ = cli.RegisterEnumFlagCompletion(c, "cot-size", []string{"1", "2"})
		_ = cli.RegisterEnumFlagCompletion(c, "ioa-size", []string{"1", "2", "3"})
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
				ready := func(net.Addr) {}
				if err := station.Run(ctx, &cfg, log, cmd.ErrOrStderr(), ready); err != nil {
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
		Long: `Write the model of the built-in demo station: a small substation bay with a point of
every type, a command for each and two files. Edit it and serve it with
"iec104ctl server start --config <file>".

The YAML form carries comments that explain every section; --format json writes the same
model as JSON. An existing file is not overwritten without --force.`,
		Example: `
  iec104ctl server generate-config --output station.yaml
  iec104ctl server generate-config --format json --output station.json
  iec104ctl server generate-config            # to stdout
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
