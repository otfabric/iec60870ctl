// SPDX-License-Identifier: MIT

// Package cmd is the command tree of iec101ctl.
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
	"github.com/otfabric/iec60870ctl/internal/iec101ctl/cmd/client"
	"github.com/otfabric/iec60870ctl/internal/iec101ctl/cmd/discover"
	"github.com/otfabric/iec60870ctl/internal/iec101ctl/cmd/server"
	"github.com/otfabric/iec60870ctl/internal/iec101ctl/config"
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
		Use:   "iec101ctl",
		Short: "IEC 60870-5-101 command-line client and station simulator",
		Long: `iec101ctl speaks IEC 60870-5-101, the serial telecontrol protocol of power system
SCADA (FT 1.2 frames with the link procedures of IEC 60870-5-2), in both roles.

As a controlling station ("iec101ctl client") it brings up the link, interrogates a
station, monitors what it sends, sends commands and set points, synchronizes its clock and
measures the transmission delay. As a controlled station ("iec101ctl server") it simulates
an RTU from a model of points and commands. "iec101ctl discover" finds stations behind
terminal servers on a network.

The line is a serial port (serial:///dev/ttyUSB0) or a TCP connection to a terminal server
that carries the serial byte stream unchanged (tcp://host:2401). Over TCP it is still
IEC 60870-5-101, not IEC 60870-5-104.

Results go to stdout as text, a table or JSON (--format); progress and diagnostics go to
stderr. Exit codes are stable: see docs/exitcodes.md.`,
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.PersistentFlags().Bool("debug", false, "Trace every frame sent and received to stderr, and show the causes of an error")
	root.AddCommand(
		client.NewCmd(),
		server.NewCmd(),
		discover.NewCmd(),
		newCompletionCmd(),
		newVersionCmd(),
	)
	// Every other flag and argument completes no file names, except these files.
	clihelp.CompleteRest(root, map[string][]string{
		"config": {"yaml", "yml", "json"}, "output": nil, "serial": nil,
	}, nil)
	return root
}

func newVersionCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "version",
		Short: "Print the version of iec101ctl",
		Long: `Print the version of iec101ctl on one line: the version, the git tag and commit it
was built from, the build date, the Go version and the platform. --json (or
IEC101CTL_FORMAT=json) prints the same as one JSON object.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			info := buildinfo.Resolve("iec101ctl", buildMeta.version, buildMeta.tag, buildMeta.commit, buildMeta.buildDate)
			line := info.String()
			asJSON, _ := cmd.Flags().GetBool("json")
			if asJSON || strings.EqualFold(strings.TrimSpace(config.Getenv("IEC101CTL_FORMAT")), "json") {
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

// Run executes iec101ctl with args, writing to stdout and stderr, and returns the exit
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
			msg := strings.TrimPrefix(fmt.Sprint(r), "iec101ctl: ")
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
