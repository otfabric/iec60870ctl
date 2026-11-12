// SPDX-License-Identifier: MIT

package iec

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"

	iec104 "github.com/otfabric/go-iec104"
	"github.com/otfabric/go-iec104/asdu"
	"github.com/otfabric/iec104ctl/internal/errs"
	"github.com/otfabric/iec104ctl/internal/types"
)

// WrapError classifies an error of go-iec104 as a typed CLI error with a stable kind and
// code. A typed error is returned as it is.
func WrapError(err error) error {
	if err == nil {
		return nil
	}
	if _, ok := errs.As(err); ok {
		return err
	}
	if neg, ok := errors.AsType[*iec104.NegativeError](err); ok {
		return errs.New(errs.KindNegative, negativeCode(neg.Cause()), negativeMessage(neg), err)
	}
	var (
		certErr  *tls.CertificateVerificationError
		unknown  x509.UnknownAuthorityError
		hostname x509.HostnameError
		record   tls.RecordHeaderError
	)
	switch {
	case errors.Is(err, context.Canceled):
		return errs.New(errs.KindInterrupted, errs.CodeContextCanceled, "interrupted", err)
	case errors.As(err, &certErr), errors.As(err, &unknown), errors.As(err, &hostname), errors.As(err, &record):
		return errs.New(errs.KindTransport, errs.CodeTransportTLSFailed, fmt.Sprintf("TLS error: %v", err), err)
	case isSecError(err):
		return errs.New(errs.KindTransport, errs.CodeSecurityFailed, fmt.Sprintf("security error: %v", err), err)
	case errors.Is(err, iec104.ErrConnectFailed):
		if isTimeout(err) {
			return errs.New(errs.KindTransport, errs.CodeTransportConnectFailed, fmt.Sprintf("connection error: timed out: %v", err), err)
		}
		return errs.New(errs.KindTransport, errs.CodeTransportConnectFailed, fmt.Sprintf("connection error: %v", err), err)
	case errors.Is(err, iec104.ErrTimeout):
		return errs.New(errs.KindTimeout, errs.CodeLinkTimeout, fmt.Sprintf("timeout: the station did not acknowledge in time (t1): %v", err), err)
	case errors.Is(err, context.DeadlineExceeded):
		return errs.New(errs.KindTimeout, errs.CodeRequestTimeout, fmt.Sprintf("timeout: no answer from the station: %v", err), err)
	case errors.Is(err, iec104.ErrConnectionLost):
		return errs.New(errs.KindTransport, errs.CodeConnectionLost, fmt.Sprintf("connection error: %v", err), err)
	case errors.Is(err, iec104.ErrNotConnected), errors.Is(err, iec104.ErrClosed):
		return errs.New(errs.KindTransport, errs.CodeConnectionLost, fmt.Sprintf("connection error: %v", err), err)
	case errors.Is(err, iec104.ErrProtocol):
		return errs.New(errs.KindProtocol, errs.CodeProtocolViolation, fmt.Sprintf("protocol error: %v", err), err)
	case errors.Is(err, iec104.ErrNotStarted):
		return errs.New(errs.KindProtocol, errs.CodeNotStarted, fmt.Sprintf("protocol error: %v", err), err)
	case errors.Is(err, iec104.ErrBusy):
		return errs.New(errs.KindProtocol, errs.CodeRequestBusy, fmt.Sprintf("protocol error: %v", err), err)
	case errors.Is(err, iec104.ErrInvalidOption), errors.Is(err, asdu.ErrInvalidValue), errors.Is(err, asdu.ErrTypeMismatch):
		return errs.InvalidInput(errs.CodeInvalidInput, fmt.Sprintf("invalid input: %v", err), err)
	}
	if _, ok := errors.AsType[net.Error](err); ok {
		return errs.New(errs.KindTransport, errs.CodeConnectionLost, fmt.Sprintf("connection error: %v", err), err)
	}
	return errs.Internal(err)
}

func isTimeout(err error) bool {
	var netErr net.Error
	return errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &netErr) && netErr.Timeout())
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

func negativeMessage(neg *iec104.NegativeError) string {
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
	if e, ok := errs.As(WrapError(err)); ok {
		return &types.ErrorInfo{Kind: string(e.Kind), Code: e.Code, Message: e.Error()}
	}
	return &types.ErrorInfo{Kind: string(errs.KindInternal), Code: errs.CodeInternalUnexpected, Message: err.Error()}
}

// Fatal reports whether err ends the connection, so that a command working through a list
// should stop instead of trying the next item.
func Fatal(err error) bool {
	if err == nil {
		return false
	}
	if _, ok := errors.AsType[*iec104.NegativeError](err); ok {
		return false
	}
	if errors.Is(err, context.Canceled) {
		return true
	}
	// A request that timed out leaves the connection up; t1 and a lost connection do not.
	if errors.Is(err, context.DeadlineExceeded) && !errors.Is(err, iec104.ErrConnectFailed) {
		return false
	}
	return true
}
