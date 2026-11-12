// SPDX-License-Identifier: MIT

package meter

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"

	"github.com/otfabric/go-iec60870-5/ft12"
	"github.com/otfabric/go-iec60870-5/iec102"
	"github.com/otfabric/go-iec60870-5/link"
	"github.com/otfabric/iec60870ctl/internal/errs"
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
	if refused, ok := errors.AsType[*iec102.RefusedError](err); ok {
		return errs.New(errs.KindNegative, refusalCode(refused.Cause()), refusalMessage(refused), err)
	}
	switch {
	case errors.Is(err, context.Canceled):
		return errs.New(errs.KindInterrupted, errs.CodeContextCanceled, "interrupted", err)
	case errors.Is(err, link.ErrClosed), errors.Is(err, iec102.ErrClosed), errors.Is(err, io.EOF), errors.Is(err, io.ErrUnexpectedEOF), errors.Is(err, net.ErrClosed):
		return errs.New(errs.KindTransport, errs.CodeConnectionLost, fmt.Sprintf("connection error: %v", err), err)
	case errors.Is(err, link.ErrTimeout):
		return errs.New(errs.KindTimeout, errs.CodeLinkTimeout, fmt.Sprintf("timeout: the meter stopped answering the link: %v", err), err)
	case errors.Is(err, context.DeadlineExceeded):
		return errs.New(errs.KindTimeout, errs.CodeRequestTimeout, fmt.Sprintf("timeout: the meter did not complete the request in time: %v", err), err)
	case errors.Is(err, link.ErrNotFunctioning), errors.Is(err, link.ErrNotImplemented), errors.Is(err, link.ErrUnexpected),
		errors.Is(err, ft12.ErrMalformed), errors.Is(err, ft12.ErrChecksum), errors.Is(err, iec102.ErrShort), errors.Is(err, iec102.ErrTrailing):
		return errs.New(errs.KindProtocol, errs.CodeProtocolViolation, fmt.Sprintf("protocol error: %v", err), err)
	case errors.Is(err, iec102.ErrInvalid), errors.Is(err, iec102.ErrTooLong), errors.Is(err, iec102.ErrInvalidConfig), errors.Is(err, link.ErrInvalidConfig):
		return errs.InvalidInput(errs.CodeInvalidInput, fmt.Sprintf("invalid input: %v", err), err)
	}
	if _, ok := errors.AsType[net.Error](err); ok {
		return errs.New(errs.KindTransport, errs.CodeConnectionLost, fmt.Sprintf("connection error: %v", err), err)
	}
	return errs.Internal(err)
}

// refusalCode returns the error code of a cause of refusal.
func refusalCode(c iec102.Cause) string {
	switch c.Code() {
	case iec102.CauseNoRecord:
		return errs.CodeNoRecord
	case iec102.CauseUnknownType:
		return errs.CodeUnknownType
	case iec102.CauseUnknownRecordNum:
		return errs.CodeUnknownRecordNumber
	case iec102.CauseUnknownRecordAddr:
		return errs.CodeUnknownRecordAddr
	case iec102.CauseNoObject:
		return errs.CodeNoObject
	case iec102.CauseNoPeriod:
		return errs.CodeNoPeriod
	default:
		return errs.CodeNegativeConfirmation
	}
}

func refusalMessage(e *iec102.RefusedError) string {
	if e.ASDU == nil {
		return "the meter refused the request"
	}
	a := e.ASDU
	why := CauseName(a.Cause)
	if a.Cause.Negative() {
		why += ", negative"
	}
	return fmt.Sprintf("the meter refused %s on record %d of measuring point %d: %s (cause %d)",
		TypeName(a.Type), a.Record, a.CommonAddr, why, a.Cause.Code())
}
