// SPDX-License-Identifier: MIT

// Package interop runs the built iec101ctl against the independent IEC 60870-5-101
// implementations published by otfabric/iec60870-5-interop v0.5.0 (contract 2.3), pinned by
// the same digests as go-iec60870-5's iec101/interop: `iec101ctl client` against each
// image's server, and each image's client against `iec101ctl server start`, in unbalanced
// and balanced mode, with the default field sizes and the non-default sets of field sizes
// of go-iec60870-5's iec101/interop/fields.go where the image takes them.
//
// Expected values come from the images (print-fixture baseline) and from the harness's
// case table (testdata/cases.tsv, copied from the harness at v0.5.0), never from
// iec60870ctl or go-iec60870-5.
//
// Environment:
//
//	INTEROP_BIN_DIR                 directory of the built iec101ctl (make interop sets it)
//	IEC101_INTEROP_<STACK>_IMAGE    image of a stack (LIB60870, CBSIGMA, DIGITALPETRI); empty drops it
//	IEC101_INTEROP_ADAPTERS         comma-separated subset of stacks
//	IEC101_REQUIRE_INTEROP=1        fail instead of skip when Docker or an image is missing
//
// The tests carry the interop build tag; run them with make interop.
package interop
