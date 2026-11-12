//go:build interop

// SPDX-License-Identifier: MIT

package interop

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// gisceImage is the reference: the gisce/iec870ree v0.14.1 client of
// otfabric/iec60870-5-interop v0.4.0 (contract 2.3), pinned by multi-arch
// index digest (the release's manifest.json): the same digest as
// go-iec60870-5 v0.4.1 iec102/interop/harness.go.
const gisceImage = "ghcr.io/otfabric/iec60870-5-interop-iec102-gisce@sha256:dedd69a06eb7f138f34827827120f69e2a43760c9383e425ddf0b05df9766139"

type adapter struct {
	name, image string
	caps        capabilities
}

type capabilities struct {
	Adapter  string `json:"adapter"`
	Standard string `json:"standard"`
	Roles    struct {
		Server bool `json:"server"`
		Client bool `json:"client"`
	} `json:"roles"`
	Features         map[string]bool `json:"features"`
	LinkAddressSizes []int           `json:"linkAddressSizes"`
	CASizes          []int           `json:"caSizes"`
	Profiles         []string        `json:"profiles"`
	ClientOperations []string        `json:"clientOperations"`
	Upstream         struct {
		Name, Version, Revision string
		IndependentEngine       bool `json:"independentEngine"`
	} `json:"upstream"`
}

func require() bool { return os.Getenv("IEC102_REQUIRE_INTEROP") == "1" }

// skipOrFail skips for missing infrastructure (Docker, the image), or fails
// with IEC102_REQUIRE_INTEROP=1.
func skipOrFail(t *testing.T, format string, args ...any) {
	t.Helper()
	if require() {
		t.Fatalf(format, args...)
	}
	t.Skipf(format, args...)
}

func docker(ctx context.Context, args ...string) ([]byte, error) {
	var out, errb bytes.Buffer
	cmd := exec.CommandContext(ctx, "docker", args...)
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		return out.Bytes(), fmt.Errorf("docker %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(errb.String()))
	}
	return out.Bytes(), nil
}

var (
	gisceOnce sync.Once
	gisce     adapter
	gisceErr  error
)

// reference returns the gisce image and its capabilities.
func reference(t *testing.T) adapter {
	t.Helper()
	gisceOnce.Do(func() {
		img := gisceImage
		if v, ok := os.LookupEnv("IEC102_INTEROP_GISCE_IMAGE"); ok {
			img = v
		}
		if img == "" {
			gisceErr = errors.New("IEC102_INTEROP_GISCE_IMAGE is empty")
			return
		}
		if _, err := exec.LookPath("docker"); err != nil {
			gisceErr = err
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		out, err := docker(ctx, "run", "--rm", img, "print-capabilities")
		if err != nil {
			gisceErr = err
			return
		}
		gisce = adapter{name: "gisce", image: img}
		if err := json.Unmarshal(out, &gisce.caps); err != nil {
			gisceErr = fmt.Errorf("gisce capabilities: %w", err)
			return
		}
		if gisce.caps.Standard != "iec60870-5-102" || !gisce.caps.Roles.Client {
			gisceErr = fmt.Errorf("%s is not an IEC 102 client image: %s", img, out)
		}
	})
	if gisceErr != nil {
		skipOrFail(t, "reference unavailable: %v", gisceErr)
	}
	return gisce
}

// fixture is the IEC 102 baseline fixture as the reference prints it
// (iec60870-5-interop docs/FIXTURES.md, "IEC 102").
type fixture struct {
	Station struct {
		CommonAddress int    `json:"commonAddress"`
		AccessKey     uint32 `json:"accessKey"`
		Clock         string `json:"clock"`
		Product       struct {
			Standard     uint8  `json:"standard"`
			Manufacturer uint8  `json:"manufacturer"`
			Product      uint32 `json:"product"`
		} `json:"product"`
	} `json:"station"`
	Totals []struct {
		Record  uint8  `json:"record"`
		Kind    string `json:"kind"`
		Periods []struct {
			End    string  `json:"end"`
			Totals []total `json:"totals"`
		} `json:"periods"`
	} `json:"totals"`
	Tariffs []struct {
		Register uint8          `json:"register"`
		Current  []tariffRecord `json:"current"`
		Stored   []tariffRecord `json:"stored"`
	} `json:"tariffs"`
}

// total is an integrated total as the fixture and the documents write it.
type total struct {
	Address uint8  `json:"address"`
	Value   uint32 `json:"value"`
	Quality uint8  `json:"quality"`
}

// tariffRecord is a record of M_TA_VC_2 / M_TA_VM_2 as the fixture and the
// documents write it.
type tariffRecord struct {
	Address            uint8  `json:"address"`
	ActiveAbs          uint32 `json:"activeAbs"`
	ActiveInc          uint32 `json:"activeInc"`
	ActiveQuality      uint8  `json:"activeQuality"`
	ReactiveIndAbs     uint32 `json:"reactiveIndAbs"`
	ReactiveIndInc     uint32 `json:"reactiveIndInc"`
	ReactiveIndQuality uint8  `json:"reactiveIndQuality"`
	ReactiveCapAbs     uint32 `json:"reactiveCapAbs"`
	ReactiveCapInc     uint32 `json:"reactiveCapInc"`
	ReactiveCapQuality uint8  `json:"reactiveCapQuality"`
	MaxPower           uint32 `json:"maxPower"`
	MaxPowerTime       string `json:"maxPowerTime"`
	MaxPowerQuality    uint8  `json:"maxPowerQuality"`
	ExcessPower        uint32 `json:"excessPower"`
	ExcessPowerQuality uint8  `json:"excessPowerQuality"`
	PeriodStart        string `json:"periodStart"`
	PeriodEnd          string `json:"periodEnd"`
}

func loadFixture(t *testing.T, a adapter) *fixture {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	out, err := docker(ctx, "run", "--rm", a.image, "print-fixture", "baseline")
	if err != nil {
		t.Fatal(err)
	}
	var f fixture
	if err := json.Unmarshal(out, &f); err != nil {
		t.Fatal(err)
	}
	if len(f.Totals) == 0 || len(f.Tariffs) == 0 {
		t.Fatalf("fixture without totals or tariffs: %s", out)
	}
	return &f
}

// object is an information object of an IEC 102 ASDU document (contract
// 2.2, "IEC 102 ASDU documents"): an integrated total, a time, a product
// identification, a tariff record or the elements of a mirrored request.
type object struct {
	tariffRecord
	Value        uint32 `json:"value"`
	Quality      uint8  `json:"quality"`
	Time         string `json:"time"`
	SU           bool   `json:"su"`
	TimeInvalid  bool   `json:"timeInvalid"`
	Standard     uint8  `json:"standard"`
	Manufacturer uint8  `json:"manufacturer"`
	Product      uint32 `json:"product"`
	First        uint8  `json:"first"`
	Last         uint8  `json:"last"`
	From         string `json:"from"`
	To           string `json:"to"`
}

type asduDoc struct {
	Type          string   `json:"type"`
	TypeID        int      `json:"typeId"`
	Cot           int      `json:"cot"`
	Negative      bool     `json:"negative"`
	CommonAddress int      `json:"commonAddress"`
	Record        int      `json:"record"`
	Count         int      `json:"count"`
	Objects       []object `json:"objects"`
	Time          string   `json:"time"`
	SU            bool     `json:"su"`
	TimeInvalid   bool     `json:"timeInvalid"`
}

type confirmation struct {
	Cot      int  `json:"cot"`
	Negative bool `json:"negative"`
}

// result is the client result document of the contract.
type result struct {
	OK    bool `json:"ok"`
	Error *struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
	Connected     bool           `json:"connected"`
	Confirmations []confirmation `json:"confirmations"`
	Terminated    bool           `json:"terminated"`
	Session       *struct {
		Login  bool `json:"login"`
		Logout bool `json:"logout"`
	} `json:"session"`
	ASDUs []asduDoc `json:"asdus"`
}

func (r result) code() string {
	if r.Error == nil {
		return "-"
	}
	return r.Error.Code
}

// of returns the ASDUs of a type identification.
func (r result) of(typeID int) []asduDoc {
	var out []asduDoc
	for _, a := range r.ASDUs {
		if a.TypeID == typeID {
			out = append(out, a)
		}
	}
	return out
}

// runClient runs one client operation of the reference against port on this
// host (host.docker.internal in the container) and returns its result and
// exit code.
func runClient(ctx context.Context, a adapter, port int, op []string) (result, int, error) {
	args := []string{"run", "--rm", "--add-host=host.docker.internal:host-gateway", a.image, "client"}
	args = append(args, op...)
	args = append(args, "--host", "host.docker.internal", "--port", strconv.Itoa(port))
	var out, errb bytes.Buffer
	cmd := exec.CommandContext(ctx, "docker", args...)
	cmd.Stdout, cmd.Stderr = &out, &errb
	err := cmd.Run()
	code := 0
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		code = ee.ExitCode()
	} else if err != nil {
		return result{}, -1, err
	}
	var r result
	if err := json.Unmarshal(bytes.TrimSpace(out.Bytes()), &r); err != nil {
		return r, code, fmt.Errorf("result %q (stderr %q): %w", out.String(), errb.String(), err)
	}
	return r, code, nil
}

// knownBug is an entry of the harness's known-bugs.tsv.
type knownBug struct {
	adapter, version string
	match            *regexp.Regexp
	what, id, url    string
}

func knownBugs(t *testing.T) []knownBug {
	t.Helper()
	f, err := os.Open("testdata/known-bugs.tsv")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	var bugs []knownBug
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		col := strings.Split(line, "\t")
		if len(col) != 6 {
			t.Fatalf("known-bugs.tsv: %d columns in %q", len(col), line)
		}
		re, err := regexp.Compile(col[2])
		if err != nil {
			t.Fatalf("known-bugs.tsv: %v", err)
		}
		bugs = append(bugs, knownBug{adapter: col[0], version: col[1], match: re, what: col[3], id: col[4], url: col[5]})
	}
	return bugs
}

// knownDifference skips a test whose failure a known bug of the reference
// explains (never counted as a pass) and reports whether it did.
func knownDifference(t *testing.T, a adapter, op []string, got string) bool {
	t.Helper()
	s := strings.Join(op, " ")
	for _, b := range knownBugs(t) {
		if b.adapter == a.name && b.version == a.caps.Upstream.Version && b.match.MatchString(s) {
			t.Skipf("known difference %s (%s %s): %s; got %s; %s", b.id, b.adapter, b.version, b.what, got, b.url)
			return true
		}
	}
	return false
}
