// SPDX-License-Identifier: MIT

package station_test

import (
	"context"
	"math"
	"testing"
	"time"

	"github.com/otfabric/go-iec60870-5/asdu"
	"github.com/otfabric/go-iec60870-5/iec104/client"
	"github.com/otfabric/iec60870ctl/internal/iec104ctl/stationtest"
)

// The refusals of a parameter of measured value and of a parameter activation
// (IEC 60870-5-101:2003 7.2.6.24, 7.2.6.25; IEC 60870-5-104:2000 7.9): what the
// station does not take is answered negatively or with "unknown information object
// address", and confirms nothing.
func TestParameterRefusals(t *testing.T) {
	st := stationtest.Start(t, lively)
	rec := &recorder{}
	c := dial(t, st, client.WithHandler(rec))
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	for _, tc := range []struct {
		name  string
		obj   asdu.InformationObject
		cause asdu.Cause
	}{
		{"activation qualifier 9", asdu.ParameterActivation{IOA: 2, Qualifier: 9}, asdu.CauseActivationCon},
		{"object activation without an object", asdu.ParameterActivation{IOA: 0, Qualifier: asdu.ActivateObjectParameter}, asdu.CauseUnknownIOA},
		{"loaded parameters at an object", asdu.ParameterActivation{IOA: 2, Qualifier: asdu.ActivateLoadedParameters}, asdu.CauseActivationCon},
		{"activation of an unknown object", asdu.ParameterActivation{IOA: 99, Qualifier: asdu.ActivateCyclic}, asdu.CauseUnknownIOA},
		{"kind of parameter 9", asdu.ParameterFloat{IOA: 2, Value: 1, Qualifier: 9}, asdu.CauseActivationCon},
		{"NaN", asdu.ParameterFloat{IOA: 2, Value: float32(math.NaN()), Qualifier: asdu.ParameterThreshold}, asdu.CauseActivationCon},
		{"not a measured value", asdu.ParameterFloat{IOA: 1, Value: 1, Qualifier: asdu.ParameterThreshold}, asdu.CauseActivationCon},
		{"unknown object", asdu.ParameterFloat{IOA: 99, Value: 1, Qualifier: asdu.ParameterThreshold}, asdu.CauseUnknownIOA},
	} {
		t.Run(tc.name, func(t *testing.T) {
			negative(t, c.Parameter(ctx, 7, tc.obj), tc.cause)
		})
	}

	// A cause other than activation is refused with a negative mirror of its own cause.
	if err := c.Send(ctx, asdu.New(asdu.CauseDeactivation, 7, asdu.ParameterActivation{IOA: 2, Qualifier: asdu.ActivateObjectParameter})); err != nil {
		t.Fatal(err)
	}
	eventually(t, "the refusal of a deactivation", func() bool {
		return rec.count(func(a *asdu.ASDU) bool {
			return a.Type == asdu.P_AC_NA_1 && a.Cause == asdu.CauseDeactivationCon && a.Negative
		}) == 1
	})

	// What the station takes: every kind, scaled and normalized, and each activation.
	for _, obj := range []asdu.InformationObject{
		asdu.ParameterScaled{IOA: 3, Value: 10, Qualifier: asdu.ParameterHighLimit},
		asdu.ParameterNormalized{IOA: 2, Value: asdu.Normalized(8192), Qualifier: asdu.ParameterSmoothing},
		asdu.ParameterFloat{IOA: 2, Value: 0.5, Qualifier: asdu.ParameterLowLimit},
		asdu.ParameterActivation{IOA: 0, Qualifier: asdu.ActivateLoadedParameters},
		asdu.ParameterActivation{IOA: 2, Qualifier: asdu.ActivateCyclic},
	} {
		if err := c.Parameter(ctx, 7, obj); err != nil {
			t.Errorf("%T %+v: %v", obj, obj, err)
		}
	}
}
