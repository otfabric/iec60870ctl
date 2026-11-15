// SPDX-License-Identifier: MIT

package types

import (
	"fmt"
	"strconv"
	"strings"
)

// DataSet is one data set of generic data (clause 7.4.8.3): GIN, KOD, GDD and GID.
type DataSet struct {
	// GIN is GROUP.ENTRY.
	GIN     string `json:"gin"`
	KOD     uint8  `json:"kod"`
	KODName string `json:"kod_name"`
	// DataType is DATATYPE (clause 7.2.6.32), TypeName its name.
	DataType uint8  `json:"data_type"`
	TypeName string `json:"type_name"`
	Size     uint8  `json:"size"`
	Number   uint8  `json:"number"`
	// Values are the elements of the GID: the text of type 1, the numbers of types 3, 4, 7,
	// 8, 22 (and the unsigned types 2, 9, 10, 21, 24), the fields of a structure (23) as
	// nested values. Absent for the other data types: see Data.
	Values []any `json:"values,omitempty"`
	// NonFinite names, for each element of Values, a number JSON has no number for: "NaN",
	// "+Inf" or "-Inf" where the element is null, "" for every other element. Omitted when
	// every element is finite.
	NonFinite []string `json:"non_finite,omitempty"`
	// Reply is the name of a generic reply code (type 22).
	Reply string `json:"reply,omitempty"`
	// Data is the GID as hex.
	Data string `json:"data"`
}

// DescElement is one description element of a generic identification (ASDU 11).
type DescElement struct {
	KOD      uint8  `json:"kod"`
	KODName  string `json:"kod_name"`
	DataType uint8  `json:"data_type"`
	TypeName string `json:"type_name"`
	Size     uint8  `json:"size"`
	Number   uint8  `json:"number"`
	Values   []any  `json:"values,omitempty"`
	// NonFinite is as in [DataSet].
	NonFinite []string `json:"non_finite,omitempty"`
	Reply     string   `json:"reply,omitempty"`
	Data      string   `json:"data"`
}

// GenericResult is the result of `client generic ...`.
type GenericResult struct {
	Device
	// Service is headings, read, entry, group, directory, interrogate or write.
	Service string `json:"service"`
	// Inf is the information number of the request (table 18).
	Inf uint8 `json:"inf"`
	// Mode is the procedure of a write: execute or abort.
	Mode string `json:"mode,omitempty"`
	// Cause is the cause of the answer: 42 valid data, 43 invalid data, 40 write
	// acknowledged, 10 end of the generic interrogation.
	Cause     uint8  `json:"cause"`
	CauseName string `json:"cause_name"`
	// GIN and Elements are the answer to a directory read (ASDU 11).
	GIN        string        `json:"gin,omitempty"`
	Elements   []DescElement `json:"elements,omitempty"`
	Sets       []DataSet     `json:"sets"`
	DurationMs float64       `json:"duration_ms"`
}

func valuesText(values []any, nonFinite []string, data, reply string) string {
	if reply != "" {
		return "reply " + reply
	}
	if len(values) == 0 {
		return "data " + data
	}
	parts := make([]string, len(values))
	for i, v := range values {
		switch x := v.(type) {
		case string:
			parts[i] = strconv.Quote(x)
		case nil:
			parts[i] = "-"
			if i < len(nonFinite) && nonFinite[i] != "" {
				parts[i] = nonFinite[i]
			}
		default:
			parts[i] = fmt.Sprint(v)
		}
	}
	return strings.Join(parts, ", ")
}

// MarshalTextOutput implements format.TextMarshaler.
func (r *GenericResult) MarshalTextOutput() (string, error) {
	var b strings.Builder
	what := "Generic " + r.Service
	if r.Mode != "" {
		what += " (" + r.Mode + ")"
	}
	_, _ = fmt.Fprintf(&b, "%s of %s: %s in %.0f ms\n", what, r.Device, r.CauseName, r.DurationMs)
	if r.GIN != "" {
		_, _ = fmt.Fprintf(&b, "  directory of %s\n", r.GIN)
		for _, e := range r.Elements {
			_, _ = fmt.Fprintf(&b, "  kod %-3d %-22s %-10s %s\n", e.KOD, e.KODName, e.TypeName, valuesText(e.Values, e.NonFinite, e.Data, e.Reply))
		}
	}
	for _, d := range r.Sets {
		_, _ = fmt.Fprintf(&b, "  %-7s kod %-3d %-22s %-10s %s\n", d.GIN, d.KOD, d.KODName, d.TypeName, valuesText(d.Values, d.NonFinite, d.Data, d.Reply))
	}
	return b.String(), nil
}

// TableHeaders implements format.TableMarshaler.
func (r *GenericResult) TableHeaders() []string {
	return []string{"GIN", "KOD", "KOD_NAME", "TYPE", "SIZE", "NUMBER", "VALUE"}
}

// TableRows implements format.TableMarshaler.
func (r *GenericResult) TableRows() [][]string {
	var rows [][]string
	for _, e := range r.Elements {
		rows = append(rows, []string{r.GIN, strconv.Itoa(int(e.KOD)), e.KODName, e.TypeName, strconv.Itoa(int(e.Size)), strconv.Itoa(int(e.Number)), valuesText(e.Values, e.NonFinite, e.Data, e.Reply)})
	}
	for _, d := range r.Sets {
		rows = append(rows, []string{d.GIN, strconv.Itoa(int(d.KOD)), d.KODName, d.TypeName, strconv.Itoa(int(d.Size)), strconv.Itoa(int(d.Number)), valuesText(d.Values, d.NonFinite, d.Data, d.Reply)})
	}
	return rows
}
