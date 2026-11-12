// SPDX-License-Identifier: MIT

// Package types holds the results of the iec102ctl commands: what --format json writes, and
// their text and table renderings.
package types

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/otfabric/iec60870ctl/internal/errs"
)

// ErrorInfo is the structured error object for JSON output (shared by every tool of the
// repository).
type ErrorInfo = errs.Info

// ErrorEnvelope is the top-level JSON shape for fatal errors (--format json).
type ErrorEnvelope = errs.Envelope

// Session is the REE session around an operation: whether the start (C_AC_NA_2) and the end
// (C_FS_NA_2) were confirmed.
type Session struct {
	Login  bool `json:"login"`
	Logout bool `json:"logout"`
}

// Meter identifies the meter an operation addressed.
type Meter struct {
	Target      string `json:"target"`
	LinkAddress uint16 `json:"link_address"`
	CommonAddr  uint16 `json:"ca"`
	// Session is set when an access key was given.
	Session    *Session `json:"session,omitempty"`
	DurationMs float64  `json:"duration_ms"`
}

func (m Meter) header(what string) string {
	s := fmt.Sprintf("%s of meter %d (link %d) at %s in %.0f ms", what, m.CommonAddr, m.LinkAddress, m.Target, m.DurationMs)
	if m.Session != nil {
		s += fmt.Sprintf(", REE session login %s logout %s", yes(m.Session.Login), yes(m.Session.Logout))
	}
	return s + "\n"
}

func yes(b bool) string {
	if b {
		return "confirmed"
	}
	return "not confirmed"
}

// Time is a time a or time b of IEC 102: the wall clock as the meter sent it (the meter's
// local time, no zone), with the SU (summer time) and IV (invalid) bits.
type Time struct {
	Time    string `json:"time"`
	SU      bool   `json:"su,omitempty"`
	Invalid bool   `json:"time_invalid,omitempty"`
}

func (t Time) text() string {
	s := t.Time
	if t.SU {
		s += " SU"
	}
	if t.Invalid {
		s += " IV"
	}
	return s
}

// InfoResult is the result of `client info`: P_MP_NA_2.
type InfoResult struct {
	Meter
	// Standard is the first octet (edition of the standard).
	Standard     uint8  `json:"standard"`
	Manufacturer uint8  `json:"manufacturer"`
	Product      uint32 `json:"product"`
}

// MarshalTextOutput implements format.TextMarshaler.
func (r *InfoResult) MarshalTextOutput() (string, error) {
	return r.header("Product specification") + fmt.Sprintf("  standard %d, manufacturer %d, product %d\n", r.Standard, r.Manufacturer, r.Product), nil
}

// TableHeaders implements format.TableMarshaler.
func (r *InfoResult) TableHeaders() []string {
	return []string{"CA", "STANDARD", "MANUFACTURER", "PRODUCT"}
}

// TableRows implements format.TableMarshaler.
func (r *InfoResult) TableRows() [][]string {
	return [][]string{{u(uint64(r.CommonAddr)), u(uint64(r.Standard)), u(uint64(r.Manufacturer)), u(uint64(r.Product))}}
}

// TimeResult is the result of `client time`: M_TI_TA_2.
type TimeResult struct {
	Meter
	Time
	// Timestamp is the meter's clock read in the zone of --timezone, RFC 3339; empty when the
	// fields do not form a valid time.
	Timestamp string `json:"timestamp,omitempty"`
	// DriftMs is the meter's clock minus this computer's clock when the answer arrived.
	DriftMs *float64 `json:"drift_ms,omitempty"`
}

// MarshalTextOutput implements format.TextMarshaler.
func (r *TimeResult) MarshalTextOutput() (string, error) {
	s := r.header("Clock") + "  " + r.text()
	if r.DriftMs != nil {
		s += fmt.Sprintf(" (%+.0f ms against this computer)", *r.DriftMs)
	}
	return s + "\n", nil
}

// TableHeaders implements format.TableMarshaler.
func (r *TimeResult) TableHeaders() []string { return []string{"CA", "TIME", "SU", "IV", "DRIFT_MS"} }

// TableRows implements format.TableMarshaler.
func (r *TimeResult) TableRows() [][]string {
	drift := ""
	if r.DriftMs != nil {
		drift = strconv.FormatFloat(*r.DriftMs, 'f', 0, 64)
	}
	return [][]string{{u(uint64(r.CommonAddr)), r.Time.Time, flag(r.SU), flag(r.Invalid), drift}}
}

// Total is one integrated total.
type Total struct {
	Address uint8  `json:"address"`
	Value   uint32 `json:"value"`
	// Quality is the quality descriptor octet as received; Flags names its set bits.
	Quality uint8  `json:"quality"`
	Flags   string `json:"flags"`
	// Signature is the signature octet, when the meter sends one (not checked).
	Signature *uint8 `json:"signature,omitempty"`
}

// Period is the integrated totals of one integration period.
type Period struct {
	Type   string `json:"type"`
	TypeID uint8  `json:"type_id"`
	// End is the end of the integration period, the common time tag (time a).
	End    Time    `json:"end"`
	Totals []Total `json:"totals"`
}

// TotalsResult is the result of `client totals`.
type TotalsResult struct {
	Meter
	Record uint8 `json:"record"`
	// Request is the read command sent (C_CI_NT_2), RequestID its number.
	Request   string `json:"request"`
	RequestID uint8  `json:"request_id"`
	// Kind is the family of integrated totals.
	Kind    string   `json:"kind"`
	From    string   `json:"from,omitempty"`
	To      string   `json:"to,omitempty"`
	First   *uint8   `json:"first,omitempty"`
	Last    *uint8   `json:"last,omitempty"`
	Count   int      `json:"count"`
	Periods []Period `json:"periods"`
}

// MarshalTextOutput implements format.TextMarshaler.
func (r *TotalsResult) MarshalTextOutput() (string, error) {
	var b strings.Builder
	b.WriteString(r.header(fmt.Sprintf("%s (%s) of record %d", r.Request, r.Kind, r.Record)))
	_, _ = fmt.Fprintf(&b, "  %d integration periods\n", r.Count)
	for _, p := range r.Periods {
		_, _ = fmt.Fprintf(&b, "  %s  %s\n", p.End.text(), p.Type)
		for _, t := range p.Totals {
			_, _ = fmt.Fprintf(&b, "    %3d  %10d  %s\n", t.Address, t.Value, t.Flags)
		}
	}
	return b.String(), nil
}

// TableHeaders implements format.TableMarshaler.
func (r *TotalsResult) TableHeaders() []string {
	return []string{"END", "TYPE", "ADDRESS", "VALUE", "QUALITY", "FLAGS"}
}

// TableRows implements format.TableMarshaler.
func (r *TotalsResult) TableRows() [][]string {
	rows := [][]string{}
	for _, p := range r.Periods {
		for _, t := range p.Totals {
			rows = append(rows, []string{p.End.text(), p.Type, u(uint64(t.Address)), u(uint64(t.Value)), u(uint64(t.Quality)), t.Flags})
		}
	}
	return rows
}

// Event is one single-point information with time tag (M_SP_TA_2).
type Event struct {
	Address   uint8 `json:"address"`
	On        bool  `json:"on"`
	Qualifier uint8 `json:"qualifier"`
	Time      Time  `json:"time"`
}

// EventsResult is the result of `client events`.
type EventsResult struct {
	Meter
	Record  uint8   `json:"record"`
	Request string  `json:"request"`
	From    string  `json:"from,omitempty"`
	To      string  `json:"to,omitempty"`
	Count   int     `json:"count"`
	Events  []Event `json:"events"`
}

// MarshalTextOutput implements format.TextMarshaler.
func (r *EventsResult) MarshalTextOutput() (string, error) {
	var b strings.Builder
	b.WriteString(r.header(fmt.Sprintf("%s of record %d", r.Request, r.Record)))
	_, _ = fmt.Fprintf(&b, "  %d events\n", r.Count)
	for _, e := range r.Events {
		_, _ = fmt.Fprintf(&b, "  %s  %3d  %s  qualifier %d\n", e.Time.text(), e.Address, onOff(e.On), e.Qualifier)
	}
	return b.String(), nil
}

// TableHeaders implements format.TableMarshaler.
func (r *EventsResult) TableHeaders() []string {
	return []string{"TIME", "ADDRESS", "STATE", "QUALIFIER"}
}

// TableRows implements format.TableMarshaler.
func (r *EventsResult) TableRows() [][]string {
	rows := [][]string{}
	for _, e := range r.Events {
		rows = append(rows, []string{e.Time.text(), u(uint64(e.Address)), onOff(e.On), u(uint64(e.Qualifier))})
	}
	return rows
}

// ASDU is one ASDU as received or sent, for `client request`.
type ASDU struct {
	Type   string `json:"type"`
	TypeID uint8  `json:"type_id"`
	// Cause is the name of the cause of transmission, Cot its number (6 bits).
	Cause    string `json:"cause"`
	Cot      uint8  `json:"cot"`
	Negative bool   `json:"negative,omitempty"`
	Test     bool   `json:"test,omitempty"`
	// CommonAddr is the common address of ASDU, Record the record address.
	CommonAddr uint16 `json:"ca"`
	Record     uint8  `json:"record"`
	// Objects is the number of information objects of the VSQ, SQ its sequence bit.
	Objects int  `json:"objects"`
	SQ      bool `json:"sq,omitempty"`
	// Data is the octets after the record address, as lower-case hex.
	Data string `json:"data"`
}

func (a ASDU) text() string {
	s := fmt.Sprintf("%s (%d) %s (%d) ca=%d record=%d objects=%d", a.Type, a.TypeID, a.Cause, a.Cot, a.CommonAddr, a.Record, a.Objects)
	if a.Negative {
		s += " negative"
	}
	if a.Test {
		s += " test"
	}
	if a.Data != "" {
		s += " data=" + a.Data
	}
	return s
}

// RequestResult is the result of `client request`.
type RequestResult struct {
	Meter
	Request ASDU   `json:"request"`
	Until   string `json:"until"`
	Count   int    `json:"count"`
	Answers []ASDU `json:"answers"`
}

// MarshalTextOutput implements format.TextMarshaler.
func (r *RequestResult) MarshalTextOutput() (string, error) {
	var b strings.Builder
	b.WriteString(r.header("Request"))
	_, _ = fmt.Fprintf(&b, "  -> %s\n", r.Request.text())
	for _, a := range r.Answers {
		_, _ = fmt.Fprintf(&b, "  <- %s\n", a.text())
	}
	return b.String(), nil
}

// TableHeaders implements format.TableMarshaler.
func (r *RequestResult) TableHeaders() []string {
	return []string{"DIR", "TYPE", "TYPE_ID", "COT", "NEGATIVE", "CA", "RECORD", "OBJECTS", "DATA"}
}

// TableRows implements format.TableMarshaler.
func (r *RequestResult) TableRows() [][]string {
	row := func(dir string, a ASDU) []string {
		return []string{dir, a.Type, u(uint64(a.TypeID)), u(uint64(a.Cot)), flag(a.Negative), u(uint64(a.CommonAddr)), u(uint64(a.Record)), strconv.Itoa(a.Objects), a.Data}
	}
	rows := [][]string{row("->", r.Request)}
	for _, a := range r.Answers {
		rows = append(rows, row("<-", a))
	}
	return rows
}

// LoginResult is the result of `client ree login`.
type LoginResult struct {
	Meter
}

// MarshalTextOutput implements format.TextMarshaler.
func (r *LoginResult) MarshalTextOutput() (string, error) { return r.header("REE session"), nil }

// TableHeaders implements format.TableMarshaler.
func (r *LoginResult) TableHeaders() []string { return []string{"CA", "LOGIN", "LOGOUT"} }

// TableRows implements format.TableMarshaler.
func (r *LoginResult) TableRows() [][]string {
	var s Session
	if r.Session != nil {
		s = *r.Session
	}
	return [][]string{{u(uint64(r.CommonAddr)), flag(s.Login), flag(s.Logout)}}
}

// SetTimeResult is the result of `client ree set-time`.
type SetTimeResult struct {
	Meter
	// Time is the time b sent.
	Time
	RTTMs float64 `json:"rtt_ms"`
}

// MarshalTextOutput implements format.TextMarshaler.
func (r *SetTimeResult) MarshalTextOutput() (string, error) {
	return r.header("Clock set") + fmt.Sprintf("  %s confirmed in %.0f ms\n", r.text(), r.RTTMs), nil
}

// TableHeaders implements format.TableMarshaler.
func (r *SetTimeResult) TableHeaders() []string { return []string{"CA", "TIME", "SU", "RTT_MS"} }

// TableRows implements format.TableMarshaler.
func (r *SetTimeResult) TableRows() [][]string {
	return [][]string{{u(uint64(r.CommonAddr)), r.Time.Time, flag(r.SU), strconv.FormatFloat(r.RTTMs, 'f', 1, 64)}}
}

// TariffRecord is one record of M_TA_VC_2 or M_TA_VM_2 (REE profile).
type TariffRecord struct {
	Address            uint8  `json:"address"`
	ActiveAbs          uint32 `json:"active_abs"`
	ActiveInc          uint32 `json:"active_inc"`
	ActiveQuality      uint8  `json:"active_quality"`
	ReactiveIndAbs     uint32 `json:"reactive_ind_abs"`
	ReactiveIndInc     uint32 `json:"reactive_ind_inc"`
	ReactiveIndQuality uint8  `json:"reactive_ind_quality"`
	ReactiveCapAbs     uint32 `json:"reactive_cap_abs"`
	ReactiveCapInc     uint32 `json:"reactive_cap_inc"`
	ReactiveCapQuality uint8  `json:"reactive_cap_quality"`
	MaxPower           uint32 `json:"max_power"`
	MaxPowerTime       Time   `json:"max_power_time"`
	MaxPowerQuality    uint8  `json:"max_power_quality"`
	ExcessPower        uint32 `json:"excess_power"`
	ExcessPowerQuality uint8  `json:"excess_power_quality"`
	PeriodStart        Time   `json:"period_start"`
	PeriodEnd          Time   `json:"period_end"`
}

// TariffsResult is the result of `client ree tariffs`.
type TariffsResult struct {
	Meter
	Register uint8          `json:"register"`
	Request  string         `json:"request"`
	From     string         `json:"from,omitempty"`
	To       string         `json:"to,omitempty"`
	Count    int            `json:"count"`
	Records  []TariffRecord `json:"records"`
}

// MarshalTextOutput implements format.TextMarshaler.
func (r *TariffsResult) MarshalTextOutput() (string, error) {
	var b strings.Builder
	b.WriteString(r.header(fmt.Sprintf("%s of register %d", r.Request, r.Register)))
	_, _ = fmt.Fprintf(&b, "  %d records\n", r.Count)
	for _, t := range r.Records {
		_, _ = fmt.Fprintf(&b, "  %3d  %s .. %s  active %d (+%d)  reactive ind %d (+%d) cap %d (+%d)  max power %d at %s  excess %d\n",
			t.Address, t.PeriodStart.text(), t.PeriodEnd.text(), t.ActiveAbs, t.ActiveInc, t.ReactiveIndAbs, t.ReactiveIndInc,
			t.ReactiveCapAbs, t.ReactiveCapInc, t.MaxPower, t.MaxPowerTime.text(), t.ExcessPower)
	}
	return b.String(), nil
}

// TableHeaders implements format.TableMarshaler.
func (r *TariffsResult) TableHeaders() []string {
	return []string{"ADDRESS", "START", "END", "ACTIVE_ABS", "ACTIVE_INC", "REACTIVE_IND_ABS", "REACTIVE_CAP_ABS", "MAX_POWER", "MAX_POWER_TIME", "EXCESS_POWER"}
}

// TableRows implements format.TableMarshaler.
func (r *TariffsResult) TableRows() [][]string {
	rows := [][]string{}
	for _, t := range r.Records {
		rows = append(rows, []string{u(uint64(t.Address)), t.PeriodStart.text(), t.PeriodEnd.text(), u(uint64(t.ActiveAbs)), u(uint64(t.ActiveInc)),
			u(uint64(t.ReactiveIndAbs)), u(uint64(t.ReactiveCapAbs)), u(uint64(t.MaxPower)), t.MaxPowerTime.text(), u(uint64(t.ExcessPower))})
	}
	return rows
}

// ContractedPower is one contracted power of M_PC_NA_2.
type ContractedPower struct {
	Address uint8  `json:"address"`
	PowerKW uint32 `json:"power_kw"`
}

// PowersResult is the result of `client ree contracted-powers`.
type PowersResult struct {
	Meter
	Record uint8             `json:"record"`
	Time   *Time             `json:"time,omitempty"`
	Count  int               `json:"count"`
	Powers []ContractedPower `json:"powers"`
}

// MarshalTextOutput implements format.TextMarshaler.
func (r *PowersResult) MarshalTextOutput() (string, error) {
	var b strings.Builder
	b.WriteString(r.header("Contracted powers"))
	if r.Time != nil {
		_, _ = fmt.Fprintf(&b, "  as of %s\n", r.Time.text())
	}
	for _, p := range r.Powers {
		_, _ = fmt.Fprintf(&b, "  period %d  %d kW\n", p.Address, p.PowerKW)
	}
	return b.String(), nil
}

// TableHeaders implements format.TableMarshaler.
func (r *PowersResult) TableHeaders() []string { return []string{"ADDRESS", "POWER_KW"} }

// TableRows implements format.TableMarshaler.
func (r *PowersResult) TableRows() [][]string {
	rows := [][]string{}
	for _, p := range r.Powers {
		rows = append(rows, []string{u(uint64(p.Address)), u(uint64(p.PowerKW))})
	}
	return rows
}

// SpecialDaysResult is the result of `client ree special-days`.
type SpecialDaysResult struct {
	Meter
	Record uint8  `json:"record"`
	Count  int    `json:"count"`
	Days   []Time `json:"days"`
}

// MarshalTextOutput implements format.TextMarshaler.
func (r *SpecialDaysResult) MarshalTextOutput() (string, error) {
	var b strings.Builder
	b.WriteString(r.header("Special days"))
	for _, d := range r.Days {
		_, _ = fmt.Fprintf(&b, "  %s\n", d.text())
	}
	return b.String(), nil
}

// TableHeaders implements format.TableMarshaler.
func (r *SpecialDaysResult) TableHeaders() []string { return []string{"DAY"} }

// TableRows implements format.TableMarshaler.
func (r *SpecialDaysResult) TableRows() [][]string {
	rows := [][]string{}
	for _, d := range r.Days {
		rows = append(rows, []string{d.text()})
	}
	return rows
}

// TypeInfo describes one type identification, for `client types`.
type TypeInfo struct {
	ID   uint8  `json:"id"`
	Name string `json:"name"`
	// Direction is "monitor" or "control".
	Direction string `json:"direction"`
	// Profile is "standard" or "ree".
	Profile string `json:"profile"`
	// Decoded reports whether iec102ctl decodes its objects (else they are shown as hex).
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
		d := ""
		if !t.Decoded {
			d = "  (hex)"
		}
		_, _ = fmt.Fprintf(&b, "%3d  %-10s  %-7s  %-8s%s\n", t.ID, t.Name, t.Direction, t.Profile, d)
	}
	return b.String(), nil
}

// TableHeaders implements format.TableMarshaler.
func (r *TypesResult) TableHeaders() []string {
	return []string{"ID", "NAME", "DIRECTION", "PROFILE", "DECODED"}
}

// TableRows implements format.TableMarshaler.
func (r *TypesResult) TableRows() [][]string {
	rows := [][]string{}
	for _, t := range r.Types {
		rows = append(rows, []string{u(uint64(t.ID)), t.Name, t.Direction, t.Profile, flag(t.Decoded)})
	}
	return rows
}

// ServerEvent is one line of the event log of `server start`.
type ServerEvent struct {
	Timestamp string `json:"timestamp"`
	// Event is "listening", "connected", "busy", "link-up", "disconnected", "request",
	// "answer", "login", "logout", "set-time", "refused" or "stopping".
	Event  string `json:"event"`
	Remote string `json:"remote,omitempty"`
	// CommonAddr, Record, Type and Cot name the ASDU an event is about.
	CommonAddr *uint16 `json:"ca,omitempty"`
	Record     *uint8  `json:"record,omitempty"`
	Type       string  `json:"type,omitempty"`
	Cot        *uint8  `json:"cot,omitempty"`
	Detail     string  `json:"detail,omitempty"`
}

// MarshalTextOutput implements format.TextMarshaler.
func (e *ServerEvent) MarshalTextOutput() (string, error) {
	var b strings.Builder
	_, _ = fmt.Fprintf(&b, "%s  %-12s", e.Timestamp, e.Event)
	if e.Remote != "" {
		_, _ = fmt.Fprintf(&b, "  %s", e.Remote)
	}
	if e.CommonAddr != nil {
		_, _ = fmt.Fprintf(&b, "  ca=%d", *e.CommonAddr)
	}
	if e.Record != nil {
		_, _ = fmt.Fprintf(&b, " record=%d", *e.Record)
	}
	if e.Type != "" {
		_, _ = fmt.Fprintf(&b, "  %s", e.Type)
	}
	if e.Cot != nil {
		_, _ = fmt.Fprintf(&b, " cot=%d", *e.Cot)
	}
	if e.Detail != "" {
		_, _ = fmt.Fprintf(&b, "  %s", e.Detail)
	}
	return b.String(), nil
}

// TableHeaders implements format.TableMarshaler.
func (e *ServerEvent) TableHeaders() []string {
	return []string{"TIMESTAMP", "EVENT", "REMOTE", "CA", "RECORD", "TYPE", "COT", "DETAIL"}
}

// TableRows implements format.TableMarshaler.
func (e *ServerEvent) TableRows() [][]string {
	ca, rec, cot := "", "", ""
	if e.CommonAddr != nil {
		ca = u(uint64(*e.CommonAddr))
	}
	if e.Record != nil {
		rec = u(uint64(*e.Record))
	}
	if e.Cot != nil {
		cot = u(uint64(*e.Cot))
	}
	return [][]string{{e.Timestamp, e.Event, e.Remote, ca, rec, e.Type, cot, e.Detail}}
}

func u(n uint64) string { return strconv.FormatUint(n, 10) }

func flag(b bool) string {
	if b {
		return "yes"
	}
	return ""
}

func onOff(b bool) string {
	if b {
		return "on"
	}
	return "off"
}
