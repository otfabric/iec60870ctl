// SPDX-License-Identifier: MIT

//go:build interop

package interop

import (
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

// The references: the IEC 101 images of otfabric/iec60870-5-interop v0.5.0 (contract
// 2.3), pinned by the same multi-arch index digests as go-iec60870-5's
// iec101/interop/harness.go.
var stacks = []struct{ name, env, image string }{
	{"lib60870", "IEC101_INTEROP_LIB60870_IMAGE", "ghcr.io/otfabric/iec60870-5-interop-iec101-lib60870@sha256:2b256a3c7c3cb1b9920029f078266148bd9126e8f892ab73efda347c156aef72"},
	{"cbsigma", "IEC101_INTEROP_CBSIGMA_IMAGE", "ghcr.io/otfabric/iec60870-5-interop-iec101-cbsigma@sha256:b781f7ae94d49ff7654f63753e8098ea73e6cd4c00775512f8e429ea8000f47f"},
	{"digitalpetri", "IEC101_INTEROP_DIGITALPETRI_IMAGE", "ghcr.io/otfabric/iec60870-5-interop-iec101-digitalpetri@sha256:7cd73b01d8b67e49366330e8898a35ca51a13364b306b284aa45e7bd675fa3eb"},
}

var modes = []string{"unbalanced", "balanced"}

type adapter struct {
	name, image string
	caps        capabilities
}

type capabilities struct {
	Standard         string          `json:"standard"`
	LinkModes        []string        `json:"linkModes"`
	LinkAddressSizes []int           `json:"linkAddressSizes"`
	CotSizes         []int           `json:"cotSizes"`
	CaSizes          []int           `json:"caSizes"`
	IoaSizes         []int           `json:"ioaSizes"`
	Features         map[string]bool `json:"features"`
	Upstream         struct {
		Name, Version string
	} `json:"upstream"`
}

func require() bool { return os.Getenv("IEC101_REQUIRE_INTEROP") == "1" }

func skipOrFail(t *testing.T, format string, args ...any) {
	t.Helper()
	if require() {
		t.Fatalf(format, args...)
	}
	t.Skipf(format, args...)
}

// bin returns the built iec101ctl (make interop sets INTEROP_BIN_DIR).
func bin(t *testing.T) string {
	t.Helper()
	dir := os.Getenv("INTEROP_BIN_DIR")
	if dir == "" {
		skipOrFail(t, "INTEROP_BIN_DIR is not set; run make interop")
	}
	return filepath.Join(dir, "iec101ctl")
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
		only := os.Getenv("IEC101_INTEROP_ADAPTERS")
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
			if a.caps.Standard != "iec60870-5-101" {
				adapterErr = fmt.Errorf("%s is %q, not an IEC 101 image", img, a.caps.Standard)
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

// fields are the field sizes and the link address of a run (contract 2.3).
type fields struct {
	addrSize, cot, ca, ioa int
	linkAddr               int
}

func defaultFields() fields { return fields{addrSize: 1, cot: 1, ca: 1, ioa: 2, linkAddr: 1} }

// flags are the link flags of f, the same for the image and for iec101ctl.
func (f fields) flags(mode string) []string {
	return []string{"--link-mode", mode, "--link-address", strconv.Itoa(f.linkAddr), "--link-address-size", strconv.Itoa(f.addrSize),
		"--cot-size", strconv.Itoa(f.cot), "--ca-size", strconv.Itoa(f.ca), "--ioa-size", strconv.Itoa(f.ioa)}
}

// fieldSizeRuns are the non-default field sizes of go-iec60870-5's iec101/interop/fields.go.
var fieldSizeRuns = []struct {
	name string
	f    fields
}{
	{"2223", fields{addrSize: 2, cot: 2, ca: 2, ioa: 3, linkAddr: 300}},
	{"1213", fields{addrSize: 1, cot: 2, ca: 1, ioa: 3, linkAddr: 1}},
	{"2122", fields{addrSize: 2, cot: 1, ca: 2, ioa: 2, linkAddr: 1}},
	{"0112", fields{addrSize: 0, cot: 1, ca: 1, ioa: 2, linkAddr: 0}},
}

func flagValue(op []string, flag string, def int) int {
	for i := 0; i+1 < len(op); i++ {
		if op[i] == flag {
			if n, err := strconv.Atoi(op[i+1]); err == nil {
				return n
			}
		}
	}
	return def
}

var linkFlagNames = []string{"--link-address", "--link-address-size", "--cot-size", "--ca-size", "--ioa-size"}

// caseFields returns the field sizes of a case's operation and the key of the server those
// flags need ("" for the defaults).
func caseFields(op []string) (fields, string) {
	d := defaultFields()
	f := fields{
		addrSize: flagValue(op, "--link-address-size", d.addrSize),
		cot:      flagValue(op, "--cot-size", d.cot),
		ca:       flagValue(op, "--ca-size", d.ca),
		ioa:      flagValue(op, "--ioa-size", d.ioa),
		linkAddr: flagValue(op, "--link-address", d.linkAddr),
	}
	var key []string
	for i := 0; i+1 < len(op); i++ {
		if slices.Contains(linkFlagNames, op[i]) {
			key = append(key, op[i], op[i+1])
		}
	}
	return f, strings.Join(key, " ")
}

func sizesOf(declared []int, def int) []int {
	if declared == nil {
		return []int{def}
	}
	return declared
}

// takes reports whether an image takes the field sizes of f in the link mode.
func (a adapter) takes(f fields, mode string) bool {
	d := defaultFields()
	if f.addrSize == 0 && mode != "balanced" {
		return false
	}
	return slices.Contains(sizesOf(a.caps.LinkAddressSizes, d.addrSize), f.addrSize) &&
		slices.Contains(sizesOf(a.caps.CotSizes, d.cot), f.cot) &&
		slices.Contains(sizesOf(a.caps.CaSizes, d.ca), f.ca) &&
		slices.Contains(sizesOf(a.caps.IoaSizes, d.ioa), f.ioa)
}

// clientCanSend reports whether the image's client can send the case's request (an
// unbalanced client without unbalancedCommonAddress sends only to the common address equal
// to the link address).
func (a adapter) clientCanSend(op []string, f fields, mode string) bool {
	if mode != "unbalanced" || len(op) == 0 || op[0] == "connect" || op[0] == "monitor" {
		return true
	}
	if a.caps.Features["unbalancedCommonAddress"] {
		return true
	}
	return flagValue(op, "--common-address", 1) == f.linkAddr
}

// knownBug returns the known bug of the image's upstream version in a role that may
// explain a mismatch, from testdata/known-bugs.tsv (copied from the harness v0.5.0).
func knownBug(t *testing.T, a adapter, role, mode string, op []string) string {
	t.Helper()
	raw, err := os.ReadFile("testdata/known-bugs.tsv")
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(string(raw), "\n") {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		col := strings.Split(line, "\t")
		if len(col) < 4 {
			continue
		}
		who, only, scoped := strings.Cut(col[0], "@")
		if who != a.name || scoped && only != role || col[1] != a.caps.Upstream.Version {
			continue
		}
		if re, err := regexp.Compile(col[2]); err == nil && re.MatchString(strings.Join(op, " ")+" --link-mode "+mode) {
			return col[3]
		}
	}
	return ""
}

// fixture is the baseline fixture as the image prints it.
type fixture struct {
	Station struct {
		CommonAddress int `json:"commonAddress"`
	} `json:"station"`
	Points []struct {
		IOA      int             `json:"ioa"`
		Type     string          `json:"type"`
		Value    json.RawMessage `json:"value"`
		Quality  []string        `json:"quality"`
		Sequence int             `json:"sequence"`
	} `json:"points"`
	Commands []struct {
		IOA            int    `json:"ioa"`
		Type           string `json:"type"`
		Target         int    `json:"target"`
		ReportCause    int    `json:"reportCause"`
		SelectRequired bool   `json:"selectRequired"`
	} `json:"commands"`
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
	return &f
}

// startImageServer runs the image's server and returns its host port on 127.0.0.1.
func startImageServer(t *testing.T, a adapter, args ...string) int {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	out, err := docker(ctx, append([]string{"run", "-d", "-p", "127.0.0.1::2401", a.image, "server"}, args...)...)
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

// result is the client result document of the contract.
type result struct {
	OK    bool `json:"ok"`
	Error *struct {
		Code string `json:"code"`
	} `json:"error"`
	Confirmations []struct {
		Cot      int  `json:"cot"`
		Negative bool `json:"negative"`
	} `json:"confirmations"`
	Terminated bool `json:"terminated"`
	ASDUs      []struct {
		Type   string `json:"type"`
		TypeID int    `json:"typeId"`
		Cot    int    `json:"cot"`
	} `json:"asdus"`
}

// summary renders a result as cases.tsv states it.
func (r result) summary() string {
	code := "-"
	if r.Error != nil && r.Error.Code != "" {
		code = r.Error.Code
	}
	var conf, asdus []string
	for _, c := range r.Confirmations {
		s := strconv.Itoa(c.Cot)
		if c.Negative {
			s += "n"
		}
		conf = append(conf, s)
	}
	for _, a := range r.ASDUs {
		typ := a.Type
		if typ == "" {
			typ = strconv.Itoa(a.TypeID)
		}
		asdus = append(asdus, fmt.Sprintf("%s/%d", typ, a.Cot))
	}
	return fmt.Sprintf("ok=%t error=%s conf=%s term=%t asdus=%s", r.OK, code, strings.Join(conf, ","), r.Terminated, strings.Join(asdus, ","))
}

// runImageClient runs one client operation of an image against port on the host
// (host.docker.internal) and returns its result document and exit code.
func runImageClient(ctx context.Context, a adapter, port int, mode string, op []string) (result, int, error) {
	args := []string{"run", "--rm", "--add-host=host.docker.internal:host-gateway", a.image, "client"}
	args = append(args, op...)
	args = append(args, "--host", "host.docker.internal", "--port", strconv.Itoa(port), "--link-mode", mode)
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

// cli runs iec101ctl with args and returns stdout, stderr and the exit code.
func cli(t *testing.T, args ...string) (string, string, int) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	var out, errb bytes.Buffer
	cmd := exec.CommandContext(ctx, bin(t), args...)
	cmd.Stdout, cmd.Stderr = &out, &errb
	err := cmd.Run()
	var ee *exec.ExitError
	switch {
	case err == nil:
		return out.String(), errb.String(), 0
	case errors.As(err, &ee):
		return out.String(), errb.String(), ee.ExitCode()
	}
	t.Fatalf("run %s: %v", bin(t), err)
	return "", "", -1
}
