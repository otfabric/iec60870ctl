// SPDX-License-Identifier: MIT

package types

import "github.com/otfabric/iec60870ctl/internal/errs"

// ErrorInfo is the structured error object for JSON output and embedded per-row failures
// (shared by every tool of the repository).
type ErrorInfo = errs.Info

// ErrorEnvelope is the top-level JSON shape for fatal errors (--format json).
type ErrorEnvelope = errs.Envelope

// ErrorMessage returns the display/API message for an embedded error pointer.
func ErrorMessage(e *ErrorInfo) string { return errs.Message(e) }
