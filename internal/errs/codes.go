// SPDX-License-Identifier: MIT

package errs

// Stable machine-oriented codes for JSON error.code and scripts.
const (
	CodeInvalidFormat          = "invalid_format"
	CodeInvalidInput           = "invalid_input"
	CodeInvalidURL             = "invalid_url"
	CodeInvalidFlagCombination = "invalid_flag_combination"
	CodeInvalidConfig          = "invalid_config"

	CodeTransportConnectFailed = "transport_connect_failed"
	CodeTransportTLSFailed     = "transport_tls_failed"
	// The application-layer security of IEC 62351-5 could not be established: a refused
	// certificate, an algorithm not supported, a MAC that did not verify, or no answer to
	// the key management.
	CodeSecurityFailed = "security_failed"
	CodeConnectionLost = "connection_lost"
	CodeListenFailed   = "listen_failed"

	// The station did not answer a request in time.
	CodeRequestTimeout = "request_timeout"
	// An I frame was not acknowledged, or a U frame not confirmed, within t1.
	CodeLinkTimeout = "link_timeout"

	CodeProtocolViolation = "protocol_violation"
	CodeNotStarted        = "data_transfer_not_started"
	CodeRequestBusy       = "request_busy"

	// The station refused the request: P/N = 1 in the confirmation.
	CodeNegativeConfirmation = "negative_confirmation"
	// The station does not know the type, cause, common address or object address (causes 44..47).
	CodeUnknownType       = "unknown_type"
	CodeUnknownCause      = "unknown_cause"
	CodeUnknownCommonAddr = "unknown_common_address"
	CodeUnknownIOA        = "unknown_object_address"

	// Context was canceled (not invalid user input).
	CodeContextCanceled = "context_canceled"

	CodeJSONEncodeFailed       = "json_encode_failed"
	CodeTableRenderFailed      = "table_render_failed"
	CodeOutputWriteFailed      = "output_write_failed"
	CodeOutputFileWriteFailed  = "output_file_write_failed"
	CodeOutputFileCreateFailed = "output_file_create_failed"
	CodeInputFileOpenFailed    = "input_file_open_failed"
	CodeInputDecodeFailed      = "input_decode_failed"

	CodeInternalUnexpected = "internal_unexpected"
)
