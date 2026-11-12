// SPDX-License-Identifier: MIT

package types

import (
	"fmt"
	"strconv"
	"strings"
)

// Device names the protection equipment a result is about.
type Device struct {
	Target      string `json:"target"`
	LinkAddress uint8  `json:"link_address"`
	CA          uint8  `json:"ca"`
}

func (d Device) String() string {
	return fmt.Sprintf("device %d (ca %d) at %s", d.LinkAddress, d.CA, d.Target)
}

// InterrogateResult is the result of `client interrogate`.
type InterrogateResult struct {
	Device
	DurationMs float64   `json:"duration_ms"`
	Count      int       `json:"count"`
	Messages   []Message `json:"messages"`
}

// MarshalTextOutput implements format.TextMarshaler.
func (r *InterrogateResult) MarshalTextOutput() (string, error) {
	var b strings.Builder
	_, _ = fmt.Fprintf(&b, "General interrogation of %s: %d messages in %.0f ms\n", r.Device, r.Count, r.DurationMs)
	for i := range r.Messages {
		b.WriteString("  " + r.Messages[i].line() + "\n")
	}
	return b.String(), nil
}

// TableHeaders implements format.TableMarshaler.
func (r *InterrogateResult) TableHeaders() []string { return MessageHeaders }

// TableRows implements format.TableMarshaler.
func (r *InterrogateResult) TableRows() [][]string { return messageRows(r.Messages) }

// CommandResult is the result of `client command`.
type CommandResult struct {
	Device
	Fun  uint8  `json:"fun"`
	Inf  uint8  `json:"inf"`
	Name string `json:"name,omitempty"`
	// Command is the double command sent: "on" or "off"; DCO its value (2 or 1).
	Command string `json:"command"`
	DCO     uint8  `json:"dco"`
	// Acknowledged is true: the device acknowledged positively (cause 20). A negative
	// acknowledgement is an error.
	Acknowledged bool    `json:"acknowledged"`
	DurationMs   float64 `json:"duration_ms"`
	// Report is the change of state with cause 12 that followed, when --wait-report
	// waited for one and it came.
	Report *Message `json:"report,omitempty"`
}

// MarshalTextOutput implements format.TextMarshaler.
func (r *CommandResult) MarshalTextOutput() (string, error) {
	name := ""
	if r.Name != "" {
		name = " (" + r.Name + ")"
	}
	s := fmt.Sprintf("General command FUN %d INF %d%s %s to %s: acknowledged in %.0f ms", r.Fun, r.Inf, name, strings.ToUpper(r.Command), r.Device, r.DurationMs)
	if r.Report != nil {
		s += "\n  change of state: " + r.Report.line()
	}
	return s, nil
}

// TableHeaders implements format.TableMarshaler.
func (r *CommandResult) TableHeaders() []string {
	return []string{"FUN", "INF", "NAME", "COMMAND", "ACKNOWLEDGED", "REPORT", "MS"}
}

// TableRows implements format.TableMarshaler.
func (r *CommandResult) TableRows() [][]string {
	report := ""
	if r.Report != nil {
		report = r.Report.Summary()
	}
	return [][]string{{strconv.Itoa(int(r.Fun)), strconv.Itoa(int(r.Inf)), r.Name, r.Command, strconv.FormatBool(r.Acknowledged), report, fmt.Sprintf("%.0f", r.DurationMs)}}
}

// ClockResult is the result of `client clock`.
type ClockResult struct {
	Device
	Broadcast bool `json:"broadcast"`
	// Sent is the time the synchronization carried (RFC 3339, in the zone of the time tags).
	Sent string `json:"sent"`
	// DeviceTime is the time of the ASDU 6 the device answered with (not for a broadcast).
	DeviceTime string `json:"device_time,omitempty"`
	// OffsetMs is DeviceTime minus Sent.
	OffsetMs   *float64 `json:"offset_ms,omitempty"`
	DurationMs float64  `json:"duration_ms"`
}

// MarshalTextOutput implements format.TextMarshaler.
func (r *ClockResult) MarshalTextOutput() (string, error) {
	if r.Broadcast {
		return fmt.Sprintf("Clock synchronization broadcast on %s: %s (not confirmed)", r.Target, r.Sent), nil
	}
	s := fmt.Sprintf("Clock of %s set to %s, confirmed in %.0f ms; device time %s", r.Device, r.Sent, r.DurationMs, r.DeviceTime)
	if r.OffsetMs != nil {
		s += fmt.Sprintf(" (%+.0f ms)", *r.OffsetMs)
	}
	return s, nil
}

// TableHeaders implements format.TableMarshaler.
func (r *ClockResult) TableHeaders() []string {
	return []string{"BROADCAST", "SENT", "DEVICE_TIME", "OFFSET_MS", "MS"}
}

// TableRows implements format.TableMarshaler.
func (r *ClockResult) TableRows() [][]string {
	off := ""
	if r.OffsetMs != nil {
		off = fmt.Sprintf("%.0f", *r.OffsetMs)
	}
	return [][]string{{strconv.FormatBool(r.Broadcast), r.Sent, r.DeviceTime, off, fmt.Sprintf("%.0f", r.DurationMs)}}
}

// Identification is one identification (ASDU 5) of the device.
type Identification struct {
	Cause        uint8  `json:"cause"`
	CauseName    string `json:"cause_name"`
	CA           uint8  `json:"ca"`
	Fun          uint8  `json:"fun"`
	FunName      string `json:"fun_name"`
	Inf          uint8  `json:"inf"`
	COL          uint8  `json:"col"`
	Manufacturer string `json:"manufacturer"`
	Software     string `json:"software"`
}

// IdentifyResult is the result of `client identify`.
type IdentifyResult struct {
	Device
	Identifications []Identification `json:"identifications"`
}

// MarshalTextOutput implements format.TextMarshaler.
func (r *IdentifyResult) MarshalTextOutput() (string, error) {
	var b strings.Builder
	_, _ = fmt.Fprintf(&b, "Identification of %s\n", r.Device)
	for _, id := range r.Identifications {
		_, _ = fmt.Fprintf(&b, "  %-20s %q  COL %d  software %s  FUN %s\n", id.CauseName, id.Manufacturer, id.COL, id.Software, id.FunName)
	}
	return b.String(), nil
}

// TableHeaders implements format.TableMarshaler.
func (r *IdentifyResult) TableHeaders() []string {
	return []string{"COT", "CAUSE", "FUN", "INF", "COL", "MANUFACTURER", "SOFTWARE"}
}

// TableRows implements format.TableMarshaler.
func (r *IdentifyResult) TableRows() [][]string {
	var rows [][]string
	for _, id := range r.Identifications {
		rows = append(rows, []string{strconv.Itoa(int(id.Cause)), id.CauseName, strconv.Itoa(int(id.Fun)), strconv.Itoa(int(id.Inf)), strconv.Itoa(int(id.COL)), id.Manufacturer, id.Software})
	}
	return rows
}

// ScanRow is a device `client scan` found.
type ScanRow struct {
	LinkAddress uint8 `json:"link_address"`
	// Identified reports that it sent an identification; the fields below are its.
	Identified   bool   `json:"identified"`
	CA           uint8  `json:"ca"`
	Fun          uint8  `json:"fun"`
	FunName      string `json:"fun_name,omitempty"`
	COL          uint8  `json:"col"`
	Manufacturer string `json:"manufacturer,omitempty"`
	Software     string `json:"software,omitempty"`
}

// ScanResult is the result of `client scan`.
type ScanResult struct {
	Target     string    `json:"target"`
	Start      uint8     `json:"start"`
	End        uint8     `json:"end"`
	Tried      int       `json:"tried"`
	DurationMs float64   `json:"duration_ms"`
	Found      []ScanRow `json:"found"`
}

// MarshalTextOutput implements format.TextMarshaler.
func (r *ScanResult) MarshalTextOutput() (string, error) {
	var b strings.Builder
	_, _ = fmt.Fprintf(&b, "Scan of link addresses %d-%d on %s: %d of %d answered in %.0f ms\n", r.Start, r.End, r.Target, len(r.Found), r.Tried, r.DurationMs)
	for _, f := range r.Found {
		if f.Identified {
			_, _ = fmt.Fprintf(&b, "  link %3d  ca %3d  %q  COL %d  software %s  FUN %s\n", f.LinkAddress, f.CA, f.Manufacturer, f.COL, f.Software, f.FunName)
		} else {
			_, _ = fmt.Fprintf(&b, "  link %3d  link up, no identification\n", f.LinkAddress)
		}
	}
	return b.String(), nil
}

// TableHeaders implements format.TableMarshaler.
func (r *ScanResult) TableHeaders() []string {
	return []string{"LINK", "IDENTIFIED", "CA", "FUN", "COL", "MANUFACTURER", "SOFTWARE"}
}

// TableRows implements format.TableMarshaler.
func (r *ScanResult) TableRows() [][]string {
	var rows [][]string
	for _, f := range r.Found {
		rows = append(rows, []string{strconv.Itoa(int(f.LinkAddress)), strconv.FormatBool(f.Identified), strconv.Itoa(int(f.CA)), strconv.Itoa(int(f.Fun)), strconv.Itoa(int(f.COL)), f.Manufacturer, f.Software})
	}
	return rows
}

// MonitorSummary is what `client monitor` writes to stderr when it ends.
type MonitorSummary struct {
	Target     string
	Messages   int
	Reconnects int
	DurationMs float64
}

func (s MonitorSummary) String() string {
	return fmt.Sprintf("monitor %s: %d messages in %.0f ms, %d reconnects", s.Target, s.Messages, s.DurationMs, s.Reconnects)
}

// ServerEvent is one line of the event log of `server start`.
type ServerEvent struct {
	Timestamp string `json:"timestamp"`
	// Event is "listening", "connected", "disconnected", "busy", "link", "interrogation",
	// "clock-sync", "command", "generic-read", "generic-write", "generic-interrogation",
	// "disturbance-transmitted", "spontaneous" or "stopping".
	Event  string `json:"event"`
	Remote string `json:"remote,omitempty"`
	// Fun and Inf name the object of a command or an event.
	Fun    *uint8 `json:"fun,omitempty"`
	Inf    *uint8 `json:"inf,omitempty"`
	Value  any    `json:"value,omitempty"`
	Detail string `json:"detail,omitempty"`
}

// MarshalTextOutput implements format.TextMarshaler.
func (e *ServerEvent) MarshalTextOutput() (string, error) {
	var b strings.Builder
	_, _ = fmt.Fprintf(&b, "%s  %-23s", e.Timestamp, e.Event)
	if e.Remote != "" {
		_, _ = fmt.Fprintf(&b, "  %s", e.Remote)
	}
	if e.Fun != nil && e.Inf != nil {
		_, _ = fmt.Fprintf(&b, "  fun=%d inf=%d", *e.Fun, *e.Inf)
	}
	if e.Value != nil {
		_, _ = fmt.Fprintf(&b, "  %v", e.Value)
	}
	if e.Detail != "" {
		_, _ = fmt.Fprintf(&b, "  %s", e.Detail)
	}
	return b.String(), nil
}

// TableHeaders implements format.TableMarshaler.
func (e *ServerEvent) TableHeaders() []string {
	return []string{"TIMESTAMP", "EVENT", "REMOTE", "FUN", "INF", "VALUE", "DETAIL"}
}

// TableRows implements format.TableMarshaler.
func (e *ServerEvent) TableRows() [][]string {
	num := func(p *uint8) string {
		if p == nil {
			return ""
		}
		return strconv.Itoa(int(*p))
	}
	value := ""
	if e.Value != nil {
		value = fmt.Sprint(e.Value)
	}
	return [][]string{{e.Timestamp, e.Event, e.Remote, num(e.Fun), num(e.Inf), value, e.Detail}}
}
