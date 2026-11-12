// SPDX-License-Identifier: MIT

package iec

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/otfabric/go-iec60870-5/iec103"
	"github.com/otfabric/iec60870ctl/internal/errs"
	"github.com/otfabric/iec60870ctl/internal/iec103ctl/config"
	"github.com/otfabric/iec60870ctl/internal/iec103ctl/linecfg"
	"github.com/otfabric/iec60870ctl/internal/iec103ctl/stationtest"
	"github.com/otfabric/iec60870ctl/internal/iec103ctl/types"
)

// conn returns the connection to url with short timeouts.
func conn(url string) config.ConnConfig {
	c := config.ConnConfig{URL: url, Timeout: 3000}
	c.LinkAddress, c.Retries, c.LinkTimeout, c.PollInterval = 1, 1, 200, 10
	return c
}

func kindOf(t *testing.T, err error) errs.Kind {
	t.Helper()
	e, ok := errs.As(err)
	if !ok {
		t.Fatalf("untyped error %v", err)
	}
	return e.Kind
}

func codeOf(t *testing.T, err error) string {
	t.Helper()
	e, ok := errs.As(err)
	if !ok {
		t.Fatalf("untyped error %v", err)
	}
	return e.Code
}

func TestInterrogate(t *testing.T) {
	st := stationtest.Start(t, stationtest.Model)
	var trace bytes.Buffer
	cfg := config.InterrogateConfig{ConnConfig: conn(st.URL())}
	cfg.Debug = true
	res, err := Interrogate(context.Background(), &cfg, &syncWriter{w: &trace})
	if err != nil {
		t.Fatal(err)
	}
	if res.Count != 8 || res.Messages[0].Fun != 160 || res.Messages[0].Inf != 16 || *res.Messages[0].DPI != 2 || res.Messages[0].Cause != 9 {
		t.Fatalf("%+v", res)
	}
	if !strings.Contains(trace.String(), "tx") || !strings.Contains(trace.String(), "rx") {
		t.Fatalf("no trace: %q", trace.String())
	}
	if err := CheckInterrogate(&cfg); err != nil {
		t.Fatal(err)
	}
}

// syncWriter serializes writes from the goroutines of the client.
type syncWriter struct {
	mu sync.Mutex
	w  *bytes.Buffer
}

func (s *syncWriter) Write(b []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.w.Write(b)
}

func TestCommand(t *testing.T) {
	st := stationtest.Start(t, stationtest.Model)
	cfg := config.CommandConfig{ConnConfig: conn(st.URL()), Fun: 160, Inf: 17, Value: "on", WaitReport: 2000}
	res, err := Command(context.Background(), &cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Acknowledged || res.Command != "on" || res.Report == nil || *res.Report.DPI != 2 || res.Report.Cause != 12 {
		t.Fatalf("%+v %+v", res, res.Report)
	}
	if st.DPI(160, 17) != iec103.DPIOn {
		t.Fatal("state not changed")
	}
	// Again: no change, so no report within the wait.
	cfg.WaitReport = 100
	res, err = Command(context.Background(), &cfg, nil)
	if err != nil || res.Report != nil {
		t.Fatalf("%v %+v", err, res)
	}
	// OFF without waiting for the report.
	cfg.Value, cfg.WaitReport = "off", 0
	if res, err = Command(context.Background(), &cfg, nil); err != nil || res.Command != "off" {
		t.Fatalf("%v %+v", err, res)
	}
	// 160/18 is no command of the device: cause 21.
	cfg.Inf = 18
	_, err = Command(context.Background(), &cfg, nil)
	if kindOf(t, err) != errs.KindNegative || codeOf(t, err) != errs.CodeNegativeConfirmation {
		t.Fatalf("%v", err)
	}
}

func TestCommandValidation(t *testing.T) {
	base := conn("tcp://127.0.0.1:1")
	for _, c := range []config.CommandConfig{
		{ConnConfig: base, Fun: 300, Inf: 16, Value: "on"},
		{ConnConfig: base, Fun: 160, Inf: 16, Value: "maybe"},
		{ConnConfig: base, Fun: 160, Inf: 19, Value: "off"},
		{ConnConfig: base, Fun: 160, Inf: 40, Value: "on"},
	} {
		if err := CheckCommand(&c); err == nil {
			t.Errorf("%+v accepted", c)
		}
		if _, err := Command(context.Background(), &c, nil); kindOf(t, err) != errs.KindInvalidInput {
			t.Errorf("%+v: %v", c, err)
		}
	}
}

func TestClock(t *testing.T) {
	st := stationtest.Start(t, stationtest.Model)
	cfg := config.ClockConfig{ConnConfig: conn(st.URL()), Time: "2026-03-01T12:00:00Z"}
	res, err := Clock(context.Background(), &cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.Sent != "2026-03-01T12:00:00.000Z" || res.DeviceTime == "" || res.OffsetMs == nil {
		t.Fatalf("%+v", res)
	}
	cfg.Time, cfg.Broadcast = "now", true
	if res, err = Clock(context.Background(), &cfg, nil); err != nil || !res.Broadcast || res.DeviceTime != "" {
		t.Fatalf("%v %+v", err, res)
	}
	cfg.Time = "yesterday"
	if err := CheckClock(&cfg); err == nil {
		t.Fatal("bad time accepted")
	}
	if _, err := Clock(context.Background(), &cfg, nil); kindOf(t, err) != errs.KindInvalidInput {
		t.Fatal(err)
	}
}

func TestIdentify(t *testing.T) {
	st := stationtest.Start(t, stationtest.Model)
	cfg := config.IdentifyConfig{ConnConfig: conn(st.URL()), Wait: 200}
	res, err := Identify(context.Background(), &cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Identifications) < 1 || res.Identifications[0].Cause != 4 || res.Identifications[0].Manufacturer != "OTFAB103" ||
		res.Identifications[0].Software != "01020304" || res.Identifications[0].Fun != 160 || res.Identifications[0].COL != 2 {
		t.Fatalf("%+v", res)
	}
	cfg.ResetFCB = true
	if res, err = Identify(context.Background(), &cfg, nil); err != nil || res.Identifications[0].Cause != 3 {
		t.Fatalf("%v %+v", err, res)
	}
	if err := CheckIdentify(&cfg); err != nil {
		t.Fatal(err)
	}
}

func TestGeneric(t *testing.T) {
	st := stationtest.Start(t, stationtest.Model)
	ctx := context.Background()
	g := config.GenericConfig{ConnConfig: conn(st.URL())}
	res, err := Headings(ctx, &g, nil)
	if err != nil || len(res.Sets) != 1 || res.Sets[0].GIN != "1.0" || res.Sets[0].Values[0] != "SETTINGS" {
		t.Fatalf("%v %+v", err, res)
	}
	if res, err = GenericGI(ctx, &g, nil); err != nil || len(res.Sets) != 5 {
		t.Fatalf("%v %+v", err, res)
	}
	e := config.GenericEntryConfig{ConnConfig: conn(st.URL()), GIN: "1.5"}
	if res, err = GenericReadEntry(ctx, &e, nil); err != nil || res.Sets[0].Values[0] != int64(-20) || InvalidData(res) != 0 {
		t.Fatalf("%v %+v", err, res)
	}
	e.GIN, e.KOD = "1.4", "description"
	if res, err = GenericReadEntry(ctx, &e, nil); err != nil || res.Sets[0].Values[0] != "FEEDER" {
		t.Fatalf("%v %+v", err, res)
	}
	e.GIN, e.KOD = "2.1", ""
	if res, err = GenericReadEntry(ctx, &e, nil); err != nil || res.Cause != 43 || res.Sets[0].Reply != "invalid-gin" || InvalidData(res) != errs.ExitPartial {
		t.Fatalf("%v %+v", err, res)
	}
	e.GIN, e.KOD = "1.1", "range"
	if res, err = GenericReadEntry(ctx, &e, nil); err != nil || res.Cause != 43 || res.Sets[0].Reply != "not-exist" {
		t.Fatalf("%v %+v", err, res)
	}
	e.GIN = "1.2"
	if res, err = GenericReadDirectory(ctx, &e, nil); err != nil || res.GIN != "1.2" || len(res.Elements) != 2 {
		t.Fatalf("%v %+v", err, res)
	}
	e.GIN = "3.3"
	if res, err = GenericReadDirectory(ctx, &e, nil); err != nil || res.Cause != 43 {
		t.Fatalf("%v %+v", err, res)
	}
	gr := config.GenericGroupConfig{ConnConfig: conn(st.URL()), Group: 1, KOD: "description"}
	if res, err = GenericReadGroup(ctx, &gr, nil); err != nil || len(res.Sets) != 5 {
		t.Fatalf("%v %+v", err, res)
	}
	gr.Group, gr.KOD = 9, ""
	if res, err = GenericReadGroup(ctx, &gr, nil); err != nil || res.Cause != 43 {
		t.Fatalf("%v %+v", err, res)
	}
	gr.Group, gr.KOD = 1, "range"
	if res, err = GenericReadGroup(ctx, &gr, nil); err != nil || res.Cause != 43 {
		t.Fatalf("%v %+v", err, res)
	}
	r := config.GenericReadConfig{ConnConfig: conn(st.URL()), Inf: 244, Items: "1.1,1.3:10"}
	if res, err = GenericReadItems(ctx, &r, nil); err != nil || len(res.Sets) != 2 {
		t.Fatalf("%v %+v", err, res)
	}
	r.Inf, r.Items = 240, ""
	if res, err = GenericReadItems(ctx, &r, nil); err != nil || len(res.Sets) != 1 {
		t.Fatalf("%v %+v", err, res)
	}
}

func TestGenericWrite(t *testing.T) {
	st := stationtest.Start(t, stationtest.Model)
	ctx := context.Background()
	w := config.GenericWriteConfig{ConnConfig: conn(st.URL()), GIN: "1.2", Type: "float32", Value: "2.25", Mode: WriteExecute}
	res, err := GenericWriteSet(ctx, &w, nil)
	if err != nil || res.Cause != 40 || res.Mode != WriteExecute {
		t.Fatalf("%v %+v", err, res)
	}
	if v, _ := st.Generic(iec103.GIN{Group: 1, Entry: 2}); v.Type != iec103.DataFloat32 {
		t.Fatalf("%+v", v)
	} else if f, _ := v.Float(0); f != 2.25 {
		t.Fatalf("value %v", f)
	}
	w.GIN, w.Type, w.Size, w.Value, w.Mode = "1.3", "uint", 2, "450", WriteAbort
	if res, err = GenericWriteSet(ctx, &w, nil); err != nil || res.Cause != 40 || res.Inf != 251 {
		t.Fatalf("%v %+v", err, res)
	}
	if v, _ := st.Generic(iec103.GIN{Group: 1, Entry: 3}); v.Data[0] != 0x2C {
		t.Fatalf("aborted write applied: %x", v.Data)
	}
	w.Mode = WriteExecute
	w.GIN = "1.1"
	_, err = GenericWriteSet(ctx, &w, nil)
	if kindOf(t, err) != errs.KindNegative || !strings.Contains(err.Error(), "1.1 read-only") {
		t.Fatalf("%v", err)
	}
	for _, bad := range []config.GenericWriteConfig{
		{ConnConfig: conn(st.URL()), GIN: "1", Type: "uint", Size: 2, Value: "1", Mode: "execute"},
		{ConnConfig: conn(st.URL()), GIN: "1.1", Type: "uint", Size: 2, Value: "1", Mode: "maybe"},
		{ConnConfig: conn(st.URL()), GIN: "1.1", KOD: "nope", Type: "uint", Size: 2, Value: "1", Mode: "execute"},
		{ConnConfig: conn(st.URL()), GIN: "1.1", Type: "bits", Size: 2, Value: "1", Mode: "execute"},
		{ConnConfig: conn(st.URL()), GIN: "1.1", Type: "uint", Size: 2, Value: "x", Mode: "execute"},
		{ConnConfig: conn("udp://x"), GIN: "1.1", Type: "uint", Size: 2, Value: "1", Mode: "execute"},
	} {
		if err := CheckGenericWrite(&bad); err == nil {
			t.Errorf("%+v accepted", bad)
		}
		if _, err := GenericWriteSet(ctx, &bad, nil); kindOf(t, err) != errs.KindInvalidInput {
			t.Errorf("%+v: %v", bad, err)
		}
	}
}

func TestGenericValidation(t *testing.T) {
	ctx := context.Background()
	base := conn("tcp://127.0.0.1:1")
	for _, r := range []config.GenericReadConfig{{ConnConfig: base, Inf: 245}, {ConnConfig: base, Inf: 244}, {ConnConfig: base, Inf: 244, Items: "x"}} {
		if err := CheckGenericRead(&r); err == nil {
			t.Errorf("%+v accepted", r)
		}
		if _, err := GenericReadItems(ctx, &r, nil); kindOf(t, err) != errs.KindInvalidInput {
			t.Error(err)
		}
	}
	e := config.GenericEntryConfig{ConnConfig: base, GIN: "1.x"}
	if CheckGenericEntry(&e) == nil {
		t.Error("bad GIN accepted")
	}
	if _, err := GenericReadEntry(ctx, &e, nil); kindOf(t, err) != errs.KindInvalidInput {
		t.Error(err)
	}
	if _, err := GenericReadDirectory(ctx, &e, nil); kindOf(t, err) != errs.KindInvalidInput {
		t.Error(err)
	}
	e.URL = ""
	if CheckGenericEntry(&e) == nil {
		t.Error("no line accepted")
	}
	g := config.GenericGroupConfig{ConnConfig: base, Group: 256}
	if CheckGenericGroup(&g) == nil {
		t.Error("group 256 accepted")
	}
	if _, err := GenericReadGroup(ctx, &g, nil); kindOf(t, err) != errs.KindInvalidInput {
		t.Error(err)
	}
	g.URL = ""
	if CheckGenericGroup(&g) == nil {
		t.Error("no line accepted")
	}
	gc := config.GenericConfig{ConnConfig: base}
	gc.Retries = 99
	if CheckGeneric(&gc) == nil {
		t.Error("retries 99 accepted")
	}
	if _, err := Headings(ctx, &gc, nil); kindOf(t, err) != errs.KindInvalidInput {
		t.Error(err)
	}
}

func TestDisturbance(t *testing.T) {
	st := stationtest.Start(t, stationtest.Model)
	ctx := context.Background()
	l := config.DisturbanceListConfig{ConnConfig: conn(st.URL())}
	res, err := DisturbanceList(ctx, &l, nil)
	if err != nil || res.Fun != 160 || len(res.Faults) != 1 || res.Faults[0].FAN != 7 || !res.Faults[0].SOF.TP || res.Faults[0].Time != "2026-10-09T15:30:00.012Z" {
		t.Fatalf("%v %+v", err, res)
	}
	l.Fun = 160
	if _, err := DisturbanceList(ctx, &l, nil); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	out, csvPath := filepath.Join(dir, "f.json"), filepath.Join(dir, "f.csv")
	g := config.DisturbanceGetConfig{ConnConfig: conn(st.URL()), FAN: 7, Output: out, CSV: csvPath}
	rec, err := DisturbanceGet(ctx, &g, nil)
	if err != nil {
		t.Fatal(err)
	}
	if rec.FAN != 7 || rec.NOF != 3 || rec.NOC != 2 || rec.NOE != 40 || rec.IntervalUs != 1000 || rec.Start != "15:30:00.000" ||
		len(rec.Tags) != 4 || rec.Tags[2].Pos != 12 || rec.Channels[1].Values[12] != 20885 || rec.Channels[0].Name != "IL1" {
		t.Fatalf("%+v", rec)
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	var back types.Disturbance
	if err := json.Unmarshal(data, &back); err != nil || back.FAN != 7 {
		t.Fatalf("%v %s", err, data)
	}
	c, _ := os.ReadFile(csvPath)
	lines := strings.Split(strings.TrimSpace(string(c)), "\n")
	if len(lines) != 41 || lines[0] != "index,time_us,acc1_IL1,acc2_IL2" || lines[13] != "12,12000,-12343,20885" {
		t.Fatalf("%q", lines[:3])
	}
	// The files exist now: refused before anything is sent, unless --force.
	if err := CheckDisturbanceGet(&g); err == nil {
		t.Fatal("existing file accepted")
	}
	g.Force = true
	if _, err := DisturbanceGet(ctx, &g, nil); err != nil {
		t.Fatal(err)
	}
	// A fault the device does not have.
	g.FAN, g.Output, g.CSV = 9, "", ""
	_, err = DisturbanceGet(ctx, &g, nil)
	if err == nil {
		t.Fatal("unknown FAN transferred")
	}
}

func TestDisturbanceValidation(t *testing.T) {
	base := conn("tcp://127.0.0.1:1")
	dir := t.TempDir()
	for _, g := range []config.DisturbanceGetConfig{
		{ConnConfig: base, FAN: 70000},
		{ConnConfig: base, Fun: 256},
		{ConnConfig: base, Output: filepath.Join(dir, "x"), CSV: filepath.Join(dir, "x")},
		{ConnConfig: config.ConnConfig{}},
	} {
		if err := CheckDisturbanceGet(&g); err == nil {
			t.Errorf("%+v accepted", g)
		}
		if _, err := DisturbanceGet(context.Background(), &g, nil); err == nil {
			t.Errorf("%+v ran", g)
		}
	}
	l := config.DisturbanceListConfig{ConnConfig: base, Fun: 256}
	if _, err := DisturbanceList(context.Background(), &l, nil); kindOf(t, err) != errs.KindInvalidInput {
		t.Error(err)
	}
	l.ConnConfig = config.ConnConfig{}
	if CheckDisturbanceList(&l) == nil {
		t.Error("no line accepted")
	}
}

func TestChannelsCSVUnevenChannels(t *testing.T) {
	d := &types.Disturbance{IntervalUs: 500, Channels: []types.Channel{{ACC: 1, Name: "IL1", Values: []int16{1, 2}}, {ACC: 2, Name: "IL2", Values: []int16{3}}}}
	if got := string(ChannelsCSV(d)); got != "index,time_us,acc1_IL1,acc2_IL2\n0,0,1,3\n1,500,2,\n" {
		t.Fatalf("%q", got)
	}
}

func TestWriteFileErrors(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "a")
	if err := writeFile(p, []byte("x"), false); err != nil {
		t.Fatal(err)
	}
	if err := writeFile(p, []byte("x"), false); codeOf(t, err) != errs.CodeOutputFileCreateFailed || kindOf(t, err) != errs.KindInvalidInput {
		t.Fatal(err)
	}
	if err := writeFile(filepath.Join(dir, "no", "such"), []byte("x"), true); kindOf(t, err) != errs.KindOutput {
		t.Fatal(err)
	}
}

func TestNoDevice(t *testing.T) {
	ctx := context.Background()
	c := conn(stationtest.Silent(t))
	c.Timeout = 300
	_, err := Interrogate(ctx, &config.InterrogateConfig{ConnConfig: c}, nil)
	if kindOf(t, err) != errs.KindTimeout || codeOf(t, err) != errs.CodeLinkTimeout {
		t.Fatalf("%v", err)
	}
	c.URL = stationtest.Refused(t)
	_, err = Interrogate(ctx, &config.InterrogateConfig{ConnConfig: c}, nil)
	if kindOf(t, err) != errs.KindTransport || codeOf(t, err) != errs.CodeTransportConnectFailed {
		t.Fatalf("%v", err)
	}
	cctx, cancel := context.WithCancel(ctx)
	cancel()
	_, err = Interrogate(cctx, &config.InterrogateConfig{ConnConfig: c}, nil)
	if kindOf(t, err) != errs.KindInterrupted {
		t.Fatalf("%v", err)
	}
}

// A device without disturbance data, generic services or identification.
const bareModel = `{"station": {"commonAddress": 1, "identification": {"fun": 128, "col": 2, "ascii": "BARE", "software": "00000000"}},
 "indications": [{"fun": 128, "inf": 16, "dpi": 1}]}`

func TestBareDevice(t *testing.T) {
	st := stationtest.Start(t, bareModel)
	ctx := context.Background()
	c := conn(st.URL())
	c.Timeout = 500
	// No disturbance data: the order is not answered.
	if _, err := DisturbanceList(ctx, &config.DisturbanceListConfig{ConnConfig: c}, nil); kindOf(t, err) != errs.KindTimeout {
		t.Fatalf("%v", err)
	}
	g, err := GenericReadEntry(ctx, &config.GenericEntryConfig{ConnConfig: conn(st.URL()), GIN: "1.1"}, nil)
	if err != nil || g.Cause != 43 {
		t.Fatalf("%v %+v", err, g)
	}
}

func TestErrorsOfTheLibraryAreTyped(t *testing.T) {
	for err, kind := range map[error]errs.Kind{
		context.Canceled:                       errs.KindInterrupted,
		context.DeadlineExceeded:               errs.KindTimeout,
		iec103.ErrAborted:                      errs.KindNegative,
		iec103.ErrNegative:                     errs.KindNegative,
		iec103.ErrLinkDown:                     errs.KindTransport,
		iec103.ErrClosed:                       errs.KindTransport,
		iec103.ErrBusy:                         errs.KindProtocol,
		iec103.ErrInvalid:                      errs.KindInvalidInput,
		iec103.ErrShort:                        errs.KindProtocol,
		errors.New("x"):                        errs.KindInternal,
		&timeoutErr{}:                          errs.KindTransport,
		errs.New(errs.KindUsage, "u", "", nil): errs.KindUsage,
	} {
		if got := kindOf(t, WrapError(err)); got != kind {
			t.Errorf("%v: %s, want %s", err, got, kind)
		}
	}
	if WrapError(nil) != nil || EmbeddedErrorInfo(nil) != nil {
		t.Fatal("nil")
	}
	if e := EmbeddedErrorInfo(iec103.ErrBusy); e.Code != errs.CodeRequestBusy {
		t.Fatal(e)
	}
}

type timeoutErr struct{}

func (*timeoutErr) Error() string   { return "net" }
func (*timeoutErr) Timeout() bool   { return true }
func (*timeoutErr) Temporary() bool { return false }

func TestCheckConn(t *testing.T) {
	ok := conn("tcp://h")
	if _, err := CheckConn(&ok); err != nil {
		t.Fatal(err)
	}
	for _, f := range []func(*config.ConnConfig){
		func(c *config.ConnConfig) { c.URL = "" },
		func(c *config.ConnConfig) { c.LinkAddress = 255 },
		func(c *config.ConnConfig) { c.CommonAddress = 256 },
		func(c *config.ConnConfig) { c.Retries = -1 },
		func(c *config.ConnConfig) { c.TimeZone = "Mars/Base" },
		func(c *config.ConnConfig) { c.Parity = "mark" },
	} {
		c := conn("tcp://h")
		f(&c)
		if _, err := CheckConn(&c); err == nil {
			t.Errorf("%+v accepted", c)
		}
	}
	if l, err := linecfg.Location("local"); err != nil || l != time.Local {
		t.Fatal(err)
	}
	if l, err := linecfg.Location("Europe/Brussels"); err != nil || l.String() != "Europe/Brussels" {
		t.Fatal(err)
	}
}
