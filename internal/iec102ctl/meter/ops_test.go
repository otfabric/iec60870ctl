// SPDX-License-Identifier: MIT

package meter_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/otfabric/go-iec60870-5/iec102"
	"github.com/otfabric/iec60870ctl/internal/errs"
	"github.com/otfabric/iec60870ctl/internal/iec102ctl/config"
	"github.com/otfabric/iec60870ctl/internal/iec102ctl/meter"
	"github.com/otfabric/iec60870ctl/internal/iec102ctl/stationtest"
	"github.com/otfabric/iec60870ctl/internal/iec102ctl/types"
)

func conn(s *stationtest.Station) config.ConnConfig {
	c := config.Defaults()
	c.URL = s.URL()
	c.Timeout = 5000
	return c
}

func ctx(t *testing.T) context.Context {
	t.Helper()
	c, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	t.Cleanup(cancel)
	return c
}

// wantCode fails unless err is a typed error with the code.
func wantCode(t *testing.T, err error, kind errs.Kind, code string) {
	t.Helper()
	e, ok := errs.As(err)
	if !ok || e.Kind != kind || e.Code != code {
		t.Fatalf("error %v (%+v), want %s/%s", err, e, kind, code)
	}
}

func TestInfoAndTime(t *testing.T) {
	s := stationtest.Start(t)
	ic := config.InfoConfig{ConnConfig: conn(s)}
	if err := meter.CheckInfo(&ic); err != nil {
		t.Fatal(err)
	}
	trace := &lockedBuffer{}
	ic.Debug = true
	info, err := meter.Info(ctx(t), &ic, trace)
	if err != nil || info.Standard != 42 || info.Manufacturer != 33 || info.Product != 20261115 || info.Target != s.URL() {
		t.Fatalf("info %+v %v", info, err)
	}
	if !strings.Contains(trace.String(), "-> fixed C=49") || !strings.Contains(trace.String(), "P_MP_NA_2") {
		t.Errorf("trace:\n%s", trace.String())
	}
	tc := config.TimeConfig{ConnConfig: conn(s)}
	if err := meter.CheckTime(&tc); err != nil {
		t.Fatal(err)
	}
	tr, err := meter.Time(ctx(t), &tc, nil)
	if err != nil || tr.Time.Time != "2026-01-15T10:20:30.500" || tr.Timestamp != "2026-01-15T10:20:30.500Z" || tr.DriftMs == nil || *tr.DriftMs > 0 {
		t.Fatalf("time %+v %v", tr, err)
	}
}

func TestTotals(t *testing.T) {
	s := stationtest.Start(t)
	cfg := config.TotalsConfig{ConnConfig: conn(s), Record: 11, First: 1, Last: 8, From: "2026-01-01T01:00", To: "2026-01-01T03:00"}
	if err := meter.CheckTotals(&cfg); err != nil {
		t.Fatal(err)
	}
	r, err := meter.Totals(ctx(t), &cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	if r.Request != "C_CI_NT_2" || r.RequestID != 122 || r.Kind != "operational" || r.Count != 3 || r.From != "2026-01-01T01:00" || *r.First != 1 || *r.Last != 8 {
		t.Fatalf("result %+v", r)
	}
	want := []types.Total{{Address: 1, Value: 150, Quality: 16, Flags: "SEQ=16"}, {Address: 2, Value: 6, Flags: "good"}, {Address: 3, Value: 9, Flags: "good"}}
	if p := r.Periods[1]; p.Type != "M_IT_TG_2" || p.End.Time != "2026-01-01T02:00" || !reflect.DeepEqual(p.Totals, want) {
		t.Fatalf("period %+v", p)
	}
	// Incremental, REE quality bits, an address range, in a session.
	cfg = config.TotalsConfig{ConnConfig: conn(s), Record: 11, Kind: "incremental", First: 1, Last: 1, From: "2026-01-01", To: "2026-01-02"}
	cfg.AccessKey = "7"
	r, err = meter.Totals(ctx(t), &cfg, nil)
	if err != nil || r.Count != 2 || len(r.Periods[0].Totals) != 1 || r.Session == nil || !r.Session.Login || !r.Session.Logout || meter.SessionPartial(r.Meter) {
		t.Fatalf("incremental %+v %v", r, err)
	}
	// The oldest period, a past period, signatures.
	cfg = config.TotalsConfig{ConnConfig: conn(s), Record: 1, Kind: "accounting", Oldest: true}
	r, err = meter.Totals(ctx(t), &cfg, nil)
	if err != nil || r.Count != 1 || r.Periods[0].Totals[0].Signature == nil || *r.Periods[0].Totals[0].Signature != 170 || r.First != nil {
		t.Fatalf("oldest %+v %v", r, err)
	}
	cfg = config.TotalsConfig{ConnConfig: conn(s), Record: 11, Period: "2026-01-01T02:00", First: 2, Last: 3, AddressRange: true}
	r, err = meter.Totals(ctx(t), &cfg, nil)
	if err != nil || r.Request != "C_CI_NM_2" || r.From != "2026-01-01T02:00" || r.To != "" || len(r.Periods[0].Totals) != 2 {
		t.Fatalf("period range %+v %v", r, err)
	}
	cfg.AddressRange = false
	if r, err = meter.Totals(ctx(t), &cfg, nil); err != nil || r.Request != "C_CI_NL_2" || len(r.Periods[0].Totals) != 3 {
		t.Fatalf("period %+v %v", r, err)
	}
	// Refusals.
	for _, c := range []struct {
		edit func(*config.TotalsConfig)
		code string
	}{
		{func(c *config.TotalsConfig) { c.From, c.To = "2027-01-01", "2027-01-02" }, errs.CodeNoPeriod},
		{func(c *config.TotalsConfig) { c.Record = 12 }, errs.CodeUnknownRecordAddr},
		{func(c *config.TotalsConfig) { c.First, c.Last = 7, 8 }, errs.CodeNoObject},
		{func(c *config.TotalsConfig) { c.Kind = "accounting" }, errs.CodeUnknownType},
		{func(c *config.TotalsConfig) { c.CommonAddress = 2 }, errs.CodeUnknownRecordAddr},
	} {
		cfg := config.TotalsConfig{ConnConfig: conn(s), Record: 11, First: 1, Last: 8, From: "2026-01-01", To: "2026-01-02"}
		c.edit(&cfg)
		_, err := meter.Totals(ctx(t), &cfg, nil)
		wantCode(t, err, errs.KindNegative, c.code)
		if !strings.Contains(err.Error(), "the meter refused C_CI_") {
			t.Errorf("message %q", err)
		}
	}
	bad := config.TotalsConfig{ConnConfig: conn(s)}
	if _, err := meter.Totals(ctx(t), &bad, nil); err == nil {
		t.Error("totals without a selection")
	}
	if err := meter.CheckTotals(&bad); err == nil {
		t.Error("check without a selection")
	}
	bad.URL = ""
	if err := meter.CheckTotals(&bad); err == nil {
		t.Error("check without a line")
	}
}

func TestEvents(t *testing.T) {
	s := stationtest.Start(t)
	cfg := config.EventsConfig{ConnConfig: conn(s), Record: 52}
	if err := meter.CheckEvents(&cfg); err != nil {
		t.Fatal(err)
	}
	r, err := meter.Events(ctx(t), &cfg, nil)
	if err != nil || r.Count != 2 || r.Request != "C_SP_NA_2" || r.Events[1] != (types.Event{Address: 1, On: true, Qualifier: 3, Time: types.Time{Time: "2026-01-02T03:14:02.500"}}) {
		t.Fatalf("events %+v %v", r, err)
	}
	cfg.From, cfg.To = "2026-01-02", "2026-01-03"
	r, err = meter.Events(ctx(t), &cfg, nil)
	if err != nil || r.Count != 1 || r.Request != "C_SP_NB_2" || r.From != "2026-01-02T00:00" {
		t.Fatalf("range %+v %v", r, err)
	}
	cfg.Record = 53
	_, err = meter.Events(ctx(t), &cfg, nil)
	wantCode(t, err, errs.KindNegative, errs.CodeUnknownRecordAddr)
	for _, edit := range []func(*config.EventsConfig){
		func(c *config.EventsConfig) { c.To = "" },
		func(c *config.EventsConfig) { c.From = "x" },
		func(c *config.EventsConfig) { c.To = "x" },
		func(c *config.EventsConfig) { c.From, c.To = "2026-01-03", "2026-01-02" },
		func(c *config.EventsConfig) { c.TimeZone = "Mars/Base" },
		func(c *config.EventsConfig) { c.URL = "" },
	} {
		c := config.EventsConfig{ConnConfig: conn(s), From: "2026-01-01", To: "2026-01-02"}
		edit(&c)
		if err := meter.CheckEvents(&c); err == nil {
			t.Errorf("accepted %+v", c)
		}
		if _, err := meter.Events(ctx(t), &c, nil); err == nil {
			t.Errorf("ran %+v", c)
		}
	}
}

func TestRequest(t *testing.T) {
	s := stationtest.Start(t)
	cfg := config.RequestConfig{ConnConfig: conn(s), Type: "C_TI_NA_2", Cause: "request", Objects: -1}
	if err := meter.CheckRequest(&cfg); err != nil {
		t.Fatal(err)
	}
	r, err := meter.Request(ctx(t), &cfg, nil)
	if err != nil || r.Until != "reply" || r.Count != 1 || r.Answers[0].Type != "M_TI_TA_2" || r.Request.Data != "" || r.Request.Objects != 0 {
		t.Fatalf("request %+v %v", r, err)
	}
	// C_CI_NT_2 written out, completed by its termination: confirmation, three periods, termination.
	from, _ := meter.ParseTimeA("2026-01-01T01:00", time.UTC)
	to, _ := meter.ParseTimeA("2026-01-01T03:00", time.UTC)
	body, _ := iec102.Read{T: iec102.C_CI_NT_2, First: 1, Last: 8, From: from, To: to}.AppendTo(nil, iec102.Params{})
	cfg = config.RequestConfig{ConnConfig: conn(s), Type: "122", Record: 11, Data: string(hexOf(body)), Objects: -1, Until: "auto"}
	r, err = meter.Request(ctx(t), &cfg, nil)
	if err != nil || r.Until != "termination" || r.Count != 5 || r.Answers[0].Cot != 7 || r.Answers[4].Cot != 10 || r.Request.Objects != 1 {
		t.Fatalf("written out %+v %v", r, err)
	}
	// A private type the meter does not know is refused with cause 14.
	cfg = config.RequestConfig{ConnConfig: conn(s), Type: "200", Data: "01", Objects: 5, Until: "confirmation"}
	_, err = meter.Request(ctx(t), &cfg, nil)
	wantCode(t, err, errs.KindNegative, errs.CodeUnknownType)
	for _, edit := range []func(*config.RequestConfig){
		func(c *config.RequestConfig) { c.Type = "C_XX" },
		func(c *config.RequestConfig) { c.Cause = "x" },
		func(c *config.RequestConfig) { c.Data = "zz" },
		func(c *config.RequestConfig) { c.Objects = 128 },
		func(c *config.RequestConfig) { c.Until = "x" },
		func(c *config.RequestConfig) { c.CASize = 1; c.CommonAddress = 300 },
		func(c *config.RequestConfig) { c.Data = strings.Repeat("00", 300) },
		func(c *config.RequestConfig) { c.URL = "" },
	} {
		c := config.RequestConfig{ConnConfig: conn(s), Type: "C_TI_NA_2", Objects: -1}
		edit(&c)
		if err := meter.CheckRequest(&c); err == nil {
			t.Errorf("accepted %+v", c)
		}
		if _, err := meter.Request(ctx(t), &c, nil); err == nil {
			t.Errorf("ran %+v", c)
		}
	}
}

func TestREE(t *testing.T) {
	s := stationtest.Start(t)
	lc := config.SessionOnlyConfig{ConnConfig: conn(s)}
	if err := meter.CheckLogin(&lc); err == nil {
		t.Error("a login without a key was accepted")
	}
	lc.AccessKey = "7"
	if err := meter.CheckLogin(&lc); err != nil {
		t.Fatal(err)
	}
	lr, err := meter.Login(ctx(t), &lc, nil)
	if err != nil || !lr.Session.Login || !lr.Session.Logout {
		t.Fatalf("login %+v %v", lr, err)
	}
	lc.AccessKey = "8"
	_, err = meter.Login(ctx(t), &lc, nil)
	wantCode(t, err, errs.KindNegative, errs.CodeLoginRefused)
	lc.URL = ""
	if err := meter.CheckLogin(&lc); err == nil {
		t.Error("a login without a line was accepted")
	}

	tc := config.TariffsConfig{ConnConfig: conn(s), Register: 134}
	if err := meter.CheckTariffs(&tc); err != nil {
		t.Fatal(err)
	}
	tr, err := meter.Tariffs(ctx(t), &tc, nil)
	if err != nil || tr.Request != "C_TA_VC_2" || tr.Count != 1 || tr.Records[0].ActiveAbs != 2160 || tr.Records[0].PeriodEnd.Time != "2026-01-15T10:00" {
		t.Fatalf("current %+v %v", tr, err)
	}
	tc.From, tc.To = "2025-11-15", "2026-01-01"
	tr, err = meter.Tariffs(ctx(t), &tc, nil)
	if err != nil || tr.Request != "C_TA_VM_2" || tr.Count != 2 || tr.Records[0].ActiveQuality != 128 || tr.From != "2025-11-15T00:00" {
		t.Fatalf("stored %+v %v", tr, err)
	}
	tc.From, tc.To = "2024-01-01", "2024-02-01"
	_, err = meter.Tariffs(ctx(t), &tc, nil)
	wantCode(t, err, errs.KindNegative, errs.CodeNoPeriod)
	tc.From, tc.To, tc.Register = "", "", 135
	_, err = meter.Tariffs(ctx(t), &tc, nil)
	wantCode(t, err, errs.KindNegative, errs.CodeUnknownRecordAddr)
	for _, edit := range []func(*config.TariffsConfig){
		func(c *config.TariffsConfig) { c.From = "2026-01-01" },
		func(c *config.TariffsConfig) { c.From, c.To = "x", "2026-01-01" },
		func(c *config.TariffsConfig) { c.From, c.To = "2026-01-01", "x" },
		func(c *config.TariffsConfig) { c.From, c.To = "2026-01-02", "2026-01-01" },
		func(c *config.TariffsConfig) { c.From, c.To, c.TimeZone = "2026-01-01", "2026-01-02", "Mars/Base" },
		func(c *config.TariffsConfig) { c.URL = "" },
	} {
		c := config.TariffsConfig{ConnConfig: conn(s), Register: 134}
		edit(&c)
		if err := meter.CheckTariffs(&c); err == nil {
			t.Errorf("accepted %+v", c)
		}
		if _, err := meter.Tariffs(ctx(t), &c, nil); err == nil {
			t.Errorf("ran %+v", c)
		}
	}

	rc := config.REEReadConfig{ConnConfig: conn(s)}
	if err := meter.CheckREERead(&rc); err != nil {
		t.Fatal(err)
	}
	pr, err := meter.ContractedPowers(ctx(t), &rc, nil)
	if err != nil || pr.Count != 2 || pr.Powers[1] != (types.ContractedPower{Address: 2, PowerKW: 20}) || pr.Time == nil || pr.Time.Time != "2026-01-15T10:20" {
		t.Fatalf("powers %+v %v", pr, err)
	}
	dr, err := meter.SpecialDays(ctx(t), &rc, nil)
	if err != nil || dr.Count != 1 || dr.Days[0].Time != "2026-01-01T00:00" {
		t.Fatalf("days %+v %v", dr, err)
	}
	rc.CommonAddress = 9
	if _, err := meter.ContractedPowers(ctx(t), &rc, nil); err == nil {
		t.Error("powers of another measuring point")
	}
	if _, err := meter.SpecialDays(ctx(t), &rc, nil); err == nil {
		t.Error("days of another measuring point")
	}

	sc := config.SetTimeConfig{ConnConfig: conn(s), Time: "2026-02-03T04:05:06.789"}
	sc.AccessKey = "7"
	if err := meter.CheckSetTime(&sc); err != nil {
		t.Fatal(err)
	}
	sr, err := meter.SetTime(ctx(t), &sc, nil)
	if err != nil || sr.Time.Time != "2026-02-03T04:05:06.789" || sr.SU {
		t.Fatalf("set-time %+v %v", sr, err)
	}
	if got := s.Now().Format(time.RFC3339Nano); got != "2026-02-03T04:05:06.789Z" {
		t.Errorf("the station's clock is %s", got)
	}
	sc.Time = "later"
	if err := meter.CheckSetTime(&sc); err == nil {
		t.Error("--time later was accepted")
	}
	if _, err := meter.SetTime(ctx(t), &sc, nil); err == nil {
		t.Error("--time later was sent")
	}
	sc.Time, sc.URL = "now", ""
	if err := meter.CheckSetTime(&sc); err == nil {
		t.Error("set-time without a line")
	}
}

func TestTypes(t *testing.T) {
	r, err := meter.Types(&config.TypesConfig{})
	if err != nil || r.Count < 50 {
		t.Fatalf("types %d %v", r.Count, err)
	}
	r, _ = meter.Types(&config.TypesConfig{Direction: "control", Profile: "ree"})
	for _, ti := range r.Types {
		if ti.Direction != "control" || ti.Profile != "ree" {
			t.Errorf("filtered %+v", ti)
		}
	}
	r, _ = meter.Types(&config.TypesConfig{Direction: "monitor", Profile: "standard"})
	found := false
	for _, ti := range r.Types {
		found = found || ti.Name == "M_IT_TK_2" && ti.Decoded
	}
	if !found {
		t.Error("M_IT_TK_2 missing")
	}
	if _, err := meter.Types(&config.TypesConfig{Direction: "up"}); err == nil {
		t.Error("--direction up")
	}
	if _, err := meter.Types(&config.TypesConfig{Profile: "x"}); err == nil {
		t.Error("--profile x")
	}
}

// A line nobody listens on, and a meter that does not answer the link.
func TestConnectFailures(t *testing.T) {
	c := config.InfoConfig{ConnConfig: config.Defaults()}
	c.URL, c.Timeout = "tcp://127.0.0.1:1", 2000
	_, err := meter.Info(ctx(t), &c, nil)
	wantCode(t, err, errs.KindTransport, errs.CodeTransportConnectFailed)

	s := stationtest.Start(t)
	c.URL, c.LinkAddress, c.LinkTimeout, c.LinkRetries = s.URL(), 9, 100, 1
	_, err = meter.Info(ctx(t), &c, nil)
	wantCode(t, err, errs.KindTimeout, errs.CodeLinkTimeout)

	c.URL = ""
	if _, err := meter.Info(ctx(t), &c, nil); err == nil {
		t.Error("no line")
	}
	cc, cancel := context.WithCancel(context.Background())
	cancel()
	c.URL = s.URL()
	_, err = meter.Info(cc, &c, nil)
	wantCode(t, err, errs.KindInterrupted, errs.CodeContextCanceled)
}

func TestSerialLine(t *testing.T) {
	c := config.InfoConfig{ConnConfig: config.Defaults()}
	c.URL = "serial://" + filepath.Join(t.TempDir(), "no-such-port")
	_, err := meter.Info(ctx(t), &c, nil)
	wantCode(t, err, errs.KindTransport, errs.CodeTransportConnectFailed)
}

func TestDescribeASDU(t *testing.T) {
	p := iec102.Params{}
	key, _ := iec102.New(6, 1, 0, iec102.Raw{Data: []byte{7, 0, 0, 0}}).Encode(p)
	key[0] = 183
	if d := meter.DescribeASDU(key, iec102.Params{Profile: nil}); !strings.Contains(d, "C_AC_NA_2") || !strings.Contains(d, "hidden") || strings.Contains(d, "07 00") {
		t.Errorf("access key: %s", d)
	}
	if d := meter.DescribeASDU([]byte{1}, p); !strings.Contains(d, "undecodable") {
		t.Errorf("short: %s", d)
	}
	tr := &meter.Tracer{W: io.Discard}
	tr.Discard([]byte{1, 2}, errors.New("checksum"))
}

func TestKeyFileIsRead(t *testing.T) {
	s := stationtest.Start(t)
	f := filepath.Join(t.TempDir(), "key")
	if err := os.WriteFile(f, []byte("7\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	c := config.SessionOnlyConfig{ConnConfig: conn(s)}
	c.AccessKeyFile = f
	if r, err := meter.Login(ctx(t), &c, nil); err != nil || !r.Session.Logout {
		t.Fatalf("%+v %v", r, err)
	}
}

func hexOf(b []byte) []byte {
	const digits = "0123456789abcdef"
	out := make([]byte, 0, 2*len(b))
	for _, x := range b {
		out = append(out, digits[x>>4], digits[x&15])
	}
	return out
}

// lockedBuffer is a trace buffer read under the lock it is written under: the link's
// writer goroutine of go-iec60870-5 can still trace a frame after the command returned.
type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}
