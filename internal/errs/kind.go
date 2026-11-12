// SPDX-License-Identifier: MIT

package errs

// Kind is a coarse failure category for exit-code mapping and JSON error.kind.
type Kind string

const (
	KindUsage        Kind = "usage"
	KindInvalidInput Kind = "invalid_input"
	KindTransport    Kind = "transport"
	KindTimeout      Kind = "timeout"
	KindProtocol     Kind = "protocol"
	// KindNegative is a request the station answered and refused: a negative confirmation
	// or an "unknown type / cause / common address / object address" reply.
	KindNegative    Kind = "negative_confirmation"
	KindOutput      Kind = "output"
	KindInternal    Kind = "internal"
	KindInterrupted Kind = "interrupted"
)
