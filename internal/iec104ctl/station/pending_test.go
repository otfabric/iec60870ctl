// SPDX-License-Identifier: MIT

package station

import (
	"net"
	"net/netip"
	"testing"

	"github.com/otfabric/go-iec60870-5/asdu"
)

// A reset of the pending information with time tag (QRP 2) removes the queued events with a
// time tag and keeps the others.
func TestClearPendingEvents(t *testing.T) {
	m, err := Parse([]byte(`
station: {common_address: 1}
points:
  - {ioa: 1, type: M_SP_NA_1, value: true}
  - {ioa: 2, type: M_SP_NA_1, value: true, time_tag: false}
  - {ioa: 3, type: M_DP_NA_1, value: on}
`))
	if err != nil {
		t.Fatal(err)
	}
	st, err := New(m, Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.srv.Close() })
	// No controlling station is connected: the changes of state wait in the queue.
	for _, ioa := range []asdu.IOA{1, 2, 3, 2} {
		st.announce(ioa, asdu.CauseSpontaneous)
	}
	if n := st.srv.Pending("default"); n != 4 {
		t.Fatalf("%d events queued, want 4", n)
	}
	if n := st.clearPendingEvents(); n != 2 {
		t.Errorf("cleared %d events, want the 2 with a time tag", n)
	}
	if n := st.srv.Pending("default"); n != 2 {
		t.Errorf("%d events left, want the 2 without a time tag", n)
	}
	if n := st.clearPendingEvents(); n != 0 {
		t.Errorf("a second reset cleared %d", n)
	}
}

// A redundancy group with an allow-list takes only TCP peers whose address it holds: an
// address that is not TCP, or a TCP address without an IP, is in no list.
func TestAllowsNonTCPAddresses(t *testing.T) {
	p := []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")}
	for _, a := range []net.Addr{&net.UnixAddr{Name: "/tmp/x", Net: "unix"}, &net.TCPAddr{}} {
		if allows(p, a) {
			t.Errorf("%T %v allowed", a, a)
		}
	}
	if !allows(p, &net.TCPAddr{IP: net.ParseIP("::ffff:10.1.2.3")}) {
		t.Error("an IPv4-mapped address in the list refused")
	}
	if !allows(nil, &net.UnixAddr{Name: "/tmp/x", Net: "unix"}) {
		t.Error("a group without a list refused an address")
	}
}
