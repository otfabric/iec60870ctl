// SPDX-License-Identifier: MIT

package station

import (
	"context"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/otfabric/go-iec60870-5/ft12"
	"github.com/otfabric/go-iec60870-5/iec102"
	"github.com/otfabric/go-iec60870-5/iec102/ree"
	"github.com/otfabric/go-iec60870-5/iec102/server"
	"github.com/otfabric/iec60870ctl/internal/iec102ctl/meter"
	"github.com/otfabric/iec60870ctl/internal/iec102ctl/types"
)

// Options configure a station beyond its model.
type Options struct {
	// LinkAddrSize is the library's link address size (1, 2 or iec102.NoLinkAddr).
	LinkAddrSize int
	// CASize is the size of the common address, 1 or 2.
	CASize int
	// Location is the zone of the meter's clock and of the model's times.
	Location *time.Location
	// LinkTimeout bounds the gap inside a frame (link.Config.FrameGap); zero is the
	// library's default.
	LinkTimeout time.Duration
	// LinkAddr and CommonAddr, when not nil, replace the model's.
	LinkAddr, CommonAddr *uint16
	// Log receives the events; nil drops them.
	Log func(types.ServerEvent)
	// Trace, when set, receives the frame trace of --debug.
	Trace io.Writer
	// Now is the computer's clock; nil is time.Now. Tests replace it.
	Now func() time.Time
}

// Station is a simulated integrated total station. Its store and clock live as long as the
// station; every connection gets a fresh link (and REE session).
type Station struct {
	m     *meterModel
	opts  Options
	store *server.Store

	mu     sync.Mutex
	fixed  *time.Time    // the clock stands still at this time
	offset time.Duration // else: the computer's clock plus this
	logMu  sync.Mutex
}

// New returns the station of model m.
func New(m *Model, opts Options) (*Station, error) {
	if opts.Location == nil {
		opts.Location = time.UTC
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	if opts.CASize == 0 {
		opts.CASize = iec102.DefaultCommonAddrSize
	}
	if opts.LinkAddrSize == 0 {
		opts.LinkAddrSize = iec102.DefaultLinkAddrSize
	}
	mm, err := compile(m, opts.Location)
	if err != nil {
		return nil, err
	}
	if opts.LinkAddr != nil {
		mm.linkAddr = *opts.LinkAddr
	}
	if opts.CommonAddr != nil {
		mm.commonAddr = *opts.CommonAddr
	}
	if err := meter.CheckLinkAddress(mm.linkAddr, opts.LinkAddrSize); err != nil {
		return nil, err
	}
	if opts.CASize != 1 && opts.CASize != 2 {
		return nil, fmt.Errorf("--ca-size %d: use 1 or 2", opts.CASize)
	}
	if err := meter.CheckCommonAddress(mm.commonAddr, uint8(opts.CASize)); err != nil { //nolint:gosec // 1 or 2, checked above
		return nil, err
	}
	s := &Station{m: mm, opts: opts, store: server.NewStore(), fixed: mm.fixedClock}
	for _, rec := range mm.recordOrder {
		for _, p := range mm.periods[rec] {
			s.store.AddPeriod(rec, p)
		}
	}
	for rec, pts := range mm.events {
		s.store.AddSinglePoints(rec, pts...)
	}
	return s, nil
}

// LinkAddr returns the station's link address.
func (s *Station) LinkAddr() uint16 { return s.m.linkAddr }

// CommonAddr returns the station's common address (measuring point address).
func (s *Station) CommonAddr() uint16 { return s.m.commonAddr }

// Name returns the station's name.
func (s *Station) Name() string { return s.m.name }

// Now returns the station's clock.
func (s *Station) Now() time.Time {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.fixed != nil {
		return *s.fixed
	}
	return s.opts.Now().Add(s.offset).In(s.opts.Location)
}

// setTime sets the clock to tb: a clock that stands still stands at tb, a running one keeps
// running from it.
func (s *Station) setTime(tb iec102.TimeB) {
	t, ok := tb.Time(s.opts.Location)
	if !ok {
		s.event(types.ServerEvent{Event: "set-time", Detail: "invalid time " + meter.TextB(tb) + " ignored"})
		return
	}
	s.mu.Lock()
	if s.fixed != nil {
		s.fixed = &t
	} else {
		s.offset = t.Sub(s.opts.Now())
	}
	s.mu.Unlock()
	d := meter.TextB(tb)
	if tb.SummerTime {
		d += " SU"
	}
	s.event(types.ServerEvent{Event: "set-time", Detail: d})
}

// params returns the codec parameters of the station.
func (s *Station) params() iec102.Params {
	p := iec102.Params{CommonAddrSize: s.opts.CASize, Location: s.opts.Location}
	if s.m.ree != nil {
		p.Profile = ree.Profile
	}
	return p
}

// event writes one event to the log.
func (s *Station) event(e types.ServerEvent) {
	if s.opts.Log == nil {
		return
	}
	if e.Timestamp == "" {
		e.Timestamp = time.Now().UTC().Format("2006-01-02T15:04:05.000Z")
	}
	s.logMu.Lock()
	defer s.logMu.Unlock()
	s.opts.Log(e)
}

// Serve runs the station on one line (a TCP connection or a serial port) until ctx ends or
// the line fails. remote names the peer in the log.
func (s *Station) Serve(ctx context.Context, rw io.ReadWriteCloser, remote string) {
	p := s.params()
	var tracer *meter.Tracer
	if s.opts.Trace != nil {
		tracer = &meter.Tracer{W: s.opts.Trace, Params: p}
	}
	cfg := server.Config{
		LinkAddrSize: s.opts.LinkAddrSize, LinkAddr: s.m.linkAddr, CommonAddr: s.m.commonAddr, Params: p,
		ProductSpec: s.m.product, Store: s.store, Clock: s.Now,
	}
	cfg.Link.FrameGap = s.opts.LinkTimeout
	cfg.Link.OnFrame = func(tx bool, f ft12.Frame) {
		if tracer != nil {
			tracer.Frame(tx, f)
		}
		if f.Kind == ft12.KindVariable {
			s.logASDU(tx, f.Data, p, remote)
		}
	}
	if tracer != nil {
		cfg.Link.OnDiscard = tracer.Discard
	}
	cfg.OnState = func(st iec102.State, _ error) {
		if st == iec102.StateUp {
			s.event(types.ServerEvent{Event: "link-up", Remote: remote})
		}
	}
	h := &handler{s: s}
	if s.m.ree != nil {
		cfg.Handler = &ree.Session{Key: s.m.ree.key, OnSetTime: s.setTime, Next: h}
	} else {
		cfg.Handler = h
	}
	srv, err := server.New(rw, cfg)
	if err != nil {
		_ = rw.Close()
		s.event(types.ServerEvent{Event: "disconnected", Remote: remote, Detail: err.Error()})
		return
	}
	select {
	case <-ctx.Done():
	case <-srv.Done():
	}
	_ = srv.Close()
}

// logASDU writes the event of an ASDU on the line: every request received, every refusal
// sent.
func (s *Station) logASDU(tx bool, data []byte, p iec102.Params, remote string) {
	a, err := iec102.Decode(data, p)
	if err != nil {
		if !tx {
			s.event(types.ServerEvent{Event: "request", Remote: remote, Detail: "undecodable ASDU: " + err.Error()})
		}
		return
	}
	ca, rec, cot := a.CommonAddr, a.Record, uint8(a.Cause.Code())
	e := types.ServerEvent{Remote: remote, CommonAddr: &ca, Record: &rec, Type: meter.TypeName(a.Type), Cot: &cot}
	if tx {
		if !a.Cause.Refusal() {
			return
		}
		e.Event, e.Detail = "refused", meter.CauseName(a.Cause)
		if a.Cause.Negative() {
			e.Detail += ", negative"
		}
		s.event(e)
		return
	}
	e.Event = "request"
	switch b := a.Body.(type) {
	case ree.AccessKey:
		e.Event = "login"
		e.Detail = "access key accepted"
		if s.m.ree == nil || b.Key != s.m.ree.key {
			e.Detail = "access key refused"
		}
	case ree.Request:
		switch b.T {
		case ree.C_FS_NA_2:
			e.Event = "logout"
		case ree.C_TA_VM_2:
			e.Detail = meter.TextA(b.From) + " .. " + meter.TextA(b.To)
		default:
		}
	case ree.SetClock:
		return // the set-time event comes from the session
	case iec102.Read:
		switch b.T.Selection() {
		case iec102.SelectTimeRange:
			e.Detail = fmt.Sprintf("%s .. %s, addresses %d-%d", meter.TextA(b.From), meter.TextA(b.To), b.First, b.Last)
		case iec102.SelectPeriod:
			e.Detail = "period " + meter.TextA(b.From)
		case iec102.SelectPeriodRange:
			e.Detail = fmt.Sprintf("period %s, addresses %d-%d", meter.TextA(b.From), b.First, b.Last)
		case iec102.SelectOldestRange:
			e.Detail = fmt.Sprintf("oldest period, addresses %d-%d", b.First, b.Last)
		case iec102.SelectOldest:
			e.Detail = "oldest period"
		case iec102.SelectNone:
			if b.T == iec102.C_SP_NB_2 {
				e.Detail = meter.TextA(b.From) + " .. " + meter.TextA(b.To)
			}
		}
	}
	s.event(e)
}

// handler answers the REE reads the session passes on: tariff information, contracted
// powers and special days. Everything else is refused with cause 14.
type handler struct{ s *Station }

// HandleASDU implements server.Handler.
func (h *handler) HandleASDU(srv *server.Server, a *iec102.ASDU) {
	q, ok := a.Body.(ree.Request)
	r := h.s.m.ree
	if !ok || r == nil {
		_ = srv.Mirror(a, iec102.CauseUnknownType)
		return
	}
	var bodies []iec102.Body
	switch q.T {
	case ree.C_TA_VC_2, ree.C_TA_VM_2:
		t, known := r.tariffs[a.Record]
		if !known {
			_ = srv.Mirror(a, iec102.CauseUnknownRecordAddr)
			return
		}
		recs := t.current
		answer := ree.M_TA_VC_2
		if q.T == ree.C_TA_VM_2 {
			answer, recs = ree.M_TA_VM_2, nil
			lo, hi := meter.TextA(q.From), meter.TextA(q.To)
			for _, rec := range t.stored {
				if e := meter.TextA(rec.End); e >= lo && e <= hi {
					recs = append(recs, rec)
				}
			}
		}
		for _, rec := range recs {
			bodies = append(bodies, ree.TariffInfo{T: answer, Records: []ree.TariffRecord{rec}})
		}
	case ree.C_PC_NA_2:
		if len(r.powers) > 0 {
			bodies = append(bodies, ree.ContractedPowers{Powers: r.powers, Time: iec102.TimeAOf(h.s.Now(), h.s.opts.Location)})
		}
	case ree.C_DF_NA_2:
		if len(r.days) > 0 {
			bodies = append(bodies, ree.SpecialDays{Days: r.days})
		}
	default:
		_ = srv.Mirror(a, iec102.CauseUnknownType)
		return
	}
	if len(bodies) == 0 {
		cause := iec102.CauseNoPeriod
		if q.T == ree.C_PC_NA_2 || q.T == ree.C_DF_NA_2 {
			cause = iec102.CauseNoObject
		}
		_ = srv.Mirror(a, cause)
		return
	}
	_ = srv.Mirror(a, iec102.CauseActivationCon)
	for _, b := range bodies {
		_ = srv.Send(iec102.New(iec102.CauseRequest, a.CommonAddr, a.Record, b))
	}
	_ = srv.Mirror(a, iec102.CauseActivationTerm)
}
