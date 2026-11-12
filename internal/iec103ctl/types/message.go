// SPDX-License-Identifier: MIT

// Package types holds the results of the iec103ctl commands: what --format json writes,
// and their text and table renderings.
package types

import (
	"fmt"
	"strconv"
	"strings"
)

// Measurand is one value of ASDU 3 or 9 (clause 7.2.6.8).
type Measurand struct {
	// Raw is MVAL, the signed 13-bit value -4096..4095.
	Raw int16 `json:"raw"`
	// Value is MVAL as a fraction of full scale (Raw / 4096).
	Value float64 `json:"value"`
	OV    bool    `json:"ov"`
	ER    bool    `json:"er"`
}

// Message is one ASDU of the monitor direction as iec103ctl reports it.
type Message struct {
	// Received is when it arrived (RFC 3339), set by `client monitor`.
	Received  string `json:"received,omitempty"`
	Type      string `json:"type"`
	TypeID    uint8  `json:"type_id"`
	Cause     uint8  `json:"cause"`
	CauseName string `json:"cause_name"`
	CA        uint8  `json:"ca"`
	Fun       uint8  `json:"fun"`
	Inf       uint8  `json:"inf"`
	// Name is the meaning of FUN/INF in tables 8 to 18, where they define one.
	Name string `json:"name,omitempty"`
	// DPI is the double-point information (ASDU 1, 2), State its name.
	DPI   *uint8 `json:"dpi,omitempty"`
	State string `json:"state,omitempty"`
	// Time is the time tag: HH:MM:SS.mmm for a CP32Time2a, RFC 3339 for a CP56Time2a.
	Time        string      `json:"time,omitempty"`
	TimeInvalid bool        `json:"time_invalid,omitempty"`
	SIN         *uint8      `json:"sin,omitempty"`
	SCN         *uint8      `json:"scn,omitempty"`
	RET         *uint16     `json:"ret,omitempty"`
	FAN         *uint16     `json:"fan,omitempty"`
	SCL         *float64    `json:"scl,omitempty"`
	Values      []Measurand `json:"values,omitempty"`
	// COL, Manufacturer and Software are the identification (ASDU 5).
	COL          *uint8 `json:"col,omitempty"`
	Manufacturer string `json:"manufacturer,omitempty"`
	Software     string `json:"software,omitempty"`
	// Data is the information elements as hex, for what is not decoded into fields.
	Data string `json:"data,omitempty"`
}

// Summary is the one-line description of what m carries.
func (m *Message) Summary() string {
	var parts []string
	if m.DPI != nil {
		parts = append(parts, "dpi="+m.State)
	}
	if m.Time != "" {
		t := "time=" + m.Time
		if m.TimeInvalid {
			t += "(invalid)"
		}
		parts = append(parts, t)
	}
	for _, f := range []struct {
		name string
		v    *uint8
	}{{"sin", m.SIN}, {"scn", m.SCN}, {"col", m.COL}} {
		if f.v != nil {
			parts = append(parts, fmt.Sprintf("%s=%d", f.name, *f.v))
		}
	}
	if m.RET != nil {
		parts = append(parts, fmt.Sprintf("ret=%dms", *m.RET))
	}
	if m.FAN != nil {
		parts = append(parts, fmt.Sprintf("fan=%d", *m.FAN))
	}
	if m.SCL != nil {
		parts = append(parts, "scl="+strconv.FormatFloat(*m.SCL, 'g', -1, 64))
	}
	if len(m.Values) > 0 {
		vs := make([]string, len(m.Values))
		for i, v := range m.Values {
			vs[i] = strconv.Itoa(int(v.Raw))
			if v.OV {
				vs[i] += "(ov)"
			}
			if v.ER {
				vs[i] += "(er)"
			}
		}
		parts = append(parts, "values="+strings.Join(vs, ","))
	}
	if m.Manufacturer != "" {
		parts = append(parts, fmt.Sprintf("manufacturer=%q", m.Manufacturer))
	}
	if m.Software != "" {
		parts = append(parts, "software="+m.Software)
	}
	if m.Data != "" {
		parts = append(parts, "data="+m.Data)
	}
	return strings.Join(parts, " ")
}

// line is the text form of m, without the time it was received.
func (m *Message) line() string {
	s := fmt.Sprintf("<%d> %-28s cot=%-2d ca=%d fun=%d inf=%d", m.TypeID, m.Type, m.Cause, m.CA, m.Fun, m.Inf)
	if m.Name != "" {
		s += " (" + m.Name + ")"
	}
	if sum := m.Summary(); sum != "" {
		s += "  " + sum
	}
	return s
}

// MarshalTextOutput implements format.TextMarshaler: one line, as `client monitor` streams it.
func (m *Message) MarshalTextOutput() (string, error) {
	if m.Received != "" {
		return m.Received + "  " + m.line(), nil
	}
	return m.line(), nil
}

// MessageHeaders are the columns of a message row.
var MessageHeaders = []string{"TYPE", "COT", "CA", "FUN", "INF", "NAME", "CONTENT"}

func messageRow(m *Message) []string {
	return []string{m.Type, strconv.Itoa(int(m.Cause)), strconv.Itoa(int(m.CA)), strconv.Itoa(int(m.Fun)), strconv.Itoa(int(m.Inf)), m.Name, m.Summary()}
}

// TableHeaders implements format.TableMarshaler.
func (m *Message) TableHeaders() []string { return append([]string{"RECEIVED"}, MessageHeaders...) }

// TableRows implements format.TableMarshaler.
func (m *Message) TableRows() [][]string {
	return [][]string{append([]string{m.Received}, messageRow(m)...)}
}

func messageRows(ms []Message) [][]string {
	rows := make([][]string, 0, len(ms))
	for i := range ms {
		rows = append(rows, messageRow(&ms[i]))
	}
	return rows
}
