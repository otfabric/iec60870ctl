// SPDX-License-Identifier: MIT

package iec

import (
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/otfabric/go-iec60870-5/cp"
	"github.com/otfabric/go-iec60870-5/iec103"
	"github.com/otfabric/iec60870ctl/internal/iec103ctl/types"
)

// Time formats of the output: a CP32Time2a carries the time of day only.
const (
	TimeOfDay = "15:04:05.000"
	FullTime  = "2006-01-02T15:04:05.000Z07:00"
)

// typeNames are the names of the type identifications, as the interop contract writes them.
var typeNames = map[iec103.TypeID]string{
	iec103.TypeTimeTagged:        "time-tagged-message",
	iec103.TypeRelTimeTagged:     "time-tagged-message-relative",
	iec103.TypeMeasurands1:       "measurands-i",
	iec103.TypeRelTimeMeasurand:  "time-tagged-measurands",
	iec103.TypeIdentification:    "identification",
	iec103.TypeTimeSync:          "time-synchronization",
	iec103.TypeGIInit:            "general-interrogation",
	iec103.TypeGITermination:     "general-interrogation-termination",
	iec103.TypeMeasurands2:       "measurands-ii",
	iec103.TypeGenericData:       "generic-data",
	iec103.TypeGenericIdent:      "generic-identification",
	iec103.TypeGeneralCommand:    "general-command",
	iec103.TypeGenericCommand:    "generic-command",
	iec103.TypeDisturbanceList:   "disturbance-list",
	iec103.TypeDisturbanceOrder:  "disturbance-order",
	iec103.TypeDisturbanceAck:    "disturbance-acknowledgement",
	iec103.TypeDisturbanceReady:  "disturbance-ready",
	iec103.TypeChannelReady:      "channel-ready",
	iec103.TypeTagsReady:         "tags-ready",
	iec103.TypeTags:              "tags",
	iec103.TypeDisturbanceValues: "disturbance-values",
	iec103.TypeEndOfTransmission: "disturbance-end",
}

// TypeName returns the name of a type identification: "private" or "reserved" for one
// tables 3 and 4 do not define.
func TypeName(t iec103.TypeID) string {
	if n, ok := typeNames[t]; ok {
		return n
	}
	if t.Compatible() {
		return "reserved"
	}
	return "private"
}

// CauseName returns the name of a cause of transmission without its number.
func CauseName(c iec103.Cause) string { return stripNumber(c.String()) }

// FunName returns the name of a function type without its number.
func FunName(f iec103.FunctionType) string { return stripNumber(f.String()) }

// stripNumber strips the "<n> " prefix the library's String methods write.
func stripNumber(s string) string {
	if i := strings.Index(s, "> "); i >= 0 {
		return s[i+2:]
	}
	return s
}

var catalog iec103.Catalog

// InfoName returns the meaning of fun/inf in an ASDU of type typ in direction dir, or "".
func InfoName(dir iec103.Direction, typ iec103.TypeID, fun iec103.FunctionType, inf iec103.InfoNumber) string {
	if spec, ok := catalog.Lookup(dir, typ, fun, inf); ok {
		return spec.Name
	}
	return ""
}

func u8(v uint8) *uint8    { return &v }
func u16(v uint16) *uint16 { return &v }

// TimeText renders a time tag as the output writes it.
func TimeText(t cp.Timestamp, full bool) string {
	if full {
		return t.Format(FullTime)
	}
	return t.Format(TimeOfDay)
}

// MessageOf renders a monitor-direction ASDU.
func MessageOf(a *iec103.ASDU) types.Message {
	m := types.Message{
		Type: TypeName(a.Type), TypeID: uint8(a.Type), Cause: uint8(a.Cause), CauseName: CauseName(a.Cause),
		CA: a.CommonAddr, Fun: uint8(a.Fun), Inf: uint8(a.Inf),
		Name: InfoName(iec103.Monitor, a.Type, a.Fun, a.Inf),
	}
	switch b := a.Body.(type) {
	case iec103.TimeTagged:
		m.DPI, m.State = u8(uint8(b.DPI)), b.DPI.String()
		m.Time, m.TimeInvalid = TimeText(b.Time, false), b.Time.Invalid
		m.SIN = u8(b.SIN)
	case iec103.RelTimeTagged:
		m.DPI, m.State = u8(uint8(b.DPI)), b.DPI.String()
		m.Time, m.TimeInvalid = TimeText(b.Time, false), b.Time.Invalid
		m.SIN, m.RET, m.FAN = u8(b.SIN), u16(b.RET), u16(b.FAN)
	case iec103.Measurands:
		for _, v := range b.Values {
			m.Values = append(m.Values, types.Measurand{Raw: v.Raw, Value: v.Value(), OV: v.Overflow, ER: v.Error})
		}
	case iec103.RelTimeMeasurand:
		scl := float64(b.SCL)
		m.SCL, m.RET, m.FAN = &scl, u16(b.RET), u16(b.FAN)
		m.Time, m.TimeInvalid = TimeText(b.Time, false), b.Time.Invalid
	case iec103.Identification:
		m.COL, m.Manufacturer, m.Software = u8(b.COL), strings.TrimRight(b.Manufacturer(), " \x00"), hex.EncodeToString(b.Software[:])
	case iec103.TimeSync:
		m.Time, m.TimeInvalid = TimeText(b.Time, true), b.Time.Invalid
	case iec103.GITermination:
		m.SCN = u8(b.SCN)
	case iec103.GIInit:
		m.SCN = u8(b.SCN)
	default:
		m.Data = elementsHex(a)
	}
	return m
}

// elementsHex returns the information elements of a (after FUN and INF) as hex.
func elementsHex(a *iec103.ASDU) string {
	if r, ok := a.Body.(iec103.Raw); ok {
		return hex.EncodeToString(r.Data)
	}
	b, err := a.Encode()
	if err != nil || len(b) < 6 {
		return ""
	}
	return hex.EncodeToString(b[6:])
}

// IdentificationOf renders an identification (ASDU 5).
func IdentificationOf(a *iec103.ASDU) (types.Identification, bool) {
	b, ok := a.Body.(iec103.Identification)
	if !ok {
		return types.Identification{}, false
	}
	return types.Identification{
		Cause: uint8(a.Cause), CauseName: CauseName(a.Cause), CA: a.CommonAddr, Fun: uint8(a.Fun), FunName: FunName(a.Fun),
		Inf: uint8(a.Inf), COL: b.COL, Manufacturer: strings.TrimRight(b.Manufacturer(), " \x00"), Software: hex.EncodeToString(b.Software[:]),
	}, true
}

// kodNames are the kinds of description (clause 7.2.6.34) by name.
var kodNames = map[iec103.KOD]string{
	iec103.KODNone: "none", iec103.KODActualValue: "actual", iec103.KODDefaultValue: "default",
	iec103.KODRange: "range", iec103.KODPrecision: "precision", iec103.KODFactor: "factor",
	iec103.KODReference: "reference", iec103.KODEnumeration: "enumeration", iec103.KODDimension: "dimension",
	iec103.KODDescription: "description", iec103.KODPassword: "password", iec103.KODReadOnly: "read-only",
	iec103.KODWriteOnly: "write-only", iec103.KODFunInf: "fun-inf", iec103.KODEvent: "event",
	iec103.KODTextArray: "text-array", iec103.KODValueArray: "value-array", iec103.KODRelatedEntries: "related-entries",
}

// KODName returns the name of a kind of description, or "kod-N".
func KODName(k iec103.KOD) string {
	if n, ok := kodNames[k]; ok {
		return n
	}
	return fmt.Sprintf("kod-%d", k)
}

// KODNames lists the names --kod takes.
func KODNames() []string {
	out := make([]string, 0, len(kodNames))
	for _, n := range kodNames {
		out = append(out, n)
	}
	return out
}

var dataTypeNames = map[iec103.DataType]string{
	iec103.DataNone: "none", iec103.DataASCII: "ascii", iec103.DataBitString: "bitstring", iec103.DataUint: "uint",
	iec103.DataInt: "int", iec103.DataUFixed: "ufixed", iec103.DataFixed: "fixed", iec103.DataFloat32: "float32",
	iec103.DataFloat64: "float64", iec103.DataDPI: "dpi", iec103.DataSPI: "spi", iec103.DataDPITransErr: "dpi-te",
	iec103.DataMEA: "mea", iec103.DataTime: "time", iec103.DataGIN: "gin", iec103.DataRelTime: "rel-time",
	iec103.DataFunInf: "fun-inf", iec103.DataTimeTagged: "time-tagged", iec103.DataRelTagged: "rel-time-tagged",
	iec103.DataRelMeasure: "rel-time-measurand", iec103.DataTextNumber: "text-number", iec103.DataGRC: "reply",
	iec103.DataStructure: "structure", iec103.DataIndex: "index",
}

// DataTypeName returns the name of a DATATYPE, or "type-N".
func DataTypeName(t iec103.DataType) string {
	if n, ok := dataTypeNames[t]; ok {
		return n
	}
	return fmt.Sprintf("type-%d", t)
}

var grcNames = map[iec103.GRC]string{
	iec103.GRCAck: "ack", iec103.GRCInvalidGIN: "invalid-gin", iec103.GRCNotExist: "not-exist",
	iec103.GRCNotAvailable: "not-available", iec103.GRCVerifyError: "verify-error", iec103.GRCOutOfRange: "out-of-range",
	iec103.GRCEntryTooLarge: "entry-too-large", iec103.GRCTooManyCommands: "too-many-commands", iec103.GRCReadOnly: "read-only",
	iec103.GRCPasswordProtect: "password-protected", iec103.GRCLocalSetting: "local-setting", iec103.GRCErrorDescription: "error-with-description",
}

// GRCName returns the name of a generic reply code, or "grc-N".
func GRCName(g iec103.GRC) string {
	if n, ok := grcNames[g]; ok {
		return n
	}
	return fmt.Sprintf("grc-%d", g)
}

// maxStructureDepth bounds the nesting of data structures that are rendered.
const maxStructureDepth = 4

// ValuesOf renders the elements of a GID: text, numbers, or nested structure fields; nil
// for a data type whose elements are not rendered (see Data), or a GID that does not hold
// what its GDD says.
func ValuesOf(v iec103.Value) []any {
	return valuesOf(v, 0)
}

func valuesOf(v iec103.Value, depth int) []any {
	var out []any
	switch v.Type {
	case iec103.DataASCII:
		return []any{strings.TrimRight(v.Text(), "\x00")}
	case iec103.DataUint, iec103.DataGRC, iec103.DataIndex, iec103.DataTextNumber, iec103.DataDPI, iec103.DataSPI:
		for i := range int(v.Number) {
			n, err := v.Uint(i)
			if err != nil {
				return nil
			}
			out = append(out, n)
		}
	case iec103.DataInt:
		for i := range int(v.Number) {
			n, err := v.Int(i)
			if err != nil {
				return nil
			}
			out = append(out, n)
		}
	case iec103.DataFloat32, iec103.DataFloat64:
		for i := range int(v.Number) {
			f, err := v.Float(i)
			if err != nil {
				return nil
			}
			out = append(out, f)
		}
	case iec103.DataStructure:
		if depth >= maxStructureDepth {
			return nil
		}
		fields, err := v.Fields()
		if err != nil {
			return nil
		}
		for _, f := range fields {
			out = append(out, map[string]any{"type": DataTypeName(f.Type), "values": valuesOf(f, depth+1), "data": hex.EncodeToString(f.Data)})
		}
	default:
		return nil
	}
	return out
}

func replyOf(v iec103.Value) string {
	if v.Type != iec103.DataGRC || len(v.Data) == 0 {
		return ""
	}
	return GRCName(iec103.GRC(v.Data[0]))
}

// GINText renders a GIN as GROUP.ENTRY.
func GINText(g iec103.GIN) string { return fmt.Sprintf("%d.%d", g.Group, g.Entry) }

// DataSetOf renders a data set.
func DataSetOf(d iec103.DataSet) types.DataSet {
	return types.DataSet{
		GIN: GINText(d.GIN), KOD: uint8(d.KOD), KODName: KODName(d.KOD),
		DataType: uint8(d.Value.Type), TypeName: DataTypeName(d.Value.Type), Size: d.Value.Size, Number: d.Value.Number,
		Values: ValuesOf(d.Value), Reply: replyOf(d.Value), Data: hex.EncodeToString(d.Value.Data),
	}
}

// DescElementOf renders a description element.
func DescElementOf(e iec103.DescElement) types.DescElement {
	return types.DescElement{
		KOD: uint8(e.KOD), KODName: KODName(e.KOD), DataType: uint8(e.Value.Type), TypeName: DataTypeName(e.Value.Type),
		Size: e.Value.Size, Number: e.Value.Number, Values: ValuesOf(e.Value), Reply: replyOf(e.Value), Data: hex.EncodeToString(e.Value.Data),
	}
}

// SOFOf renders a status of fault.
func SOFOf(s iec103.SOF) types.SOF {
	return types.SOF{Raw: uint8(s), TP: s&iec103.SOFTrip != 0, TM: s&iec103.SOFTM != 0, Test: s&iec103.SOFTest != 0, OTEV: s&iec103.SOFOTEV != 0}
}

// FaultOf renders an entry of the list of recorded disturbances.
func FaultOf(f iec103.RecordedFault) types.Fault {
	return types.Fault{FAN: f.FAN, SOF: SOFOf(f.SOF), Time: TimeText(f.Time, true)}
}

// accNames are the actual channels of clause 7.2.6.1.
var accNames = map[uint8]string{
	iec103.ACCGlobal: "global", iec103.ACCIL1: "IL1", iec103.ACCIL2: "IL2", iec103.ACCIL3: "IL3", iec103.ACCIN: "IN",
	iec103.ACCVL1E: "VL1E", iec103.ACCVL2E: "VL2E", iec103.ACCVL3E: "VL3E", iec103.ACCVEN: "VEN",
}

// ACCName returns the name of an actual channel, or "acc-N".
func ACCName(acc uint8) string {
	if n, ok := accNames[acc]; ok {
		return n
	}
	return fmt.Sprintf("acc-%d", acc)
}

// DisturbanceOf renders a transferred record.
func DisturbanceOf(d *iec103.Disturbance) types.Disturbance {
	out := types.Disturbance{
		Fun: uint8(d.Fun), FAN: d.FAN, TOV: d.TOV, NOF: d.NOF, NOC: len(d.Channels), IntervalUs: d.Interval,
		Start: TimeText(d.Start, false), Tags: []types.Tag{}, Channels: []types.Channel{},
	}
	for _, t := range d.Tags {
		out.Tags = append(out.Tags, types.Tag{Pos: t.Pos, Fun: uint8(t.Fun), Inf: uint8(t.Inf), DPI: uint8(t.DPI), State: t.DPI.String()})
	}
	for _, c := range d.Channels {
		vals := c.Values
		if vals == nil {
			vals = []int16{}
		}
		out.Channels = append(out.Channels, types.Channel{ACC: c.ACC, Name: ACCName(c.ACC), RPV: c.RPV, RSV: c.RSV, RFA: c.RFA, Values: vals})
		out.NOE = max(out.NOE, len(c.Values))
	}
	return out
}

// timeAt returns t in loc, for output.
func timeAt(t time.Time, loc *time.Location) string { return t.In(loc).Format(FullTime) }
