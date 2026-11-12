// SPDX-License-Identifier: MIT

package types

import (
	"fmt"
	"strconv"
	"strings"
)

// SOF is the status of fault (clause 7.2.6.24).
type SOF struct {
	// Raw is the octet.
	Raw uint8 `json:"raw"`
	// TP: recorded fault with trip; TM: disturbance data being transmitted; Test: recorded
	// during test mode; OTEV: recording initiated by another event than start/pick-up.
	TP   bool `json:"tp"`
	TM   bool `json:"tm"`
	Test bool `json:"test"`
	OTEV bool `json:"otev"`
}

func (s SOF) String() string {
	var parts []string
	for _, f := range []struct {
		on   bool
		name string
	}{{s.TP, "trip"}, {s.TM, "transmitting"}, {s.Test, "test"}, {s.OTEV, "other-event"}} {
		if f.on {
			parts = append(parts, f.name)
		}
	}
	if len(parts) == 0 {
		return "-"
	}
	return strings.Join(parts, ",")
}

// Fault is one entry of the list of recorded disturbances (ASDU 23).
type Fault struct {
	FAN uint16 `json:"fan"`
	SOF SOF    `json:"sof"`
	// Time is the time of the fault (CP56Time2a, RFC 3339).
	Time string `json:"time"`
}

// DisturbanceListResult is the result of `client disturbance list`.
type DisturbanceListResult struct {
	Device
	Fun        uint8   `json:"fun"`
	DurationMs float64 `json:"duration_ms"`
	Faults     []Fault `json:"faults"`
}

// MarshalTextOutput implements format.TextMarshaler.
func (r *DisturbanceListResult) MarshalTextOutput() (string, error) {
	var b strings.Builder
	_, _ = fmt.Fprintf(&b, "Recorded disturbances of %s, FUN %d: %d\n", r.Device, r.Fun, len(r.Faults))
	for _, f := range r.Faults {
		_, _ = fmt.Fprintf(&b, "  FAN %-5d %s  %s\n", f.FAN, f.Time, f.SOF)
	}
	return b.String(), nil
}

// TableHeaders implements format.TableMarshaler.
func (r *DisturbanceListResult) TableHeaders() []string {
	return []string{"FAN", "TIME", "SOF", "TP", "TM", "TEST", "OTEV"}
}

// TableRows implements format.TableMarshaler.
func (r *DisturbanceListResult) TableRows() [][]string {
	var rows [][]string
	for _, f := range r.Faults {
		rows = append(rows, []string{strconv.Itoa(int(f.FAN)), f.Time, strconv.Itoa(int(f.SOF.Raw)),
			strconv.FormatBool(f.SOF.TP), strconv.FormatBool(f.SOF.TM), strconv.FormatBool(f.SOF.Test), strconv.FormatBool(f.SOF.OTEV)})
	}
	return rows
}

// Tag is the state of a binary signal in a disturbance record from value Pos on.
type Tag struct {
	Pos   uint16 `json:"pos"`
	Fun   uint8  `json:"fun"`
	Inf   uint8  `json:"inf"`
	DPI   uint8  `json:"dpi"`
	State string `json:"state"`
}

// Channel is one analogue channel of a disturbance record.
type Channel struct {
	// ACC is the actual channel (clause 7.2.6.1), Name its name (IL1, VL1E, ...).
	ACC  uint8  `json:"acc"`
	Name string `json:"name"`
	// RPV, RSV and RFA are the rated primary value, the rated secondary value and the
	// reference factor of ASDU 27, as the device sent them.
	RPV float32 `json:"rpv"`
	RSV float32 `json:"rsv"`
	RFA float32 `json:"rfa"`
	// Values are the NOE values (SDV, the signed integers of ASDU 30) by value number.
	Values []int16 `json:"values"`
}

// Disturbance is a recorded disturbance as transferred (clause 7.4.7).
type Disturbance struct {
	Fun uint8  `json:"fun"`
	FAN uint16 `json:"fan"`
	TOV uint8  `json:"tov"`
	NOF uint16 `json:"nof"`
	NOC int    `json:"noc"`
	NOE int    `json:"noe"`
	// IntervalUs is INT, the time between two values in microseconds.
	IntervalUs uint16 `json:"interval_us"`
	// Start is the time of the first value (CP32Time2a of ASDU 26, HH:MM:SS.mmm).
	Start    string    `json:"start"`
	Tags     []Tag     `json:"tags"`
	Channels []Channel `json:"channels"`
}

// DisturbanceResult is the result of `client disturbance get`.
type DisturbanceResult struct {
	Device
	Disturbance
	// Output and CSV are the files written, when asked for.
	Output     string  `json:"output,omitempty"`
	CSV        string  `json:"csv,omitempty"`
	DurationMs float64 `json:"duration_ms"`
}

// MarshalTextOutput implements format.TextMarshaler.
func (r *DisturbanceResult) MarshalTextOutput() (string, error) {
	var b strings.Builder
	_, _ = fmt.Fprintf(&b, "Disturbance FAN %d of %s, FUN %d: NOF %d, %d channels of %d values every %d us from %s, %d tags, in %.0f ms\n",
		r.FAN, r.Device, r.Fun, r.NOF, r.NOC, r.NOE, r.IntervalUs, r.Start, len(r.Tags), r.DurationMs)
	for _, t := range r.Tags {
		_, _ = fmt.Fprintf(&b, "  tag     pos %-5d fun=%d inf=%d %s\n", t.Pos, t.Fun, t.Inf, t.State)
	}
	for _, c := range r.Channels {
		lo, hi := minMax(c.Values)
		_, _ = fmt.Fprintf(&b, "  channel acc %-3d %-5s rpv=%g rsv=%g rfa=%g values=%d min=%d max=%d\n", c.ACC, c.Name, c.RPV, c.RSV, c.RFA, len(c.Values), lo, hi)
	}
	if r.Output != "" {
		_, _ = fmt.Fprintf(&b, "  record written to %s\n", r.Output)
	}
	if r.CSV != "" {
		_, _ = fmt.Fprintf(&b, "  values written to %s\n", r.CSV)
	}
	return b.String(), nil
}

func minMax(vs []int16) (int16, int16) {
	if len(vs) == 0 {
		return 0, 0
	}
	lo, hi := vs[0], vs[0]
	for _, v := range vs[1:] {
		lo, hi = min(lo, v), max(hi, v)
	}
	return lo, hi
}

// TableHeaders implements format.TableMarshaler.
func (r *DisturbanceResult) TableHeaders() []string {
	return []string{"ACC", "NAME", "RPV", "RSV", "RFA", "VALUES", "MIN", "MAX"}
}

// TableRows implements format.TableMarshaler.
func (r *DisturbanceResult) TableRows() [][]string {
	var rows [][]string
	for _, c := range r.Channels {
		lo, hi := minMax(c.Values)
		rows = append(rows, []string{strconv.Itoa(int(c.ACC)), c.Name, fmt.Sprint(c.RPV), fmt.Sprint(c.RSV), fmt.Sprint(c.RFA),
			strconv.Itoa(len(c.Values)), strconv.Itoa(int(lo)), strconv.Itoa(int(hi))})
	}
	return rows
}
