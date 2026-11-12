// SPDX-License-Identifier: MIT

package errs

// Codes of the FT 1.2 link layer of IEC 60870-5-101 (iec101ctl).
const (
	// The link to the station was not initialized in time: request status of link and
	// reset of remote link were not both answered.
	CodeLinkInitFailed = "link_init_failed"
	// A request was made while the link was down: the station stopped answering the link
	// layer, and the link was not yet initialized again.
	CodeLinkDown = "link_down"
	// The link layer of the station refused a message: NACK, data flow control, "link
	// service not functioning" or "link service not implemented".
	CodeLinkRefused = "link_refused"
	// An ASDU was refused by the IEC 60870-5-101 profile checks (--strict).
	CodeProfileViolation = "profile_violation"
)
