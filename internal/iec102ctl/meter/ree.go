// SPDX-License-Identifier: MIT

package meter

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/otfabric/go-iec60870-5/iec102"
	"github.com/otfabric/go-iec60870-5/iec102/client"
	"github.com/otfabric/go-iec60870-5/iec102/ree"
	"github.com/otfabric/iec60870ctl/internal/iec102ctl/config"
	"github.com/otfabric/iec60870ctl/internal/iec102ctl/types"
)

// CheckLogin validates `client ree login`: it needs an access key.
func CheckLogin(cfg *config.SessionOnlyConfig) error {
	cfg.REE = true
	if err := Validate(&cfg.ConnConfig); err != nil {
		return err
	}
	p, _ := ResolveProfile(&cfg.ConnConfig)
	if !p.HasKey {
		return errors.New("an REE session needs the access key: give --access-key or --access-key-file")
	}
	return nil
}

// Login opens an REE session and closes it again: the access key is checked, nothing else
// is read.
func Login(ctx context.Context, cfg *config.SessionOnlyConfig, trace io.Writer) (*types.LoginResult, error) {
	cfg.REE = true
	m, err := Run(ctx, &cfg.ConnConfig, trace, func(context.Context, *Session) error { return nil })
	if err != nil {
		return nil, err
	}
	return &types.LoginResult{Meter: m}, nil
}

// CheckSetTime validates `client ree set-time`.
func CheckSetTime(cfg *config.SetTimeConfig) error {
	cfg.REE = true
	if err := Validate(&cfg.ConnConfig); err != nil {
		return err
	}
	_, err := ParseTimeB(cfg.Time, location(&cfg.ConnConfig), time.Now())
	return err
}

// SetTime sets the meter's clock (C_CS_TA_2, confirmed with cause 7). The request is sent
// once; "now" is taken when the link is up, just before sending.
func SetTime(ctx context.Context, cfg *config.SetTimeConfig, trace io.Writer) (*types.SetTimeResult, error) {
	cfg.REE = true
	loc := location(&cfg.ConnConfig)
	if _, err := ParseTimeB(cfg.Time, loc, time.Now()); err != nil {
		return nil, err
	}
	var (
		sent iec102.TimeB
		rtt  time.Duration
	)
	m, err := Run(ctx, &cfg.ConnConfig, trace, func(ctx context.Context, s *Session) error {
		tb, err := ParseTimeB(cfg.Time, loc, time.Now())
		if err != nil {
			return err
		}
		sent = tb
		cctx, cancel := s.bound(ctx)
		defer cancel()
		start := time.Now()
		err = ree.SetTime(cctx, s.Client, tb)
		rtt = time.Since(start)
		return err
	})
	if err != nil {
		return nil, err
	}
	return &types.SetTimeResult{Meter: m, Time: TimeOfB(sent), RTTMs: ms(rtt)}, nil
}

// tariffRequest returns the request of `client ree tariffs`.
func tariffRequest(cfg *config.TariffsConfig) (ree.Request, error) {
	if (cfg.From == "") != (cfg.To == "") {
		return ree.Request{}, errors.New("--from and --to go together: give both for stored values, neither for current values")
	}
	if cfg.From == "" {
		return ree.Request{T: ree.C_TA_VC_2}, nil
	}
	loc, err := Location(cfg.TimeZone)
	if err != nil {
		return ree.Request{}, err
	}
	q := ree.Request{T: ree.C_TA_VM_2}
	if q.From, err = ParseTimeA(cfg.From, loc); err != nil {
		return q, fmt.Errorf("--from: %w", err)
	}
	if q.To, err = ParseTimeA(cfg.To, loc); err != nil {
		return q, fmt.Errorf("--to: %w", err)
	}
	if TextA(q.From) > TextA(q.To) {
		return q, fmt.Errorf("--from %s is later than --to %s", TextA(q.From), TextA(q.To))
	}
	return q, nil
}

// CheckTariffs validates `client ree tariffs`.
func CheckTariffs(cfg *config.TariffsConfig) error {
	cfg.REE = true
	if err := Validate(&cfg.ConnConfig); err != nil {
		return err
	}
	_, err := tariffRequest(cfg)
	return err
}

// Tariffs reads the tariff information of a contract register: current values (C_TA_VC_2)
// or the stored values of a range (C_TA_VM_2).
func Tariffs(ctx context.Context, cfg *config.TariffsConfig, trace io.Writer) (*types.TariffsResult, error) {
	cfg.REE = true
	q, err := tariffRequest(cfg)
	if err != nil {
		return nil, err
	}
	var answers []*iec102.ASDU
	m, err := Run(ctx, &cfg.ConnConfig, trace, func(ctx context.Context, s *Session) error {
		cctx, cancel := s.bound(ctx)
		defer cancel()
		var err error
		answers, err = s.Request(cctx, iec102.New(iec102.CauseActivation, s.CommonAddr(), cfg.Register, q), client.UntilTermination)
		return err
	})
	if err != nil {
		return nil, err
	}
	r := &types.TariffsResult{Meter: m, Register: cfg.Register, Request: TypeName(q.T), Records: []types.TariffRecord{}}
	if q.T == ree.C_TA_VM_2 {
		r.From, r.To = TextA(q.From), TextA(q.To)
	}
	for _, a := range answers {
		if b, ok := a.Body.(ree.TariffInfo); ok {
			for _, t := range b.Records {
				r.Records = append(r.Records, TariffOf(t))
			}
		}
	}
	r.Count = len(r.Records)
	return r, nil
}

// TariffOf returns the result form of a tariff record.
func TariffOf(t ree.TariffRecord) types.TariffRecord {
	return types.TariffRecord{
		Address: t.Addr, ActiveAbs: t.ActiveAbs, ActiveInc: t.ActiveInc, ActiveQuality: t.ActiveQual,
		ReactiveIndAbs: t.ReactiveIndAbs, ReactiveIndInc: t.ReactiveIndInc, ReactiveIndQuality: t.ReactiveIndQual,
		ReactiveCapAbs: t.ReactiveCapAbs, ReactiveCapInc: t.ReactiveCapInc, ReactiveCapQuality: t.ReactiveCapQual,
		MaxPower: t.MaxPower, MaxPowerTime: TimeOfA(t.MaxPowerTime), MaxPowerQuality: t.MaxPowerQual,
		ExcessPower: t.ExcessPower, ExcessPowerQuality: t.ExcessPowerQual,
		PeriodStart: TimeOfA(t.Start), PeriodEnd: TimeOfA(t.End),
	}
}

// CheckREERead validates `client ree contracted-powers` and `client ree special-days`.
func CheckREERead(cfg *config.REEReadConfig) error {
	cfg.REE = true
	return Validate(&cfg.ConnConfig)
}

// reeRead sends an REE request without objects (cause 6) and collects the answers until the
// activation termination.
func reeRead(ctx context.Context, cfg *config.REEReadConfig, trace io.Writer, t iec102.TypeID) (types.Meter, []*iec102.ASDU, error) {
	cfg.REE = true
	var answers []*iec102.ASDU
	m, err := Run(ctx, &cfg.ConnConfig, trace, func(ctx context.Context, s *Session) error {
		cctx, cancel := s.bound(ctx)
		defer cancel()
		var err error
		answers, err = s.Request(cctx, iec102.New(iec102.CauseActivation, s.CommonAddr(), cfg.Record, ree.Request{T: t}), client.UntilTermination)
		return err
	})
	return m, answers, err
}

// ContractedPowers reads the contracted powers (C_PC_NA_2, answered by M_PC_NA_2).
func ContractedPowers(ctx context.Context, cfg *config.REEReadConfig, trace io.Writer) (*types.PowersResult, error) {
	m, answers, err := reeRead(ctx, cfg, trace, ree.C_PC_NA_2)
	if err != nil {
		return nil, err
	}
	r := &types.PowersResult{Meter: m, Record: cfg.Record, Powers: []types.ContractedPower{}}
	for _, a := range answers {
		if b, ok := a.Body.(ree.ContractedPowers); ok {
			t := TimeOfA(b.Time)
			r.Time = &t
			for _, p := range b.Powers {
				r.Powers = append(r.Powers, types.ContractedPower{Address: p.Addr, PowerKW: p.Power})
			}
		}
	}
	r.Count = len(r.Powers)
	return r, nil
}

// SpecialDays reads the special days (C_DF_NA_2, answered by M_DF_NA_2).
func SpecialDays(ctx context.Context, cfg *config.REEReadConfig, trace io.Writer) (*types.SpecialDaysResult, error) {
	m, answers, err := reeRead(ctx, cfg, trace, ree.C_DF_NA_2)
	if err != nil {
		return nil, err
	}
	r := &types.SpecialDaysResult{Meter: m, Record: cfg.Record, Days: []types.Time{}}
	for _, a := range answers {
		if b, ok := a.Body.(ree.SpecialDays); ok {
			for _, d := range b.Days {
				r.Days = append(r.Days, TimeOfA(d))
			}
		}
	}
	r.Count = len(r.Days)
	return r, nil
}
