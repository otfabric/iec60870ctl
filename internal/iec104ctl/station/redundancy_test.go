// SPDX-License-Identifier: MIT

package station

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/otfabric/go-iec60870-5/asdu"
	"github.com/otfabric/go-iec60870-5/iec104/apci"
	"github.com/otfabric/go-iec60870-5/iec104/client"
	"github.com/otfabric/iec60870ctl/internal/iec104ctl/types"
)

const redundancyModel = `
station: {common_address: 1}
points:
  - {ioa: 1, type: M_SP_NA_1, value: true}
`

func TestParseGroups(t *testing.T) {
	got, err := ParseGroups([]string{"scada=10.0.0.5", " backup = 10.0.1.0/24 ", "scada=10.0.0.6", "", "rest=*"})
	if err != nil {
		t.Fatal(err)
	}
	want := []GroupSpec{
		{Name: "scada", Allow: []string{"10.0.0.5", "10.0.0.6"}},
		{Name: "backup", Allow: []string{"10.0.1.0/24"}},
		{Name: "rest"},
	}
	if len(got) != len(want) {
		t.Fatalf("%+v", got)
	}
	for i := range want {
		if got[i].Name != want[i].Name || strings.Join(got[i].Allow, ",") != strings.Join(want[i].Allow, ",") {
			t.Errorf("group %d: %+v, want %+v", i, got[i], want[i])
		}
	}
	for _, bad := range [][]string{
		{"scada"},                     // no address
		{"=10.0.0.5"},                 // no name
		{"scada="},                    // empty address
		{"scada=10.0.0.5,10.0.0.6"},   // one address per flag (the list was not split)
		{"scada=host.example"},        // not an address
		{"scada=*", "scada=10.0.0.5"}, // * and addresses
		{"scada=10.0.0.5", "scada=*"}, // addresses and *
	} {
		if _, err := ParseGroups(bad); err == nil {
			t.Errorf("%q: accepted", bad)
		}
	}
}

// parseValid parses and validates a model.
func parseValid(model string) error {
	m, err := Parse([]byte(model))
	if err != nil {
		return err
	}
	return m.Validate()
}

func TestRedundancyModelValidation(t *testing.T) {
	for _, tc := range []struct{ section, want string }{
		{`{event_queue: -1}`, "redundancy.event_queue -1"},
		{`{event_queue: 65537}`, "redundancy.event_queue 65537"},
		{`{groups: [{name: ""}]}`, "a group needs a name"},
		{`{groups: [{name: a, allow: [10.0.0.1]}, {name: a}]}`, `the name "a" is already used`},
		{`{groups: [{name: "a=b"}]}`, "no = or ,"},
		{`{groups: [{name: any}, {name: b, allow: [10.0.0.1]}]}`, "only the last group may allow every address"},
		{`{groups: [{name: a, allow: [nowhere]}]}`, `allow "nowhere"`},
	} {
		err := parseValid(redundancyModel + "redundancy: " + tc.section + "\n")
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: %v, want %q", tc.section, err, tc.want)
		}
	}
	var many strings.Builder
	many.WriteString("redundancy: {groups: [")
	for i := range MaxGroups + 1 {
		if i > 0 {
			many.WriteString(", ")
		}
		many.WriteString("{name: g" + strings.Repeat("x", i) + ", allow: [10.0.0.1]}")
	}
	many.WriteString("]}\n")
	if err := parseValid(redundancyModel + many.String()); err == nil || !strings.Contains(err.Error(), "use at most 64") {
		t.Errorf("65 groups: %v", err)
	}
	m, err := Parse([]byte(redundancyModel + "redundancy: {event_queue: 8, groups: [{name: scada, allow: [127.0.0.1]}, {name: rest}]}\n"))
	if err != nil {
		t.Fatal(err)
	}
	// Options replace what the model declares, and are validated as the model is.
	if _, err := New(m, Options{Redundancy: RedundancySpec{EventQueue: MaxEventQueue + 1}}); err == nil {
		t.Error("an event queue beyond the maximum was accepted")
	}
	if _, err := New(m, Options{Redundancy: RedundancySpec{Groups: []GroupSpec{{Name: "a"}, {Name: "a"}}}}); err == nil {
		t.Error("two groups with one name were accepted")
	}
}

// eventLog collects the events of a station.
type eventLog struct {
	mu     sync.Mutex
	events []types.ServerEvent
}

func (l *eventLog) add(e types.ServerEvent) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.events = append(l.events, e)
}

func (l *eventLog) find(event, detail string) (types.ServerEvent, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, e := range l.events {
		if e.Event == event && strings.Contains(e.Detail, detail) {
			return e, true
		}
	}
	return types.ServerEvent{}, false
}

func waitUntil(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func serve(t *testing.T, model string, opts Options) (*Station, string, *eventLog) {
	t.Helper()
	m, err := Parse([]byte(model))
	if err != nil {
		t.Fatal(err)
	}
	log := &eventLog{}
	opts.Log = log.add
	st, err := New(m, opts)
	if err != nil {
		t.Fatal(err)
	}
	ln, err := st.Listen("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() { defer close(done); _ = st.Serve(ln) }()
	t.Cleanup(func() { _ = st.Close(); <-done })
	waitUntil(t, "listening", func() bool { _, ok := log.find("listening", ""); return ok })
	return st, ln.Addr().String(), log
}

func dial(t *testing.T, addr string) *client.Client {
	t.Helper()
	p := apci.Params{K: 12, W: 8, T0: 2 * time.Second, T1: 2 * time.Second, T2: 100 * time.Millisecond}.WithDefaults()
	c, err := client.New(addr, client.WithParams(p), client.WithRequestTimeout(3*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := c.Connect(ctx); err != nil {
		t.Fatal(err)
	}
	return c
}

// IEC 60870-5-104 clause 10: of the connections of a redundancy group one has data transfer
// started. A STARTDT on a second connection takes over; the first is closed. The connection
// events name the group.
func TestRedundancyGroupTakeover(t *testing.T) {
	st, addr, log := serve(t, redundancyModel, Options{Redundancy: RedundancySpec{
		EventQueue: 16,
		Groups:     []GroupSpec{{Name: "scada", Allow: []string{"127.0.0.1"}}},
	}})
	if e, _ := log.find("listening", ""); !strings.Contains(e.Detail, "redundancy groups scada") || !strings.Contains(e.Detail, "event queue 16") {
		t.Errorf("listening: %q", e.Detail)
	}
	first := dial(t, addr)
	waitUntil(t, "the first start", func() bool { e, ok := log.find("started", ""); return ok && e.Group == "scada" })
	if _, err := first.Interrogate(context.Background(), 1, asdu.QOIStation); err != nil {
		t.Fatal(err)
	}
	dial(t, addr)
	waitUntil(t, "the first connection to be closed", func() bool { _, ok := log.find("disconnected", ""); return ok })
	if e, _ := log.find("disconnected", ""); e.Group != "scada" {
		t.Errorf("disconnected event: %+v", e)
	}
	if n := st.Sessions(); n != 1 {
		t.Errorf("%d sessions, want the one that took over", n)
	}
}

// A connection that no redundancy group allows is refused, and the event log says why.
func TestRedundancyGroupRefuses(t *testing.T) {
	_, addr, log := serve(t, redundancyModel, Options{Redundancy: RedundancySpec{
		Groups: []GroupSpec{{Name: "scada", Allow: []string{"10.9.9.9"}}},
	}})
	p := apci.Params{T0: time.Second}.WithDefaults()
	c, err := client.New(addr, client.WithParams(p))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = c.Connect(ctx)
	waitUntil(t, "the refusal", func() bool {
		_, ok := log.find("refused", "no redundancy group allows the address")
		return ok
	})
	if _, ok := log.find("connected", ""); ok {
		t.Error("a connection no group allows was served")
	}
}

// --allow is checked before the groups: an address it does not allow is refused as before.
func TestRedundancyGroupAfterAllow(t *testing.T) {
	_, addr, log := serve(t, redundancyModel, Options{
		Allow:      nil,
		Redundancy: RedundancySpec{Groups: []GroupSpec{{Name: "any"}}},
	})
	dial(t, addr)
	waitUntil(t, "the start", func() bool { e, ok := log.find("started", ""); return ok && e.Group == "any" })
}
