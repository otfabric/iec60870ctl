//go:build interop

// SPDX-License-Identifier: MIT

package interop

// Run with: make interop (V=1 for -v), or
//
//	INTEROP_BIN_DIR=$PWD/bin go test -tags=interop ./interop/iec103ctl/
//
// Pairings, both directions where an image has the role (contract 2.3 of
// otfabric/iec60870-5-interop v0.5.0):
//
//	iec103ctl client -> vitusdong server         client_test.go
//	vitusdong, ariec103, open103, goiecp5 client -> iec103ctl server start   server_test.go
//
// Known upstream defects (testdata/known-bugs.tsv, known-bugs-consumer.tsv) are skipped
// with their bug id after their evidence was seen; they never count as a pass.
