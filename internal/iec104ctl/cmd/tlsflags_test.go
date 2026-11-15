// SPDX-License-Identifier: MIT

package cmd

import (
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/otfabric/iec60870ctl/internal/iec104ctl/station"
	"github.com/otfabric/iec60870ctl/internal/iec104ctl/stationtest"
)

// countingListener is a TCP listener that accepts and closes every connection made to it.
// noneConnected fails the test when a connection other than its own barrier was made
// before it is called: the listener accepts the connections in the order they were made,
// so every connection of an earlier run comes before the barrier.
func countingListener(t *testing.T) (host, port string, noneConnected func(what string)) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	accepted := make(chan string, 64)
	go func() {
		defer close(accepted)
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			accepted <- c.RemoteAddr().String()
			_ = c.Close()
		}
	}()
	t.Cleanup(func() { _ = ln.Close() })
	host, port, _ = net.SplitHostPort(ln.Addr().String())
	return host, port, func(what string) {
		t.Helper()
		barrier, err := net.Dial("tcp", ln.Addr().String())
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = barrier.Close() }()
		for remote := range accepted {
			if remote == barrier.LocalAddr().String() {
				return
			}
			t.Errorf("%s connected (from %s)", what, remote)
		}
	}
}

// A TLS flag given with a tcp:// URL, on the command line or by its variable, is refused
// with a usage error (exit 2) that names the flags and the tls:// form, before any
// connection, by every client command.
func TestTLSFlagsWithATCPURL(t *testing.T) {
	host, port, noneConnected := countingListener(t)
	addr := net.JoinHostPort(host, port)
	url := "tcp://" + addr
	wantMsg := func(flags string) string {
		return flags + " cannot be used with the URL " + url + ": a tcp:// URL connects without TLS; give a tls:// URL (tls://" + addr + ")"
	}
	for _, c := range []struct {
		name  string
		args  []string
		env   map[string]string
		flags string
	}{
		{"--tls", []string{"--tls"}, nil, "--tls"},
		{"--tls-ca", []string{"--tls-ca", "ca.pem"}, nil, "--tls-ca"},
		{"--tls-insecure", []string{"--tls-insecure"}, nil, "--tls-insecure"},
		{"every TLS flag", []string{"--tls", "--tls-ca", "ca.pem", "--tls-cert", "c.pem", "--tls-key", "c.key", "--tls-server-name", "rtu", "--tls-insecure"}, nil,
			"--tls, --tls-ca, --tls-cert, --tls-key, --tls-server-name, --tls-insecure"},
		{"IEC104CTL_TLS", nil, map[string]string{"IEC104CTL_TLS": "true"}, "--tls"},
		{"IEC104CTL_TLS_CA", nil, map[string]string{"IEC104CTL_TLS_CA": "ca.pem"}, "--tls-ca"},
		{"IEC104CTL_TLS_CERT and _KEY", nil, map[string]string{"IEC104CTL_TLS_CERT": "c.pem", "IEC104CTL_TLS_KEY": "c.key"}, "--tls-cert, --tls-key"},
		{"IEC104CTL_TLS_SERVER_NAME", nil, map[string]string{"IEC104CTL_TLS_SERVER_NAME": "rtu"}, "--tls-server-name"},
		{"IEC104CTL_TLS_INSECURE", nil, map[string]string{"IEC104CTL_TLS_INSECURE": "true"}, "--tls-insecure"},
	} {
		t.Run(c.name, func(t *testing.T) {
			for k, v := range c.env {
				t.Setenv(k, v)
			}
			args := append([]string{"client", "read", "--url", url, "--ioa", "100"}, c.args...)
			run(t, args...).want(t, 2).errHas(t, wantMsg(c.flags))
		})
	}

	// Every command that connects refuses, before it connects.
	dir := t.TempDir()
	in := filepath.Join(dir, "in.bin")
	if err := os.WriteFile(in, []byte("content"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"interrogate"},
		{"counters"},
		{"read", "--ioa", "100"},
		{"monitor", "--interrogate", "--count", "1", "--duration", "1"},
		{"command", "--type", "single", "--ioa", "5000", "--value", "on"},
		{"clock"},
		{"reset", "--qualifier", "events"},
		{"ping", "--count", "1"},
		{"scan"},
		{"file", "list"},
		{"file", "get", "--ioa", "1", "--name", "1", "--output", filepath.Join(dir, "out.bin")},
		{"file", "put", "--ioa", "1", "--name", "1", "--input", in},
	} {
		t.Run(strings.Join(args[:min(2, len(args))], " "), func(t *testing.T) {
			full := append(append([]string{"client"}, args...), "--url", url, "--tls-insecure")
			run(t, full...).want(t, 2).errHas(t, wantMsg("--tls-insecure"))
		})
	}

	t.Run("json", func(t *testing.T) {
		r := run(t, "client", "read", "--url", url, "--ioa", "100", "--tls", "--format", "json").want(t, 2)
		if kind, code := r.errorCode(t); kind != "usage" || code != "invalid_flag_combination" {
			t.Errorf("error %s/%s, want usage/invalid_flag_combination", kind, code)
		}
	})

	noneConnected("a command with a TLS flag and a tcp:// URL")
}

// Without TLS flags a tcp:// URL connects over plain TCP; with --ip the TLS flags turn TLS
// on, and with a tls:// URL they configure it: none of these is refused.
func TestTLSFlagsNotRefused(t *testing.T) {
	st := stationtest.Start(t, stationtest.Model)
	run(t, "client", "read", "--url", "tcp://"+st.Addr(), "--ioa", "100").want(t, 0).has(t, "at tcp://"+st.Addr())
	cli(t, st, "read", "--ioa", "100").want(t, 0)

	certs := stationtest.NewCerts(t)
	tst := stationtest.Start(t, stationtest.Model, func(o *station.Options) { o.TLS = certs.ServerTLS })
	run(t, "client", "read", "--ip", tst.Host, "--port", strconv.Itoa(int(tst.Port)), "--tls-ca", certs.CA, "--tls-cert", certs.ClientCert,
		"--tls-key", certs.ClientKey, "--tls-server-name", "localhost", "--ioa", "100").want(t, 0).has(t, "at tls://"+tst.Addr())
	run(t, "client", "read", "--url", "tls://"+tst.Addr(), "--tls-ca", certs.CA, "--tls-cert", certs.ClientCert,
		"--tls-key", certs.ClientKey, "--ioa", "100").want(t, 0).has(t, "at tls://"+tst.Addr())
}
