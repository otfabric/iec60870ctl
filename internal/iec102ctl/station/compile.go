// SPDX-License-Identifier: MIT

package station

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/otfabric/go-iec60870-5/iec102"
	"github.com/otfabric/go-iec60870-5/iec102/ree"
	"github.com/otfabric/iec60870ctl/internal/iec102ctl/meter"
)

// meterModel is a validated model: what the station serves, in the codec's types.
type meterModel struct {
	name        string
	linkAddr    uint16
	commonAddr  uint16
	fixedClock  *time.Time
	product     iec102.ProductSpec
	periods     map[uint8][]iec102.IntegratedTotals // per record, oldest first, in model order
	recordOrder []uint8
	events      map[uint8][]iec102.SinglePoint
	ree         *reeModel
}

type reeModel struct {
	key     uint32
	powers  []ree.ContractedPower
	days    []iec102.TimeA
	tariffs map[uint8]tariffs
}

type tariffs struct {
	current, stored []ree.TariffRecord
}

// Validate checks the model, with its times in the zone loc.
func (m *Model) Validate(loc *time.Location) error {
	_, err := compile(m, loc)
	return err
}

func compile(m *Model, loc *time.Location) (*meterModel, error) {
	if loc == nil {
		loc = time.UTC
	}
	out := &meterModel{
		name: m.Station.Name, linkAddr: 1, commonAddr: 1,
		periods: map[uint8][]iec102.IntegratedTotals{}, events: map[uint8][]iec102.SinglePoint{},
		product: iec102.ProductSpec{Standard: m.Station.Product.Standard, Manufacturer: m.Station.Product.Manufacturer, Product: m.Station.Product.Product},
	}
	if m.Station.LinkAddress != nil {
		out.linkAddr = *m.Station.LinkAddress
	}
	if m.Station.CommonAddress != nil {
		out.commonAddr = *m.Station.CommonAddress
	}
	switch c := strings.TrimSpace(m.Station.Clock); strings.ToLower(c) {
	case "", "running":
	default:
		tb, err := meter.ParseTimeB(c, loc, time.Time{})
		if err != nil {
			return nil, fmt.Errorf("station.clock: %w", err)
		}
		t, _ := tb.Time(loc)
		out.fixedClock = &t
	}
	if err := compileTotals(m, loc, out); err != nil {
		return nil, err
	}
	if err := compileEvents(m, loc, out); err != nil {
		return nil, err
	}
	if m.REE != nil {
		r, err := compileREE(m.REE, loc)
		if err != nil {
			return nil, err
		}
		out.ree = r
	}
	return out, nil
}

func compileTotals(m *Model, loc *time.Location, out *meterModel) error {
	if len(m.Totals) > MaxRecords {
		return fmt.Errorf("totals: more than %d lists", MaxRecords)
	}
	seen := map[[2]uint8]bool{}
	n := 0
	for i, l := range m.Totals {
		where := fmt.Sprintf("totals[%d] (record %d)", i, l.Record)
		typ, err := l.typeOf()
		if err != nil {
			return fmt.Errorf("%s: %w", where, err)
		}
		key := [2]uint8{l.Record, uint8(typ.Family())}
		if seen[key] {
			return fmt.Errorf("%s: a second list of %s totals on the record", where, meter.KindName(typ.Family()))
		}
		seen[key] = true
		width, _ := typ.IntegratedTotalsType()
		periods, err := l.periods(typ, loc)
		if err != nil {
			return fmt.Errorf("%s: %w", where, err)
		}
		if len(periods) == 0 {
			return fmt.Errorf("%s: no periods", where)
		}
		n += len(periods)
		if n > MaxPeriods {
			return fmt.Errorf("totals: more than %d periods", MaxPeriods)
		}
		prev := ""
		for j, p := range periods {
			end := meter.TextA(p.Time)
			if prev != "" && end <= prev {
				return fmt.Errorf("%s: period %d ends at %s, not after %s: periods go oldest first", where, j, end, prev)
			}
			prev = end
			addrs := map[uint8]bool{}
			for _, t := range p.Totals {
				if addrs[t.Addr] {
					return fmt.Errorf("%s: period %s has address %d twice", where, end, t.Addr)
				}
				addrs[t.Addr] = true
				if width < 4 && t.Value>>(8*uint(width)) != 0 {
					return fmt.Errorf("%s: period %s, address %d: %d does not fit the %d octets of %s", where, end, t.Addr, t.Value, width, meter.TypeName(typ))
				}
			}
			if len(p.Totals) == 0 {
				return fmt.Errorf("%s: period %s has no totals", where, end)
			}
		}
		if _, ok := out.periods[l.Record]; !ok {
			out.recordOrder = append(out.recordOrder, l.Record)
		}
		out.periods[l.Record] = append(out.periods[l.Record], periods...)
	}
	return nil
}

// periods returns the periods of a totals list: the listed ones, then the generated ones.
func (l TotalsSpec) periods(typ iec102.TypeID, loc *time.Location) ([]iec102.IntegratedTotals, error) {
	var out []iec102.IntegratedTotals
	if len(l.Periods) > MaxPeriods {
		return nil, fmt.Errorf("more than %d periods", MaxPeriods)
	}
	for j, p := range l.Periods {
		end, err := meter.ParseTimeA(p.End, loc)
		if err != nil {
			return nil, fmt.Errorf("periods[%d].end: %w", j, err)
		}
		body := iec102.IntegratedTotals{T: typ, Signed: l.Signed, Time: end}
		for _, t := range p.Totals {
			body.Totals = append(body.Totals, iec102.IntegratedTotal{Addr: t.Address, Value: t.Value, Quality: iec102.Quality(t.Quality), Signature: t.Signature})
		}
		out = append(out, body)
	}
	if g := l.Generate; g != nil {
		switch {
		case g.Count < 1 || g.Count > MaxPeriods:
			return nil, fmt.Errorf("generate.count %d: use 1-%d", g.Count, MaxPeriods)
		case g.Interval < Duration(time.Minute):
			return nil, errors.New("generate.interval: at least 1m (time a counts minutes)")
		case len(g.Totals) == 0:
			return nil, errors.New("generate.totals: none")
		}
		start, err := meter.ParseTimeA(g.Start, loc)
		if err != nil {
			return nil, fmt.Errorf("generate.start: %w", err)
		}
		t0, ok := start.Time(loc)
		if !ok {
			return nil, fmt.Errorf("generate.start %q is not a valid time", g.Start)
		}
		for k := range g.Count {
			end := t0.Add(time.Duration(k) * time.Duration(g.Interval))
			if y := end.Year(); y > 2099 {
				return nil, fmt.Errorf("generate: period %d ends in %d, after 2099", k, y)
			}
			body := iec102.IntegratedTotals{T: typ, Signed: l.Signed, Time: iec102.TimeAOf(end, loc)}
			for _, t := range g.Totals {
				v := uint64(t.Step)
				if !reset(typ) {
					v = uint64(t.Start) + uint64(k)*uint64(t.Step)
				}
				if v > 0xFFFFFFFF {
					return nil, fmt.Errorf("generate: address %d reaches %d after %d periods, more than 32 bits", t.Address, v, k)
				}
				body.Totals = append(body.Totals, iec102.IntegratedTotal{Addr: t.Address, Value: uint32(v)})
			}
			out = append(out, body)
		}
	}
	return out, nil
}

func compileEvents(m *Model, loc *time.Location, out *meterModel) error {
	n := 0
	for i, l := range m.Events {
		where := fmt.Sprintf("events[%d] (record %d)", i, l.Record)
		if _, ok := out.events[l.Record]; ok {
			return fmt.Errorf("%s: a second list on the record", where)
		}
		n += len(l.Points)
		if n > MaxEvents {
			return fmt.Errorf("events: more than %d points", MaxEvents)
		}
		pts := []iec102.SinglePoint{}
		for j, p := range l.Points {
			if p.Qualifier > 127 {
				return fmt.Errorf("%s: points[%d].qualifier %d: 0-127", where, j, p.Qualifier)
			}
			tb, err := meter.ParseTimeB(p.Time, loc, time.Time{})
			if err != nil || strings.TrimSpace(p.Time) == "" || strings.EqualFold(strings.TrimSpace(p.Time), "now") {
				if err == nil {
					err = errors.New("give the time of the event")
				}
				return fmt.Errorf("%s: points[%d].time: %w", where, j, err)
			}
			pts = append(pts, iec102.SinglePoint{Addr: p.Address, On: p.On, Qualifier: p.Qualifier, Time: tb})
		}
		out.events[l.Record] = pts
	}
	return nil
}

func compileREE(r *REESpec, loc *time.Location) (*reeModel, error) {
	out := &reeModel{key: r.AccessKey, tariffs: map[uint8]tariffs{}}
	for _, p := range r.ContractedPowers {
		out.powers = append(out.powers, ree.ContractedPower{Addr: p.Address, Power: p.PowerKW})
	}
	if len(r.ContractedPowers) > 127 || len(r.SpecialDays) > 127 {
		return nil, errors.New("ree: at most 127 contracted powers and 127 special days")
	}
	for i, d := range r.SpecialDays {
		t, err := meter.ParseTimeA(d, loc)
		if err != nil {
			return nil, fmt.Errorf("ree.special_days[%d]: %w", i, err)
		}
		out.days = append(out.days, t)
	}
	for i, t := range r.Tariffs {
		where := fmt.Sprintf("ree.tariffs[%d] (register %d)", i, t.Register)
		if _, ok := out.tariffs[t.Register]; ok {
			return nil, fmt.Errorf("%s: a second entry for the register", where)
		}
		if len(t.Current)+len(t.Stored) > MaxEvents {
			return nil, fmt.Errorf("%s: more than %d records", where, MaxEvents)
		}
		var tr tariffs
		for j, rec := range t.Current {
			x, err := tariffRecord(rec, loc)
			if err != nil {
				return nil, fmt.Errorf("%s: current[%d]: %w", where, j, err)
			}
			tr.current = append(tr.current, x)
		}
		for j, rec := range t.Stored {
			x, err := tariffRecord(rec, loc)
			if err != nil {
				return nil, fmt.Errorf("%s: stored[%d]: %w", where, j, err)
			}
			tr.stored = append(tr.stored, x)
		}
		out.tariffs[t.Register] = tr
	}
	return out, nil
}

func tariffRecord(r TariffRecordSpec, loc *time.Location) (ree.TariffRecord, error) {
	var times [3]iec102.TimeA
	for i, s := range []struct{ name, v string }{{"max_power_time", r.MaxPowerTime}, {"period_start", r.PeriodStart}, {"period_end", r.PeriodEnd}} {
		t, err := meter.ParseTimeA(s.v, loc)
		if err != nil {
			return ree.TariffRecord{}, fmt.Errorf("%s: %w", s.name, err)
		}
		times[i] = t
	}
	if meter.TextA(times[2]) < meter.TextA(times[1]) {
		return ree.TariffRecord{}, fmt.Errorf("period_end %s is before period_start %s", meter.TextA(times[2]), meter.TextA(times[1]))
	}
	return ree.TariffRecord{
		Addr: r.Address, ActiveAbs: r.ActiveAbs, ActiveInc: r.ActiveInc, ActiveQual: r.ActiveQuality,
		ReactiveIndAbs: r.ReactiveIndAbs, ReactiveIndInc: r.ReactiveIndInc, ReactiveIndQual: r.ReactiveIndQuality,
		ReactiveCapAbs: r.ReactiveCapAbs, ReactiveCapInc: r.ReactiveCapInc, ReactiveCapQual: r.ReactiveCapQuality,
		MaxPower: r.MaxPower, MaxPowerTime: times[0], MaxPowerQual: r.MaxPowerQuality,
		ExcessPower: r.ExcessPower, ExcessPowerQual: r.ExcessPowerQuality, Start: times[1], End: times[2],
	}, nil
}
