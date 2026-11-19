// SPDX-License-Identifier: MIT

package cmd

import (
	"context"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/otfabric/iec60870ctl/internal/iec104ctl/stationtest"
	"github.com/otfabric/iec60870ctl/internal/iec104ctl/types"
)

func url(st *stationtest.Station) string { return "tcp://" + st.Addr() }

// --standby-url makes the connections a redundancy group (IEC 60870-5-104 clause 10): a
// one-shot command runs on the connection that has data transfer started, and reports it.
func TestClientStandbyURL(t *testing.T) {
	a := stationtest.Start(t, stationtest.Model)
	b := stationtest.Start(t, stationtest.Model)
	link := []string{"--t1", "2", "--t2", "1", "--timeout", "5000", "--format", "json"}

	var r types.InterrogateResult
	run(t, append([]string{"client", "interrogate", "--url", url(a), "--standby-url", url(b)}, link...)...).want(t, 0).decode(t, &r)
	// The group starts data transfer on the connection established first.
	if (r.Target != url(a) && r.Target != url(b)) || r.Received == 0 {
		t.Errorf("both paths up: %+v", r)
	}
	if served := map[string]bool{url(a): a.HasEvent("interrogation"), url(b): b.HasEvent("interrogation")}; !served[r.Target] || len(served) != 2 || served[url(a)] == served[url(b)] {
		t.Errorf("the interrogation went to %v, the result names %s", served, r.Target)
	}

	// The station URL does not answer: the standby carries the command.
	down := "tcp://127.0.0.1:" + strconv.Itoa(int(stationtest.FreePort(t)))
	run(t, append([]string{"client", "interrogate", "--url", down, "--standby-url", url(b)}, link...)...).want(t, 0).decode(t, &r)
	if r.Target != url(b) {
		t.Errorf("station URL down: target %s, want %s", r.Target, url(b))
	}

	// Nothing answers: a transport error after --timeout.
	r2 := run(t, "client", "interrogate", "--url", down, "--standby-url", "tcp://127.0.0.1:"+strconv.Itoa(int(stationtest.FreePort(t))), "--timeout", "500", "--format", "json").want(t, 4)
	if kind, _ := r2.errorCode(t); kind != "transport" {
		t.Errorf("no path up: kind %s", kind)
	}
}

func TestClientStandbyURLInvalid(t *testing.T) {
	st := stationtest.Start(t, stationtest.Model)
	for _, args := range [][]string{
		{"--standby-url", "10.0.0.2"},                            // no scheme
		{"--standby-url", "tls://127.0.0.1:1"},                   // another transport
		{"--standby-url", url(st)},                               // the station itself
		{"--standby-url", "tcp://127.0.0.1:1,tcp://127.0.0.1:1"}, // twice
		{"--standby-url", "tcp://h1,tcp://h2,tcp://h3,tcp://h4,tcp://h5,tcp://h6,tcp://h7,tcp://h8,tcp://h9"},
	} {
		r := run(t, append([]string{"client", "interrogate", "--url", url(st), "--format", "json"}, args...)...)
		if r.code != 3 {
			t.Errorf("%v: exit %d, want 3\n%s%s", args, r.code, r.stdout, r.stderr)
			continue
		}
		if kind, _ := r.errorCode(t); kind != "invalid_input" {
			t.Errorf("%v: kind %s", args, kind)
		}
	}
	if st.HasEvent("connected") {
		t.Error("an invalid group connected")
	}
}

// activeOf waits until a or b has logged event and returns that one, and the other.
func activeOf(t *testing.T, a, b *stationtest.Station, event string) (active, standby *stationtest.Station) {
	t.Helper()
	eventually(t, "the first "+event, func() bool { return a.HasEvent(event) || b.HasEvent(event) })
	if a.HasEvent(event) {
		return a, b
	}
	return b, a
}

// client monitor follows a switchover: when the connection with data transfer started is
// lost, the standby takes over, and the stream goes on.
func TestMonitorSwitchover(t *testing.T) {
	a := stationtest.Start(t, stationtest.Model)
	b := stationtest.Start(t, stationtest.Model)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var stdout, stderr syncBuffer
	done := make(chan int, 1)
	go func() {
		done <- Run(ctx, []string{"client", "monitor", "--url", url(a), "--standby-url", url(b), "--t1", "2", "--t2", "1",
			"--interrogate", "--duration", "3", "--format", "json"}, &stdout, &stderr)
	}()
	active, standby := activeOf(t, a, b, "interrogation")
	eventually(t, "the standby connection", func() bool { return standby.HasEvent("connected") })
	_ = active.Close()
	eventually(t, "the switchover", func() bool {
		return strings.Contains(stderr.String(), "monitor: switchover: data transfer on "+url(standby))
	})
	// --interrogate runs on every connection that takes over.
	eventually(t, "the interrogation of the standby", func() bool { return standby.HasEvent("interrogation") })
	select {
	case code := <-done:
		if code != 0 {
			t.Fatalf("exit %d\n%s", code, stderr.String())
		}
	case <-time.After(10 * time.Second):
		t.Fatal("monitor did not end")
	}
	if !strings.Contains(stderr.String(), "1 switchovers") {
		t.Errorf("summary: %s", stderr.String())
	}
	if strings.Contains(stderr.String(), "reconnects") {
		t.Errorf("a switchover was counted as a reconnect: %s", stderr.String())
	}
}

// With --no-reconnect a monitor of a group ends with exit 4 when no connection of it is
// left, not when the first one is lost.
func TestMonitorSwitchoverNoReconnect(t *testing.T) {
	a := stationtest.Start(t, stationtest.Model)
	b := stationtest.Start(t, stationtest.Model)
	var stdout, stderr syncBuffer
	done := make(chan int, 1)
	go func() {
		done <- Run(context.Background(), []string{"client", "monitor", "--url", url(a), "--standby-url", url(b), "--t1", "2", "--t2", "1",
			"--no-reconnect", "--format", "json"}, &stdout, &stderr)
	}()
	active, standby := activeOf(t, a, b, "started")
	eventually(t, "the standby connection", func() bool { return standby.HasEvent("connected") })
	_ = active.Close()
	eventually(t, "the switchover", func() bool { return standby.HasEvent("started") })
	select {
	case code := <-done:
		t.Fatalf("monitor ended with %d when the standby took over\n%s", code, stderr.String())
	case <-time.After(200 * time.Millisecond):
	}
	_ = standby.Close()
	select {
	case code := <-done:
		if code != 4 {
			t.Fatalf("exit %d, want 4\n%s", code, stderr.String())
		}
	case <-time.After(10 * time.Second):
		t.Fatal("monitor did not end when the group was lost")
	}
}

// server start --redundancy-group and --event-queue: the groups and the queue of the
// station, in the event log; a connection no group allows is refused.
func TestServerRedundancyFlags(t *testing.T) {
	port, log := startServer(t, "--redundancy-group", "scada=127.0.0.1", "--redundancy-group", "rest=*", "--event-queue", "32")
	if !strings.Contains(log.String(), "redundancy groups scada, rest") || !strings.Contains(log.String(), "event queue 32") {
		t.Errorf("listening: %s", log.String())
	}
	run(t, "client", "interrogate", "--ip", "127.0.0.1", "--port", port, "--format", "json").want(t, 0)
	if !strings.Contains(log.String(), `"group":"scada"`) {
		t.Errorf("no connection event of group scada: %s", log.String())
	}

	port, log = startServer(t, "--redundancy-group", "far=10.9.9.9")
	run(t, "client", "interrogate", "--ip", "127.0.0.1", "--port", port, "--timeout", "1000", "--t0", "1", "--format", "json")
	eventually(t, "the refusal", func() bool { return strings.Contains(log.String(), "no redundancy group allows the address") })

	for _, args := range [][]string{
		{"--redundancy-group", "scada"},
		{"--redundancy-group", "scada=nowhere"},
		{"--redundancy-group", "a=*", "--redundancy-group", "a=10.0.0.1"},
		{"--redundancy-group", "a=*", "--redundancy-group", "b=10.0.0.1"},
		{"--event-queue", "65537"},
	} {
		r := run(t, append([]string{"server", "start", "--listen", "127.0.0.1", "--port", "0", "--format", "json"}, args...)...)
		if r.code != 3 {
			t.Errorf("%v: exit %d, want 3\n%s%s", args, r.code, r.stdout, r.stderr)
		}
	}
}
