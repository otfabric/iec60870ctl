// SPDX-License-Identifier: MIT

package iec

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"

	iec104 "github.com/otfabric/go-iec104"
	"github.com/otfabric/go-iec104/apci"
	"github.com/otfabric/go-iec104/asdu"
	"github.com/otfabric/go-iec104/sec"
	"github.com/otfabric/iec104ctl/internal/config"
	"github.com/otfabric/iec104ctl/internal/errs"
	"github.com/otfabric/iec104ctl/internal/types"
)

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
	r, err := ParseIOAs(" 5, 0x10-18 ,,7 ")
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
	if _, err := ParseIOAs("1-200000"); err == nil || !strings.Contains(err.Error(), "too many") {
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

	f, err := NewFilter("M_SP_NA_1", "1-2")
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
		return &iec104.NegativeError{ASDU: &asdu.ASDU{Type: asdu.C_SC_NA_1, Cause: cause, CommonAddr: 2, Negative: true}}
	}
	timeout := &net.OpError{Op: "dial", Err: context.DeadlineExceeded}
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
		{&iec104.NegativeError{}, errs.KindNegative, errs.CodeNegativeConfirmation, 6},
		{fmt.Errorf("%w: %w", iec104.ErrConnectFailed, errors.New("refused")), errs.KindTransport, errs.CodeTransportConnectFailed, 4},
		{fmt.Errorf("%w: %w", iec104.ErrConnectFailed, timeout), errs.KindTransport, errs.CodeTransportConnectFailed, 4},
		{fmt.Errorf("%w: %w", iec104.ErrConnectFailed, x509.UnknownAuthorityError{}), errs.KindTransport, errs.CodeTransportTLSFailed, 4},
		{fmt.Errorf("x: %w", x509.HostnameError{Certificate: &x509.Certificate{}, Host: "h"}), errs.KindTransport, errs.CodeTransportTLSFailed, 4},
		{tls.RecordHeaderError{Msg: "not TLS"}, errs.KindTransport, errs.CodeTransportTLSFailed, 4},
		{fmt.Errorf("%w: %w", iec104.ErrConnectFailed, sec.ErrCertificate), errs.KindTransport, errs.CodeSecurityFailed, 4},
		{fmt.Errorf("%w: %w", iec104.ErrConnectFailed, sec.ErrProcedure), errs.KindTransport, errs.CodeSecurityFailed, 4},
		{sec.ErrAlgorithm, errs.KindTransport, errs.CodeSecurityFailed, 4},
		{sec.ErrAuthentication, errs.KindTransport, errs.CodeSecurityFailed, 4},
		{iec104.ErrTimeout, errs.KindTimeout, errs.CodeLinkTimeout, 5},
		{fmt.Errorf("waiting: %w", context.DeadlineExceeded), errs.KindTimeout, errs.CodeRequestTimeout, 5},
		{iec104.ErrConnectionLost, errs.KindTransport, errs.CodeConnectionLost, 4},
		{iec104.ErrNotConnected, errs.KindTransport, errs.CodeConnectionLost, 4},
		{iec104.ErrClosed, errs.KindTransport, errs.CodeConnectionLost, 4},
		{iec104.ErrProtocol, errs.KindProtocol, errs.CodeProtocolViolation, 6},
		{iec104.ErrNotStarted, errs.KindProtocol, errs.CodeNotStarted, 6},
		{iec104.ErrBusy, errs.KindProtocol, errs.CodeRequestBusy, 6},
		{iec104.ErrInvalidOption, errs.KindInvalidInput, errs.CodeInvalidInput, 3},
		{asdu.ErrInvalidValue, errs.KindInvalidInput, errs.CodeInvalidInput, 3},
		{&net.OpError{Op: "read", Err: errors.New("reset")}, errs.KindTransport, errs.CodeConnectionLost, 4},
		{context.Canceled, errs.KindInterrupted, errs.CodeContextCanceled, 130},
		{errors.New("surprise"), errs.KindInternal, errs.CodeInternalUnexpected, 10},
	} {
		e, ok := errs.As(WrapError(c.err))
		if !ok || e.Kind != c.kind || e.Code != c.code || errs.ExitCodeForKind(e.Kind) != c.exit {
			t.Errorf("%v: classified as %+v, want %s/%s exit %d", c.err, e, c.kind, c.code, c.exit)
			continue
		}
		if e.Error() == "" || !errors.Is(e, c.err) {
			t.Errorf("%v: the typed error has no message or lost its cause", c.err)
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

	// What ends a connection, and what leaves it up.
	for err, fatal := range map[error]bool{
		nil:                         false,
		neg(asdu.CauseUnknownIOA):   false,
		context.DeadlineExceeded:    false,
		context.Canceled:            true,
		iec104.ErrTimeout:           true,
		iec104.ErrConnectionLost:    true,
		iec104.ErrProtocol:          true,
		errors.New("anything else"): true,
	} {
		if Fatal(err) != fatal {
			t.Errorf("Fatal(%v) = %v", err, !fatal)
		}
	}
}

func TestParameters(t *testing.T) {
	p, err := LinkParams(config.LinkConfig{})
	if err != nil || p != apci.DefaultParams() {
		t.Errorf("default link parameters: %+v, %v", p, err)
	}
	p, err = LinkParams(config.LinkConfig{K: 4, W: 2, T0: 5, T1: 6, T2: 3, T3: 9})
	if err != nil || p.K != 4 || p.W != 2 || p.T0 != 5*time.Second || p.T1 != 6*time.Second || p.T2 != 3*time.Second || p.T3 != 9*time.Second {
		t.Errorf("link parameters: %+v, %v", p, err)
	}
	if _, err := LinkParams(config.LinkConfig{T1: 5, T2: 9}); err == nil {
		t.Error("t2 above t1 was accepted")
	}
	a, err := ASDUParams(config.LinkConfig{CauseSize: 1, CASize: 1, IOASize: 2, TimeZone: "local"})
	if err != nil || a.CauseSize != 1 || a.CommonAddrSize != 1 || a.IOASize != 2 || a.Location != time.Local {
		t.Errorf("ASDU parameters: %+v, %v", a, err)
	}
	if a, _ := ASDUParams(config.LinkConfig{}); a.IOASize != 3 || a.Location != time.UTC {
		t.Errorf("default ASDU parameters: %+v", a)
	}
	if loc, err := Location("Europe/Brussels"); err != nil || loc.String() != "Europe/Brussels" {
		t.Errorf("location: %v, %v", loc, err)
	}
	if Timeout(0) != DefaultTimeout || Timeout(250) != 250*time.Millisecond {
		t.Error("Timeout")
	}
	for ca, broadcast := range map[uint16]bool{0: true, 65535: false} {
		if CheckCommonAddr(ca, broadcast) == nil {
			t.Errorf("common address %d accepted", ca)
		}
	}
	if CheckCommonAddr(65535, true) != nil || CheckCommonAddr(1, false) != nil {
		t.Error("a valid common address was refused")
	}
}

func TestExpandHosts(t *testing.T) {
	for _, c := range []struct {
		specs []string
		want  string
	}{
		{[]string{"192.168.1.0/30"}, "[192.168.1.1 192.168.1.2]"},
		{[]string{"10.0.0.5/31"}, "[10.0.0.4 10.0.0.5]"},
		{[]string{"10.0.0.5/32", "10.0.0.5"}, "[10.0.0.5]"},
		{[]string{"10.0.0.254-255", " rtu.example.net "}, "[10.0.0.254 10.0.0.255 rtu.example.net]"},
	} {
		got, err := ExpandHosts(c.specs, 0)
		if err != nil || fmt.Sprint(got) != c.want {
			t.Errorf("ExpandHosts(%v) = %v, %v; want %s", c.specs, got, err, c.want)
		}
	}
	if _, err := ExpandHosts([]string{"10.0.0.0/24"}, 100); err == nil {
		t.Error("a subnet above the limit was accepted")
	}
	if _, err := ExpandHosts([]string{"10.0.0.1-200", "10.0.1.1-200"}, 300); err == nil {
		t.Error("ranges that add up above the limit were accepted")
	}
	if hosts, err := ExpandHosts([]string{"10.0.0.0/20"}, 0); err != nil || len(hosts) != 4094 {
		t.Errorf("a /20 without limit: %d hosts, %v", len(hosts), err)
	}
}

// A port that accepts connections and does not speak IEC 60870-5-104 is "open", whatever
// it answers; a station that sends something else first is still recognized.
func TestProbe(t *testing.T) {
	serve := func(handle func(net.Conn)) string {
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = ln.Close() })
		go func() {
			for {
				conn, err := ln.Accept()
				if err != nil {
					return
				}
				go func() { defer func() { _ = conn.Close() }(); handle(conn) }()
			}
		}()
		return ln.Addr().String()
	}
	frame := func(f apci.Frame) []byte { b, _ := f.MarshalBinary(); return b }
	read := func(c net.Conn) { _, _ = c.Read(make([]byte, 16)) }

	silent := serve(func(c net.Conn) { read(c); time.Sleep(time.Second) })
	closes := serve(func(net.Conn) {})
	http := serve(func(c net.Conn) { read(c); _, _ = c.Write([]byte("HTTP/1.1 400 Bad Request\r\n\r\n")) })
	chatty := serve(func(c net.Conn) {
		read(c)
		_, _ = c.Write(append(frame(apci.NewU(apci.TestFRAct)), frame(apci.NewU(apci.TestFRCon))...))
	})
	endless := serve(func(c net.Conn) {
		read(c)
		_, _ = c.Write(bytes.Repeat(frame(apci.NewS(0)), 8))
	})
	ctx := context.Background()
	for addr, want := range map[string]string{silent: probeOpen, closes: probeOpen, http: probeOpen, endless: probeOpen, chatty: probeIEC104} {
		if got, _ := probe(ctx, addr, 300*time.Millisecond); got != want {
			t.Errorf("probe of %s: %q, want %q", addr, got, want)
		}
	}

	host, port, _ := net.SplitHostPort(http)
	var progress bytes.Buffer
	res, err := Discover(ctx, &config.DiscoverConfig{Subnets: []string{host}, Ports: []string{port}, Parallel: 2, Timeout: 300, Debug: true}, &progress)
	if err != nil || res.Found != 0 || len(res.Devices) != 1 || res.Devices[0].Status != "open" {
		t.Errorf("discover of a port that is no station: %+v, %v", res, err)
	}
	if !strings.Contains(progress.String(), "open") {
		t.Errorf("with --debug an open port is reported as progress: %q", progress.String())
	}
	if out, _ := res.MarshalTextOutput(); !strings.Contains(out, "0 IEC 60870-5-104 stations among 1 hosts") {
		t.Errorf("text: %s", out)
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := Discover(cancelled, &config.DiscoverConfig{Subnets: []string{"127.0.0.1-9"}, Ports: []string{port}, Parallel: 1}, nil); err == nil {
		t.Error("an interrupted discovery returned a result")
	}
}

func TestTracerAndSections(t *testing.T) {
	var buf bytes.Buffer
	tr := &Tracer{W: &buf, Params: asdu.IEC104}
	addr := &net.TCPAddr{IP: net.IPv4(10, 0, 0, 1), Port: 2404}
	raw, _ := asdu.New(asdu.CauseSpontaneous, 1, asdu.SinglePoint{IOA: 5, Value: true}).Encode(asdu.IEC104)
	tr.OnConnect(addr)
	tr.OnFrameSent(addr, apci.NewU(apci.StartDTAct), 6)
	tr.OnFrameReceived(addr, apci.NewI(1, 2, raw), 16)
	tr.OnFrameReceived(addr, apci.NewI(1, 2, []byte{1}), 7)
	tr.OnDisconnect(addr, errors.New("gone"))
	tr.OnDisconnect(nil, nil)
	out := buf.String()
	for _, want := range []string{"-- 10.0.0.1:2404 connected", "-> 10.0.0.1:2404 U STARTDT act", "<- 10.0.0.1:2404 I N(S)=1 N(R)=2", "M_SP_NA_1 spontaneous ca=1",
		"[01 01 03 00 01 00 05 00 00 01]", "undecodable ASDU", "disconnected: gone"} {
		if !strings.Contains(out, want) {
			t.Errorf("trace lacks %q:\n%s", want, out)
		}
	}
	if strings.Count(out, "\n") != 6 {
		t.Errorf("%d lines, want 6:\n%s", strings.Count(out, "\n"), out)
	}

	data := []byte("abcdefghij")
	for size, want := range map[int]int{0: 1, 3: 4, 5: 2, 10: 1, 99: 1} {
		parts := sections(data, size)
		if len(parts) != want || !bytes.Equal(bytes.Join(parts, nil), data) {
			t.Errorf("sections of %d octets: %d parts", size, len(parts))
		}
	}
}
