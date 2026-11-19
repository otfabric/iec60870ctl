// SPDX-License-Identifier: MIT

package station

import (
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
