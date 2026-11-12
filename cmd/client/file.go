// SPDX-License-Identifier: MIT

package client

import (
	"context"

	"github.com/otfabric/iec104ctl/internal/cli"
	"github.com/otfabric/iec104ctl/internal/config"
	"github.com/otfabric/iec104ctl/internal/format"
	"github.com/otfabric/iec104ctl/internal/iec"
	"github.com/otfabric/iec104ctl/internal/runner"
	"github.com/spf13/cobra"
)

func newFileCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "file",
		Args:  cobra.NoArgs,
		RunE:  func(cmd *cobra.Command, _ []string) error { return cmd.Help() },
		Short: "File transfer: list the directory, fetch a file, deliver a file",
		Long: `The file transfer of IEC 60870-5-101/104 (types F_FR_NA_1 to F_DR_TA_1): disturbance
records, event logs and configuration files of a station.

A file is identified by the information object address --ioa and the name of file --name,
a number (NOF). "file list" shows what a station offers.`,
	}
	cmd.AddCommand(newFileListCmd(), newFileGetCmd(), newFilePutCmd())
	return cmd
}

func newFileListCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List the file directory of a station (F_SC_NA_1 call directory, F_DR_TA_1)",
		Long: `Call the directory of a station and list its entries: address, name of file, length
and creation time. --ioa selects a subdirectory; 0 is the station's default directory.

A station without a directory refuses the call (exit code 6).`,
		Example: `
  iec104ctl client file list --ip 192.168.1.10 --ca 1
  iec104ctl client file list --ip 192.168.1.10 --format json
`,
	}
	cfg := config.FileListConfig{StationConfig: stationDefaults(), OutputFormat: string(format.FormatText)}
	wire(cmd, &cfg)
	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		cfg.Debug = cli.Debug(cmd)
		return runner.RunFormattedChecked(cmd, cfg.OutputFormat,
			func() error { return iec.CheckFileList(&cfg) },
			func(ctx context.Context) (any, error) { return iec.FileList(ctx, &cfg, cmd.ErrOrStderr()) })
	}
	return cmd
}

func newFileGetCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "get",
		Short: "Fetch a file from a station",
		Long: `Select the file --ioa / --name, call it section by section, verify the checksum of
every section and of the file, acknowledge it and write the content to --output.

With --output - the content goes to stdout and the result (length, SHA-256) to stderr, so
that the file can be piped.`,
		Example: `
  # Fetch a disturbance record
  iec104ctl client file get --ip 192.168.1.10 --ioa 30000 --name 1 --output record.bin

  # Pipe a text file
  iec104ctl client file get --ip 192.168.1.10 --ioa 30001 --name 1 --output - | less
`,
	}
	cfg := config.FileGetConfig{StationConfig: stationDefaults(), Name: 1, OutputFormat: string(format.FormatText)}
	wire(cmd, &cfg)
	for _, f := range []string{"ioa", "output"} {
		cli.MustMarkFlagRequired(cmd, f)
	}
	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		cfg.Debug = cli.Debug(cmd)
		content := cmd.OutOrStdout()
		if cfg.Output == "-" {
			// The content takes stdout; the result goes to stderr.
			cmd.SetOut(cmd.ErrOrStderr())
		}
		return runner.RunFormattedChecked(cmd, cfg.OutputFormat,
			func() error { return iec.CheckFileGet(&cfg) },
			func(ctx context.Context) (any, error) { return iec.FileGet(ctx, &cfg, cmd.ErrOrStderr(), content) })
	}
	return cmd
}

func newFilePutCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "put",
		Short: "Deliver a file to a station",
		Long: `Announce the file --input to the station as --ioa / --name, send the sections and
segments the station calls for, each with its checksum, and wait until the station
acknowledges the file.

--section-size splits the file into sections of that many octets (at most 255 sections);
by default it is sent as one section. A file holds at most 16 777 215 octets.

THIS CHANGES THE STATION: a delivered file may be a configuration or firmware the station
acts on. It is sent once and not repeated.`,
		Example: `
  iec104ctl client file put --ip 192.168.1.10 --ioa 40000 --name 1 --input settings.bin
  iec104ctl client file put --ip 192.168.1.10 --ioa 40000 --name 1 --input settings.bin --section-size 4096
`,
	}
	cfg := config.FilePutConfig{StationConfig: stationDefaults(), Name: 1, OutputFormat: string(format.FormatText)}
	wire(cmd, &cfg)
	for _, f := range []string{"ioa", "input"} {
		cli.MustMarkFlagRequired(cmd, f)
	}
	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		cfg.Debug = cli.Debug(cmd)
		return runner.RunFormattedChecked(cmd, cfg.OutputFormat,
			func() error { return iec.CheckFilePut(&cfg) },
			func(ctx context.Context) (any, error) { return iec.FilePut(ctx, &cfg, cmd.ErrOrStderr()) })
	}
	return cmd
}
