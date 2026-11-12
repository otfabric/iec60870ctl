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

func newInterrogateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "interrogate",
		Aliases: []string{"gi"},
		Short:   "Run a general or group interrogation (C_IC_NA_1) and list every value",
		Long: `Send an interrogation command and collect what the station answers until its
activation termination: the current value of every information object, with quality and,
where the type has one, time tag.

Without --group this is the general (station) interrogation, which every station
supports and which is how a controlling station learns the whole process image. With
--group 1-16 only the objects the station has assigned to that group are returned.

Integrated totals (counters) are not part of an interrogation: see "client counters".
Use --type and --ioa to narrow what is shown; the station is still asked for everything.

A large station can take longer than the default 10 s: raise --timeout.`,
		Example: `
  # General interrogation of station 1
  iec104ctl client interrogate --ip 192.168.1.10 --ca 1

  # The same as a table, or as JSON for a script
  iec104ctl client interrogate --ip 192.168.1.10 --format table
  iec104ctl client interrogate --ip 192.168.1.10 --format json

  # Interrogation group 3, floating point measured values only
  iec104ctl client interrogate --ip 192.168.1.10 --group 3 --type M_ME_NC_1

  # Over TLS, verifying the station with a private CA
  iec104ctl client interrogate --url tls://rtu.example.net --tls-ca ca.pem

  # With the application-layer security of IEC 62351-5, data encrypted with AES-256-GCM
  iec104ctl client interrogate --ip 192.168.1.10 --secure --security-cert scada.pem --security-key scada.key --security-trust ca.pem --dpa aes-256-gcm
`,
	}
	cfg := config.InterrogateConfig{StationConfig: stationDefaults(), OutputFormat: string(format.FormatText)}
	wire(cmd, &cfg, typeCompletion)
	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		cfg.Debug = cli.Debug(cmd)
		return runner.RunFormattedChecked(cmd, cfg.OutputFormat,
			func() error { return iec.CheckInterrogate(&cfg) },
			func(ctx context.Context) (any, error) { return iec.Interrogate(ctx, &cfg, cmd.ErrOrStderr()) })
	}
	return cmd
}

func newCountersCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "counters",
		Short: "Run a counter interrogation (C_CI_NA_1) and list the integrated totals",
		Long: `Send a counter interrogation command and collect the integrated totals (M_IT_NA_1)
the station answers with, until its activation termination.

--group selects a counter group 1-4; without it the request is general. --freeze says what
the request does to the counters besides reading them:

  read          report the current values (default)
  freeze        freeze the counters and report the frozen values
  freeze-reset  freeze, report, and reset the counters to zero
  reset         reset the counters to zero; nothing is reported

Anything but "read" changes the station: energy counters that are reset are gone.`,
		Example: `
  # Read every counter
  iec104ctl client counters --ip 192.168.1.10

  # Counter group 1 as JSON
  iec104ctl client counters --ip 192.168.1.10 --group 1 --format json

  # Freeze the counters and read the frozen values
  iec104ctl client counters --ip 192.168.1.10 --freeze freeze
`,
	}
	cfg := config.CountersConfig{StationConfig: stationDefaults(), Freeze: "read", OutputFormat: string(format.FormatText)}
	wire(cmd, &cfg, enum("freeze", iec.FreezeValues()...))
	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		cfg.Debug = cli.Debug(cmd)
		return runner.RunFormattedChecked(cmd, cfg.OutputFormat,
			func() error { return iec.CheckCounters(&cfg) },
			func(ctx context.Context) (any, error) { return iec.Counters(ctx, &cfg, cmd.ErrOrStderr()) })
	}
	return cmd
}
