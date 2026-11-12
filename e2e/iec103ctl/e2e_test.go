// SPDX-License-Identifier: MIT

//go:build e2e

package e2e

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/otfabric/go-iec60870-5/cp"
	"github.com/otfabric/go-iec60870-5/iec103"
	"github.com/otfabric/go-iec60870-5/iec103/client"
	"github.com/otfabric/go-iec60870-5/iec103/server"
	"github.com/otfabric/go-iec60870-5/link"
)

// bin returns the binary under test: $E2E_BIN_DIR/iec103ctl (set by make e2e).
func bin(t *testing.T) string {
	t.Helper()
	dir := os.Getenv("E2E_BIN_DIR")
	if dir == "" {
		t.Fatal("E2E_BIN_DIR is not set; run make e2e")
	}
	return filepath.Join(dir, "iec103ctl")
}

// run executes the binary and returns stdout, stderr and the exit code.
func run(t *testing.T, args ...string) (stdout, stderr string, code int) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	var out, errb bytes.Buffer
	cmd := exec.CommandContext(ctx, bin(t), args...)
	cmd.Stdout, cmd.Stderr = &out, &errb
	err := cmd.Run()
	var exit *exec.ExitError
	switch {
	case err == nil:
		return out.String(), errb.String(), 0
	case errors.As(err, &exit):
		return out.String(), errb.String(), exit.ExitCode()
	default:
		t.Fatalf("run %s: %v", bin(t), err)
		return "", "", -1
	}
}

func TestVersionNamesTheTool(t *testing.T) {
	out, _, code := run(t, "version")
	if code != 0 || !strings.HasPrefix(out, "iec103ctl ") {
		t.Fatalf("exit %d: %q", code, out)
	}
}

// An unknown command is a usage error: exit code 2 in docs/exitcodes.md.
func TestUnknownCommandIsAUsageError(t *testing.T) {
	_, errOut, code := run(t, "nosuchcommand")
	if code != 2 || !strings.Contains(errOut, `unknown command "nosuchcommand"`) {
		t.Fatalf("exit %d: %s", code, errOut)
	}
}

// device is the application of go-iec60870-5's protection equipment (iec103/server) the
// binary's client commands run against: two indications, one command, two generic entries
// and one recorded disturbance.
type device struct {
	mu   sync.Mutex
	dpi  iec103.DPI
	pick float32
}

func (d *device) HandleASDU(s *server.Server, a *iec103.ASDU) {
	d.mu.Lock()
	defer d.mu.Unlock()
	switch b := a.Body.(type) {
	case iec103.GIInit:
		_ = s.AnswerGI(a,
			iec103.New(0, s.CommonAddr(), 128, 16, iec103.TimeTagged{DPI: d.dpi, Time: s.Now()}),
			iec103.New(0, s.CommonAddr(), 128, 18, iec103.TimeTagged{DPI: iec103.DPIOn, Time: s.Now()}))
	case iec103.GeneralCommand:
		ok := a.Fun == 128 && a.Inf == 16
		_ = s.AckCommand(a, ok)
		if ok && d.dpi != b.DCO {
			d.dpi = b.DCO
			_ = s.Event(iec103.CauseRemoteOperation, a.Fun, a.Inf, b.DCO)
		}
	case iec103.GenericCommand:
		if a.Inf == iec103.InfReadEntry && len(b.Items) == 1 && b.Items[0].GIN == (iec103.GIN{Group: 2, Entry: 1}) {
			_ = s.GenericAnswer(a, iec103.CauseGenericReadValid, iec103.DataSet{GIN: b.Items[0].GIN, KOD: 1, Value: iec103.Float32s(d.pick)})
			return
		}
		if a.Inf == iec103.InfReadHeadings {
			_ = s.GenericAnswer(a, iec103.CauseGenericReadValid, iec103.DataSet{GIN: iec103.GIN{Group: 2}, KOD: 10, Value: iec103.ASCII("SETTINGS")})
		}
	case iec103.GenericData:
		if a.Inf == iec103.InfWriteExecute && len(b.Sets) == 1 && b.Sets[0].Value.Type == iec103.DataFloat32 {
			f, _ := b.Sets[0].Value.Float(0)
			d.pick = float32(f)
			_ = s.GenericAnswer(a, iec103.CauseGenericWriteAck)
		}
	}
}

var faultTime = time.Date(2026, 3, 4, 5, 6, 7, 8e6, time.UTC)

func (d *device) List() []iec103.RecordedFault {
	return []iec103.RecordedFault{{FAN: 3, SOF: iec103.SOFTrip, Time: cp.At(faultTime)}}
}

func (d *device) Record(fan uint16) (*iec103.Disturbance, bool) {
	if fan != 3 {
		return nil, false
	}
	vals := make([]int16, 60)
	for i := range vals {
		vals[i] = int16(i * 100)
	}
	return &iec103.Disturbance{Fun: 128, TOV: 1, FAN: 3, NOF: 1, Interval: 500, Start: cp.At(faultTime),
		Tags:     []iec103.TagChange{{Pos: 0, Tag: iec103.Tag{Fun: 128, Inf: 84, DPI: 1}}, {Pos: 30, Tag: iec103.Tag{Fun: 128, Inf: 84, DPI: 2}}},
		Channels: []iec103.Channel{{ACC: 1, RPV: 1000, RSV: 1, RFA: 1, Values: vals}}}, true
}

func (d *device) Transmitted(uint16, bool) {}

// libraryDevice serves go-iec60870-5's protection equipment on a loopback port, one line
// at a time, and returns its URL.
func libraryDevice(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	d := &device{dpi: iec103.DPIOff, pick: 1.5}
	var wg sync.WaitGroup
	wg.Go(func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			w := &watched{Conn: c, closed: make(chan struct{})}
			s, err := server.New(w, server.Config{LinkAddr: 4, Fun: 128, Identification: iec103.IdentificationOf(3, "LIBRARY", [4]byte{9, 8, 7, 6}),
				Handler: d, Disturbances: d, SyncDelay: -1})
			if err != nil {
				t.Error(err)
				return
			}
			<-w.closed
			_ = s.Close()
		}
	})
	t.Cleanup(func() { _ = ln.Close(); wg.Wait() })
	return "tcp://" + ln.Addr().String()
}

// watched reports the end of a line: the first failed read.
type watched struct {
	net.Conn
	once   sync.Once
	closed chan struct{}
}

func (w *watched) Read(b []byte) (int, error) {
	n, err := w.Conn.Read(b)
	if err != nil {
		w.once.Do(func() { close(w.closed) })
	}
	return n, err
}

// client runs `iec103ctl client <args> --url <url> --link-address 4 --format json` and
// decodes its JSON result.
func clientJSON(t *testing.T, url string, v any, args ...string) {
	t.Helper()
	full := append([]string{"client"}, args...)
	full = append(full, "--url", url, "--link-address", "4", "--link-timeout", "300", "--format", "json")
	out, errOut, code := run(t, full...)
	if code != 0 {
		t.Fatalf("%v: exit %d\n%s%s", args, code, out, errOut)
	}
	if err := json.Unmarshal([]byte(out), v); err != nil {
		t.Fatalf("%v: %v\n%s", args, err, out)
	}
}

// The binary's controlling system against go-iec60870-5's protection equipment.
func TestClientAgainstLibraryServer(t *testing.T) {
	url := libraryDevice(t)
	var id struct {
		Identifications []struct {
			Cause        int    `json:"cause"`
			Manufacturer string `json:"manufacturer"`
			Software     string `json:"software"`
		} `json:"identifications"`
	}
	clientJSON(t, url, &id, "identify")
	if len(id.Identifications) == 0 || id.Identifications[0].Cause != 4 || id.Identifications[0].Manufacturer != "LIBRARY" || id.Identifications[0].Software != "09080706" {
		t.Fatalf("%+v", id)
	}
	var gi struct {
		Count    int `json:"count"`
		Messages []struct {
			Inf int `json:"inf"`
			DPI int `json:"dpi"`
		} `json:"messages"`
	}
	clientJSON(t, url, &gi, "interrogate")
	if gi.Count != 2 || gi.Messages[0].Inf != 16 || gi.Messages[0].DPI != 1 {
		t.Fatalf("%+v", gi)
	}
	var cmd struct {
		Acknowledged bool `json:"acknowledged"`
		Report       *struct {
			Cause int `json:"cause"`
			DPI   int `json:"dpi"`
		} `json:"report"`
	}
	clientJSON(t, url, &cmd, "command", "--fun", "128", "--inf", "16", "--value", "on", "--wait-report", "2000")
	if !cmd.Acknowledged || cmd.Report == nil || cmd.Report.Cause != 12 || cmd.Report.DPI != 2 {
		t.Fatalf("%+v", cmd)
	}
	if _, _, code := run(t, "client", "command", "--url", url, "--link-address", "4", "--fun", "128", "--inf", "17", "--value", "on"); code != 6 {
		t.Fatalf("refused command: exit %d", code)
	}
	var clock struct {
		DeviceTime string `json:"device_time"`
	}
	clientJSON(t, url, &clock, "clock", "--time", "2026-05-06T07:08:09Z")
	if !strings.HasPrefix(clock.DeviceTime, "2026-05-06T07:08:09") {
		t.Fatalf("%+v", clock)
	}
	var g struct {
		Cause int `json:"cause"`
		Sets  []struct {
			GIN    string `json:"gin"`
			Values []any  `json:"values"`
		} `json:"sets"`
	}
	clientJSON(t, url, &g, "generic", "write", "--gin", "2.1", "--type", "float32", "--value", "2.75")
	if g.Cause != 40 {
		t.Fatalf("%+v", g)
	}
	clientJSON(t, url, &g, "generic", "entry", "--gin", "2.1")
	if g.Cause != 42 || len(g.Sets) != 1 || g.Sets[0].Values[0] != 2.75 {
		t.Fatalf("%+v", g)
	}
	clientJSON(t, url, &g, "generic", "headings")
	if len(g.Sets) != 1 || g.Sets[0].GIN != "2.0" {
		t.Fatalf("%+v", g)
	}
	var list struct {
		Fun    int `json:"fun"`
		Faults []struct {
			FAN  int    `json:"fan"`
			Time string `json:"time"`
		} `json:"faults"`
	}
	clientJSON(t, url, &list, "disturbance", "list")
	if list.Fun != 128 || len(list.Faults) != 1 || list.Faults[0].FAN != 3 || list.Faults[0].Time != "2026-03-04T05:06:07.008Z" {
		t.Fatalf("%+v", list)
	}
	csv := filepath.Join(t.TempDir(), "f.csv")
	var rec struct {
		FAN      int `json:"fan"`
		NOE      int `json:"noe"`
		Tags     []any
		Channels []struct {
			Values []int `json:"values"`
		} `json:"channels"`
	}
	clientJSON(t, url, &rec, "disturbance", "get", "--fan", "3", "--csv", csv)
	if rec.FAN != 3 || rec.NOE != 60 || len(rec.Tags) != 2 || rec.Channels[0].Values[59] != 5900 {
		t.Fatalf("%+v", rec)
	}
	if data, err := os.ReadFile(csv); err != nil || !strings.Contains(string(data), "59,29500,5900") {
		t.Fatalf("%v %s", err, data)
	}
	var scan struct {
		Found []struct {
			LinkAddress  int    `json:"link_address"`
			Manufacturer string `json:"manufacturer"`
		} `json:"found"`
	}
	clientJSON(t, url, &scan, "scan", "--start", "3", "--end", "5", "--timeout", "500")
	if len(scan.Found) != 1 || scan.Found[0].LinkAddress != 4 || scan.Found[0].Manufacturer != "LIBRARY" {
		t.Fatalf("%+v", scan)
	}
	out, _, code := run(t, "client", "monitor", "--url", url, "--link-address", "4", "--interrogate", "--count", "3", "--format", "json")
	if code != 0 || strings.Count(out, "\n") != 3 {
		t.Fatalf("exit %d\n%s", code, out)
	}
}

// startServer runs `iec103ctl server start` on a free port and returns its address.
func startServer(t *testing.T, args ...string) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, bin(t), append([]string{"server", "start", "--listen", "127.0.0.1", "--port", strconv.Itoa(port), "--format", "json"}, args...)...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cancel(); _ = cmd.Wait() })
	sc := bufio.NewScanner(stdout)
	for sc.Scan() {
		var e struct{ Event, Remote string }
		if json.Unmarshal(sc.Bytes(), &e) == nil && e.Event == "listening" {
			go func() { _, _ = io.Copy(io.Discard, stdout) }()
			return e.Remote
		}
	}
	t.Fatal("server start did not listen")
	return ""
}

// The binary's simulated device against go-iec60870-5's controlling system.
func TestServerAgainstLibraryClient(t *testing.T) {
	addr := startServer(t, "--link-address", "2")
	c, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var reports []*iec103.ASDU
	cl, err := client.New(c, client.Config{LinkAddr: 2, NoInitGI: true, NoInitTimeSync: true, Link: link.Config{Timeout: 500 * time.Millisecond},
		Handler: func(a *iec103.ASDU) {
			if a.Cause == iec103.CauseRemoteOperation {
				mu.Lock()
				reports = append(reports, a)
				mu.Unlock()
			}
		}})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cl.Close() }()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := cl.WaitUp(ctx); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for cl.IdentificationFor(iec103.CauseResetCU) == nil && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if id := cl.IdentificationFor(iec103.CauseResetCU); id == nil || id.Fun != 160 || id.Body.(iec103.Identification).Manufacturer() != "IEC103CT" {
		t.Fatalf("identification %v", id)
	}
	if _, err := cl.ClockSync(ctx, time.Now()); err != nil {
		t.Fatal(err)
	}
	gi, err := cl.Interrogate(ctx)
	if err != nil || len(gi) != 6 {
		t.Fatalf("%v %d", err, len(gi))
	}
	if err := cl.Command(ctx, 160, 17, iec103.DPIOn); err != nil {
		t.Fatal(err)
	}
	if err := cl.Command(ctx, 160, 18, iec103.DPIOn); !errors.Is(err, iec103.ErrNegative) {
		t.Fatalf("160/18: %v", err)
	}
	h, err := cl.ReadHeadings(ctx)
	if err != nil || len(h.Sets) != 2 {
		t.Fatalf("%v %+v", err, h)
	}
	if _, err := cl.GenericWrite(ctx, iec103.InfWriteExecute, iec103.DataSet{GIN: iec103.GIN{Group: 1, Entry: 2}, KOD: 1, Value: iec103.Float32s(2.5)}); err != nil {
		t.Fatal(err)
	}
	r, err := cl.ReadEntry(ctx, iec103.GIN{Group: 1, Entry: 2}, iec103.KODActualValue)
	if f, _ := r.Sets[0].Value.Float(0); err != nil || f != 2.5 {
		t.Fatalf("%v %v", err, f)
	}
	faults, err := cl.DisturbanceList(ctx, 160)
	if err != nil || len(faults) != 1 {
		t.Fatalf("%v %+v", err, faults)
	}
	rec, err := cl.Disturbance(ctx, 160, faults[0].FAN)
	if err != nil || len(rec.Channels) != 2 || len(rec.Tags) != 4 {
		t.Fatalf("%v %+v", err, rec)
	}
	deadline = time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		n := len(reports)
		mu.Unlock()
		if n > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(reports) != 1 || reports[0].Inf != 17 {
		t.Fatalf("reports %v", reports)
	}
}
