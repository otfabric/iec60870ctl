// SPDX-License-Identifier: MIT

package types

import (
	"strings"
	"testing"

	"github.com/otfabric/iec60870ctl/internal/format"
)

type rendered interface {
	format.TextMarshaler
	format.TableMarshaler
}

// Every result renders as text and as a table whose rows have the columns of the header.
func TestRenderings(t *testing.T) {
	sig, one := uint8(7), uint8(1)
	drift := 12.5
	m := Meter{Target: "tcp://h:2401", LinkAddress: 1, CommonAddr: 2, Session: &Session{Login: true}}
	tm := Time{Time: "2026-01-01T00:00", SU: true, Invalid: true}
	ca, rec, cot := uint16(1), uint8(11), uint8(18)
	for name, c := range map[string]struct {
		r    rendered
		want string
	}{
		"info":  {&InfoResult{Meter: m, Product: 9}, "product 9"},
		"time":  {&TimeResult{Meter: m, Time: tm, DriftMs: &drift}, "SU IV (+12 ms"},
		"time0": {&TimeResult{Meter: Meter{}, Time: tm}, "IV"},
		"totals": {&TotalsResult{Meter: m, Request: "C_CI_NT_2", Kind: "operational", First: &one, Last: &one,
			Periods: []Period{{Type: "M_IT_TG_2", End: tm, Totals: []Total{{Address: 1, Value: 5, Flags: "good", Signature: &sig}}}}}, "not confirmed"},
		"events":  {&EventsResult{Meter: m, Request: "C_SP_NA_2", Events: []Event{{Address: 1, On: true, Time: tm}, {Address: 2}}}, "  on  "},
		"request": {&RequestResult{Meter: m, Request: ASDU{Type: "X", Negative: true, Test: true, Data: "01"}, Answers: []ASDU{{Type: "Y"}}}, "negative test data=01"},
		"login":   {&LoginResult{Meter: m}, "REE session"},
		"login0":  {&LoginResult{Meter: Meter{}}, "REE session"},
		"settime": {&SetTimeResult{Meter: m, Time: tm, RTTMs: 3}, "confirmed in 3 ms"},
		"tariffs": {&TariffsResult{Meter: m, Request: "C_TA_VC_2", Records: []TariffRecord{{Address: 1, MaxPower: 4}}}, "max power 4"},
		"powers":  {&PowersResult{Meter: m, Time: &tm, Powers: []ContractedPower{{Address: 1, PowerKW: 15}}}, "15 kW"},
		"powers0": {&PowersResult{Meter: m}, "Contracted powers"},
		"days":    {&SpecialDaysResult{Meter: m, Days: []Time{tm}}, "2026-01-01T00:00"},
		"types":   {&TypesResult{Types: []TypeInfo{{ID: 1, Name: "M_SP_TA_2", Decoded: true}, {ID: 200, Name: "X"}}}, "(hex)"},
		"event":   {&ServerEvent{Timestamp: "t", Event: "request", Remote: "r", CommonAddr: &ca, Record: &rec, Type: "C_CI_NT_2", Cot: &cot, Detail: "d"}, "ca=1 record=11  C_CI_NT_2 cot=18  d"},
		"event0":  {&ServerEvent{Timestamp: "t", Event: "stopping"}, "stopping"},
	} {
		text, err := c.r.MarshalTextOutput()
		if err != nil || !strings.Contains(text, c.want) {
			t.Errorf("%s: %q, %v", name, text, err)
		}
		h := c.r.TableHeaders()
		for _, row := range c.r.TableRows() {
			if len(row) != len(h) {
				t.Errorf("%s: row %v for header %v", name, row, h)
			}
		}
	}
}
