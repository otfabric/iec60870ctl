//go:build interop

// SPDX-License-Identifier: MIT

// Package interop runs the built iec104ctl against the IEC 104 reference images of
// otfabric/iec60870-5-interop v0.4.0, in both directions: iec104ctl client against each
// reference server, and each reference client against iec104ctl server start. Run it with
// make interop (Docker required).
package interop

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// The references: the IEC 104 images of otfabric/iec60870-5-interop v0.4.0 (lib60870-C
// v2.4.1, OpenMUC j60870 1.8.0, wendy512/iec104 v1.0.4, cB-Sigma/iec60870 v0.3.0,
// pascaldekloe/part5 v0.3.0), pinned by multi-arch index digest: the digests of the
// release's manifest.json, the same as go-iec60870-5 v0.4.1 iec104/interop/harness.go.
const (
	interopRelease = "v0.4.0"
	readyFile      = "/run/iec60870-5-interop/ready"
)

type adapter struct{ name, env, image string }

var references = []adapter{
	{"lib60870", "IEC104_INTEROP_LIB60870_IMAGE", "ghcr.io/otfabric/iec60870-5-interop-iec104-lib60870@sha256:adda416d5659671e28080bca361a7fb6708aa8dce1877cba8207008049d1bb04"},
	{"openmuc", "IEC104_INTEROP_OPENMUC_IMAGE", "ghcr.io/otfabric/iec60870-5-interop-iec104-openmuc@sha256:a619fc3e15a44cff6793d1d17f6db3f8783fbb0557b79ff46bbb8280ee95e8e5"},
	{"wendy512", "IEC104_INTEROP_WENDY512_IMAGE", "ghcr.io/otfabric/iec60870-5-interop-iec104-wendy512@sha256:03773fceb73cd6a6923209ad81fc5dbdcfff9cf538544b4067f1b21ee5b4a679"},
	{"cbsigma", "IEC104_INTEROP_CBSIGMA_IMAGE", "ghcr.io/otfabric/iec60870-5-interop-iec104-cbsigma@sha256:f25d56f35badd81134813c6098ecf0cf63e6e8c9aec69f9e404f77cd2f626506"},
	{"part5", "IEC104_INTEROP_PART5_IMAGE", "ghcr.io/otfabric/iec60870-5-interop-iec104-part5@sha256:6ff105c63340053a8365ce70373f0a72c514a847003046d48208a0df3f591ea6"},
}

// require makes a missing prerequisite a failure instead of a skip (CI sets it).
func require() bool { return os.Getenv("IEC104_REQUIRE_INTEROP") == "1" }

func skipOrFail(t testing.TB, format string, args ...any) {
	t.Helper()
	if require() {
		t.Fatalf(format, args...)
	}
	t.Skipf(format+" (set IEC104_REQUIRE_INTEROP=1 to fail instead)", args...)
}

// binary is the iec104ctl that make interop built.
func binary(t testing.TB) string {
	t.Helper()
	dir := os.Getenv("INTEROP_BIN_DIR")
	if dir == "" {
		skipOrFail(t, "INTEROP_BIN_DIR is not set; run make interop")
	}
	return filepath.Join(dir, "iec104ctl")
}

// adapters returns the references, with IEC104_INTEROP_<NAME>_IMAGE overriding an image
// (empty leaves it out) and IEC104_INTEROP_ADAPTERS selecting a subset.
func adapters(t testing.TB) []adapter {
	t.Helper()
	if _, err := exec.LookPath("docker"); err != nil {
		skipOrFail(t, "docker not found: %v", err)
	}
	only := os.Getenv("IEC104_INTEROP_ADAPTERS")
	var out []adapter
	for _, a := range references {
		if only != "" && !strings.Contains(","+only+",", ","+a.name+",") {
			continue
		}
		if v, ok := os.LookupEnv(a.env); ok {
			a.image = v
		}
		if a.image != "" {
			out = append(out, a)
		}
	}
	if len(out) == 0 {
		skipOrFail(t, "no reference selected")
	}
	return out
}

// capabilities is the part of the document print-capabilities prints that the tests use.
type capabilities struct {
	AdapterVersion string `json:"adapterVersion"`
	Standard       string `json:"standard"`
	Upstream       struct {
		Name    string `json:"name"`
		Version string `json:"version"`
	} `json:"upstream"`
	Roles struct {
		Server bool `json:"server"`
		Client bool `json:"client"`
	} `json:"roles"`
	Features map[string]bool `json:"features"`
}

var capsCache sync.Map

func (a adapter) caps(t testing.TB) *capabilities {
	t.Helper()
	if c, ok := capsCache.Load(a.image); ok {
		return c.(*capabilities)
	}
	out, stderr, err := docker("run", "--rm", a.image, "print-capabilities")
	if err != nil {
		skipOrFail(t, "print-capabilities of %s: %v\n%s", a.image, err, stderr)
	}
	c := &capabilities{}
	if err := json.Unmarshal([]byte(out), c); err != nil {
		t.Fatalf("capabilities of %s: %v", a.image, err)
	}
	if c.Standard != "iec60870-5-104" {
		t.Fatalf("%s states the standard %q, not iec60870-5-104", a.image, c.Standard)
	}
	capsCache.Store(a.image, c)
	return c
}

// knownReadBug: j60870 1.8.0 cannot decode C_RD_NA_1 and closes the connection on one
// (iec60870-5-interop tests/iec104/known-bugs.tsv).
func (a adapter) knownReadBug(t testing.TB) bool {
	c := a.caps(t)
	return c.Upstream.Name == "j60870" && c.Upstream.Version == "1.8.0"
}

func docker(args ...string) (stdout, stderr string, err error) {
	var out, errb bytes.Buffer
	cmd := exec.Command("docker", args...)
	cmd.Stdout, cmd.Stderr = &out, &errb
	err = cmd.Run()
	return out.String(), errb.String(), err
}

var seq atomic.Int64

// refServer is a running reference server container, published on a loopback port.
type refServer struct {
	name string
	port int
}

func startServer(t testing.TB, a adapter) *refServer {
	t.Helper()
	port := freePort(t)
	name := fmt.Sprintf("iec104ctl-interop-%s-%d-%d", a.name, os.Getpid(), seq.Add(1))
	if _, stderr, err := docker("run", "-d", "--name", name, "-p", fmt.Sprintf("127.0.0.1:%d:2404", port), a.image, "server"); err != nil {
		t.Fatalf("start %s server: %v\n%s", a.name, err, stderr)
	}
	t.Cleanup(func() {
		if t.Failed() {
			out, errOut, _ := docker("logs", "--tail", "60", name)
			t.Logf("%s server log:\n%s%s", a.name, out, errOut)
		}
		_, _, _ = docker("rm", "-f", name)
	})
	deadline := time.Now().Add(60 * time.Second)
	for {
		if _, _, err := docker("exec", name, "test", "-f", readyFile); err == nil {
			return &refServer{name: name, port: port}
		}
		if time.Now().After(deadline) {
			t.Fatalf("%s server not ready after 60 s", a.name)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// result is the document a reference client prints (container contract).
type result struct {
	OK        bool   `json:"ok"`
	Operation string `json:"operation"`
	Connected bool   `json:"connected"`
	Error     *struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
	ASDUs []struct {
		Type    string           `json:"type"`
		Cot     int              `json:"cot"`
		Objects []map[string]any `json:"objects"`
	} `json:"asdus"`
	exit int
}

func (r *result) objects(typ string) int {
	n := 0
	for _, a := range r.ASDUs {
		if typ == "" || a.Type == typ {
			n += len(a.Objects)
		}
	}
	return n
}

// runClient runs one operation of the reference client of a against iec104ctl listening
// on the host at port: the container reaches the host through the gateway alias.
func runClient(t testing.TB, a adapter, port string, args ...string) *result {
	t.Helper()
	cmd := append([]string{"run", "--rm", "--add-host=host.docker.internal:host-gateway", a.image, "client"}, args...)
	cmd = append(cmd, "--host", "host.docker.internal", "--port", port)
	stdout, stderr, err := docker(cmd...)
	r := &result{}
	var exitErr *exec.ExitError
	switch {
	case err == nil:
	case errors.As(err, &exitErr):
		r.exit = exitErr.ExitCode()
	default:
		t.Fatalf("run %s client: %v", a.name, err)
	}
	line := strings.TrimSpace(stdout)
	if line == "" || strings.Contains(line, "\n") {
		t.Fatalf("%s client %v: want one result line, got %q (exit %d, stderr %q)", a.name, args, stdout, r.exit, stderr)
	}
	if err := json.Unmarshal([]byte(line), r); err != nil {
		t.Fatalf("%s client result: %v\n%s", a.name, err, line)
	}
	return r
}

// ctl runs the iec104ctl binary and returns stdout and the exit code.
func ctl(t testing.TB, args ...string) (string, int) {
	t.Helper()
	var out, errb bytes.Buffer
	cmd := exec.Command(binary(t), args...)
	cmd.Stdout, cmd.Stderr = &out, &errb
	err := cmd.Run()
	var exitErr *exec.ExitError
	switch {
	case err == nil:
		return out.String(), 0
	case errors.As(err, &exitErr):
		if errb.Len() > 0 {
			t.Logf("iec104ctl %v stderr:\n%s", args, errb.String())
		}
		return out.String(), exitErr.ExitCode()
	default:
		t.Fatalf("run iec104ctl: %v", err)
		return "", -1
	}
}

func freePort(t testing.TB) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ln.Close() }()
	return ln.Addr().(*net.TCPAddr).Port
}

// TestReferences checks that every pinned image is the release it is pinned as.
func TestReferences(t *testing.T) {
	for _, a := range adapters(t) {
		c := a.caps(t)
		t.Logf("%s: iec60870-5-interop %s, %s %s", a.name, c.AdapterVersion, c.Upstream.Name, c.Upstream.Version)
		if strings.Contains(a.image, "@sha256:") && c.AdapterVersion != interopRelease {
			t.Errorf("%s is pinned as %s but reports %s", a.name, interopRelease, c.AdapterVersion)
		}
		if !c.Roles.Server || !c.Roles.Client {
			t.Errorf("%s does not provide both roles", a.name)
		}
	}
}
