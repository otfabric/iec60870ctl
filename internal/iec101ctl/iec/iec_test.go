// SPDX-License-Identifier: MIT

package iec

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/otfabric/go-iec60870-5/asdu"
	"github.com/otfabric/go-iec60870-5/ft12"
	"github.com/otfabric/go-iec60870-5/iec101"
	"github.com/otfabric/go-iec60870-5/link"
	"github.com/otfabric/go-iec60870-5/telecontrol"
	"github.com/otfabric/iec60870ctl/internal/errs"
	"github.com/otfabric/iec60870ctl/internal/iec101ctl/config"
	"github.com/otfabric/iec60870ctl/internal/iec101ctl/types"
)

// newFilter3 and parseIOAs3 are the parsers with three-octet addresses.
func newFilter3(typeList, ioaList string) (Filter, error) {
	return NewFilter(typeList, ioaList, MaxIOA)
}
func parseIOAs3(s string) (Ranges, error) { return ParseIOAs(s, MaxIOA) }

func TestPointsOfEveryType(t *testing.T) {
	at := asdu.At(time.Date(2026, 1, 31, 12, 0, 0, 500e6, time.UTC))
	for _, c := range []struct {
		obj     asdu.InformationObject
		value   any
		text    string
		quality string
	}{
		{asdu.SinglePoint{IOA: 1, Value: true, Quality: asdu.QualityInvalid}, true, "on", "IV"},
		{asdu.DoublePoint{IOA: 1, Value: asdu.DoubleOff}, "off", "off", "good"},
		{asdu.StepPosition{IOA: 1, Value: -5, Transient: true}, -5, "-5 (transient)", "good"},
		{asdu.Bitstring32{IOA: 1, Value: 255}, uint32(255), "255", "good"},
		{asdu.MeasuredNormalized{IOA: 1, Value: 16384}, 0.5, "0.5 (raw=16384)", "good"},
		{asdu.MeasuredScaled{IOA: 1, Value: -7, Quality: asdu.QualityOverflow}, -7, "-7", "OV"},
		{asdu.MeasuredFloat{IOA: 1, Value: 1.5}, float32(1.5), "1.5", "good"},
		{asdu.IntegratedTotal{IOA: 1, Value: 9, Sequence: 3, Carry: true, Invalid: true}, 9, "9 (carry sequence=3)", "IV"},
		{asdu.IntegratedTotal{IOA: 1, Value: 9, Adjusted: true}, 9, "9 (adjusted sequence=0)", "good"},
		{asdu.ProtectionEvent{IOA: 1, State: asdu.DoubleOn, Elapsed: 40, Time: at}, "on", "on (elapsed_ms=40)", "good"},
		{asdu.ProtectionStart{IOA: 1, Events: 3, Duration: 20, Time: at}, 3, "3 (duration_ms=20)", "good"},
		{asdu.ProtectionOutput{IOA: 1, Circuits: 1, Operating: 10, Time: at}, 1, "1 (operating_ms=10)", "good"},
		{asdu.PackedSinglePoints{IOA: 1, Status: 5, Changed: 1}, 5, "5 (changed=1)", "good"},
		{asdu.EndOfInitialization{Cause: 2, LocalChange: true}, 2, "2 (local_change)", ""},
		{asdu.SingleCommand{IOA: 1, Value: true, Select: true, Qualifier: asdu.QualifierLongPulse}, true, "on (qualifier=long select)", ""},
		{asdu.DoubleCommand{IOA: 1, Value: asdu.DoubleOn, Qualifier: asdu.QualifierPersistent}, "on", "on (qualifier=persistent)", ""},
		{asdu.StepCommand{IOA: 1, Value: asdu.StepLower, Qualifier: asdu.QualifierShortPulse}, "lower", "lower (qualifier=short)", ""},
		{asdu.StepCommand{IOA: 1, Value: 0, Qualifier: 9}, "not-permitted", "not-permitted (qualifier=reserved)", ""},
		{asdu.SetpointNormalized{IOA: 1, Value: -16384}, -0.5, "-0.5 (qualifier=0 raw=-16384)", ""},
		{asdu.SetpointScaled{IOA: 1, Value: 12, Qualifier: 4}, 12, "12 (qualifier=4)", ""},
		{asdu.SetpointFloat{IOA: 1, Value: 2.5}, float32(2.5), "2.5 (qualifier=0)", ""},
		{asdu.BitstringCommand{IOA: 1, Value: 7}, uint32(7), "7", ""},
		{asdu.Interrogation{Qualifier: asdu.QOIStation}, 20, "20", ""},
		{asdu.CounterInterrogation{Request: 5, Freeze: 1}, 5, "5 (freeze=1)", ""},
		{asdu.TestCommand{Counter: 9, Time: at}, 9, "9", ""},
		{asdu.ResetProcess{Qualifier: 1}, 1, "1", ""},
		{asdu.DelayAcquisition{Delay: 30}, 30, "30", ""},
		{asdu.ParameterNormalized{IOA: 1, Value: 16384, Qualifier: 1}, 0.5, "0.5 (qualifier=1)", ""},
		{asdu.ParameterScaled{IOA: 1, Value: 3, Qualifier: 2}, 3, "3 (qualifier=2)", ""},
		{asdu.ParameterFloat{IOA: 1, Value: 0.5, Qualifier: 3}, float32(0.5), "0.5 (qualifier=3)", ""},
		{asdu.ParameterActivation{IOA: 1, Qualifier: 3}, 3, "3", ""},
		{asdu.FileDirectoryEntry{IOA: 1, Name: 2, Length: 100, Time: at}, 100, "100 (name=2)", ""},
		{asdu.Read{IOA: 1}, nil, "-", ""},
	} {
		a := asdu.New(asdu.CauseSpontaneous, 3, c.obj)
		pts := Points(a)
		if len(pts) != 1 {
			t.Fatalf("%T: %d points", c.obj, len(pts))
		}
		p := pts[0]
		if p.Value != c.value || p.ValueText() != c.text || p.Quality != c.quality || p.CommonAddr != 3 || p.Type != a.Type.String() {
			t.Errorf("%T: value %v (%T), text %q, quality %q; want %v, %q, %q", c.obj, p.Value, p.Value, p.ValueText(), p.Quality, c.value, c.text, c.quality)
		}
		if a.Type.HasTimeTag() != (p.Timestamp != "") {
			t.Errorf("%s: time tag %q", a.Type, p.Timestamp)
		}
	}

	// Time tags, with their flags, and the P/N and T bits.
	stamped := at
	stamped.Substituted, stamped.SummerTime, stamped.Invalid = true, true, true
	a := asdu.New(asdu.CauseSpontaneous, 1, asdu.SinglePoint{IOA: 5, Time: stamped})
	a.Negative, a.Test, a.Originator = true, true, 9
	p := Points(a)[0]
	if p.Timestamp != "2026-01-31T12:00:00.500Z" || !p.TimestampInvalid || p.Attributes["time_substituted"] != true || p.Attributes["summer_time"] != true {
		t.Errorf("time tag: %+v", p)
	}
	line := p.Line()
	for _, want := range []string{"M_SP_TB_1", "ca=1", "ioa=5", "spontaneous [negative,test]", "@ 2026-01-31T12:00:00.500Z (invalid)"} {
		if !strings.Contains(line, want) {
			t.Errorf("line %q lacks %q", line, want)
		}
	}
	if row := p.Row(); row[5] != "spontaneous [negative,test]" || row[6] != "2026-01-31T12:00:00.500Z (invalid)" {
		t.Errorf("row %v", row)
	}
	// A time tag that is no real date.
	zero := asdu.New(asdu.CauseSpontaneous, 1, asdu.SinglePoint{IOA: 5, Time: asdu.Timestamp{Invalid: true}})
	zero.Type = asdu.M_SP_TB_1
	if p := Points(zero)[0]; !p.TimestampInvalid || !strings.HasPrefix(p.Timestamp, "0001-01-01") {
		t.Errorf("invalid time tag: %+v", p)
	}
	// M_ME_ND_1 has no quality descriptor; a type without object model is still reported.
	nd := asdu.New(asdu.CausePeriodic, 1, asdu.MeasuredNormalized{IOA: 1})
	nd.Type = asdu.M_ME_ND_1
	if p := Points(nd)[0]; p.Quality != "" || p.Row()[4] != "-" {
		t.Errorf("M_ME_ND_1 with a quality: %+v", p)
	}
	raw := &asdu.ASDU{Type: 200, Cause: asdu.CauseSpontaneous, CommonAddr: 4, Raw: []byte{1, 2, 3}}
	if pts := Points(raw); len(pts) != 1 || pts[0].Attributes["raw_octets"] != 3 || pts[0].ValueText() != "- (raw_octets=3)" {
		t.Errorf("raw ASDU: %+v", pts)
	}
	if Points(nil) != nil {
		t.Error("points of a nil ASDU")
	}
	if got := (types.Point{Value: 0.1234567891}).ValueText(); got != "0.123457" {
		t.Errorf("float text %q", got)
	}
}

func TestRangesAndFilters(t *testing.T) {
	r, err := parseIOAs3(" 5, 0x10-18 ,,7 ")
	if err != nil || r.Count() != 5 || !r.Contains(17) || r.Contains(19) {
		t.Fatalf("ranges %v, %v", r, err)
	}
	var got []uint32
	r.Each(func(n uint32) bool { got = append(got, n); return n != 17 })
	if fmt.Sprint(got) != "[5 16 17]" {
		t.Errorf("Each visited %v", got)
	}
	// The whole 24-bit space ends, and does not wrap around.
	n := 0
	Ranges{{MaxIOA - 1, MaxIOA}}.Each(func(uint32) bool { n++; return true })
	if n != 2 {
		t.Errorf("a range that ends at the maximum visited %d addresses", n)
	}
	if !(Ranges{}).Contains(1) {
		t.Error("an empty set contains everything")
	}
	if _, err := parseIOAs3("1-200000"); err == nil || !strings.Contains(err.Error(), "too many") {
		t.Errorf("a huge list: %v", err)
	}

	for in, want := range map[string]asdu.TypeID{"m_sp_na_1": asdu.M_SP_NA_1, " 13 ": asdu.M_ME_NC_1, "C_SC_TA_1": asdu.C_SC_TA_1, "200": 200} {
		if got, err := ParseType(in); err != nil || got != want {
			t.Errorf("ParseType(%q) = %v, %v", in, got, err)
		}
	}
	for _, in := range []string{"", "0", "256", "TypeID(200)", "M_SP"} {
		if _, err := ParseType(in); err == nil {
			t.Errorf("ParseType(%q) succeeded", in)
		}
	}
	if names := TypeNames(); names[0] != "M_SP_NA_1" || len(names) < 60 {
		t.Errorf("type names: %d, first %q", len(names), names[0])
	}

	f, err := newFilter3("M_SP_NA_1", "1-2")
	if err != nil {
		t.Fatal(err)
	}
	f.CommonAddr, f.HasCommonAddr = 4, true
	pts := []types.Point{
		{CommonAddr: 4, IOA: 1, TypeID: 1}, {CommonAddr: 5, IOA: 1, TypeID: 1}, {CommonAddr: 4, IOA: 3, TypeID: 1}, {CommonAddr: 4, IOA: 2, TypeID: 3},
	}
	if kept := f.Apply(pts); len(kept) != 1 || kept[0].IOA != 1 || kept[0].CommonAddr != 4 {
		t.Errorf("filtered: %+v", kept)
	}
	if kept := (Filter{}).Apply(pts); len(kept) != 4 {
		t.Errorf("the zero filter dropped points: %d", len(kept))
	}
}

func TestErrorClassification(t *testing.T) {
	neg := func(cause asdu.Cause) error {
		return &telecontrol.NegativeError{ASDU: &asdu.ASDU{Type: asdu.C_SC_NA_1, Cause: cause, CommonAddr: 2, Negative: true}}
	}
	for _, c := range []struct {
		err  error
		kind errs.Kind
		code string
		exit int
	}{
		{neg(asdu.CauseActivationCon), errs.KindNegative, errs.CodeNegativeConfirmation, 6},
		{neg(asdu.CauseUnknownType), errs.KindNegative, errs.CodeUnknownType, 6},
		{neg(asdu.CauseUnknownCause), errs.KindNegative, errs.CodeUnknownCause, 6},
		{neg(asdu.CauseUnknownCommonAddr), errs.KindNegative, errs.CodeUnknownCommonAddr, 6},
		{neg(asdu.CauseUnknownIOA), errs.KindNegative, errs.CodeUnknownIOA, 6},
		{&telecontrol.NegativeError{}, errs.KindNegative, errs.CodeNegativeConfirmation, 6},
		{context.Canceled, errs.KindInterrupted, errs.CodeContextCanceled, 130},
		{&iec101.ProfileError{Reason: "x"}, errs.KindInvalidInput, errs.CodeProfileViolation, 3},
		{fmt.Errorf("x: %w", link.ErrTimeout), errs.KindTimeout, errs.CodeLinkTimeout, 5},
		{link.ErrNack, errs.KindProtocol, errs.CodeLinkRefused, 6},
		{link.ErrDataFlow, errs.KindProtocol, errs.CodeLinkRefused, 6},
		{link.ErrNotFunctioning, errs.KindProtocol, errs.CodeLinkRefused, 6},
		{link.ErrNotImplemented, errs.KindProtocol, errs.CodeLinkRefused, 6},
		{link.ErrUnexpected, errs.KindProtocol, errs.CodeProtocolViolation, 6},
		{iec101.ErrLinkDown, errs.KindTransport, errs.CodeLinkDown, 4},
		{fmt.Errorf("waiting: %w", context.DeadlineExceeded), errs.KindTimeout, errs.CodeRequestTimeout, 5},
		{telecontrol.ErrBusy, errs.KindProtocol, errs.CodeRequestBusy, 6},
		{iec101.ErrClosed, errs.KindTransport, errs.CodeConnectionLost, 4},
		{link.ErrClosed, errs.KindTransport, errs.CodeConnectionLost, 4},
		{io.EOF, errs.KindTransport, errs.CodeConnectionLost, 4},
		{net.ErrClosed, errs.KindTransport, errs.CodeConnectionLost, 4},
		{iec101.ErrInvalidConfig, errs.KindInvalidInput, errs.CodeInvalidInput, 3},
		{asdu.ErrInvalidValue, errs.KindInvalidInput, errs.CodeInvalidInput, 3},
		{&net.OpError{Op: "read", Err: errors.New("reset")}, errs.KindTransport, errs.CodeConnectionLost, 4},
		{errors.New("surprise"), errs.KindInternal, errs.CodeInternalUnexpected, 10},
	} {
		e, ok := errs.As(WrapError(c.err))
		if !ok || e.Kind != c.kind || e.Code != c.code || errs.ExitCodeForKind(e.Kind) != c.exit {
			t.Errorf("%v: classified as %+v, want %s/%s exit %d", c.err, e, c.kind, c.code, c.exit)
			continue
		}
		if info := EmbeddedErrorInfo(c.err); info.Kind != string(c.kind) || info.Code != c.code || info.Message == "" {
			t.Errorf("%v: embedded as %+v", c.err, info)
		}
	}
	if WrapError(nil) != nil || EmbeddedErrorInfo(nil) != nil {
		t.Error("nil is not an error")
	}
	typed := errs.InvalidInput(errs.CodeInvalidURL, "bad", nil)
	if WrapError(typed) != error(typed) {
		t.Error("a typed error was wrapped again")
	}
	if e, _ := errs.As(connectError(context.Canceled)); e.Kind != errs.KindInterrupted {
		t.Errorf("an interrupted connect: %+v", e)
	}
	for err, fatal := range map[error]bool{
		nil:                       false,
		neg(asdu.CauseUnknownIOA): false,
		context.DeadlineExceeded:  false,
		context.Canceled:          true,
		iec101.ErrLinkDown:        true,
		iec101.ErrClosed:          true,
		errors.New("other"):       true,
	} {
		if Fatal(err) != fatal {
			t.Errorf("Fatal(%v) = %v", err, !fatal)
		}
	}
}

func TestLinkParameters(t *testing.T) {
	ok := []config.LinkConfig{
		config.DefaultLink(),
		{Mode: "balanced", LinkAddrSize: 0, CauseSize: 2, CASize: 2, IOASize: 3, LinkAddr: 77, RemoteLinkAddr: "0x10"},
		{Mode: "BALANCED", LinkAddrSize: 2, CauseSize: 1, CASize: 1, IOASize: 1, LinkAddr: 65534, RemoteLinkAddr: "3", LinkTimeout: 50, LinkRetries: 1, Strict: true, TimeZone: "local"},
	}
	for _, c := range ok {
		l, err := LinkParams(c)
		if err != nil {
			t.Fatalf("%+v: %v", c, err)
		}
		if c.LinkAddrSize == 0 && (l.Addr != 0 || l.Remote != 0) {
			t.Errorf("no address field, but addresses %d/%d", l.Addr, l.Remote)
		}
	}
	l, _ := LinkParams(ok[2])
	if l.Remote != 3 || l.Params.Link.Timeout != 50*time.Millisecond || l.Params.Link.Retries != 1 || !l.Params.Strict {
		t.Errorf("parameters %+v", l)
	}
	if v := MaxIOAFor(1); v != 255 {
		t.Errorf("MaxIOAFor(1) = %d", v)
	}
	if v := MaxIOAFor(3); v != MaxIOA {
		t.Errorf("MaxIOAFor(3) = %d", v)
	}
	if v := BroadcastCA(2); v != 65535 {
		t.Errorf("BroadcastCA(2) = %d", v)
	}
	for _, s := range []string{"", "0", "1"} {
		if _, err := ParseLinkAddr(s, 0, 0); err != nil {
			t.Errorf("no address field: %q: %v", s, err)
		}
	}
	if _, err := ParseLinkAddr("65535", 2, 0); err == nil {
		t.Error("the broadcast address of two octets")
	}
	if _, err := ParseLinkAddr("x", 1, 0); err == nil {
		t.Error("not a number")
	}
	bad := config.DefaultLink()
	bad.LinkAddr = 300
	if _, err := LinkParams(bad); err == nil {
		t.Error("link address 300 in one octet")
	}
	bad = config.DefaultLink()
	bad.LinkAddrSize, bad.LinkAddr = 2, 65535
	if _, err := LinkParams(bad); err == nil {
		t.Error("link address 65535 in two octets")
	}
	bad = config.DefaultLink()
	bad.Mode, bad.RemoteLinkAddr = "balanced", "255"
	if _, err := LinkParams(bad); err == nil {
		t.Error("remote link address 255 in one octet")
	}
	if err := CheckCommonAddr(70000&0xFFFF, 1, true); err == nil {
		t.Error("common address beyond one octet")
	}
}

func TestTracer(t *testing.T) {
	var buf bytes.Buffer
	tr := NewTracer(&buf, iec101.DefaultParams())
	tr.now = func() time.Time { return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC) }
	raw, _ := asdu.New(asdu.CauseSpontaneous, 1, asdu.SinglePoint{IOA: 5, Value: true}).Encode(iec101.DefaultParams().ASDU)
	tr.Frame(true, ft12.Frame{Kind: ft12.KindFixed, Control: byte(link.PrimaryControl(link.FuncRequestStatus, false, false, false)), Addr: 1})
	tr.Frame(false, ft12.Frame{Kind: ft12.KindVariable, Control: byte(link.SecondaryControl(link.FuncUserData, true, false, false)), Addr: 1, Data: raw})
	tr.Frame(false, ft12.Frame{Kind: ft12.KindVariable, Addr: 1, Data: []byte{1}})
	tr.Frame(false, ft12.Frame{Kind: ft12.KindSingle})
	tr.Discard([]byte{0xAA}, errors.New("checksum"))
	out := buf.String()
	for _, want := range []string{"debug: 00:00:00.000 -> PRM fn=9", "<- ACD fn=8", "M_SP_NA_1 spontaneous ca=1", "undecodable ASDU", "!! discarded 1 octets: checksum  [aa]"} {
		if !strings.Contains(out, want) {
			t.Errorf("trace lacks %q:\n%s", want, out)
		}
	}
	var cfg link.Config
	tr.Attach(&cfg)
	if cfg.OnFrame == nil || cfg.OnDiscard == nil {
		t.Error("not attached")
	}
	discardUser{}.Reset(true)
}

func TestTerminationWait(t *testing.T) {
	term := newTerminations(asdu.C_SC_NA_1, 1, 5)
	term.HandleASDU(asdu.New(asdu.CauseActivationTerm, 2, asdu.SingleCommand{IOA: 5}))
	term.HandleASDU(&asdu.ASDU{Type: asdu.C_SC_NA_1, Cause: asdu.CauseActivationTerm, CommonAddr: 1})
	if term.wait(context.Background(), time.Millisecond) {
		t.Fatal("the termination of another station counted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if term.wait(ctx, time.Hour) {
		t.Fatal("no termination, but wait succeeded")
	}
	term.HandleASDU(asdu.New(asdu.CauseActivationTerm, 1, asdu.SingleCommand{IOA: 5}))
	if !term.wait(context.Background(), time.Hour) {
		t.Fatal("the termination did not count")
	}
}

func TestTypesListOnlyIEC101(t *testing.T) {
	res := Types(&config.TypesConfig{Direction: "monitor"})
	for _, ti := range res.Types {
		if ti.Direction != "monitor" || !iec101.Defined101(asdu.TypeID(ti.ID)) {
			t.Errorf("%+v", ti)
		}
	}
	_ = types.TypesResult{}
}
