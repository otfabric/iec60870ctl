// SPDX-License-Identifier: MIT

package iec

import (
	"context"
	"errors"
	"fmt"
	"net"

	"github.com/otfabric/go-iec60870-5/iec103"
	"github.com/otfabric/go-iec60870-5/link"
	"github.com/otfabric/iec60870ctl/internal/errs"
	"github.com/otfabric/iec60870ctl/internal/iec103ctl/types"
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
	switch {
	case errors.Is(err, context.Canceled):
		return errs.New(errs.KindInterrupted, errs.CodeContextCanceled, "interrupted", err)
	case errors.Is(err, iec103.ErrAborted):
		return errs.New(errs.KindNegative, errs.CodeIEC103TransferAborted, fmt.Sprintf("the device aborted the transfer: %v", err), err)
	case errors.Is(err, iec103.ErrNegative):
		return errs.New(errs.KindNegative, errs.CodeNegativeConfirmation, fmt.Sprintf("the device refused the request: %v", err), err)
	case errors.Is(err, context.DeadlineExceeded):
		return errs.New(errs.KindTimeout, errs.CodeRequestTimeout, fmt.Sprintf("timeout: no answer from the device: %v", err), err)
	case errors.Is(err, iec103.ErrLinkDown):
		return errs.New(errs.KindTransport, errs.CodeLinkDown, fmt.Sprintf("connection error: %v", err), err)
	case errors.Is(err, iec103.ErrClosed), errors.Is(err, link.ErrClosed):
		return errs.New(errs.KindTransport, errs.CodeConnectionLost, fmt.Sprintf("connection error: %v", err), err)
	case errors.Is(err, link.ErrTimeout):
		return errs.New(errs.KindTimeout, errs.CodeLinkTimeout, fmt.Sprintf("timeout: the device did not answer the link layer: %v", err), err)
	case errors.Is(err, link.ErrNack), errors.Is(err, link.ErrDataFlow):
		return errs.New(errs.KindProtocol, errs.CodeRequestBusy, fmt.Sprintf("protocol error: the device did not accept the message: %v", err), err)
	case errors.Is(err, link.ErrNotFunctioning), errors.Is(err, link.ErrNotImplemented):
		return errs.New(errs.KindProtocol, errs.CodeProtocolViolation, fmt.Sprintf("protocol error: %v", err), err)
	case errors.Is(err, iec103.ErrBusy):
		return errs.New(errs.KindProtocol, errs.CodeRequestBusy, fmt.Sprintf("protocol error: %v", err), err)
	case errors.Is(err, iec103.ErrInvalidConfig), errors.Is(err, iec103.ErrInvalid), errors.Is(err, link.ErrNoData), errors.Is(err, iec103.ErrTooLong):
		return errs.InvalidInput(errs.CodeInvalidInput, fmt.Sprintf("invalid input: %v", err), err)
	case errors.Is(err, iec103.ErrShort), errors.Is(err, iec103.ErrTrailing), errors.Is(err, link.ErrUnexpected):
		return errs.New(errs.KindProtocol, errs.CodeProtocolViolation, fmt.Sprintf("protocol error: %v", err), err)
	}
	if _, ok := errors.AsType[net.Error](err); ok {
		return errs.New(errs.KindTransport, errs.CodeConnectionLost, fmt.Sprintf("connection error: %v", err), err)
	}
	return errs.Internal(err)
}

// EmbeddedErrorInfo renders err as the error object of a row in a result.
func EmbeddedErrorInfo(err error) *types.ErrorInfo {
	if err == nil {
		return nil
	}
	e, _ := errs.As(WrapError(err))
	return &types.ErrorInfo{Kind: string(e.Kind), Code: e.Code, Message: e.Error()}
}
