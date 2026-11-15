// SPDX-License-Identifier: MIT

package client

import (
	"strings"

	"github.com/otfabric/iec60870ctl/internal/envflag"
	"github.com/otfabric/iec60870ctl/internal/errs"
	"github.com/otfabric/iec60870ctl/internal/iec101ctl/config"
	"github.com/otfabric/iec60870ctl/internal/iec101ctl/iec"
	"github.com/spf13/cobra"
)

// The parameters that decide what a request changes at a station, and a common address
// that sends such a request to every station of the line, are taken only from the command
// line. When one of them is set by its environment variable alone, the flag gets its
// built-in default back and one line on stderr names the variable
// ([envflag.CommandLineOnly]); this happens before the configuration is checked, so the
// default is what is checked and sent. --mode of client command is the exception: its
// default would execute, so a mode from the environment refuses the command
// ([envflag.NotFromEnvironment]). Read-only uses of the same variables are unchanged.

// commandLineBroadcast takes a broadcast --ca (every station of the line, for the
// --ca-size in use) only from the command line.
func commandLineBroadcast(cmd *cobra.Command, cfg *config.StationConfig) {
	if cfg.CommonAddr == iec.BroadcastCA(int(cfg.CASize)) {
		envflag.CommandLineOnly(cmd, "ca")
	}
}

// checkedCounters takes a --freeze other than read only from the command line, and with
// such a freeze a broadcast --ca, then validates `client counters`. A read changes no
// counter, and may go to every station from the environment.
func checkedCounters(cmd *cobra.Command, cfg *config.CountersConfig) error {
	if cfg.Freeze != "read" {
		envflag.CommandLineOnly(cmd, "freeze")
	}
	if cfg.Freeze != "read" {
		commandLineBroadcast(cmd, &cfg.StationConfig)
	}
	return iec.CheckCounters(cfg)
}

// checkedCommand takes --mode and --qualifier only from the command line, then validates
// `client command`. The default of --mode (execute) does more than a select or a cancel:
// a mode set by IEC101CTL_COMMAND_MODE alone refuses the command, before anything is
// sent.
func checkedCommand(cmd *cobra.Command, cfg *config.CommandConfig) error {
	if err := envflag.NotFromEnvironment(cmd, "mode"); err != nil {
		return errs.InvalidInput(errs.CodeInvalidFlagCombination, err.Error(), nil)
	}
	envflag.CommandLineOnly(cmd, "qualifier")
	return iec.CheckCommand(cfg)
}

// checkedClock takes a --time other than now and a broadcast --ca only from the command
// line, then validates `client clock`.
func checkedClock(cmd *cobra.Command, cfg *config.ClockConfig) error {
	if t := strings.TrimSpace(cfg.Time); t != "" && !strings.EqualFold(t, "now") {
		envflag.CommandLineOnly(cmd, "time")
	}
	commandLineBroadcast(cmd, &cfg.StationConfig)
	return iec.CheckClock(cfg)
}

// checkedReset takes a broadcast --ca only from the command line, then validates
// `client reset`. --qualifier is a required flag: its variable never replaces it.
func checkedReset(cmd *cobra.Command, cfg *config.ResetConfig) error {
	commandLineBroadcast(cmd, &cfg.StationConfig)
	return iec.CheckReset(cfg)
}

// checkedDelay takes --set only from the command line, then validates `client delay`.
func checkedDelay(cmd *cobra.Command, cfg *config.DelayConfig) error {
	if strings.TrimSpace(cfg.Set) != "" {
		envflag.CommandLineOnly(cmd, "set")
	}
	return iec.CheckDelay(cfg)
}
