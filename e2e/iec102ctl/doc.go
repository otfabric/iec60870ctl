// SPDX-License-Identifier: MIT

// Package e2e runs the built iec102ctl binary as a user or script would: its client
// commands against go-iec60870-5's integrated total station (iec102/server with the REE
// session of iec102/ree) in this process, and its `server start` against go-iec60870-5's
// controlling station (iec102/client), over TCP on 127.0.0.1. The tests carry the e2e build
// tag; run them with make e2e.
package e2e
