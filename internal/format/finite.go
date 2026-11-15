// SPDX-License-Identifier: MIT

package format

import "math"

// The names of the numbers JSON has no number for. Text and table output print them the
// same way.
const (
	NaN    = "NaN"
	PosInf = "+Inf"
	NegInf = "-Inf"
)

// Finite returns v, or for a NaN or an infinite float32 or float64, which JSON has no number
// for, nil and the name of the number: [NaN], [PosInf] or [NegInf]. A result puts the name
// in its "non_finite" field in place of the value. Every other v is returned as it is, with
// an empty name.
func Finite(v any) (value any, nonFinite string) {
	var f float64
	switch n := v.(type) {
	case float64:
		f = n
	case float32:
		f = float64(n)
	default:
		return v, ""
	}
	if name := NonFinite(f); name != "" {
		return nil, name
	}
	return v, ""
}

// NonFinite returns the name of f when it is a NaN or infinite ([NaN], [PosInf], [NegInf]),
// and "" for a finite f.
func NonFinite(f float64) string {
	switch {
	case math.IsNaN(f):
		return NaN
	case math.IsInf(f, 1):
		return PosInf
	case math.IsInf(f, -1):
		return NegInf
	}
	return ""
}
