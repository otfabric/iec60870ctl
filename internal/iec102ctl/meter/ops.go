// SPDX-License-Identifier: MIT

package meter

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"time"

	"github.com/otfabric/go-iec60870-5/iec102"
	"github.com/otfabric/go-iec60870-5/iec102/client"
	"github.com/otfabric/go-iec60870-5/iec102/ree"
	"github.com/otfabric/iec60870ctl/internal/iec102ctl/config"
	"github.com/otfabric/iec60870ctl/internal/iec102ctl/types"
)

// location returns the time zone of cfg (checked by Validate).
func location(cfg *config.ConnConfig) *time.Location {
	loc, err := Location(cfg.TimeZone)
	if err != nil {
		return time.UTC
	}
	return loc
}

// CheckInfo validates `client info`.
func CheckInfo(cfg *config.InfoConfig) error { return Validate(&cfg.ConnConfig) }

// Info reads the manufacturer and product specification (C_RD_NA_2, P_MP_NA_2).
func Info(ctx context.Context, cfg *config.InfoConfig, trace io.Writer) (*types.InfoResult, error) {
	var ps iec102.ProductSpec
	m, err := Run(ctx, &cfg.ConnConfig, trace, func(ctx context.Context, s *Session) error {
		cctx, cancel := s.bound(ctx)
		defer cancel()
		var err error
		ps, err = s.ReadProductSpec(cctx)
		return err
	})
	if err != nil {
		return nil, err
	}
	return &types.InfoResult{Meter: m, Standard: ps.Standard, Manufacturer: ps.Manufacturer, Product: ps.Product}, nil
}

// CheckTime validates `client time`.
func CheckTime(cfg *config.TimeConfig) error { return Validate(&cfg.ConnConfig) }

// Time reads the meter's clock (C_TI_NA_2, M_TI_TA_2).
func Time(ctx context.Context, cfg *config.TimeConfig, trace io.Writer) (*types.TimeResult, error) {
	var (
		tb  iec102.TimeB
		got time.Time
	)
	m, err := Run(ctx, &cfg.ConnConfig, trace, func(ctx context.Context, s *Session) error {
		cctx, cancel := s.bound(ctx)
		defer cancel()
		var err error
		tb, err = s.ReadTime(cctx)
		got = time.Now()
		return err
	})
	if err != nil {
		return nil, err
	}
	r := &types.TimeResult{Meter: m, Time: TimeOfB(tb)}
	if t, ok := tb.Time(location(&cfg.ConnConfig)); ok {
		r.Timestamp = t.Format("2006-01-02T15:04:05.000Z07:00")
		d := ms(t.Sub(got))
		r.DriftMs = &d
	}
	return r, nil
}

// TotalsRead returns the read command `client totals` sends.
func TotalsRead(cfg *config.TotalsConfig) (iec102.Read, error) {
	loc, err := Location(cfg.TimeZone)
	if err != nil {
		return iec102.Read{}, err
	}
	if cfg.First > cfg.Last {
		return iec102.Read{}, fmt.Errorf("--first %d is larger than --last %d", cfg.First, cfg.Last)
	}
	q := iec102.Read{First: cfg.First, Last: cfg.Last}
	timeArgs := func(sel iec102.Selection) error {
		switch sel {
		case iec102.SelectTimeRange:
			if cfg.From == "" || cfg.To == "" {
				return fmt.Errorf("%s reads a time range: give --from and --to", TypeName(q.T))
			}
			if q.From, err = ParseTimeA(cfg.From, loc); err != nil {
				return fmt.Errorf("--from: %w", err)
			}
			if q.To, err = ParseTimeA(cfg.To, loc); err != nil {
				return fmt.Errorf("--to: %w", err)
			}
			if TextA(q.From) > TextA(q.To) {
				return fmt.Errorf("--from %s is later than --to %s", TextA(q.From), TextA(q.To))
			}
		case iec102.SelectPeriod, iec102.SelectPeriodRange:
			if cfg.Period == "" {
				return fmt.Errorf("%s reads one past period: give --period", TypeName(q.T))
			}
			if q.From, err = ParseTimeA(cfg.Period, loc); err != nil {
				return fmt.Errorf("--period: %w", err)
			}
		case iec102.SelectNone, iec102.SelectOldest, iec102.SelectOldestRange:
		}
		return nil
	}
	if strings.TrimSpace(cfg.Type) != "" {
		t, err := ParseTypeID(cfg.Type)
		if err != nil {
			return q, err
		}
		if t.Selection() == iec102.SelectNone {
			return q, fmt.Errorf("--type %s is not a read of integrated totals (C_CI_NA_2 .. C_CI_NU_2)", TypeName(t))
		}
		q.T = t
		return q, timeArgs(t.Selection())
	}
	kind := strings.ToLower(strings.TrimSpace(cfg.Kind))
	if a, ok := kindAliases[kind]; ok {
		kind = a
	}
	if kind == "" {
		kind = "operational"
	}
	reads, ok := kindReads[kind]
	if !ok {
		return q, fmt.Errorf("invalid --kind %q: use operational, reset-operational, accounting or reset-accounting", cfg.Kind)
	}
	ranged, oldest, period := cfg.From != "" || cfg.To != "", cfg.Oldest, cfg.Period != ""
	n := 0
	for _, b := range []bool{ranged, oldest, period} {
		if b {
			n++
		}
	}
	switch {
	case n == 0:
		return q, errors.New("select what to read: --from and --to (a time range), --period (one past period) or --oldest")
	case n > 1:
		return q, errors.New("--from/--to, --period and --oldest are mutually exclusive")
	case ranged:
		q.T = reads[4]
	case period && cfg.AddressRange:
		q.T = reads[3]
	case period:
		q.T = reads[2]
	case cfg.AddressRange:
		q.T = reads[1]
	default:
		q.T = reads[0]
	}
	return q, timeArgs(q.T.Selection())
}

// CheckTotals validates `client totals`.
func CheckTotals(cfg *config.TotalsConfig) error {
	if err := Validate(&cfg.ConnConfig); err != nil {
		return err
	}
	_, err := TotalsRead(cfg)
	return err
}

// Totals reads integrated totals (C_CI_NA_2 .. C_CI_NU_2) of a record.
func Totals(ctx context.Context, cfg *config.TotalsConfig, trace io.Writer) (*types.TotalsResult, error) {
	q, err := TotalsRead(cfg)
	if err != nil {
		return nil, err
	}
	var periods []iec102.IntegratedTotals
	reeProfile := false
	m, err := Run(ctx, &cfg.ConnConfig, trace, func(ctx context.Context, s *Session) error {
		reeProfile = s.Profile.REE
		cctx, cancel := s.bound(ctx)
		defer cancel()
		var err error
		periods, err = s.ReadTotals(cctx, cfg.Record, q)
		return err
	})
	if err != nil {
		return nil, err
	}
	r := &types.TotalsResult{Meter: m, Record: cfg.Record, Request: TypeName(q.T), RequestID: uint8(q.T), Kind: KindName(q.T.Family()), Periods: []types.Period{}}
	switch q.T.Selection() {
	case iec102.SelectTimeRange:
		r.From, r.To = TextA(q.From), TextA(q.To)
	case iec102.SelectPeriod, iec102.SelectPeriodRange:
		r.From = TextA(q.From)
	case iec102.SelectNone, iec102.SelectOldest, iec102.SelectOldestRange:
	}
	switch q.T.Selection() {
	case iec102.SelectOldestRange, iec102.SelectPeriodRange, iec102.SelectTimeRange:
		r.First, r.Last = &q.First, &q.Last
	case iec102.SelectNone, iec102.SelectOldest, iec102.SelectPeriod:
	}
	r.Periods = Periods(periods, reeProfile)
	r.Count = len(r.Periods)
	return r, nil
}

// Periods converts integration periods to their result form; the parts of one period that
// arrived in several ASDUs (same type and end) are joined.
func Periods(in []iec102.IntegratedTotals, reeProfile bool) []types.Period {
	out := []types.Period{}
	for _, p := range in {
		end := TimeOfA(p.Time)
		if n := len(out); n > 0 && out[n-1].End == end && out[n-1].TypeID == uint8(p.T) {
			out[n-1].Totals = append(out[n-1].Totals, totals(p, reeProfile)...)
			continue
		}
		out = append(out, types.Period{Type: TypeName(p.T), TypeID: uint8(p.T), End: end, Totals: totals(p, reeProfile)})
	}
	return out
}

func totals(p iec102.IntegratedTotals, reeProfile bool) []types.Total {
	out := make([]types.Total, 0, len(p.Totals))
	for _, t := range p.Totals {
		x := types.Total{Address: t.Addr, Value: t.Value, Quality: uint8(t.Quality), Flags: QualityFlags(t.Quality, reeProfile)}
		if p.Signed {
			sig := t.Signature
			x.Signature = &sig
		}
		out = append(out, x)
	}
	return out
}

// eventsRange returns the time range of `client events`, nil for the whole list.
func eventsRange(cfg *config.EventsConfig) (from, to *iec102.TimeA, err error) {
	if (cfg.From == "") != (cfg.To == "") {
		return nil, nil, errors.New("--from and --to go together: give both, or neither for the whole list")
	}
	if cfg.From == "" {
		return nil, nil, nil
	}
	loc, err := Location(cfg.TimeZone)
	if err != nil {
		return nil, nil, err
	}
	f, err := ParseTimeA(cfg.From, loc)
	if err != nil {
		return nil, nil, fmt.Errorf("--from: %w", err)
	}
	t, err := ParseTimeA(cfg.To, loc)
	if err != nil {
		return nil, nil, fmt.Errorf("--to: %w", err)
	}
	if TextA(f) > TextA(t) {
		return nil, nil, fmt.Errorf("--from %s is later than --to %s", TextA(f), TextA(t))
	}
	return &f, &t, nil
}

// CheckEvents validates `client events`.
func CheckEvents(cfg *config.EventsConfig) error {
	if err := Validate(&cfg.ConnConfig); err != nil {
		return err
	}
	_, _, err := eventsRange(cfg)
	return err
}

// Events reads single-point information with time tag (C_SP_NA_2, or C_SP_NB_2 for a time
// range).
func Events(ctx context.Context, cfg *config.EventsConfig, trace io.Writer) (*types.EventsResult, error) {
	from, to, err := eventsRange(cfg)
	if err != nil {
		return nil, err
	}
	var pts []iec102.SinglePoint
	m, err := Run(ctx, &cfg.ConnConfig, trace, func(ctx context.Context, s *Session) error {
		cctx, cancel := s.bound(ctx)
		defer cancel()
		var err error
		pts, err = s.ReadSinglePoints(cctx, cfg.Record, from, to)
		return err
	})
	if err != nil {
		return nil, err
	}
	r := &types.EventsResult{Meter: m, Record: cfg.Record, Request: TypeName(iec102.C_SP_NA_2), Events: []types.Event{}}
	if from != nil {
		r.Request, r.From, r.To = TypeName(iec102.C_SP_NB_2), TextA(*from), TextA(*to)
	}
	for _, p := range pts {
		r.Events = append(r.Events, types.Event{Address: p.Addr, On: p.On, Qualifier: p.Qualifier, Time: TimeOfB(p.Time)})
	}
	r.Count = len(r.Events)
	return r, nil
}

// request is what `client request` sends.
type request struct {
	asdu  *iec102.ASDU
	until client.Until
	name  string
}

func buildRequest(cfg *config.RequestConfig) (*request, error) {
	t, err := ParseTypeID(cfg.Type)
	if err != nil {
		return nil, err
	}
	c, err := ParseCause(cfg.Cause)
	if err != nil {
		return nil, err
	}
	data, err := ParseHex(cfg.Data)
	if err != nil {
		return nil, err
	}
	n := cfg.Objects
	switch {
	case n < -1 || n > 127:
		return nil, fmt.Errorf("--objects %d: use 0-127, or -1 for the default", n)
	case n == -1 && len(data) > 0:
		n = 1
	case n == -1:
		n = 0
	}
	u, name, err := ParseUntil(cfg.Until, c)
	if err != nil {
		return nil, err
	}
	if err := CheckCommonAddress(cfg.CommonAddress, cfg.CASize); err != nil {
		return nil, err
	}
	a := &iec102.ASDU{Type: t, VSQ: iec102.VSQ(n & 0x7F), Cause: c, CommonAddr: cfg.CommonAddress, Record: cfg.Record, Body: iec102.Raw{Data: data}}
	if _, err := a.Encode(iec102.Params{CommonAddrSize: int(cfg.CASize)}); err != nil {
		return nil, err
	}
	return &request{asdu: a, until: u, name: name}, nil
}

// CheckRequest validates `client request`.
func CheckRequest(cfg *config.RequestConfig) error {
	if err := Validate(&cfg.ConnConfig); err != nil {
		return err
	}
	_, err := buildRequest(cfg)
	return err
}

// Request sends one ASDU as it is given and collects the answers until --until. It is sent
// once and not repeated by iec102ctl (the link layer repeats an unconfirmed frame with the
// same FCB, which the station recognizes as a repetition).
func Request(ctx context.Context, cfg *config.RequestConfig, trace io.Writer) (*types.RequestResult, error) {
	req, err := buildRequest(cfg)
	if err != nil {
		return nil, err
	}
	var (
		answers []*iec102.ASDU
		params  iec102.Params
	)
	m, err := Run(ctx, &cfg.ConnConfig, trace, func(ctx context.Context, s *Session) error {
		params = s.Params
		cctx, cancel := s.bound(ctx)
		defer cancel()
		var err error
		answers, err = s.Request(cctx, req.asdu, req.until)
		return err
	})
	if err != nil {
		return nil, err
	}
	r := &types.RequestResult{Meter: m, Request: ASDUDoc(req.asdu, params), Until: req.name, Answers: []types.ASDU{}}
	for _, a := range answers {
		r.Answers = append(r.Answers, ASDUDoc(a, params))
	}
	r.Count = len(r.Answers)
	return r, nil
}

// ASDUDoc returns the result form of an ASDU.
func ASDUDoc(a *iec102.ASDU, p iec102.Params) types.ASDU {
	d := types.ASDU{
		Type: TypeName(a.Type), TypeID: uint8(a.Type), Cause: CauseName(a.Cause), Cot: uint8(a.Cause.Code()),
		Negative: a.Cause.Negative(), Test: a.Cause.Test(), CommonAddr: a.CommonAddr, Record: a.Record,
		Objects: a.VSQ.Number(), SQ: a.VSQ.SQ(),
	}
	if a.Body != nil {
		if b, err := a.Body.AppendTo(nil, p); err == nil {
			d.Data = hex.EncodeToString(b)
		}
	}
	return d
}

// decoded are the type identifications whose objects iec102ctl decodes.
var decoded = map[iec102.TypeID]bool{
	iec102.M_SP_TA_2: true, iec102.P_MP_NA_2: true, iec102.M_TI_TA_2: true,
	ree.C_AC_NA_2: true, ree.C_FS_NA_2: true, ree.C_CS_TA_2: true, ree.C_TA_VC_2: true, ree.C_TA_VM_2: true,
	ree.M_TA_VC_2: true, ree.M_TA_VM_2: true, ree.C_PC_NA_2: true, ree.M_PC_NA_2: true, ree.C_DF_NA_2: true,
	ree.M_DF_NA_2: true, ree.C_RM_NA_2: true, ree.C_CB_NT_2: true, ree.C_CB_UN_2: true,
}

// Types lists the type identifications of IEC 102 and the REE profile.
func Types(cfg *config.TypesConfig) (*types.TypesResult, error) {
	dir := strings.ToLower(strings.TrimSpace(cfg.Direction))
	if dir != "" && dir != "monitor" && dir != "control" {
		return nil, fmt.Errorf("invalid --direction %q: use monitor or control", cfg.Direction)
	}
	prof := strings.ToLower(strings.TrimSpace(cfg.Profile))
	if prof != "" && prof != config.ProfileStandard && prof != config.ProfileREE {
		return nil, fmt.Errorf("invalid --profile %q: use standard or ree", cfg.Profile)
	}
	r := &types.TypesResult{Types: []types.TypeInfo{}}
	ids := make([]iec102.TypeID, 0, len(typeNames))
	for _, t := range typeNames {
		ids = append(ids, t)
	}
	slices.Sort(ids)
	for _, t := range ids {
		info := types.TypeInfo{ID: uint8(t), Name: TypeName(t), Direction: "monitor", Profile: config.ProfileStandard}
		if strings.HasPrefix(info.Name, "C_") || (t.Private() && strings.HasPrefix(info.Name, "P_") && t != ree.P_ME_NA_2) {
			info.Direction = "control"
		}
		if t.Private() {
			info.Profile = config.ProfileREE
		}
		_, totalsType := t.IntegratedTotalsType()
		info.Decoded = decoded[t] || totalsType || t.Selection() != iec102.SelectNone ||
			t == iec102.C_RD_NA_2 || t == iec102.C_SP_NA_2 || t == iec102.C_SP_NB_2 || t == iec102.C_TI_NA_2
		if (dir != "" && info.Direction != dir) || (prof != "" && info.Profile != prof) {
			continue
		}
		r.Types = append(r.Types, info)
	}
	r.Count = len(r.Types)
	return r, nil
}
