// SPDX-License-Identifier: MIT

package iec

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/otfabric/go-iec104/asdu"
	"github.com/otfabric/iec104ctl/internal/config"
	"github.com/otfabric/iec104ctl/internal/types"
)

func ms(d time.Duration) float64 { return float64(d.Microseconds()) / 1000 }

// flatten turns the ASDUs a request collected into points.
func flatten(asdus []*asdu.ASDU) []types.Point {
	var out []types.Point
	for _, a := range asdus {
		out = append(out, Points(a)...)
	}
	return out
}

// CheckInterrogate validates the configuration of `client interrogate`.
func CheckInterrogate(cfg *config.InterrogateConfig) error {
	if err := Validate(&cfg.ConnConfig); err != nil {
		return err
	}
	if err := CheckCommonAddr(cfg.CommonAddr, true); err != nil {
		return err
	}
	if cfg.Group > 16 {
		return fmt.Errorf("invalid --group %d: use 1-16, or 0 for a general interrogation", cfg.Group)
	}
	_, err := NewFilter(cfg.Types, cfg.IOAs)
	return err
}

// Interrogate runs a general or group interrogation and returns what the station sent.
func Interrogate(ctx context.Context, cfg *config.InterrogateConfig, trace io.Writer) (*types.InterrogateResult, error) {
	filter, err := NewFilter(cfg.Types, cfg.IOAs)
	if err != nil {
		return nil, err
	}
	s, err := Dial(ctx, cfg.Conn(), trace)
	if err != nil {
		return nil, err
	}
	defer func() { _ = s.Close() }()

	qoi := asdu.QOIStation
	if cfg.Group != 0 {
		qoi = asdu.QOIGroup(int(cfg.Group))
	}
	begin := time.Now()
	data, err := s.Interrogate(ctx, asdu.CommonAddr(cfg.CommonAddr), qoi)
	if err != nil {
		return nil, WrapError(err)
	}
	all := flatten(data)
	shown := filter.Apply(all)
	return &types.InterrogateResult{
		Target:     s.Target.String(),
		CommonAddr: cfg.CommonAddr,
		Request:    "interrogation",
		Group:      cfg.Group,
		DurationMs: ms(time.Since(begin)),
		Received:   len(all),
		Count:      len(shown),
		Points:     shown,
	}, nil
}

// freezeQualifiers maps --freeze to the FRZ qualifier of a counter interrogation.
var freezeQualifiers = map[string]uint8{
	"read":         asdu.FreezeRead,
	"freeze":       asdu.FreezeWithoutReset,
	"freeze-reset": asdu.FreezeWithReset,
	"reset":        asdu.FreezeResetOnly,
}

// FreezeValues lists the values of --freeze.
func FreezeValues() []string { return []string{"read", "freeze", "freeze-reset", "reset"} }

// CheckCounters validates the configuration of `client counters`.
func CheckCounters(cfg *config.CountersConfig) error {
	if err := Validate(&cfg.ConnConfig); err != nil {
		return err
	}
	if err := CheckCommonAddr(cfg.CommonAddr, true); err != nil {
		return err
	}
	if cfg.Group > 4 {
		return fmt.Errorf("invalid --group %d: use 1-4, or 0 for a general counter request", cfg.Group)
	}
	return oneOf("freeze", cfg.Freeze, FreezeValues()...)
}

// Counters runs a counter interrogation and returns the integrated totals.
func Counters(ctx context.Context, cfg *config.CountersConfig, trace io.Writer) (*types.InterrogateResult, error) {
	s, err := Dial(ctx, cfg.Conn(), trace)
	if err != nil {
		return nil, err
	}
	defer func() { _ = s.Close() }()

	request := asdu.CounterGeneral
	if cfg.Group != 0 {
		request = asdu.CounterGroup1 + cfg.Group - 1
	}
	begin := time.Now()
	data, err := s.CounterInterrogate(ctx, asdu.CommonAddr(cfg.CommonAddr), request, freezeQualifiers[cfg.Freeze])
	if err != nil {
		return nil, WrapError(err)
	}
	points := flatten(data)
	return &types.InterrogateResult{
		Target:     s.Target.String(),
		CommonAddr: cfg.CommonAddr,
		Request:    "counter-interrogation",
		Group:      cfg.Group,
		Freeze:     cfg.Freeze,
		DurationMs: ms(time.Since(begin)),
		Received:   len(points),
		Count:      len(points),
		Points:     points,
	}, nil
}

// CheckRead validates the configuration of `client read`.
func CheckRead(cfg *config.ReadConfig) error {
	if err := Validate(&cfg.ConnConfig); err != nil {
		return err
	}
	if err := CheckCommonAddr(cfg.CommonAddr, false); err != nil {
		return err
	}
	ioas, err := ParseIOAs(cfg.IOAs)
	if err != nil {
		return err
	}
	if ioas.Count() == 0 {
		return fmt.Errorf("no information object address given: set --ioa (e.g. --ioa 100 or --ioa 100,200-203)")
	}
	return nil
}

// Read sends one read command per address. A refused or unanswered read is reported in its
// row and the next address is read; an error that ends the connection ends the command.
func Read(ctx context.Context, cfg *config.ReadConfig, trace io.Writer) (*types.ReadResult, error) {
	ioas, err := ParseIOAs(cfg.IOAs)
	if err != nil {
		return nil, err
	}
	s, err := Dial(ctx, cfg.Conn(), trace)
	if err != nil {
		return nil, err
	}
	defer func() { _ = s.Close() }()

	res := &types.ReadResult{Target: s.Target.String(), CommonAddr: cfg.CommonAddr, Requested: ioas.Count()}
	var fatal error
	ioas.Each(func(ioa uint32) bool {
		a, err := s.Read(ctx, asdu.CommonAddr(cfg.CommonAddr), asdu.IOA(ioa))
		if err != nil {
			if Fatal(err) {
				fatal = err
				return false
			}
			res.Failed++
			res.Rows = append(res.Rows, types.ReadRow{IOA: ioa, Error: EmbeddedErrorInfo(err)})
			return true
		}
		// The answer may hold more objects than the one asked for (a sequence).
		for _, p := range Points(a) {
			if p.IOA == ioa {
				res.Rows = append(res.Rows, types.ReadRow{IOA: ioa, Point: &p})
			}
		}
		return true
	})
	if fatal != nil {
		return nil, WrapError(fatal)
	}
	return res, nil
}
