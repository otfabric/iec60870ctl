// SPDX-License-Identifier: MIT

// Package iec implements what the commands do: it talks to stations through go-iec104 and
// returns the output types of package types.
package iec

import (
	"time"

	"github.com/otfabric/go-iec104/asdu"
	"github.com/otfabric/iec104ctl/internal/types"
)

// TimeLayout is how times are written in output: RFC 3339 with milliseconds.
const TimeLayout = "2006-01-02T15:04:05.000Z07:00"

// FormatTime renders t in UTC with millisecond precision.
func FormatTime(t time.Time) string { return t.UTC().Format(TimeLayout) }

// Points flattens an ASDU into one [types.Point] per information object. Types that carry
// no process information (confirmations of commands, file transfer) yield points with a nil
// value, so that nothing a station sends is hidden.
func Points(a *asdu.ASDU) []types.Point {
	if a == nil {
		return nil
	}
	out := make([]types.Point, 0, len(a.Objects))
	for _, o := range a.Objects {
		p := types.Point{
			CommonAddr: uint16(a.CommonAddr),
			IOA:        uint32(o.Address()),
			Type:       a.Type.String(),
			TypeID:     uint8(a.Type),
			Cause:      a.Cause.String(),
			CauseID:    uint8(a.Cause),
			Negative:   a.Negative,
			Test:       a.Test,
			Originator: a.Originator,
		}
		fill(&p, o, a.Type.HasTimeTag())
		out = append(out, p)
	}
	if len(a.Objects) == 0 {
		// A type without object model: report that it arrived.
		out = append(out, types.Point{
			CommonAddr: uint16(a.CommonAddr),
			Type:       a.Type.String(),
			TypeID:     uint8(a.Type),
			Cause:      a.Cause.String(),
			CauseID:    uint8(a.Cause),
			Negative:   a.Negative,
			Test:       a.Test,
			Originator: a.Originator,
			Attributes: map[string]any{"raw_octets": len(a.Raw)},
		})
	}
	return out
}

func quality(q asdu.Quality) string { return q.String() }

func stamp(p *types.Point, t asdu.Timestamp, tagged bool) {
	if !tagged {
		return
	}
	p.TimestampInvalid = t.Invalid
	if t.IsZero() {
		p.Timestamp = "0001-01-01T00:00:00.000Z"
		return
	}
	p.Timestamp = t.Format(TimeLayout)
	if t.Substituted || t.SummerTime {
		if p.Attributes == nil {
			p.Attributes = map[string]any{}
		}
		if t.Substituted {
			p.Attributes["time_substituted"] = true
		}
		if t.SummerTime {
			p.Attributes["summer_time"] = true
		}
	}
}

func attr(p *types.Point, key string, v any) {
	if p.Attributes == nil {
		p.Attributes = map[string]any{}
	}
	p.Attributes[key] = v
}

func flag(p *types.Point, key string, set bool) {
	if set {
		attr(p, key, true)
	}
}

func stepDirection(d asdu.StepDirection) string {
	switch d {
	case asdu.StepLower:
		return "lower"
	case asdu.StepHigher:
		return "higher"
	}
	return "not-permitted"
}

func commandQualifier(q asdu.CommandQualifier) string {
	switch q {
	case asdu.QualifierNone:
		return "none"
	case asdu.QualifierShortPulse:
		return "short"
	case asdu.QualifierLongPulse:
		return "long"
	case asdu.QualifierPersistent:
		return "persistent"
	}
	return "reserved"
}

// fill sets the value, quality, time tag and attributes of p from o.
func fill(p *types.Point, o asdu.InformationObject, tagged bool) {
	switch v := o.(type) {
	case asdu.SinglePoint:
		p.Value, p.Quality = v.Value, quality(v.Quality)
		stamp(p, v.Time, tagged)
	case asdu.DoublePoint:
		p.Value, p.Quality = v.Value.String(), quality(v.Quality)
		stamp(p, v.Time, tagged)
	case asdu.StepPosition:
		p.Value, p.Quality = int(v.Value), quality(v.Quality)
		flag(p, "transient", v.Transient)
		stamp(p, v.Time, tagged)
	case asdu.Bitstring32:
		p.Value, p.Quality = v.Value, quality(v.Quality)
		stamp(p, v.Time, tagged)
	case asdu.MeasuredNormalized:
		p.Value = v.Value.Float64()
		attr(p, "raw", int(v.Value))
		// M_ME_ND_1 has no quality descriptor.
		if p.TypeID != uint8(asdu.M_ME_ND_1) {
			p.Quality = quality(v.Quality)
		}
		stamp(p, v.Time, tagged)
	case asdu.MeasuredScaled:
		p.Value, p.Quality = int(v.Value), quality(v.Quality)
		stamp(p, v.Time, tagged)
	case asdu.MeasuredFloat:
		p.Value, p.Quality = v.Value, quality(v.Quality)
		stamp(p, v.Time, tagged)
	case asdu.IntegratedTotal:
		p.Value = int(v.Value)
		attr(p, "sequence", int(v.Sequence))
		flag(p, "carry", v.Carry)
		flag(p, "adjusted", v.Adjusted)
		p.Quality = "good"
		if v.Invalid {
			p.Quality = "IV"
		}
		stamp(p, v.Time, tagged)
	case asdu.ProtectionEvent:
		p.Value, p.Quality = v.State.String(), quality(v.Quality)
		attr(p, "elapsed_ms", int(v.Elapsed))
		stamp(p, v.Time, tagged)
	case asdu.ProtectionStart:
		p.Value, p.Quality = int(v.Events), quality(v.Quality)
		attr(p, "duration_ms", int(v.Duration))
		stamp(p, v.Time, tagged)
	case asdu.ProtectionOutput:
		p.Value, p.Quality = int(v.Circuits), quality(v.Quality)
		attr(p, "operating_ms", int(v.Operating))
		stamp(p, v.Time, tagged)
	case asdu.PackedSinglePoints:
		p.Value, p.Quality = int(v.Status), quality(v.Quality)
		attr(p, "changed", int(v.Changed))
	case asdu.EndOfInitialization:
		p.Value = int(v.Cause)
		flag(p, "local_change", v.LocalChange)

	case asdu.SingleCommand:
		p.Value = v.Value
		commandAttrs(p, commandQualifier(v.Qualifier), v.Select)
		stamp(p, v.Time, tagged)
	case asdu.DoubleCommand:
		p.Value = v.Value.String()
		commandAttrs(p, commandQualifier(v.Qualifier), v.Select)
		stamp(p, v.Time, tagged)
	case asdu.StepCommand:
		p.Value = stepDirection(v.Value)
		commandAttrs(p, commandQualifier(v.Qualifier), v.Select)
		stamp(p, v.Time, tagged)
	case asdu.SetpointNormalized:
		p.Value = v.Value.Float64()
		attr(p, "raw", int(v.Value))
		commandAttrs(p, int(v.Qualifier), v.Select)
		stamp(p, v.Time, tagged)
	case asdu.SetpointScaled:
		p.Value = int(v.Value)
		commandAttrs(p, int(v.Qualifier), v.Select)
		stamp(p, v.Time, tagged)
	case asdu.SetpointFloat:
		p.Value = v.Value
		commandAttrs(p, int(v.Qualifier), v.Select)
		stamp(p, v.Time, tagged)
	case asdu.BitstringCommand:
		p.Value = v.Value
		stamp(p, v.Time, tagged)

	case asdu.Interrogation:
		p.Value = int(v.Qualifier)
	case asdu.CounterInterrogation:
		p.Value = int(v.Request)
		attr(p, "freeze", int(v.Freeze))
	case asdu.ClockSync:
		stamp(p, v.Time, true)
	case asdu.TestCommand:
		p.Value = int(v.Counter)
		stamp(p, v.Time, tagged)
	case asdu.ResetProcess:
		p.Value = int(v.Qualifier)
	case asdu.DelayAcquisition:
		p.Value = int(v.Delay)
	case asdu.ParameterNormalized:
		p.Value = v.Value.Float64()
		attr(p, "qualifier", int(v.Qualifier))
	case asdu.ParameterScaled:
		p.Value = int(v.Value)
		attr(p, "qualifier", int(v.Qualifier))
	case asdu.ParameterFloat:
		p.Value = v.Value
		attr(p, "qualifier", int(v.Qualifier))
	case asdu.ParameterActivation:
		p.Value = int(v.Qualifier)
	case asdu.FileDirectoryEntry:
		p.Value = int(v.Length)
		attr(p, "name", int(v.Name))
		stamp(p, v.Time, true)
	default:
		// File transfer steps and anything a later library version adds: no value.
	}
}

func commandAttrs(p *types.Point, qualifier any, sel bool) {
	attr(p, "qualifier", qualifier)
	flag(p, "select", sel)
}
