// SPDX-License-Identifier: MIT

package cmd

import (
	"strings"
	"testing"
)

func TestVersionHelpAndUsage(t *testing.T) {
	run(t).want(t, 0).has(t, "Available Commands", "client", "server", "discover", "completion", "version")
	run(t, "client").want(t, 0).has(t, "interrogate", "monitor", "command", "delay")
	run(t, "server").want(t, 0).has(t, "start", "generate-config")
	run(t, "client", "command", "--help").want(t, 0).has(t, "THIS CHANGES THE PROCESS", "IEC101CTL_URL", "--link-mode")

	run(t, "nope").want(t, 2).errHas(t, `error: unknown command "nope"`)
	run(t, "client", "nope").want(t, 2)
	run(t, "server", "nope").want(t, 2)
	run(t, "client", "interrogate", "--nope").want(t, 2).errHas(t, "unknown flag")
	run(t, "client", "interrogate", "--port", "banana").want(t, 2)
	run(t, "version", "extra").want(t, 2)
	// IEC 104 flags do not exist here.
	run(t, "client", "interrogate", "--t1", "2").want(t, 2)
	run(t, "client", "command", "--time-tag").want(t, 2)
}

func TestVersion(t *testing.T) {
	SetBuildMeta("1.2.3", "iec101ctl/v1.2.3", "abc", "2026-11-12T00:00:00Z")
	t.Cleanup(func() { SetBuildMeta("", "", "", "") })
	run(t, "version").want(t, 0).has(t, "iec101ctl 1.2.3 (")
	run(t, "version", "--json").want(t, 0).has(t, `"name":"iec101ctl"`, `"version":"1.2.3"`)
	t.Setenv("IEC101CTL_FORMAT", "json")
	run(t, "version").want(t, 0).has(t, `"tag":"iec101ctl/v1.2.3"`)
}

func TestMalformedEnvironmentIsInvalidInput(t *testing.T) {
	t.Setenv("IEC101CTL_LINK_ADDRESS", "banana")
	r := run(t, "client", "types").want(t, 3)
	if !strings.Contains(r.stderr, "IEC101CTL_LINK_ADDRESS") {
		t.Errorf("stderr: %s", r.stderr)
	}
}

func TestCompletion(t *testing.T) {
	for shell, marker := range map[string]string{
		"bash":       "__start_iec101ctl",
		"zsh":        "#compdef iec101ctl",
		"fish":       "complete -c iec101ctl",
		"powershell": "Register-ArgumentCompleter",
	} {
		run(t, "completion", shell).want(t, 0).has(t, marker)
	}
	run(t, "completion").want(t, 2)
	run(t, "completion", "tcsh").want(t, 2)

	for _, c := range []struct {
		args []string
		want []string
	}{
		{[]string{"client", "interrogate", "--format", ""}, []string{"json", "table", "text"}},
		{[]string{"client", "interrogate", "--link-mode", ""}, []string{"balanced", "unbalanced"}},
		{[]string{"client", "interrogate", "--ioa-size", ""}, []string{"1", "2", "3"}},
		{[]string{"client", "interrogate", "--parity", ""}, []string{"even", "odd", "none"}},
		{[]string{"client", "command", "--type", "set"}, []string{"setpoint-float", "setpoint-normalized", "setpoint-scaled"}},
		{[]string{"client", "command", "--mode", ""}, []string{"cancel", "execute", "sbo", "select"}},
		{[]string{"client", "counters", "--freeze", "f"}, []string{"freeze", "freeze-reset"}},
		{[]string{"client", "monitor", "--type", "M_SP"}, []string{"M_SP_NA_1", "M_SP_TA_1", "M_SP_TB_1"}},
		{[]string{"client", "ping", "--method", "link"}, []string{"link-status", "link-test"}},
		{[]string{"client", "scan", "--method", ""}, []string{"broadcast", "ca", "link"}},
		{[]string{"client", "reset", "--qualifier", ""}, []string{"events", "process"}},
		{[]string{"client", "delay", "--set", ""}, []string{"auto"}},
		{[]string{"client", "types", "--direction", ""}, []string{"control", "monitor"}},
		{[]string{"server", "start", "--link-mode", ""}, []string{"balanced", "unbalanced"}},
		{[]string{"server", "generate-config", "--format", ""}, []string{"json", "yaml"}},
		{[]string{"discover", "--link-address-size", ""}, []string{"0", "1", "2"}},
	} {
		r := run(t, append([]string{"__complete"}, c.args...)...).want(t, 0)
		for _, w := range c.want {
			if !strings.Contains(r.stdout, w) {
				t.Errorf("%v: completions do not contain %q:\n%s", c.args, w, r.stdout)
			}
		}
	}
	// Flags without values of their own complete no file names; --config does.
	r := run(t, "__complete", "client", "interrogate", "--url", "").want(t, 0)
	if !strings.Contains(r.stdout, ":4") {
		t.Errorf("--url completes file names:\n%s", r.stdout)
	}
	r = run(t, "__complete", "server", "start", "--config", "").want(t, 0)
	if !strings.Contains(r.stdout, "yaml") {
		t.Errorf("--config completes no model files:\n%s", r.stdout)
	}
}
