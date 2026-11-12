// SPDX-License-Identifier: MIT

package errs

// Info is the structured error object of the JSON output: the fatal error envelope and the
// per-row failures of every tool.
type Info struct {
	Kind    string `json:"kind"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

// Envelope is the top-level JSON shape of a fatal error (--format json).
type Envelope struct {
	Error Info `json:"error"`
}

// Message returns the message of an embedded error pointer, or "" for nil.
func Message(e *Info) string {
	if e == nil {
		return ""
	}
	return e.Message
}
