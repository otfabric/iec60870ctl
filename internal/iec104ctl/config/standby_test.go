// SPDX-License-Identifier: MIT

package config

import (
	"strings"
	"testing"
)

func TestResolveStandby(t *testing.T) {
	tcp := Target{Host: "10.0.0.1", Port: 2404}
	tls := Target{Host: "10.0.0.1", Port: 19998, TLS: true}

	if got, err := (&ConnConfig{}).ResolveStandby(tcp); err != nil || got != nil {
		t.Errorf("no --standby-url: %v, %v", got, err)
	}
	c := ConnConfig{Standby: []string{"tcp://10.0.1.1", " ", "tcp://10.0.0.1:2405"}}
	got, err := c.ResolveStandby(tcp)
	if err != nil || len(got) != 2 || got[0].String() != "tcp://10.0.1.1:2404" || got[1].String() != "tcp://10.0.0.1:2405" {
		t.Errorf("tcp: %v, %v", got, err)
	}
	if got, err := (&ConnConfig{Standby: []string{"tls://10.0.1.1"}}).ResolveStandby(tls); err != nil || !got[0].TLS {
		t.Errorf("tls: %v, %v", got, err)
	}

	many := make([]string, MaxStandby+1)
	for i := range many {
		many[i] = "tcp://10.0.1." + string(rune('a'+i)) + ".example"
	}
	for _, tc := range []struct {
		primary Target
		urls    []string
		want    string
	}{
		{tcp, []string{"10.0.1.1"}, "missing scheme"},
		{tcp, []string{"tls://10.0.1.1"}, "give a tcp:// URL"},
		{tls, []string{"tcp://10.0.1.1"}, "give a tls:// URL"},
		{tcp, []string{"tcp://10.0.0.1"}, "already a connection of the group"},
		{tcp, []string{"tcp://10.0.1.1", "tcp://10.0.1.1:2404"}, "already a connection of the group"},
		{tcp, many, "use at most 8"},
	} {
		_, err := (&ConnConfig{Standby: tc.urls}).ResolveStandby(tc.primary)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%v: %v, want %q", tc.urls, err, tc.want)
		}
	}
}
