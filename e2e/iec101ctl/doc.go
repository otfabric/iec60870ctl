// SPDX-License-Identifier: MIT

// Package e2e runs the built iec101ctl binary as a user or script would: against the
// controlled station of go-iec60870-5, and its own simulated station against the
// controlling station of go-iec60870-5, over TCP on 127.0.0.1. The tests carry the e2e
// build tag; run them with make e2e.
package e2e
