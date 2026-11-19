// SPDX-License-Identifier: MIT

package iec

import (
	"context"
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/otfabric/go-iec60870-5/asdu"
	"github.com/otfabric/iec60870ctl/internal/iec104ctl/config"
	"github.com/otfabric/iec60870ctl/internal/iec104ctl/types"
)

// parameterKinds maps the names --type of `client parameter` accepts to the type.
var parameterKinds = map[string]asdu.TypeID{
	"normalized": asdu.P_ME_NA_1,
	"scaled":     asdu.P_ME_NB_1,
	"float":      asdu.P_ME_NC_1,
	"activation": asdu.P_AC_NA_1,
}

// ParameterTypes lists the values of --type of `client parameter` with a description for
// shell completion.
func ParameterTypes() map[string]string {
	out := map[string]string{}
	for name, t := range parameterKinds {
		out[name] = t.String() + ": " + t.Description()
	}
	return out
}

// measuredKinds maps --qualifier of a parameter of measured value to the kind of parameter
// (KPA of the QPM, IEC 60870-5-101:2003 7.2.6.24).
var measuredKinds = map[string]uint8{
	"threshold":  asdu.ParameterThreshold,
	"smoothing":  asdu.ParameterSmoothing,
	"low-limit":  asdu.ParameterLowLimit,
	"high-limit": asdu.ParameterHighLimit,
}

// activationKinds maps --qualifier of a parameter activation to the QPA (7.2.6.25).
var activationKinds = map[string]uint8{
	"loaded": asdu.ActivateLoadedParameters,
	"object": asdu.ActivateObjectParameter,
	"cyclic": asdu.ActivateCyclic,
}

// ParameterQualifiers lists the values of --qualifier of `client parameter` with a
// description for shell completion.
func ParameterQualifiers() map[string]string {
	return map[string]string{
		"threshold":  "parameter of measured value: threshold value",
		"smoothing":  "parameter of measured value: smoothing factor (filter time constant)",
		"low-limit":  "parameter of measured value: low limit for transmission",
		"high-limit": "parameter of measured value: high limit for transmission",
		"loaded":     "activation: the previously loaded parameters (--ioa 0)",
		"object":     "activation: the parameter of the addressed object",
		"cyclic":     "activation: persistent cyclic or periodic transmission of the addressed object",
	}
}

// parameterSpec is a validated `client parameter`.
type parameterSpec struct {
	typ       asdu.TypeID
	obj       asdu.InformationObject
	value     any // the value as sent, nil for an activation
	qualifier string
}

func parameterType(s string) (asdu.TypeID, error) {
	name := strings.ToLower(strings.TrimSpace(s))
	if t, ok := parameterKinds[name]; ok {
		return t, nil
	}
	for _, t := range parameterKinds {
		if strings.EqualFold(name, t.String()) {
			return t, nil
		}
	}
	return 0, fmt.Errorf("invalid --type %q: use normalized, scaled, float or activation (or P_ME_NA_1, P_ME_NB_1, P_ME_NC_1, P_AC_NA_1)", s)
}

func parseParameter(cfg *config.ParameterConfig) (parameterSpec, error) {
	var spec parameterSpec
	typ, err := parameterType(cfg.Type)
	if err != nil {
		return spec, err
	}
	spec.typ = typ
	if cfg.IOA > MaxIOA {
		return spec, fmt.Errorf("invalid --ioa %d: the largest information object address is %d", cfg.IOA, MaxIOA)
	}
	ioa := asdu.IOA(cfg.IOA)
	q := strings.ToLower(strings.TrimSpace(cfg.Qualifier))
	value := strings.TrimSpace(cfg.Value)

	if typ == asdu.P_AC_NA_1 {
		qpa, ok := activationKinds[q]
		if !ok {
			return spec, fmt.Errorf("invalid --qualifier %q for a parameter activation: use loaded, object or cyclic", cfg.Qualifier)
		}
		if value != "" {
			return spec, fmt.Errorf("a parameter activation has no value: remove --value")
		}
		// IEC 60870-5-101:2003 7.2.6.25: the previously loaded parameters are activated
		// with information object address 0; the other two qualifiers name an object.
		if qpa == asdu.ActivateLoadedParameters && ioa != 0 {
			return spec, fmt.Errorf("an activation of the previously loaded parameters is sent to --ioa 0, not %d", ioa)
		}
		if qpa != asdu.ActivateLoadedParameters && ioa == 0 {
			return spec, fmt.Errorf("--qualifier %s activates the parameter of an object: give its --ioa (1-%d)", q, MaxIOA)
		}
		spec.qualifier = q
		spec.obj = asdu.ParameterActivation{IOA: ioa, Qualifier: qpa}
		return spec, nil
	}

	kpa, ok := measuredKinds[q]
	if !ok {
		return spec, fmt.Errorf("invalid --qualifier %q for a parameter of measured value: use threshold, smoothing, low-limit or high-limit", cfg.Qualifier)
	}
	if ioa == 0 {
		return spec, fmt.Errorf("invalid --ioa 0: a parameter of measured value is sent to the address of its measured value (1-%d)", MaxIOA)
	}
	if value == "" {
		return spec, fmt.Errorf("no value given: set --value")
	}
	f, err := strconv.ParseFloat(value, 64)
	if err != nil || math.IsNaN(f) || math.IsInf(f, 0) {
		return spec, fmt.Errorf("invalid --value %q for a parameter of measured value: not a number", value)
	}
	spec.qualifier = q
	switch typ {
	case asdu.P_ME_NA_1:
		if f < -1 || f >= 1 {
			return spec, fmt.Errorf("invalid --value %v for a normalized parameter: the range is -1 up to, not including, 1", f)
		}
		n := asdu.NormalizedFromFloat(f)
		spec.value = n.Float64()
		spec.obj = asdu.ParameterNormalized{IOA: ioa, Value: n, Qualifier: kpa}
	case asdu.P_ME_NB_1:
		if f != math.Trunc(f) || f < math.MinInt16 || f > math.MaxInt16 {
			return spec, fmt.Errorf("invalid --value %q for a scaled parameter: use a whole number from -32768 to 32767", value)
		}
		spec.value = int(int16(f))
		spec.obj = asdu.ParameterScaled{IOA: ioa, Value: int16(f), Qualifier: kpa}
	default:
		if math.Abs(f) > math.MaxFloat32 {
			return spec, fmt.Errorf("invalid --value %q for a short floating point parameter: out of range", value)
		}
		spec.value = float32(f)
		spec.obj = asdu.ParameterFloat{IOA: ioa, Value: float32(f), Qualifier: kpa}
	}
	return spec, nil
}

// CheckParameter validates the configuration of `client parameter`.
func CheckParameter(cfg *config.ParameterConfig) error {
	if err := Validate(&cfg.ConnConfig); err != nil {
		return err
	}
	if err := CheckCommonAddr(cfg.CommonAddr, false); err != nil {
		return err
	}
	_, err := parseParameter(cfg)
	return err
}

// Parameter loads a parameter of measured value or sends a parameter activation, and waits
// for the activation confirmation (IEC 60870-5-104:2000 7.9; IEC 60870-5-101:2003 7.4.9,
// 7.4.10). It is sent once and not repeated.
func Parameter(ctx context.Context, cfg *config.ParameterConfig, trace io.Writer) (*types.ParameterResult, error) {
	spec, err := parseParameter(cfg)
	if err != nil {
		return nil, err
	}
	s, err := Dial(ctx, cfg.Conn(), trace)
	if err != nil {
		return nil, err
	}
	defer func() { _ = s.Close() }()
	begin := time.Now()
	if err := s.Parameter(ctx, asdu.CommonAddr(cfg.CommonAddr), spec.obj); err != nil {
		return nil, WrapError(err)
	}
	return &types.ParameterResult{
		Target:     s.Target.String(),
		CommonAddr: cfg.CommonAddr,
		IOA:        cfg.IOA,
		Type:       spec.typ.String(),
		Value:      spec.value,
		Qualifier:  spec.qualifier,
		RTTMs:      ms(time.Since(begin)),
	}, nil
}
