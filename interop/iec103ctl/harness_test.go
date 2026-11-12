//go:build interop

// SPDX-License-Identifier: MIT

// Package interop runs the built iec103ctl against the IEC 60870-5-103 images of
// otfabric/iec60870-5-interop v0.4.0 (contract 2.3), pinned by the digests of
// go-iec60870-5's iec103/interop/harness.go: `iec103ctl client` against the one reference
// server (vitusdong), and every reference client (vitusdong, ariec103, open103, goiecp5)
// against `iec103ctl server start` serving the reference's own baseline fixture.
//
// Expected values come from the images (print-fixture) and the harness's case table and
// known bugs (testdata/cases.tsv, known-bugs.tsv, known-bugs-consumer.tsv, copied from the
// harness release). A pairing whose failure a known bug of the reference explains is
// skipped with the bug id; it never counts as a pass.
//
// Environment:
//
//	INTEROP_BIN_DIR               the directory of the built iec103ctl (set by make interop)
//	IEC103_INTEROP_<STACK>_IMAGE  image of a stack (VITUSDONG, ARIEC103, OPEN103, GOIECP5); empty drops it
//	IEC103_INTEROP_ADAPTERS       comma-separated subset of stacks
//	IEC103_REQUIRE_INTEROP=1      fail instead of skip when Docker, an image or the binary is missing
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
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// The references: the IEC 103 images of otfabric/iec60870-5-interop v0.4.0, pinned by
// multi-arch index digest, the same as go-iec60870-5 v0.4.1 iec103/interop/harness.go.
var stacks = []struct{ name, env, image string }{
	{"vitusdong", "IEC103_INTEROP_VITUSDONG_IMAGE", "ghcr.io/otfabric/iec60870-5-interop-iec103-vitusdong@sha256:866a1d43eb9cedbdb79691fa6becb63cb71febe096e7107a98b50e4067393e08"},
	{"ariec103", "IEC103_INTEROP_ARIEC103_IMAGE", "ghcr.io/otfabric/iec60870-5-interop-iec103-ariec103@sha256:3b847f31d2e321e1a26309d619c540baaae2da480b7fd31d761f428691a5eb6e"},
	{"open103", "IEC103_INTEROP_OPEN103_IMAGE", "ghcr.io/otfabric/iec60870-5-interop-iec103-open103@sha256:8e848d426d1b98542810dbf06f63f78c54a40334dd72003da0ef07ae7d322ee0"},
	{"goiecp5", "IEC103_INTEROP_GOIECP5_IMAGE", "ghcr.io/otfabric/iec60870-5-interop-iec103-goiecp5@sha256:c429dd18938e1ce046231d476ec9d69c4edd3626cf99231512f7e0fae2439598"},
}

type adapter struct {
	name, image string
	caps        capabilities
}

type capabilities struct {
	Standard string `json:"standard"`
	Roles    struct {
		Server bool `json:"server"`
		Client bool `json:"client"`
	} `json:"roles"`
	Features map[string]bool `json:"features"`
	Upstream struct {
		Name, Version string
	} `json:"upstream"`
}

func require() bool { return os.Getenv("IEC103_REQUIRE_INTEROP") == "1" }

// skipOrFail skips for missing infrastructure, or fails with IEC103_REQUIRE_INTEROP=1.
func skipOrFail(t *testing.T, format string, args ...any) {
	t.Helper()
	if require() {
		t.Fatalf(format, args...)
	}
	t.Skipf(format, args...)
}

// binary is the iec103ctl that make interop built.
func binary(t *testing.T) string {
	t.Helper()
	dir := os.Getenv("INTEROP_BIN_DIR")
	if dir == "" {
		skipOrFail(t, "INTEROP_BIN_DIR is not set; run make interop")
	}
	p := filepath.Join(dir, "iec103ctl")
	if _, err := os.Stat(p); err != nil {
		skipOrFail(t, "no binary: %v", err)
	}
	return p
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
	adaptersOnce sync.Once
	adapterList  []adapter
	adapterErr   error
)

func adapters(t *testing.T) []adapter {
	t.Helper()
	adaptersOnce.Do(func() {
		if _, err := exec.LookPath("docker"); err != nil {
			adapterErr = err
			return
		}
		only := os.Getenv("IEC103_INTEROP_ADAPTERS")
		for _, s := range stacks {
			if only != "" && !slices.Contains(strings.Split(only, ","), s.name) {
				continue
			}
			img := s.image
			if v, ok := os.LookupEnv(s.env); ok {
				img = v
			}
			if img == "" {
				continue
			}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
			out, err := docker(ctx, "run", "--rm", img, "print-capabilities")
			cancel()
			if err != nil {
				adapterErr = err
				return
			}
			a := adapter{name: s.name, image: img}
			if err := json.Unmarshal(out, &a.caps); err != nil {
				adapterErr = fmt.Errorf("%s capabilities: %w", s.name, err)
				return
			}
			if a.caps.Standard != "iec60870-5-103" {
				adapterErr = fmt.Errorf("%s is %q, not an IEC 103 image", img, a.caps.Standard)
				return
			}
			adapterList = append(adapterList, a)
		}
	})
	if adapterErr != nil {
		skipOrFail(t, "references unavailable: %v", adapterErr)
	}
	if len(adapterList) == 0 {
		skipOrFail(t, "no reference selected")
	}
	return adapterList
}

// fixture is the IEC 103 baseline fixture as the reference prints it.
type fixture struct {
	raw     []byte
	Station struct {
		CommonAddress  int `json:"commonAddress"`
		Identification struct {
			Fun      int    `json:"fun"`
			Col      int    `json:"col"`
			ASCII    string `json:"ascii"`
			Software string `json:"software"`
		} `json:"identification"`
	} `json:"station"`
	Indications []struct {
		Fun int   `json:"fun"`
		Inf int   `json:"inf"`
		DPI int   `json:"dpi"`
		GI  *bool `json:"gi"`
	} `json:"indications"`
	Measurands []struct {
		Fun    int     `json:"fun"`
		Inf    int     `json:"inf"`
		Type   int     `json:"type"`
		Values []value `json:"values"`
	} `json:"measurands"`
	Generic []struct {
		GIN struct {
			Group int `json:"group"`
			Entry int `json:"entry"`
		} `json:"gin"`
		Description string          `json:"description"`
		Type        int             `json:"type"`
		Size        int             `json:"size"`
		Value       json.RawMessage `json:"value"`
		Writable    bool            `json:"writable"`
	} `json:"generic"`
	Disturbances []fixtureDisturbance `json:"disturbances"`
}

type value struct {
	Value int  `json:"value"`
	OV    bool `json:"ov"`
	ER    bool `json:"er"`
}

type tag struct {
	Pos *int `json:"pos"`
	Fun int  `json:"fun"`
	Inf int  `json:"inf"`
	DPI int  `json:"dpi"`
}

type fixtureDisturbance struct {
	FAN int `json:"fan"`
	SOF struct {
		TP   bool `json:"tp"`
		TM   bool `json:"tm"`
		Test bool `json:"test"`
		OTEV bool `json:"otev"`
	} `json:"sof"`
	Time     string `json:"time"`
	NOF      int    `json:"nof"`
	Interval int    `json:"interval"`
	Start    string `json:"start"`
	Tags     []tag  `json:"tags"`
	Channels []struct {
		ACC    int     `json:"acc"`
		RPV    float64 `json:"rpv"`
		RSV    float64 `json:"rsv"`
		RFA    float64 `json:"rfa"`
		Values []int   `json:"values"`
	} `json:"channels"`
}

func loadFixture(t *testing.T, a adapter) *fixture {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	out, err := docker(ctx, "run", "--rm", a.image, "print-fixture", "baseline")
	if err != nil {
		t.Fatal(err)
	}
	f := &fixture{raw: out}
	if err := json.Unmarshal(out, f); err != nil {
		t.Fatal(err)
	}
	if len(f.Indications) == 0 {
		t.Fatalf("%s: fixture without indications: %s", a.name, out)
	}
	return f
}

// knownBug is an entry of known-bugs.tsv or known-bugs-consumer.tsv.
type knownBug struct {
	adapter, version string
	match            *regexp.Regexp
	what, id, url    string
}

// clientBugs are the entries that describe the client of the adapter; the others describe
// its server. A server bug is no excuse for a reference client against iec103ctl's
// server, nor a client bug for iec103ctl's client against a reference server.
var clientBugs = map[string]bool{
	"ariec103-clock-sync-cp32":        true,
	"ariec103-command-no-rii":         true,
	"open103-command-typical-fun":     true,
	"vitusdong-generic-command-inf":   true,
	"vitusdong-sof-bits":              true,
	"vitusdong-disturbance-procedure": true,
	"open103-asdu30-nfe":              true,
}

func knownBugs(t *testing.T) []knownBug {
	t.Helper()
	var bugs []knownBug
	for _, name := range []string{"testdata/known-bugs.tsv", "testdata/known-bugs-consumer.tsv"} {
		f, err := os.Open(name)
		if err != nil {
			t.Fatal(err)
		}
		sc := bufio.NewScanner(f)
		for sc.Scan() {
			line := sc.Text()
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			col := strings.Split(line, "\t")
			if len(col) != 6 {
				t.Fatalf("%s: %d columns in %q", name, len(col), line)
			}
			re, err := regexp.Compile(col[2])
			if err != nil {
				t.Fatalf("%s: %v", name, err)
			}
			bugs = append(bugs, knownBug{adapter: col[0], version: col[1], match: re, what: col[3], id: col[4], url: col[5]})
		}
		_ = f.Close()
	}
	return bugs
}

// knownBugOf returns the known bug of reference a in the given role (client true) that
// explains a failure of the operation op.
func knownBugOf(t *testing.T, a adapter, client bool, op string) (knownBug, bool) {
	t.Helper()
	for _, b := range knownBugs(t) {
		if b.adapter == a.name && b.version == a.caps.Upstream.Version && clientBugs[b.id] == client && b.match.MatchString(op) {
			return b, true
		}
	}
	return knownBug{}, false
}

// knownDifference ends a test whose failure a known bug of the reference explains:
// skipped with the bug id, never counted as a pass, whatever IEC103_REQUIRE_INTEROP says.
func knownDifference(t *testing.T, b knownBug, got string) {
	t.Helper()
	t.Skipf("known difference %s (%s %s): %s; got %s; %s", b.id, b.adapter, b.version, b.what, got, b.url)
}

// runBinary runs iec103ctl and returns stdout, stderr and the exit code.
func runBinary(t *testing.T, args ...string) (string, string, int) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	var out, errb bytes.Buffer
	cmd := exec.CommandContext(ctx, binary(t), args...)
	cmd.Stdout, cmd.Stderr = &out, &errb
	err := cmd.Run()
	var exit *exec.ExitError
	switch {
	case err == nil:
		return out.String(), errb.String(), 0
	case errors.As(err, &exit):
		return out.String(), errb.String(), exit.ExitCode()
	default:
		t.Fatalf("run %v: %v", args, err)
		return "", "", -1
	}
}

// startReferenceServer runs a reference server and returns its host port.
func startReferenceServer(t *testing.T, a adapter) int {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	out, err := docker(ctx, "run", "-d", "-p", "127.0.0.1::2401", a.image, "server")
	if err != nil {
		t.Fatal(err)
	}
	id := strings.TrimSpace(string(out))
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if t.Failed() {
			logs, _ := docker(ctx, "logs", id)
			t.Logf("%s server log:\n%s", a.name, logs)
		}
		_, _ = docker(ctx, "rm", "-f", id)
	})
	deadline := time.Now().Add(30 * time.Second)
	for {
		if _, err := docker(ctx, "exec", id, "test", "-f", "/run/iec60870-5-interop/ready"); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("%s server not ready", a.name)
		}
		time.Sleep(100 * time.Millisecond)
	}
	out, err = docker(ctx, "port", id, "2401/tcp")
	if err != nil {
		t.Fatal(err)
	}
	line := strings.TrimSpace(strings.Split(string(out), "\n")[0])
	port, err := strconv.Atoi(line[strings.LastIndex(line, ":")+1:])
	if err != nil {
		t.Fatalf("port %q: %v", out, err)
	}
	return port
}

// normalize makes the numbers of single precision compare as the documents write them.
func normalize(v any) any {
	b, _ := json.Marshal(v)
	var out any
	_ = json.Unmarshal(b, &out)
	var walk func(any) any
	walk = func(x any) any {
		switch y := x.(type) {
		case []any:
			for i := range y {
				y[i] = walk(y[i])
			}
		case map[string]any:
			for k := range y {
				y[k] = walk(y[k])
			}
		case float64:
			return float64(float32(y))
		}
		return x
	}
	return walk(out)
}
