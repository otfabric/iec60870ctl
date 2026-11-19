// SPDX-License-Identifier: MIT

package types

import (
	"fmt"
	"strconv"
	"strings"
)

// TypeInfo describes one type identification of IEC 60870-5-103.
type TypeInfo struct {
	ID   uint8  `json:"id"`
	Name string `json:"name"`
	// Description is the name of the type in tables 3 and 4 of clause 7.2.1.
	Description string `json:"description"`
	// Direction is "monitor", "control" or "both" (types 6 and 10, clause 7.2.1).
	Direction string `json:"direction"`
}

// TypesResult is the result of `client types`.
type TypesResult struct {
	Count int        `json:"count"`
	Types []TypeInfo `json:"types"`
}

// MarshalTextOutput implements format.TextMarshaler.
func (r *TypesResult) MarshalTextOutput() (string, error) {
	var b strings.Builder
	for _, t := range r.Types {
		_, _ = fmt.Fprintf(&b, "%3d  %-34s %-8s %s\n", t.ID, t.Name, t.Direction, t.Description)
	}
	return b.String(), nil
}

// TableHeaders implements format.TableMarshaler.
func (r *TypesResult) TableHeaders() []string {
	return []string{"ID", "NAME", "DIRECTION", "DESCRIPTION"}
}

// TableRows implements format.TableMarshaler.
func (r *TypesResult) TableRows() [][]string {
	rows := make([][]string, 0, len(r.Types))
	for _, t := range r.Types {
		rows = append(rows, []string{strconv.Itoa(int(t.ID)), t.Name, t.Direction, t.Description})
	}
	return rows
}
