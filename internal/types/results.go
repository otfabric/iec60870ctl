// SPDX-License-Identifier: MIT

package types

import (
	"fmt"
	"strconv"
	"strings"
)

// InterrogateResult is the result of `client interrogate` and `client counters`.
type InterrogateResult struct {
	Target     string `json:"target"`
	CommonAddr uint16 `json:"ca"`
	// Request is "interrogation" or "counter-interrogation".
	Request string `json:"request"`
	// Group is the interrogation or counter group, 0 for a general request.
	Group uint8 `json:"group"`
	// Freeze is the freeze qualifier of a counter interrogation.
	Freeze     string  `json:"freeze,omitempty"`
	DurationMs float64 `json:"duration_ms"`
	// Received counts what the station sent; Count what is listed after filtering.
	Received int     `json:"received"`
	Count    int     `json:"count"`
	Points   []Point `json:"points"`
}

func (r *InterrogateResult) title() string {
	what := "General interrogation"
	if r.Request == "counter-interrogation" {
		what = "Counter interrogation"
		if r.Group != 0 {
			what = fmt.Sprintf("Counter interrogation, group %d", r.Group)
		}
		if r.Freeze != "" && r.Freeze != "read" {
			what += " (" + r.Freeze + ")"
		}
	} else if r.Group != 0 {
		what = fmt.Sprintf("Interrogation of group %d", r.Group)
	}
	return what
}

// MarshalTextOutput implements format.TextMarshaler.
func (r *InterrogateResult) MarshalTextOutput() (string, error) {
	var b strings.Builder
	_, _ = fmt.Fprintf(&b, "%s of station %d at %s: %d objects in %.0f ms", r.title(), r.CommonAddr, r.Target, r.Received, r.DurationMs)
	if r.Count != r.Received {
		_, _ = fmt.Fprintf(&b, ", %d shown", r.Count)
	}
	b.WriteString("\n")
	pointLines(&b, r.Points)
	return b.String(), nil
}

// TableHeaders implements format.TableMarshaler.
func (r *InterrogateResult) TableHeaders() []string { return PointHeaders }

// TableRows implements format.TableMarshaler.
func (r *InterrogateResult) TableRows() [][]string { return pointRows(r.Points) }

// ReadRow is the answer to one read command.
type ReadRow struct {
	IOA   uint32     `json:"ioa"`
	Point *Point     `json:"point,omitempty"`
	Error *ErrorInfo `json:"error,omitempty"`
}

// ReadResult is the result of `client read`.
type ReadResult struct {
	Target     string    `json:"target"`
	CommonAddr uint16    `json:"ca"`
	Requested  int       `json:"requested"`
	Failed     int       `json:"failed"`
	Rows       []ReadRow `json:"rows"`
}

// MarshalTextOutput implements format.TextMarshaler.
func (r *ReadResult) MarshalTextOutput() (string, error) {
	var b strings.Builder
	_, _ = fmt.Fprintf(&b, "Read from station %d at %s: %d of %d answered\n", r.CommonAddr, r.Target, r.Requested-r.Failed, r.Requested)
	for _, row := range r.Rows {
		if row.Point != nil {
			b.WriteString("  " + row.Point.Line() + "\n")
			continue
		}
		_, _ = fmt.Fprintf(&b, "  ioa=%d  error: %s\n", row.IOA, ErrorMessage(row.Error))
	}
	return b.String(), nil
}

// TableHeaders implements format.TableMarshaler.
func (r *ReadResult) TableHeaders() []string {
	return append(append([]string{}, PointHeaders...), "ERROR")
}

// TableRows implements format.TableMarshaler.
func (r *ReadResult) TableRows() [][]string {
	rows := make([][]string, 0, len(r.Rows))
	for _, row := range r.Rows {
		if row.Point != nil {
			rows = append(rows, append(row.Point.Row(), ""))
			continue
		}
		rows = append(rows, []string{strconv.Itoa(int(r.CommonAddr)), strconv.FormatUint(uint64(row.IOA), 10), "-", "-", "-", "-", "-", ErrorMessage(row.Error)})
	}
	return rows
}

// CommandStep is one step of a command procedure.
type CommandStep struct {
	// Step is "select", "execute", "cancel" or "termination".
	Step string `json:"step"`
	// Result is "confirmed", "received" (termination) or "not-received".
	Result string  `json:"result"`
	RTTMs  float64 `json:"rtt_ms"`
}

// CommandResult is the result of `client command`.
type CommandResult struct {
	Target     string        `json:"target"`
	CommonAddr uint16        `json:"ca"`
	IOA        uint32        `json:"ioa"`
	Type       string        `json:"type"`
	Value      any           `json:"value"`
	Mode       string        `json:"mode"`
	Qualifier  any           `json:"qualifier"`
	Steps      []CommandStep `json:"steps"`
}

// MarshalTextOutput implements format.TextMarshaler.
func (r *CommandResult) MarshalTextOutput() (string, error) {
	var b strings.Builder
	_, _ = fmt.Fprintf(&b, "Command %s to station %d at %s, ioa=%d, value %v (%s)\n", r.Type, r.CommonAddr, r.Target, r.IOA, textValue(r.Value), r.Mode)
	for _, s := range r.Steps {
		_, _ = fmt.Fprintf(&b, "  %-11s  %-12s  %.1f ms\n", s.Step, s.Result, s.RTTMs)
	}
	return b.String(), nil
}

// TableHeaders implements format.TableMarshaler.
func (r *CommandResult) TableHeaders() []string {
	return []string{"CA", "IOA", "TYPE", "VALUE", "STEP", "RESULT", "RTT_MS"}
}

// TableRows implements format.TableMarshaler.
func (r *CommandResult) TableRows() [][]string {
	rows := make([][]string, 0, len(r.Steps))
	for _, s := range r.Steps {
		rows = append(rows, []string{strconv.Itoa(int(r.CommonAddr)), strconv.FormatUint(uint64(r.IOA), 10), r.Type, textValue(r.Value), s.Step, s.Result, fmt.Sprintf("%.1f", s.RTTMs)})
	}
	return rows
}

func textValue(v any) string {
	if b, ok := v.(bool); ok {
		if b {
			return "on"
		}
		return "off"
	}
	return fmt.Sprint(v)
}

// ConfirmedResult is the result of a request that is answered by a confirmation alone:
// `client clock` and `client reset`.
type ConfirmedResult struct {
	Target     string `json:"target"`
	CommonAddr uint16 `json:"ca"`
	// Request is "clock-synchronization" or "reset-process".
	Request string `json:"request"`
	// Time is the time a clock synchronization set (RFC 3339, in the zone of the time tags).
	Time string `json:"time,omitempty"`
	// Qualifier is what a reset process command reset: "process" or "events".
	Qualifier string  `json:"qualifier,omitempty"`
	RTTMs     float64 `json:"rtt_ms"`
}

// MarshalTextOutput implements format.TextMarshaler.
func (r *ConfirmedResult) MarshalTextOutput() (string, error) {
	switch r.Request {
	case "clock-synchronization":
		return fmt.Sprintf("Clock of station %d at %s set to %s (confirmed in %.1f ms)", r.CommonAddr, r.Target, r.Time, r.RTTMs), nil
	default:
		return fmt.Sprintf("Reset (%s) of station %d at %s confirmed in %.1f ms", r.Qualifier, r.CommonAddr, r.Target, r.RTTMs), nil
	}
}

// TableHeaders implements format.TableMarshaler.
func (r *ConfirmedResult) TableHeaders() []string {
	return []string{"CA", "REQUEST", "DETAIL", "RESULT", "RTT_MS"}
}

// TableRows implements format.TableMarshaler.
func (r *ConfirmedResult) TableRows() [][]string {
	detail := r.Time
	if detail == "" {
		detail = r.Qualifier
	}
	return [][]string{{strconv.Itoa(int(r.CommonAddr)), r.Request, detail, "confirmed", fmt.Sprintf("%.1f", r.RTTMs)}}
}

// PingSample is one test of `client ping`, written as it completes.
type PingSample struct {
	Seq       uint32     `json:"seq"`
	Timestamp string     `json:"timestamp"`
	Method    string     `json:"method"`
	RTTMs     float64    `json:"rtt_ms"`
	Error     *ErrorInfo `json:"error,omitempty"`
}

// MarshalTextOutput implements format.TextMarshaler.
func (s *PingSample) MarshalTextOutput() (string, error) {
	if s.Error != nil {
		return fmt.Sprintf("%s  seq=%d  %s  error: %s", s.Timestamp, s.Seq, s.Method, s.Error.Message), nil
	}
	return fmt.Sprintf("%s  seq=%d  %s  rtt=%.2f ms", s.Timestamp, s.Seq, s.Method, s.RTTMs), nil
}

// TableHeaders implements format.TableMarshaler.
func (s *PingSample) TableHeaders() []string {
	return []string{"TIMESTAMP", "SEQ", "METHOD", "RTT_MS", "ERROR"}
}

// TableRows implements format.TableMarshaler.
func (s *PingSample) TableRows() [][]string {
	rtt := fmt.Sprintf("%.2f", s.RTTMs)
	if s.Error != nil {
		rtt = "-"
	}
	return [][]string{{s.Timestamp, strconv.FormatUint(uint64(s.Seq), 10), s.Method, rtt, ErrorMessage(s.Error)}}
}

// PingSummary is printed to stderr when `client ping` ends.
type PingSummary struct {
	Sent, Failed       int
	Min, Avg, Max      float64
	Target, MethodName string
}

func (s PingSummary) String() string {
	if s.Sent == s.Failed {
		return fmt.Sprintf("--- %s %s: %d tests, %d failed", s.Target, s.MethodName, s.Sent, s.Failed)
	}
	return fmt.Sprintf("--- %s %s: %d tests, %d failed, rtt min/avg/max = %.2f/%.2f/%.2f ms", s.Target, s.MethodName, s.Sent, s.Failed, s.Min, s.Avg, s.Max)
}

// ScanStation is one common address of `client scan`.
type ScanStation struct {
	CommonAddr uint16 `json:"ca"`
	// Status is "found", or "refused" for an address that answers and refuses the
	// interrogation (the error says how). Addresses that are unknown to the station side or
	// stay silent are counted in the result and not listed.
	Status string `json:"status"`
	// Objects is the number of information objects the station sent in answer.
	Objects int `json:"objects"`
	// Types lists the type identifications seen, with the number of objects of each.
	Types []TypeCount `json:"types,omitempty"`
	Error *ErrorInfo  `json:"error,omitempty"`
}

// TypeCount is a type identification and how many objects of it were seen.
type TypeCount struct {
	Type  string `json:"type"`
	Count int    `json:"count"`
}

// ScanResult is the result of `client scan`.
type ScanResult struct {
	Target     string        `json:"target"`
	Method     string        `json:"method"`
	Probed     int           `json:"probed"`
	Found      int           `json:"found"`
	DurationMs float64       `json:"duration_ms"`
	Stations   []ScanStation `json:"stations"`
}

func (s ScanStation) typesText() string {
	parts := make([]string, 0, len(s.Types))
	for _, t := range s.Types {
		parts = append(parts, fmt.Sprintf("%s×%d", t.Type, t.Count))
	}
	return strings.Join(parts, " ")
}

// MarshalTextOutput implements format.TextMarshaler.
func (r *ScanResult) MarshalTextOutput() (string, error) {
	var b strings.Builder
	_, _ = fmt.Fprintf(&b, "Scan of %s (%s): %d stations found, %d common addresses probed, %.0f ms\n", r.Target, r.Method, r.Found, r.Probed, r.DurationMs)
	for _, s := range r.Stations {
		switch {
		case s.Error != nil:
			_, _ = fmt.Fprintf(&b, "  ca=%-5d  %-8s  %s\n", s.CommonAddr, s.Status, s.Error.Message)
		case s.Status == "found":
			_, _ = fmt.Fprintf(&b, "  ca=%-5d  %-8s  %d objects  %s\n", s.CommonAddr, s.Status, s.Objects, s.typesText())
		default:
			_, _ = fmt.Fprintf(&b, "  ca=%-5d  %s\n", s.CommonAddr, s.Status)
		}
	}
	return b.String(), nil
}

// TableHeaders implements format.TableMarshaler.
func (r *ScanResult) TableHeaders() []string {
	return []string{"CA", "STATUS", "OBJECTS", "TYPES", "ERROR"}
}

// TableRows implements format.TableMarshaler.
func (r *ScanResult) TableRows() [][]string {
	rows := make([][]string, 0, len(r.Stations))
	for _, s := range r.Stations {
		rows = append(rows, []string{strconv.Itoa(int(s.CommonAddr)), s.Status, strconv.Itoa(s.Objects), s.typesText(), ErrorMessage(s.Error)})
	}
	return rows
}

// FileEntry is one entry of a station's file directory.
type FileEntry struct {
	IOA       uint32 `json:"ioa"`
	Name      uint16 `json:"name"`
	Length    uint32 `json:"length"`
	Timestamp string `json:"timestamp,omitempty"`
	Directory bool   `json:"directory,omitempty"`
	Active    bool   `json:"active,omitempty"`
}

// FileListResult is the result of `client file list`.
type FileListResult struct {
	Target     string      `json:"target"`
	CommonAddr uint16      `json:"ca"`
	IOA        uint32      `json:"ioa"`
	Count      int         `json:"count"`
	Entries    []FileEntry `json:"entries"`
}

func (e FileEntry) kind() string {
	switch {
	case e.Directory:
		return "directory"
	case e.Active:
		return "file (active)"
	}
	return "file"
}

// MarshalTextOutput implements format.TextMarshaler.
func (r *FileListResult) MarshalTextOutput() (string, error) {
	var b strings.Builder
	_, _ = fmt.Fprintf(&b, "Directory of station %d at %s: %d entries\n", r.CommonAddr, r.Target, r.Count)
	for _, e := range r.Entries {
		_, _ = fmt.Fprintf(&b, "  ioa=%-8d name=%-5d %8d octets  %s  %s\n", e.IOA, e.Name, e.Length, e.Timestamp, e.kind())
	}
	return b.String(), nil
}

// TableHeaders implements format.TableMarshaler.
func (r *FileListResult) TableHeaders() []string {
	return []string{"IOA", "NAME", "LENGTH", "TIME", "KIND"}
}

// TableRows implements format.TableMarshaler.
func (r *FileListResult) TableRows() [][]string {
	rows := make([][]string, 0, len(r.Entries))
	for _, e := range r.Entries {
		rows = append(rows, []string{strconv.FormatUint(uint64(e.IOA), 10), strconv.Itoa(int(e.Name)), strconv.FormatUint(uint64(e.Length), 10), e.Timestamp, e.kind()})
	}
	return rows
}

// FileTransferResult is the result of `client file get` and `client file put`.
type FileTransferResult struct {
	Target     string `json:"target"`
	CommonAddr uint16 `json:"ca"`
	// Direction is "download" or "upload".
	Direction  string  `json:"direction"`
	IOA        uint32  `json:"ioa"`
	Name       uint16  `json:"name"`
	Length     int     `json:"length"`
	Sections   int     `json:"sections,omitempty"`
	SHA256     string  `json:"sha256"`
	Path       string  `json:"path,omitempty"`
	DurationMs float64 `json:"duration_ms"`
}

// MarshalTextOutput implements format.TextMarshaler.
func (r *FileTransferResult) MarshalTextOutput() (string, error) {
	verb, where := "Fetched", "from"
	if r.Direction == "upload" {
		verb, where = "Delivered", "to"
	}
	s := fmt.Sprintf("%s file ioa=%d name=%d %s station %d at %s: %d octets in %.0f ms\n  sha256: %s", verb, r.IOA, r.Name, where, r.CommonAddr, r.Target, r.Length, r.DurationMs, r.SHA256)
	if r.Path != "" {
		s += "\n  path:   " + r.Path
	}
	return s, nil
}

// TableHeaders implements format.TableMarshaler.
func (r *FileTransferResult) TableHeaders() []string {
	return []string{"DIRECTION", "CA", "IOA", "NAME", "LENGTH", "SHA256", "PATH"}
}

// TableRows implements format.TableMarshaler.
func (r *FileTransferResult) TableRows() [][]string {
	return [][]string{{r.Direction, strconv.Itoa(int(r.CommonAddr)), strconv.FormatUint(uint64(r.IOA), 10), strconv.Itoa(int(r.Name)), strconv.Itoa(r.Length), r.SHA256, r.Path}}
}

// TypeInfo describes one type identification.
type TypeInfo struct {
	ID          uint8  `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	// Direction is "monitor" or "control".
	Direction string `json:"direction"`
	TimeTag   bool   `json:"time_tag"`
	// Decoded is false for a type that is passed through undecoded.
	Decoded bool `json:"decoded"`
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
		note := ""
		if !t.Decoded {
			note = "  (not decoded)"
		}
		_, _ = fmt.Fprintf(&b, "%3d  %-10s %-8s %s%s\n", t.ID, t.Name, t.Direction, t.Description, note)
	}
	return b.String(), nil
}

// TableHeaders implements format.TableMarshaler.
func (r *TypesResult) TableHeaders() []string {
	return []string{"ID", "NAME", "DIRECTION", "TIME_TAG", "DECODED", "DESCRIPTION"}
}

// TableRows implements format.TableMarshaler.
func (r *TypesResult) TableRows() [][]string {
	rows := make([][]string, 0, len(r.Types))
	for _, t := range r.Types {
		rows = append(rows, []string{strconv.Itoa(int(t.ID)), t.Name, t.Direction, strconv.FormatBool(t.TimeTag), strconv.FormatBool(t.Decoded), t.Description})
	}
	return rows
}

// DiscoveredDevice is one address that answered `discover`.
type DiscoveredDevice struct {
	IP   string `json:"ip"`
	Port uint16 `json:"port"`
	// Status is "iec104" (answered the test frame) or "open" (the port accepts connections
	// but did not answer as an IEC 60870-5-104 station).
	Status string  `json:"status"`
	RTTMs  float64 `json:"rtt_ms"`
}

// DiscoverResult is the result of `discover`.
type DiscoverResult struct {
	Subnets    []string           `json:"subnets"`
	Ports      []uint16           `json:"ports"`
	Hosts      int                `json:"hosts"`
	Found      int                `json:"found"`
	DurationMs float64            `json:"duration_ms"`
	Devices    []DiscoveredDevice `json:"devices"`
}

// MarshalTextOutput implements format.TextMarshaler.
func (r *DiscoverResult) MarshalTextOutput() (string, error) {
	var b strings.Builder
	_, _ = fmt.Fprintf(&b, "Discovery: %d IEC 60870-5-104 stations among %d hosts in %.0f ms\n", r.Found, r.Hosts, r.DurationMs)
	for _, d := range r.Devices {
		_, _ = fmt.Fprintf(&b, "  %-15s port %-5d  %-6s  %.1f ms\n", d.IP, d.Port, d.Status, d.RTTMs)
	}
	return b.String(), nil
}

// TableHeaders implements format.TableMarshaler.
func (r *DiscoverResult) TableHeaders() []string { return []string{"IP", "PORT", "STATUS", "RTT_MS"} }

// TableRows implements format.TableMarshaler.
func (r *DiscoverResult) TableRows() [][]string {
	rows := make([][]string, 0, len(r.Devices))
	for _, d := range r.Devices {
		rows = append(rows, []string{d.IP, strconv.Itoa(int(d.Port)), d.Status, fmt.Sprintf("%.1f", d.RTTMs)})
	}
	return rows
}

// ServerEvent is one line of the event log of `server start`.
type ServerEvent struct {
	Timestamp string `json:"timestamp"`
	// Event is "listening", "connected", "started", "stopped", "disconnected", "interrogation",
	// "counter-interrogation", "read", "command", "clock", "reset", "test", "file-sent",
	// "file-received", "directory", "refused", "secured", "security-refused" or "stopping".
	Event  string `json:"event"`
	Remote string `json:"remote,omitempty"`
	// CommonAddr and IOA name what the event is about, where it concerns an object.
	CommonAddr uint16 `json:"ca,omitempty"`
	IOA        uint32 `json:"ioa,omitempty"`
	Type       string `json:"type,omitempty"`
	Value      any    `json:"value,omitempty"`
	Detail     string `json:"detail,omitempty"`
}

// MarshalTextOutput implements format.TextMarshaler.
func (e *ServerEvent) MarshalTextOutput() (string, error) {
	var b strings.Builder
	_, _ = fmt.Fprintf(&b, "%s  %-21s", e.Timestamp, e.Event)
	if e.Remote != "" {
		_, _ = fmt.Fprintf(&b, "  %s", e.Remote)
	}
	if e.CommonAddr != 0 {
		_, _ = fmt.Fprintf(&b, "  ca=%d", e.CommonAddr)
	}
	if e.IOA != 0 {
		_, _ = fmt.Fprintf(&b, " ioa=%d", e.IOA)
	}
	if e.Type != "" {
		_, _ = fmt.Fprintf(&b, "  %s", e.Type)
	}
	if e.Value != nil {
		_, _ = fmt.Fprintf(&b, "  %s", textValue(e.Value))
	}
	if e.Detail != "" {
		_, _ = fmt.Fprintf(&b, "  %s", e.Detail)
	}
	return b.String(), nil
}

// TableHeaders implements format.TableMarshaler.
func (e *ServerEvent) TableHeaders() []string {
	return []string{"TIMESTAMP", "EVENT", "REMOTE", "CA", "IOA", "TYPE", "VALUE", "DETAIL"}
}

// TableRows implements format.TableMarshaler.
func (e *ServerEvent) TableRows() [][]string {
	num := func(n uint64) string {
		if n == 0 {
			return ""
		}
		return strconv.FormatUint(n, 10)
	}
	value := ""
	if e.Value != nil {
		value = textValue(e.Value)
	}
	return [][]string{{e.Timestamp, e.Event, e.Remote, num(uint64(e.CommonAddr)), num(uint64(e.IOA)), e.Type, value, e.Detail}}
}
