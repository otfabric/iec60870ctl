// SPDX-License-Identifier: MIT

package client

import (
	"context"
	"strings"

	"github.com/otfabric/iec60870ctl/internal/cli"
	"github.com/otfabric/iec60870ctl/internal/envflag"
	"github.com/otfabric/iec60870ctl/internal/errs"
	"github.com/otfabric/iec60870ctl/internal/format"
	"github.com/otfabric/iec60870ctl/internal/iec104ctl/config"
	"github.com/otfabric/iec60870ctl/internal/iec104ctl/iec"
	"github.com/otfabric/iec60870ctl/internal/runner"
	"github.com/spf13/cobra"
)

func newCommandCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "command",
		Aliases: []string{"cmd"},
		Short:   "Send a process command: switch, step or set point, direct or select-before-operate",
		Long: `Send a command in control direction to the information object --ioa and wait for the
activation confirmation of the station.

  --type single               C_SC_NA_1  --value on|off
  --type double               C_DC_NA_1  --value on|off
  --type step                 C_RC_NA_1  --value higher|lower
  --type setpoint-normalized  C_SE_NA_1  --value -1 … <1
  --type setpoint-scaled      C_SE_NB_1  --value -32768 … 32767
  --type setpoint-float       C_SE_NC_1  --value any number
  --type bitstring            C_BO_NA_1  --value 0 … 4294967295 (or 0x hex)

--mode chooses the procedure:

  execute  direct execute (default)
  sbo      select before operate: select, and when that is confirmed, execute
  select   select only, to see whether the station accepts the selection
  cancel   select, then deactivate the selection (cause "deactivation")

"select" and "cancel" execute nothing: they show how the station handles the two steps.
"cancel" deactivates its selection at once; after "select" the selection stays until the
station ends it. The simulated station of "server start" ends a selection with the
connection that made it; a real station may keep it until its own select time-out, and
meanwhile refuse a command for that object from the control centre.

--qualifier is the qualifier of command (none, short, long or persistent pulse) for
switching commands, or the QL number 0-127 for set points. --time-tag sends the
CP56Time2a variant of the type (for example C_SC_TA_1), stamped with the current time.

THIS CHANGES THE PROCESS. On real equipment a command opens a breaker, moves a tap changer
or changes a set point. Know what an address controls before you send to it. Everything is
validated before anything is sent, and a command is sent exactly once: it is never
repeated, also not when its confirmation does not arrive.

A command the station refuses (negative confirmation, or "unknown information object
address") ends with exit code 6.`,
		Example: `
  # Close a breaker: single command, direct execute
  iec104ctl client command --ip 192.168.1.10 --type single --ioa 5000 --value on

  # Open a disconnector with select-before-operate and a short pulse
  iec104ctl client command --ip 192.168.1.10 --type double --ioa 5001 --value off --mode sbo --qualifier short

  # Tap changer one step higher, and wait up to 5 s for the activation termination
  iec104ctl client command --ip 192.168.1.10 --type step --ioa 5002 --value higher --wait-termination 5000

  # Floating point set point with a time tag, result as JSON
  iec104ctl client command --ip 192.168.1.10 --type setpoint-float --ioa 5003 --value 231.5 --time-tag --format json

  # Does the station accept a select for this object, and a cancel? Nothing is operated.
  iec104ctl client command --ip 192.168.1.10 --type single --ioa 5000 --value on --mode cancel
`,
	}
	cfg := config.CommandConfig{StationConfig: stationDefaults(), Mode: iec.ModeExecute, Qualifier: "none", OutputFormat: string(format.FormatText)}
	wire(cmd, &cfg,
		described("type", iec.CommandTypes()),
		described("mode", iec.CommandModes()),
		enum("qualifier", iec.CommandQualifiers()...),
		enum("value", "on", "off", "higher", "lower"))
	for _, f := range []string{"type", "ioa", "value"} {
		cli.MustMarkFlagRequired(cmd, f)
	}
	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		cfg.Debug = cli.Debug(cmd)
		return runner.RunFormattedChecked(cmd, cfg.OutputFormat,
			func() error {
				// The procedure and the qualifier are taken only from the command line. Without
				// --mode the procedure would be execute, which does more than a select or a
				// cancel of the variable: the command refuses instead.
				if err := envflag.NotFromEnvironment(cmd, "mode"); err != nil {
					return errs.InvalidInput(errs.CodeInvalidFlagCombination, err.Error(), nil)
				}
				envflag.CommandLineOnly(cmd, "qualifier")
				return iec.CheckCommand(&cfg)
			},
			func(ctx context.Context) (any, error) {
				return iec.Command(ctx, &cfg, cmd.ErrOrStderr())
			})
	}
	return cmd
}

func newClockCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "clock",
		Short: "Set the clock of a station (clock synchronization, C_CS_NA_1)",
		Long: `Send a clock synchronization command with the time --time and wait for the
confirmation. "now" (the default) is the time of this computer at the moment of sending.

The time is sent in the zone --timezone names (UTC by default): it must be the zone the
station keeps its clock in, since the time tag on the wire carries no zone.

THIS CHANGES THE STATION: the time tags of its events follow its clock. The command is
sent once and not repeated.`,
		Example: `
  # Set the clock of station 1 to the current time, UTC on the wire
  iec104ctl client clock --ip 192.168.1.10 --ca 1

  # A station that keeps local time
  iec104ctl client clock --ip 192.168.1.10 --timezone Europe/Brussels

  # Every station behind the connection, to a given time
  iec104ctl client clock --ip 192.168.1.10 --ca 65535 --time 2026-01-31T12:00:00Z
`,
	}
	cfg := config.ClockConfig{StationConfig: stationDefaults(), Time: "now", OutputFormat: string(format.FormatText)}
	wire(cmd, &cfg)
	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		cfg.Debug = cli.Debug(cmd)
		return runner.RunFormattedChecked(cmd, cfg.OutputFormat,
			func() error {
				// A time other than now, and the broadcast address, are taken only from the
				// command line.
				var flags []string
				if t := strings.TrimSpace(cfg.Time); t != "" && !strings.EqualFold(t, "now") {
					flags = append(flags, "time")
				}
				commandLineOnly(cmd, cfg.CommonAddr, flags...)
				return iec.CheckClock(&cfg)
			},
			func(ctx context.Context) (any, error) {
				return iec.Clock(ctx, &cfg, cmd.ErrOrStderr())
			})
	}
	return cmd
}

func newResetCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "reset",
		Short: "Send a reset process command (C_RP_NA_1)",
		Long: `Send a reset process command and wait for the confirmation.

  --qualifier process  general reset of the process: the station restarts its application
  --qualifier events   reset of the pending information with time tag (the event buffer)

THIS CHANGES THE STATION: a process reset restarts it, and it may drop its connections.
--qualifier has no default for that reason. The command is sent once and not repeated.`,
		Example: `
  # Clear the event buffer of station 1
  iec104ctl client reset --ip 192.168.1.10 --ca 1 --qualifier events

  # Restart the process of station 1
  iec104ctl client reset --ip 192.168.1.10 --ca 1 --qualifier process
`,
	}
	cfg := config.ResetConfig{StationConfig: stationDefaults(), OutputFormat: string(format.FormatText)}
	wire(cmd, &cfg, described("qualifier", iec.ResetQualifiers()))
	cli.MustMarkFlagRequired(cmd, "qualifier")
	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		cfg.Debug = cli.Debug(cmd)
		return runner.RunFormattedChecked(cmd, cfg.OutputFormat,
			func() error {
				commandLineOnly(cmd, cfg.CommonAddr) // the broadcast address: command line only
				return iec.CheckReset(&cfg)
			},
			func(ctx context.Context) (any, error) {
				return iec.Reset(ctx, &cfg, cmd.ErrOrStderr())
			})
	}
	return cmd
}
