// SPDX-License-Identifier: MIT

package runner

import (
	"context"

	"github.com/otfabric/iec104ctl/internal/errs"
	"github.com/otfabric/iec104ctl/internal/format"
	"github.com/spf13/cobra"
)

// FormatRunOption configures [RunFormattedWithOutputFormat].
type FormatRunOption func(*formatRunConfig)

type formatRunConfig struct {
	successExit func(any) int
}

// WithSuccessExit sets process exit after a successful formatted write (ExitOK or ExitPartial).
func WithSuccessExit(fn func(any) int) FormatRunOption {
	return func(c *formatRunConfig) {
		c.successExit = fn
	}
}

// AttachOutputFormat parses the output format string and records it on Invocation for fatal rendering.
func AttachOutputFormat(cmd *cobra.Command, outputFormat string) (format.OutputFormat, error) {
	outFmt, ferr := format.Parse(outputFormat)
	if ferr != nil {
		return "", errs.FormatInvalid(ferr)
	}
	if inv := InvocationFrom(cmd.Context()); inv != nil {
		inv.SetOutputFormat(outFmt)
	}
	return outFmt, nil
}

// RunFormattedChecked attaches output format, runs validate, then [RunFormattedWithOutputFormat].
// It is the validate → collect → stdout write flow every command with one final result shares.
func RunFormattedChecked(cmd *cobra.Command, outputFormat string, validate func() error, collect func(context.Context) (any, error), opts ...FormatRunOption) error {
	outFmt, err := AttachOutputFormat(cmd, outputFormat)
	if err != nil {
		return err
	}
	if err := validate(); err != nil {
		return errs.WrapValidation(err)
	}
	_, err = RunFormattedWithOutputFormat(cmd, outFmt, collect, opts...)
	return err
}

// RunFormattedWithOutputFormat runs collect and format.Write using an already-parsed output format.
func RunFormattedWithOutputFormat(cmd *cobra.Command, outFmt format.OutputFormat, collect func(context.Context) (any, error), opts ...FormatRunOption) (RunResult, error) {
	var cfg formatRunConfig
	for _, o := range opts {
		o(&cfg)
	}

	ctx := cmd.Context()
	inv := InvocationFrom(ctx)
	if inv != nil {
		inv.SetOutputFormat(outFmt)
	}

	v, cerr := collect(ctx)
	if cerr != nil {
		return RunResult{}, cerr
	}

	if err := format.Write(cmd.OutOrStdout(), outFmt, v); err != nil {
		return RunResult{}, outputWriteErr(outFmt, err)
	}

	code := errs.ExitOK
	if cfg.successExit != nil {
		code = cfg.successExit(v)
	}
	if inv != nil {
		inv.SetSuccessExit(code)
	}
	return RunResult{ExitCode: code}, nil
}

func outputWriteErr(f format.OutputFormat, err error) error {
	if f == format.FormatTable {
		return errs.Output(errs.CodeTableRenderFailed, err)
	}
	return errs.Output(errs.CodeJSONEncodeFailed, err)
}
