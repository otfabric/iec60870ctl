// SPDX-License-Identifier: MIT

package iec

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"

	"github.com/otfabric/go-iec60870-5/asdu"
	"github.com/otfabric/go-iec60870-5/iec101"
	"github.com/otfabric/go-iec60870-5/link"
	"github.com/otfabric/go-iec60870-5/telecontrol"
	"github.com/otfabric/iec60870ctl/internal/errs"
	"github.com/otfabric/iec60870ctl/internal/iec101ctl/types"
)

// WrapError classifies an error of go-iec60870-5 as a typed CLI error with a stable kind and
// code. A typed error is returned as it is.
func WrapError(err error) error {
	if err == nil {
		return nil
	}
	if _, ok := errs.As(err); ok {
		return err
	}
	if neg, ok := errors.AsType[*telecontrol.NegativeError](err); ok {
		return errs.New(errs.KindNegative, negativeCode(neg.Cause()), negativeMessage(neg), err)
	}
	switch {
	case errors.Is(err, context.Canceled):
		return errs.New(errs.KindInterrupted, errs.CodeContextCanceled, "interrupted", err)
	case errors.Is(err, iec101.ErrProfile):
		return errs.InvalidInput(errs.CodeProfileViolation, fmt.Sprintf("invalid input: %v", err), err)
	case errors.Is(err, link.ErrTimeout):
		return errs.New(errs.KindTimeout, errs.CodeLinkTimeout, fmt.Sprintf("timeout: the station did not answer the link layer: %v", err), err)
	case errors.Is(err, link.ErrNack), errors.Is(err, link.ErrDataFlow), errors.Is(err, link.ErrNotFunctioning), errors.Is(err, link.ErrNotImplemented):
		return errs.New(errs.KindProtocol, errs.CodeLinkRefused, fmt.Sprintf("protocol error: %v", err), err)
	case errors.Is(err, link.ErrUnexpected):
		return errs.New(errs.KindProtocol, errs.CodeProtocolViolation, fmt.Sprintf("protocol error: %v", err), err)
	case errors.Is(err, iec101.ErrLinkDown):
		return errs.New(errs.KindTransport, errs.CodeLinkDown, fmt.Sprintf("connection error: %v", err), err)
	case errors.Is(err, context.DeadlineExceeded):
		return errs.New(errs.KindTimeout, errs.CodeRequestTimeout, fmt.Sprintf("timeout: no answer from the station: %v", err), err)
	case errors.Is(err, telecontrol.ErrBusy):
		return errs.New(errs.KindProtocol, errs.CodeRequestBusy, fmt.Sprintf("protocol error: %v", err), err)
	case errors.Is(err, iec101.ErrClosed), errors.Is(err, link.ErrClosed), errors.Is(err, io.EOF), errors.Is(err, io.ErrUnexpectedEOF), errors.Is(err, net.ErrClosed):
		return errs.New(errs.KindTransport, errs.CodeConnectionLost, fmt.Sprintf("connection error: %v", err), err)
	case errors.Is(err, iec101.ErrInvalidConfig), errors.Is(err, asdu.ErrInvalidValue), errors.Is(err, asdu.ErrTypeMismatch):
		return errs.InvalidInput(errs.CodeInvalidInput, fmt.Sprintf("invalid input: %v", err), err)
	}
	if _, ok := errors.AsType[net.Error](err); ok {
		return errs.New(errs.KindTransport, errs.CodeConnectionLost, fmt.Sprintf("connection error: %v", err), err)
	}
	return errs.Internal(err)
}

// connectError classifies a failure to open the line.
func connectError(err error) error {
	if errors.Is(err, context.Canceled) {
		return WrapError(err)
	}
	return errs.New(errs.KindTransport, errs.CodeTransportConnectFailed, fmt.Sprintf("connection error: %v", err), err)
}

func negativeCode(c asdu.Cause) string {
	switch c {
	case asdu.CauseUnknownType:
		return errs.CodeUnknownType
	case asdu.CauseUnknownCause:
		return errs.CodeUnknownCause
	case asdu.CauseUnknownCommonAddr:
		return errs.CodeUnknownCommonAddr
	case asdu.CauseUnknownIOA:
		return errs.CodeUnknownIOA
	default:
		return errs.CodeNegativeConfirmation
	}
}

func negativeMessage(neg *telecontrol.NegativeError) string {
	a := neg.ASDU
	if a == nil {
		return "the station refused the request"
	}
	what := fmt.Sprintf("%s to station %d", a.Type, a.CommonAddr)
	switch a.Cause {
	case asdu.CauseUnknownType:
		return fmt.Sprintf("the station refused %s: unknown type identification", what)
	case asdu.CauseUnknownCause:
		return fmt.Sprintf("the station refused %s: unknown cause of transmission", what)
	case asdu.CauseUnknownCommonAddr:
		return fmt.Sprintf("the station refused %s: unknown common address", what)
	case asdu.CauseUnknownIOA:
		return fmt.Sprintf("the station refused %s: unknown information object address", what)
	default:
		return fmt.Sprintf("the station refused %s: negative confirmation (%s)", what, a.Cause)
	}
}

// EmbeddedErrorInfo renders err as the error object of a row or record in a result.
func EmbeddedErrorInfo(err error) *types.ErrorInfo {
	if err == nil {
		return nil
	}
	e, _ := errs.As(WrapError(err))
	return &types.ErrorInfo{Kind: string(e.Kind), Code: e.Code, Message: e.Error()}
}

// Fatal reports whether err ends the session, so that a command working through a list
// should stop instead of trying the next item: a refusal or a request that timed out leaves
// the link up; a link that went down, a lost line or an interruption do not.
func Fatal(err error) bool {
	if err == nil {
		return false
	}
	if _, ok := errors.AsType[*telecontrol.NegativeError](err); ok {
		return false
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, iec101.ErrLinkDown) || errors.Is(err, iec101.ErrClosed) {
		return true
	}
	return !errors.Is(err, context.DeadlineExceeded)
}
