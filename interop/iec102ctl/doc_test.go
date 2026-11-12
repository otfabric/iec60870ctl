//go:build interop

// SPDX-License-Identifier: MIT

// Package interop runs the built iec102ctl against the independent IEC 60870-5-102
// controlling station published by otfabric/iec60870-5-interop v0.4.0 (contract 2.3):
// gisce/iec870ree, a Python master of the Spanish REE profile, in the image iec102-gisce,
// pinned by digest in harness_test.go. `iec102ctl server start` serves the harness's
// baseline fixture, converted to a meter model; the suite reads the fixture from the image
// (print-fixture) and checks every result document of gisce against the fixture's values,
// never against iec102ctl. The steps and their checks are those of go-iec60870-5 v0.4.1
// iec102/interop (TestGisceAgainstServer), which runs the same image against iec102/server.
//
// There is no open IEC 102 controlled station (meter) in the harness, so `iec102ctl client`
// has no independent peer here; its evidence is the e2e suite against go-iec60870-5's own
// iec102/server.
//
// A failure that the harness lists as a known bug of gisce (testdata/known-bugs.tsv) is
// skipped with the bug id; it never counts as a pass.
//
// Environment:
//
//	INTEROP_BIN_DIR             the directory of the built iec102ctl (set by make interop)
//	IEC102_INTEROP_GISCE_IMAGE  another gisce image (default: the pinned one); empty skips
//	IEC102_REQUIRE_INTEROP=1    fail instead of skip when Docker, the image or the binary is missing
//
// Run with: make interop
package interop
