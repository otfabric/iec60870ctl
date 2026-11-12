// SPDX-License-Identifier: MIT

package runner

import (
	"context"

	"github.com/otfabric/iec104ctl/internal/errs"
	"github.com/otfabric/iec104ctl/internal/format"
	"github.com/spf13/cobra"
)

// RunStream is the streaming counterpart of [RunFormattedChecked] for commands that write
// records continuously instead of one final result. It attaches the output format, runs
// validate, then calls run with a [format.StreamWriter] on stdout. run returns the process
// exit code for a completed stream (ExitOK or ExitPartial).
func RunStream(cmd *cobra.Command, outputFormat string, validate func() error, run func(ctx context.Context, out *format.StreamWriter) (int, error)) error {
	outFmt, err := AttachOutputFormat(cmd, outputFormat)
	if err != nil {
		return err
	}
	if err := validate(); err != nil {
		return errs.WrapValidation(err)
	}
	code, err := run(cmd.Context(), format.NewStreamWriter(cmd.OutOrStdout(), outFmt))
	if err != nil {
		return err
	}
	if inv := InvocationFrom(cmd.Context()); inv != nil {
		inv.SetSuccessExit(code)
	}
	return nil
}
