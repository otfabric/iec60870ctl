// SPDX-License-Identifier: MIT

package cmd

import (
	"context"
	"testing"
	"time"

	"github.com/otfabric/go-iec60870-5/asdu"
	"github.com/otfabric/go-iec60870-5/iec104/client"
	"github.com/otfabric/iec60870ctl/internal/iec104ctl/stationtest"
)

// The simulated station refuses a deactivation of clock synchronization, reset process and
// test command with cause 45 (IEC 60870-5-104 ed. 1 7.1, 7.6, 7.10, 8.8): a change from
// go-iec104, recorded in docs/MIGRATION_AUDIT.md (change 4).
func TestStationRefusesDeactivationOfActivateOnlyTypes(t *testing.T) {
	st := stationtest.Start(t, stationtest.Model)
	got := make(chan *asdu.ASDU, 16)
	c, err := client.New(st.Addr(), client.WithHandler(client.HandlerFunc(func(a *asdu.ASDU) { got <- a })))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.Close() }()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := c.Connect(ctx); err != nil {
		t.Fatal(err)
	}
	_ = c.StartDT(ctx) // the client may have started data transfer by itself
	for _, obj := range []asdu.InformationObject{
		asdu.ClockSync{Time: asdu.Now()},
		asdu.ResetProcess{Qualifier: asdu.ResetProcessGeneral},
		asdu.TestCommand{Counter: 1, Time: asdu.Now()},
	} {
		req := asdu.New(asdu.CauseDeactivation, 1, obj)
		if err := c.Send(ctx, req); err != nil {
			t.Fatal(err)
		}
		select {
		case a := <-got:
			if a.Type != req.Type || !a.Negative || a.Cause != asdu.CauseUnknownCause {
				t.Errorf("%v: answer %v cause %v negative %v, want a negative mirror with cause 45", req.Type, a.Type, a.Cause, a.Negative)
			}
		case <-ctx.Done():
			t.Fatalf("%v: no answer", req.Type)
		}
	}
}
