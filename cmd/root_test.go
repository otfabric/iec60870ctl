// SPDX-License-Identifier: MIT

package cmd

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/otfabric/iec104ctl/internal/station"
	"github.com/otfabric/iec104ctl/internal/stationtest"
	"github.com/otfabric/iec104ctl/internal/types"
	"github.com/spf13/cobra"
)

func TestVersionHelpAndUsage(t *testing.T) {
	run(t).want(t, 0).has(t, "Available Commands", "client", "server", "discover", "completion", "version")
	run(t, "client").want(t, 0).has(t, "interrogate", "monitor", "command")
	run(t, "server").want(t, 0).has(t, "start", "generate-config")
	run(t, "client", "command", "--help").want(t, 0).has(t, "THIS CHANGES THE PROCESS", "IEC104CTL_URL", "--mode")

	run(t, "nope").want(t, 2).errHas(t, `error: unknown command "nope"`)
	run(t, "client", "nope").want(t, 2)
	run(t, "server", "nope").want(t, 2)
	run(t, "client", "interrogate", "--nope").want(t, 2).errHas(t, "unknown flag")
	run(t, "client", "interrogate", "--port", "banana").want(t, 2)
	run(t, "version", "extra").want(t, 2)
}

func TestCompletion(t *testing.T) {
	for shell, marker := range map[string]string{
		"bash":       "__start_iec104ctl",
		"zsh":        "#compdef iec104ctl",
		"fish":       "complete -c iec104ctl",
		"powershell": "Register-ArgumentCompleter",
	} {
		run(t, "completion", shell).want(t, 0).has(t, marker)
	}
	run(t, "completion").want(t, 2)
	run(t, "completion", "tcsh").want(t, 2)

	// Flags with a fixed set of values complete to those values.
	for _, c := range []struct {
		args []string
		want []string
	}{
		{[]string{"client", "interrogate", "--format", ""}, []string{"json", "table", "text"}},
		{[]string{"client", "command", "--type", "set"}, []string{"setpoint-float", "setpoint-normalized", "setpoint-scaled"}},
		{[]string{"client", "command", "--mode", ""}, []string{"cancel", "execute", "sbo", "select"}},
		{[]string{"client", "counters", "--freeze", "f"}, []string{"freeze", "freeze-reset"}},
		{[]string{"client", "monitor", "--type", "M_SP"}, []string{"M_SP_NA_1", "M_SP_TA_1", "M_SP_TB_1"}},
		{[]string{"client", "ping", "--method", ""}, []string{"command", "testfr"}},
		{[]string{"client", "scan", "--method", "s"}, []string{"sweep"}},
		{[]string{"client", "reset", "--qualifier", ""}, []string{"events", "process"}},
		{[]string{"client", "clock", "--timezone", ""}, []string{"utc", "local"}},
		{[]string{"server", "generate-config", "--format", ""}, []string{"yaml", "json"}},
		{[]string{"server", "start", "--roles", ""}, []string{"viewer", "operator"}},
		{[]string{"server", "start", "--ioa-size", ""}, []string{"1", "2", "3"}},
		{[]string{"client", "interrogate", "--cot-size", ""}, []string{"1", "2"}},
		{[]string{"server", "start", "--config", ""}, []string{"yaml", "json", ":8"}},
		{[]string{"client", "interrogate", "--security-cert", ""}, []string{"pem", ":8"}},
		{[]string{"client", "interrogate", "--ip", ""}, []string{":4"}},
		{[]string{"discover", "--format", ""}, []string{"json"}},
		{[]string{"completion", ""}, []string{"bash", "zsh", "fish", "powershell"}},
	} {
		r := run(t, append([]string{cobra.ShellCompNoDescRequestCmd}, c.args...)...).want(t, 0)
		for _, w := range c.want {
			if !strings.Contains("\n"+r.stdout, "\n"+w+"\n") {
				t.Errorf("completion of %v lacks %q:\n%s", c.args, w, r.stdout)
			}
		}
	}
}

// Every flag of every command says which environment variable sets it, and no two flags of
// a command share one.
func TestFlagsDocumentTheirEnvironment(t *testing.T) {
	var walk func(c *cobra.Command)
	walk = func(c *cobra.Command) {
		if c.Name() == "help" || c.Name() == "completion" || strings.HasPrefix(c.Name(), "__") {
			return
		}
		if c.Long == "" && c.Runnable() && c.Name() != "version" {
			t.Errorf("%s has no long description", c.CommandPath())
		}
		if c.Runnable() && !c.HasSubCommands() && c.Example == "" && c.Name() != "version" {
			t.Errorf("%s has no examples", c.CommandPath())
		}
		for _, sub := range c.Commands() {
			walk(sub)
		}
	}
	root := NewRootCmd()
	walk(root)

	r := run(t, "client", "monitor", "--help").want(t, 0)
	seen := map[string]bool{}
	flags := r.stdout[strings.Index(r.stdout, "\nFlags:\n"):strings.Index(r.stdout, "\nGlobal Flags:")]
	for line := range strings.SplitSeq(flags, "\n") {
		i := strings.Index(line, "[env: ")
		if !strings.HasPrefix(strings.TrimSpace(line), "--") || strings.Contains(line, "--help") {
			continue
		}
		if i < 0 {
			t.Errorf("flag without an environment variable: %s", strings.TrimSpace(line))
			continue
		}
		name := line[i+6 : i+strings.Index(line[i:], "]")]
		if !strings.HasPrefix(name, "IEC104CTL_") || seen[name] {
			t.Errorf("environment variable %q is not IEC104CTL_* or is used twice", name)
		}
		seen[name] = true
	}
	if len(seen) < 20 {
		t.Errorf("only %d flags with an environment variable found in the help of client monitor", len(seen))
	}

	// A malformed value is an error before anything runs, whatever the command.
	t.Setenv("IEC104CTL_PORT", "banana")
	run(t, "version").want(t, 3).errHas(t, `error: invalid environment: IEC104CTL_PORT="banana"`)
	defer func() {
		if recover() == nil {
			t.Error("a malformed environment value was accepted")
		}
	}()
	NewRootCmd()
}

func TestDiscover(t *testing.T) {
	st := stationtest.Start(t, stationtest.Model)
	// The station, and a port that is closed.
	ports := strconv.Itoa(int(st.Port)) + "," + strconv.Itoa(int(stationtest.FreePort(t)))

	var res types.DiscoverResult
	run(t, "discover", "--subnets", "127.0.0.1", "--ports", ports, "--timeout", "500", "--format", "json").want(t, 0).
		errHas(t, "discover: 127.0.0.1 port "+strconv.Itoa(int(st.Port))+": iec104").decode(t, &res)
	if res.Hosts != 1 || res.Found != 1 || len(res.Devices) != 1 || res.Devices[0].Status != "iec104" || res.Devices[0].Port != st.Port {
		t.Fatalf("discover: %+v", res)
	}
	run(t, "discover", "--subnets", "127.0.0.1/32,127.0.0.1-1", "--ports", strconv.Itoa(int(st.Port))).want(t, 0).has(t, "1 IEC 60870-5-104 stations among 1 hosts", "iec104")
	run(t, "discover", "--subnets", "127.0.0.1", "--ports", strconv.Itoa(int(st.Port)), "--format", "table").want(t, 0).has(t, "IP         PORT")
	// The probe did not start data transfer.
	if st.HasEvent("started") {
		t.Error("discover started data transfer on the station")
	}

	for _, args := range [][]string{
		{"--subnets", "10.0.0.0/8"},
		{"--subnets", "10.0.0.0/33"},
		{"--subnets", "fd00::/64"},
		{"--subnets", "10.0.0.9-3"},
		{"--subnets", "10.0.0.x-3"},
		{"--subnets", " , "},
		{"--subnets", "127.0.0.1", "--ports", "0"},
		{"--subnets", "127.0.0.1", "--ports", " "},
		{"--subnets", "127.0.0.1", "--parallel", "0"},
		{"--subnets", "127.0.0.1", "--format", "xml"},
	} {
		run(t, append([]string{"discover"}, args...)...).want(t, 3)
	}
	run(t, "discover").want(t, 2)
}

func TestServer(t *testing.T) {
	dir := t.TempDir()

	t.Run("generate-config", func(t *testing.T) {
		r := run(t, "server", "generate-config").want(t, 0).has(t, "# iec104ctl station model", "common_address: 1")
		if _, err := station.Parse([]byte(r.stdout)); err != nil {
			t.Errorf("the generated YAML is not a model: %v", err)
		}
		r = run(t, "server", "generate-config", "--format", "json").want(t, 0)
		m, err := station.Parse([]byte(r.stdout))
		if err != nil || m.Validate() != nil || len(m.Points) != len(station.Demo().Points) {
			t.Errorf("the generated JSON is not the demo model: %v", err)
		}
		path := filepath.Join(dir, "station.yaml")
		run(t, "server", "generate-config", "--output", path).want(t, 0).errHas(t, "wrote "+path)
		run(t, "server", "generate-config", "--output", path).want(t, 3).errHas(t, "pass --force")
		run(t, "server", "generate-config", "--output", path, "--force", "--format", "json").want(t, 0)
		run(t, "server", "generate-config", "--output", filepath.Join(dir, "no", "such", "dir.yaml")).want(t, 10)
		run(t, "server", "generate-config", "--format", "toml").want(t, 3)
		if _, err := station.Load(path); err != nil {
			t.Errorf("the written file does not load: %v", err)
		}
	})

	t.Run("start", func(t *testing.T) {
		model := filepath.Join(dir, "model.yaml")
		if err := os.WriteFile(model, []byte(stationtest.Model), 0o600); err != nil {
			t.Fatal(err)
		}
		port := strconv.Itoa(int(stationtest.FreePort(t)))
		ctx, stop := context.WithCancel(context.Background())
		done := make(chan result, 1)
		go func() {
			done <- runCtx(t, ctx, "--debug", "server", "start", "--config", model, "--listen", "127.0.0.1", "--port", port, "--format", "json", "--t1", "2", "--t2", "1")
		}()
		target := []string{"--ip", "127.0.0.1", "--port", port}
		eventually(t, "the server to answer", func() bool {
			return run(t, append([]string{"client", "ping", "--count", "1", "--timeout", "300"}, target...)...).code == 0
		})
		run(t, append([]string{"client", "interrogate"}, target...)...).want(t, 0).has(t, "8 objects")
		run(t, append([]string{"client", "command", "--type", "single", "--ioa", "5000", "--value", "off"}, target...)...).want(t, 0)
		// A second server cannot take the port.
		run(t, "server", "start", "--listen", "127.0.0.1", "--port", port, "--duration", "1").want(t, 4).errHas(t, "error: cannot listen")
		stop()
		r := (<-done).want(t, 0)
		events := map[string]bool{}
		for _, e := range r.lines(t) {
			events[e["event"].(string)] = true
			if e["timestamp"] == "" {
				t.Errorf("event without timestamp: %v", e)
			}
		}
		for _, want := range []string{"listening", "connected", "started", "interrogation", "command", "disconnected", "stopping"} {
			if !events[want] {
				t.Errorf("the event log has no %q event:\n%s", want, r.stdout)
			}
		}
		r.errHas(t, "U STARTDT act", "C_IC_NA_1")
	})

	t.Run("demo station for a while", func(t *testing.T) {
		port := strconv.Itoa(int(stationtest.FreePort(t)))
		begin := time.Now()
		r := run(t, "server", "start", "--listen", "127.0.0.1", "--port", port, "--duration", "1").want(t, 0)
		r.has(t, "listening", "station 1 (Demo bay)", "stopping")
		if d := time.Since(begin); d < time.Second || d > 10*time.Second {
			t.Errorf("--duration 1 ran for %s", d)
		}
		run(t, "server", "start", "--listen", "127.0.0.1", "--port", port, "--duration", "1", "--format", "table").want(t, 0).has(t, "TIMESTAMP", "EVENT")
	})

	t.Run("TLS and allow-list", func(t *testing.T) {
		certs := stationtest.NewCerts(t)
		port := strconv.Itoa(int(stationtest.FreePort(t)))
		ctx, stop := context.WithCancel(context.Background())
		done := make(chan result, 1)
		go func() {
			done <- runCtx(t, ctx, "server", "start", "--listen", "127.0.0.1", "--port", port, "--tls-cert", certs.ServerCert, "--tls-key", certs.ServerKey,
				"--tls-client-ca", certs.CA, "--allow", "127.0.0.1,10.0.0.0/8", "--max-sessions", "4")
		}()
		url := "tls://127.0.0.1:" + port
		eventually(t, "the TLS server to answer", func() bool {
			return run(t, "client", "ping", "--count", "1", "--timeout", "300", "--url", url, "--tls-ca", certs.CA, "--tls-cert", certs.ClientCert, "--tls-key", certs.ClientKey).code == 0
		})
		run(t, "client", "interrogate", "--url", url, "--tls-ca", certs.CA, "--timeout", "1500").want(t, 4)
		stop()
		(<-done).want(t, 0)

		// A client that is not on the allow-list is turned away before any exchange.
		ctx, stop = context.WithCancel(context.Background())
		go func() {
			done <- runCtx(t, ctx, "server", "start", "--listen", "127.0.0.1", "--port", port, "--allow", "192.0.2.1")
		}()
		eventually(t, "the server to listen", func() bool {
			conn, err := net.DialTimeout("tcp", "127.0.0.1:"+port, 200*time.Millisecond)
			if err == nil {
				_ = conn.Close()
			}
			return err == nil
		})
		run(t, "client", "ping", "--count", "1", "--timeout", "500", "--ip", "127.0.0.1", "--port", port).want(t, 4)
		stop()
		(<-done).want(t, 0).has(t, "refused", "address not allowed")
	})

	t.Run("invalid", func(t *testing.T) {
		bad := filepath.Join(dir, "bad.yaml")
		_ = os.WriteFile(bad, []byte("station: {common_address: 0}\npoints: []\n"), 0o600)
		run(t, "server", "start", "--config", bad).want(t, 3).errHas(t, "invalid model", "common_address 0", "at least one point")
		run(t, "server", "start", "--config", filepath.Join(dir, "missing.yaml")).want(t, 3)
		run(t, "server", "start", "--tls-cert", "x.pem").want(t, 3)
		run(t, "server", "start", "--tls-client-ca", "x.pem").want(t, 3)
		run(t, "server", "start", "--tls-cert", bad, "--tls-key", bad).want(t, 3)
		run(t, "server", "start", "--allow", "everyone").want(t, 3)
		run(t, "server", "start", "--k", "1", "--w", "5").want(t, 3)
		run(t, "server", "start", "--ca-size", "3").want(t, 3)
		run(t, "server", "start", "--format", "xml").want(t, 3)
	})
}
