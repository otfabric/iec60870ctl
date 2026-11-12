// SPDX-License-Identifier: MIT

// Package cmd is the command tree of iec103ctl.
package cmd

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/otfabric/iec60870ctl/internal/buildinfo"
	clihelp "github.com/otfabric/iec60870ctl/internal/cli"
	"github.com/otfabric/iec60870ctl/internal/errs"
	"github.com/otfabric/iec60870ctl/internal/iec103ctl/cmd/client"
	"github.com/otfabric/iec60870ctl/internal/iec103ctl/cmd/server"
	"github.com/otfabric/iec60870ctl/internal/iec103ctl/config"
	"github.com/otfabric/iec60870ctl/internal/runner"
	"github.com/spf13/cobra"
)

// buildMeta holds link-time metadata set from main via SetBuildMeta.
var buildMeta struct {
	version, tag, commit, buildDate string
}

// SetBuildMeta is called from main before Execute; values come from ldflags -X main.*.
func SetBuildMeta(version, tag, commit, buildDate string) {
	buildMeta.version = version
	buildMeta.tag = tag
	buildMeta.commit = commit
	buildMeta.buildDate = buildDate
}

// NewRootCmd builds the command tree. Every call returns a fresh tree, with the flags of
// every command at the defaults the environment gives them.
func NewRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "iec103ctl",
		Short: "IEC 60870-5-103 command-line client and protection device simulator",
		Long: `iec103ctl speaks IEC 60870-5-103, the informative interface of protection equipment
(protection relays), in both roles.

As a controlling system ("iec103ctl client") it polls one item of protection equipment on
a serial line or a terminal server: general interrogation, general commands, clock
synchronization, identification, the generic services (settings and their descriptions)
and the transfer of disturbance data (recorded faults with their tags and channels). It
also monitors what the equipment sends and scans a line for link addresses. As protection
equipment ("iec103ctl server") it simulates a device from a model.

The line is the FT 1.2 byte stream of IEC 60870-5-1: a serial port (serial:///dev/ttyUSB0)
or one TCP connection to a terminal server that passes it through (tcp://host:2401).

Results go to stdout as text, a table or JSON (--format); progress and diagnostics go to
stderr. Exit codes are stable: see docs/exitcodes.md.`,
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.PersistentFlags().Bool("debug", false, "Trace every frame sent and received to stderr, and show the causes of an error")
	root.AddCommand(
		client.NewCmd(),
		server.NewCmd(),
		newCompletionCmd(),
		newVersionCmd(),
	)
	// Every other flag and argument completes no file names, except these files.
	clihelp.CompleteRest(root, map[string][]string{
		"config": {"yaml", "yml", "json"}, "output": nil, "csv": {"csv"},
	}, nil)
	return root
}

func newVersionCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "version",
		Short: "Print the version of iec103ctl",
		Long: `Print the version of iec103ctl on one line: the version, the git tag and commit it
was built from, the build date, the Go version and the platform. --json (or
IEC103CTL_FORMAT=json) prints the same as one JSON object.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			info := buildinfo.Resolve("iec103ctl", buildMeta.version, buildMeta.tag, buildMeta.commit, buildMeta.buildDate)
			line := info.String()
			asJSON, _ := cmd.Flags().GetBool("json")
			if asJSON || strings.EqualFold(strings.TrimSpace(config.Getenv("IEC103CTL_FORMAT")), "json") {
				line = info.JSON()
			}
			_, err := fmt.Fprintln(cmd.OutOrStdout(), line)
			return err
		},
	}
	cmd.Flags().Bool("json", false, "Print the version as one JSON object")
	return cmd
}

// Execute runs the root command and returns the process exit code (main should call os.Exit).
func Execute() int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return Run(ctx, os.Args[1:], os.Stdout, os.Stderr)
}

// Run executes iec103ctl with args, writing to stdout and stderr, and returns the exit
// code. It is Execute without the process: what the tests drive.
func Run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	root, err := buildRoot()
	if err != nil {
		return runner.RenderFatal(nil, err, stdout, stderr, false)
	}
	root.SetArgs(args)
	root.SetOut(stdout)
	root.SetErr(stderr)

	inv := &runner.Invocation{}
	ctx = runner.WithInvocation(ctx, inv)
	err = root.ExecuteContext(ctx)
	debug, _ := root.PersistentFlags().GetBool("debug")
	if err != nil {
		return runner.RenderFatal(inv, usageError(err), stdout, stderr, debug)
	}
	if inv.SuccessExitSet {
		return inv.SuccessExitCode
	}
	return errs.ExitOK
}

// buildRoot is NewRootCmd with a malformed environment variable reported as invalid input
// instead of a panic: the configuration of every command is read when the tree is built.
func buildRoot() (root *cobra.Command, err error) {
	defer func() {
		if r := recover(); r != nil {
			msg := strings.TrimPrefix(fmt.Sprint(r), "iec103ctl: ")
			err = errs.InvalidInput(errs.CodeInvalidInput, msg, nil)
		}
	}()
	return NewRootCmd(), nil
}

// usageError classifies what Cobra itself rejects (an unknown command or flag, a missing
// required flag, a value a flag cannot hold) as a usage error: exit code 2.
func usageError(err error) error {
	if _, ok := errs.As(err); ok {
		return err
	}
	return errs.New(errs.KindUsage, errs.CodeInvalidInput, err.Error(), err)
}
