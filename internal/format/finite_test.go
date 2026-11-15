// SPDX-License-Identifier: MIT

package format

import (
	"bytes"
	"math"
	"strings"
	"testing"
)

// TestFinite: NaN and the infinities, as float64 and float32, become nil and their name;
// every other value is returned as it is.
func TestFinite(t *testing.T) {
	cases := []struct {
		in        any
		value     any
		nonFinite string
	}{
		{math.NaN(), nil, "NaN"},
		{math.Inf(1), nil, "+Inf"},
		{math.Inf(-1), nil, "-Inf"},
		{float32(math.NaN()), nil, "NaN"},
		{float32(math.Inf(1)), nil, "+Inf"},
		{float32(math.Inf(-1)), nil, "-Inf"},
		{1.5, 1.5, ""},
		{float32(-2.25), float32(-2.25), ""},
		{math.MaxFloat64, math.MaxFloat64, ""},
		{0.0, 0.0, ""},
		{7, 7, ""},
		{"NaN", "NaN", ""},
		{true, true, ""},
		{nil, nil, ""},
	}
	for _, c := range cases {
		v, nf := Finite(c.in)
		if v != c.value || nf != c.nonFinite {
			t.Errorf("Finite(%#v) = %#v, %q; want %#v, %q", c.in, v, nf, c.value, c.nonFinite)
		}
	}
}

// TestFiniteJSON: a result with the documented form encodes, as one document and as a line
// of a stream.
func TestFiniteJSON(t *testing.T) {
	type point struct {
		Value     any    `json:"value"`
		NonFinite string `json:"non_finite,omitempty"`
	}
	var p point
	p.Value, p.NonFinite = Finite(math.NaN())
	var doc bytes.Buffer
	if err := Write(&doc, FormatJSON, p); err != nil || !strings.Contains(doc.String(), `"non_finite": "NaN"`) || !strings.Contains(doc.String(), `"value": null`) {
		t.Errorf("Write: %q, %v", doc.String(), err)
	}
	var line bytes.Buffer
	if err := NewStreamWriter(&line, FormatJSON).Write(p); err != nil || line.String() != `{"value":null,"non_finite":"NaN"}`+"\n" {
		t.Errorf("stream: %q, %v", line.String(), err)
	}
	line.Reset()
	if err := NewStreamWriter(&line, FormatJSON).Write(point{Value: 2.5}); err != nil || strings.Contains(line.String(), "non_finite") {
		t.Errorf("a finite value: %q, %v", line.String(), err)
	}
}
