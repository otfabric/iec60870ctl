// SPDX-License-Identifier: MIT

package client

import (
	"context"

	"github.com/otfabric/iec60870ctl/internal/cli"
	"github.com/otfabric/iec60870ctl/internal/iec103ctl/config"
	"github.com/otfabric/iec60870ctl/internal/iec103ctl/iec"
	"github.com/otfabric/iec60870ctl/internal/iec103ctl/types"
	"github.com/spf13/cobra"
)

func newInterrogateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "interrogate",
		Aliases: []string{"gi"},
		Short:   "Run a general interrogation (ASDU 7) and list the states of the device",
		Long: `Send a general interrogation (ASDU 7 with a new scan number, clause 7.4.3) and collect
what the device answers with cause 9 until the termination (ASDU 8) with the same scan
number: the state of every indication that takes part (ASDU 1 or 2), with its time tag.

Answers of an earlier interrogation cycle (another scan number) are left out, as clause
7.4.3 has the controlling system do. Measurands are not part of the interrogation: they
come as class 2 data, see "client monitor".`,
		Example: `
  # General interrogation of the device at link address 1 behind a terminal server
  iec103ctl client interrogate --url tcp://10.0.0.20:2401

  # On a serial line at 19200 baud, link address 5, as JSON
  iec103ctl client interrogate --url serial:///dev/ttyUSB0 --baud 19200 --link-address 5 --format json
`,
	}
	cfg := config.InterrogateConfig{ConnConfig: connDefaults(), OutputFormat: stdoutFormat}
	wire(cmd, &cfg)
	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		return formatted(cmd, cfg.OutputFormat, &cfg.Debug,
			func() error { return iec.CheckInterrogate(&cfg) },
			func(ctx context.Context) (*types.InterrogateResult, error) {
				return iec.Interrogate(ctx, &cfg, cmd.ErrOrStderr())
			})
	}
	return cmd
}

func newCommandCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "command",
		Aliases: []string{"cmd"},
		Short:   "Send a general command (ASDU 20) and wait for its acknowledgement",
		Long: `Send a general command (ASDU 20, clause 7.4.4) with function type --fun, information
number --inf and the double command --value (on or off), and wait for the device's
acknowledgement: ASDU 1 with the same FUN and INF, cause 20 and the command's return
information identifier. A negative acknowledgement (cause 21) ends with exit code 6.

Table 17 defines the commands: INF 16 auto-recloser, 17 teleprotection, 18 protection,
19 LED reset, 23-26 activate characteristic 1-4, with a function type of a compatible
range; INF 19 and 23-26 take ON only. A private function type (0-127, and the gaps of
clause 7.2.5.1) takes any INF. Anything else is refused before it is sent (exit code 3).

--wait-report also waits for the change of state the command causes, which the device
reports with cause 12 after the acknowledgement (figure 7).

THIS CHANGES THE PROCESS. On a real device a command switches the auto-recloser or the
protection on or off. Everything is validated before anything is sent, and the command is
sent exactly once: it is never repeated, also not when its acknowledgement does not come.`,
		Example: `
  # Switch the auto-recloser of an overcurrent protection off
  iec103ctl client command --url tcp://10.0.0.20 --fun 160 --inf 16 --value off

  # LED reset, and show the change of state that follows
  iec103ctl client command --url tcp://10.0.0.20 --fun 160 --inf 19 --value on --wait-report 1000
`,
	}
	cfg := config.CommandConfig{ConnConfig: connDefaults(), OutputFormat: stdoutFormat}
	wire(cmd, &cfg, enum("value", "on", "off"))
	for _, f := range []string{"fun", "inf", "value"} {
		cli.MustMarkFlagRequired(cmd, f)
	}
	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		return formatted(cmd, cfg.OutputFormat, &cfg.Debug,
			func() error { return iec.CheckCommand(&cfg) },
			func(ctx context.Context) (*types.CommandResult, error) {
				return iec.Command(ctx, &cfg, cmd.ErrOrStderr())
			})
	}
	return cmd
}

func newClockCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "clock",
		Short: "Synchronize the clock of the device (ASDU 6)",
		Long: `Send a time synchronization (ASDU 6, clause 7.4.2) with the time --time and wait for
the device's answer, the ASDU 6 with the time it set. "now" (the default) is the time of
this computer at the moment of sending.

With --broadcast the synchronization goes to link address 255 with send/no reply: every
device on the line takes it and none confirms it.

The time is sent in the zone --timezone names (UTC by default): the zone the devices keep
their clock in, since the time tag carries none.

THIS CHANGES THE DEVICE: the time tags of its events follow its clock. The command is
sent once and not repeated.`,
		Example: `
  iec103ctl client clock --url tcp://10.0.0.20
  iec103ctl client clock --url serial:///dev/ttyUSB0 --timezone Europe/Brussels
  iec103ctl client clock --url serial:///dev/ttyUSB0 --broadcast --time 2026-01-31T12:00:00Z
`,
	}
	cfg := config.ClockConfig{ConnConfig: connDefaults(), Time: "now", OutputFormat: stdoutFormat}
	wire(cmd, &cfg)
	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		return formatted(cmd, cfg.OutputFormat, &cfg.Debug,
			func() error { return iec.CheckClock(&cfg) },
			func(ctx context.Context) (*types.ClockResult, error) { return iec.Clock(ctx, &cfg, cmd.ErrOrStderr()) })
	}
	return cmd
}

func newIdentifyCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "identify",
		Short: "Show the identification of the device (ASDU 5)",
		Long: `Initialize the link and show the identification (ASDU 5, clause 7.4.1, table 8) the
reset makes the device send: its function type, compatibility level, the eight characters
naming the manufacturer and the four octets of software identification. The reset of the
communication unit gives cause 4, --reset-fcb cause 3.

A device also identifies itself once after start (cause 5 start/restart or 6 power on):
the identifications of every cause that arrive within --wait milliseconds after the first
are listed too.`,
		Example: `
  iec103ctl client identify --url tcp://10.0.0.20
  iec103ctl client identify --url serial:///dev/ttyUSB0 --link-address 3 --format json
`,
	}
	cfg := config.IdentifyConfig{ConnConfig: connDefaults(), Wait: 300, OutputFormat: stdoutFormat}
	wire(cmd, &cfg)
	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		return formatted(cmd, cfg.OutputFormat, &cfg.Debug,
			func() error { return iec.CheckIdentify(&cfg) },
			func(ctx context.Context) (*types.IdentifyResult, error) {
				return iec.Identify(ctx, &cfg, cmd.ErrOrStderr())
			})
	}
	return cmd
}
