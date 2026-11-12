// SPDX-License-Identifier: MIT

package types

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// Point is one information object as a station reported it: a state, a measured value, a
// counter or an event, with its quality and, when the type carries one, its time tag.
type Point struct {
	// ReceivedAt is when the value arrived (RFC 3339, UTC, milliseconds). Streams only.
	ReceivedAt string `json:"received_at,omitempty"`
	CommonAddr uint16 `json:"ca"`
	IOA        uint32 `json:"ioa"`
	Type       string `json:"type"`
	TypeID     uint8  `json:"type_id"`
	Cause      string `json:"cause"`
	CauseID    uint8  `json:"cot"`
	Negative   bool   `json:"negative,omitempty"`
	Test       bool   `json:"test,omitempty"`
	Originator uint8  `json:"originator,omitempty"`
	// Value is a boolean for single points, a string for double points and step directions,
	// and a number for everything else. Null for types without a value.
	Value any `json:"value"`
	// Quality is "good" or the set flags joined with "|" (for example "NT|IV"). Empty for
	// types without a quality descriptor.
	Quality string `json:"quality,omitempty"`
	// Timestamp is the time tag of the object (RFC 3339 with milliseconds), when it has one.
	Timestamp        string `json:"timestamp,omitempty"`
	TimestampInvalid bool   `json:"timestamp_invalid,omitempty"`
	// Attributes holds what only some types carry, such as "sequence" and "carry" of an
	// integrated total or "transient" of a step position.
	Attributes map[string]any `json:"attributes,omitempty"`
}

// ValueText renders the value, with its type-specific attributes, for text and table output.
func (p Point) ValueText() string {
	var s string
	switch v := p.Value.(type) {
	case nil:
		s = "-"
	case bool:
		s = "off"
		if v {
			s = "on"
		}
	case float32:
		s = strconv.FormatFloat(float64(v), 'g', -1, 32)
	case float64:
		s = strconv.FormatFloat(v, 'g', 6, 64)
	default:
		s = fmt.Sprint(v)
	}
	if len(p.Attributes) == 0 {
		return s
	}
	keys := make([]string, 0, len(p.Attributes))
	for k := range p.Attributes {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		if b, ok := p.Attributes[k].(bool); ok {
			if b {
				parts = append(parts, k)
			}
			continue
		}
		parts = append(parts, fmt.Sprintf("%s=%v", k, p.Attributes[k]))
	}
	if len(parts) == 0 {
		return s
	}
	return s + " (" + strings.Join(parts, " ") + ")"
}

func (p Point) qualityText() string {
	if p.Quality == "" {
		return "-"
	}
	return p.Quality
}

func (p Point) timeText() string {
	switch {
	case p.Timestamp == "":
		return "-"
	case p.TimestampInvalid:
		return p.Timestamp + " (invalid)"
	}
	return p.Timestamp
}

func (p Point) flagsText() string {
	var f []string
	if p.Negative {
		f = append(f, "negative")
	}
	if p.Test {
		f = append(f, "test")
	}
	return strings.Join(f, ",")
}

// causeText is the cause of transmission with the P/N and T bits.
func (p Point) causeText() string {
	if f := p.flagsText(); f != "" {
		return p.Cause + " [" + f + "]"
	}
	return p.Cause
}

// Line renders the point as one line of text.
func (p Point) Line() string {
	var b strings.Builder
	if p.ReceivedAt != "" {
		b.WriteString(p.ReceivedAt)
		b.WriteString("  ")
	}
	_, _ = fmt.Fprintf(&b, "%-9s  ca=%-3d ioa=%-7d  %-16s", p.Type, p.CommonAddr, p.IOA, p.ValueText())
	if p.Quality != "" {
		_, _ = fmt.Fprintf(&b, "  %-7s", "["+p.Quality+"]")
	}
	_, _ = fmt.Fprintf(&b, "  %s", p.causeText())
	if p.Timestamp != "" {
		_, _ = fmt.Fprintf(&b, "  @ %s", p.timeText())
	}
	return strings.TrimRight(b.String(), " ")
}

// PointHeaders are the table columns of a point.
var PointHeaders = []string{"CA", "IOA", "TYPE", "VALUE", "QUALITY", "CAUSE", "TIME"}

// Row renders the point as a table row matching [PointHeaders].
func (p Point) Row() []string {
	return []string{
		strconv.Itoa(int(p.CommonAddr)),
		strconv.FormatUint(uint64(p.IOA), 10),
		p.Type,
		p.ValueText(),
		p.qualityText(),
		p.causeText(),
		p.timeText(),
	}
}

// MarshalTextOutput implements format.TextMarshaler for a point in a stream.
func (p Point) MarshalTextOutput() (string, error) { return p.Line(), nil }

// TableHeaders implements format.TableMarshaler for a point in a stream.
func (p Point) TableHeaders() []string {
	return append([]string{"RECEIVED"}, PointHeaders...)
}

// TableRows implements format.TableMarshaler for a point in a stream.
func (p Point) TableRows() [][]string {
	return [][]string{append([]string{p.ReceivedAt}, p.Row()...)}
}

func pointLines(b *strings.Builder, points []Point) {
	for _, p := range points {
		b.WriteString("  ")
		b.WriteString(p.Line())
		b.WriteString("\n")
	}
}

func pointRows(points []Point) [][]string {
	rows := make([][]string, 0, len(points))
	for _, p := range points {
		rows = append(rows, p.Row())
	}
	return rows
}
