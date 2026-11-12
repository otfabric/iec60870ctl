// SPDX-License-Identifier: MIT

package station

import (
	"context"
	"encoding/hex"
	"fmt"
	"io"
	"slices"
	"sync"
	"time"

	"github.com/otfabric/go-iec60870-5/cp"
	"github.com/otfabric/go-iec60870-5/iec103"
	"github.com/otfabric/go-iec60870-5/iec103/server"
	"github.com/otfabric/go-iec60870-5/link"
	"github.com/otfabric/iec60870ctl/internal/iec103ctl/types"
)

func ioLimit(r io.Reader) io.Reader { return io.LimitReader(r, MaxModelSize+1) }

// Options are what `server start` adds to the model.
type Options struct {
	// LinkAddr and CommonAddr replace the model's when not negative. A new link address
	// also replaces a common address that equals the model's link address.
	LinkAddr, CommonAddr int
	// Location is the time zone of the time tags; nil means UTC.
	Location *time.Location
	// Log receives every event; nil drops them.
	Log func(types.ServerEvent)
	// Link is the link configuration: the frame trace of --debug.
	Link link.Config
	// FeedEvery is how often the class 2 queue is refilled; 0 means 5 ms.
	FeedEvery time.Duration
}

// Station is the process state of the device: the indications, the generic values and the
// recorded disturbances. It lives as long as the process; every line (connection) gets a
// fresh protection equipment station on it, as every connection is a line that has just
// come up.
type Station struct {
	m    *Model
	opts Options
	la   uint8
	ca   uint8
	fun  iec103.FunctionType

	mu      sync.Mutex
	dpi     map[funInf]iec103.DPI
	generic map[iec103.GIN]iec103.Value
	staged  map[uint8][]iec103.DataSet // generic writes in progress, by RII
}

// New returns the station of model m.
func New(m *Model, opts Options) (*Station, error) {
	if err := m.Validate(); err != nil {
		return nil, err
	}
	la, ca := m.LinkAddr(), m.Station.CommonAddress
	if opts.LinkAddr >= 0 {
		// A common address that is the link address (clause 7.2.4) follows it.
		if opts.CommonAddr < 0 && ca == la {
			ca = opts.LinkAddr
		}
		la = opts.LinkAddr
	}
	if opts.CommonAddr >= 0 {
		ca = opts.CommonAddr
	}
	if !inRange(la, 0, 254) || !inRange(ca, 0, 254) {
		return nil, fmt.Errorf("link address %d, common address %d: use 0-254", la, ca)
	}
	s := &Station{
		m: m, opts: opts, la: uint8(la & 0xFF), ca: uint8(ca & 0xFF), fun: iec103.FunctionType(m.Station.Identification.Fun & 0xFF),
		dpi: map[funInf]iec103.DPI{}, generic: map[iec103.GIN]iec103.Value{}, staged: map[uint8][]iec103.DataSet{},
	}
	for _, ind := range m.Indications {
		s.dpi[funInf{ind.Fun, ind.Inf}] = iec103.DPI(ind.DPI & 3)
	}
	for _, g := range m.Generic {
		v, _ := g.Actual()
		s.generic[gin(g.GIN)] = v
	}
	return s, nil
}

func gin(g GINDef) iec103.GIN {
	return iec103.GIN{Group: uint8(g.Group & 0xFF), Entry: uint8(g.Entry & 0xFF)}
}

func u8p(v int) *uint8 { b := uint8(v & 0xFF); return &b }

// event logs e with the current time.
func (s *Station) event(e types.ServerEvent) {
	if s.opts.Log == nil {
		return
	}
	e.Timestamp = time.Now().Format(time.RFC3339Nano)
	s.opts.Log(e)
}

// LinkAddr and CommonAddr are the addresses the station serves.
func (s *Station) LinkAddr() uint8 { return s.la }

// CommonAddr returns the common address of ASDU.
func (s *Station) CommonAddr() uint8 { return s.ca }

// DPI returns the state of the indication fun/inf.
func (s *Station) DPI(fun, inf int) iec103.DPI {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.dpi[funInf{fun, inf}]
}

// Generic returns the actual value of a generic entry.
func (s *Station) Generic(g iec103.GIN) (iec103.Value, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.generic[g]
	return v, ok
}

// config returns the protection equipment configuration of one line.
func (s *Station) config(remote string) server.Config {
	id := s.m.Station.Identification
	sw, _ := hex.DecodeString(id.Software)
	var software [4]byte
	copy(software[:], sw)
	start := iec103.CausePowerOn
	if s.m.Station.StartCause == "start-restart" {
		start = iec103.CauseStartRestart
	}
	cfg := server.Config{
		Link: s.opts.Link, LinkAddr: s.la, CommonAddr: s.ca, Fun: s.fun,
		Identification: iec103.IdentificationOf(uint8(id.Col&0xFF), id.ASCII, software),
		StartCause:     start,
		Handler:        s,
		Location:       s.opts.Location,
		SyncDelay:      -1,
		OnTimeSync: func(t time.Time, broadcast bool) {
			s.event(types.ServerEvent{Event: "clock-sync", Remote: remote, Value: t.In(loc(s.opts.Location)).Format(time.RFC3339Nano), Detail: fmt.Sprintf("broadcast=%t", broadcast)})
		},
		OnState: func(st iec103.State, err error) {
			d := st.String()
			if err != nil {
				d += ": " + err.Error()
			}
			s.event(types.ServerEvent{Event: "link", Remote: remote, Detail: d})
		},
	}
	if len(s.m.Disturbances) > 0 {
		cfg.Disturbances = s
	}
	return cfg
}

func loc(l *time.Location) *time.Location {
	if l == nil {
		return time.UTC
	}
	return l
}

// Serve runs the device on one line until ctx ends or the line fails: a fresh protection
// equipment station, the measurands queued as class 2 data and the simulated changes.
func (s *Station) Serve(ctx context.Context, rw io.ReadWriter, remote string) error {
	w := &watched{rw: rw, closed: make(chan struct{})}
	srv, err := server.New(w, s.config(remote))
	if err != nil {
		return err
	}
	var wg sync.WaitGroup
	stop := make(chan struct{})
	wg.Go(func() { s.feed(srv, stop) })
	for _, ind := range s.m.Indications {
		if ind.Simulate > 0 {
			wg.Go(func() { s.simulate(srv, ind, stop) })
		}
	}
	select {
	case <-ctx.Done():
	case <-w.closed:
	}
	close(stop)
	wg.Wait()
	return srv.Close()
}

// watched reports the end of a line: the first failed read.
type watched struct {
	rw     io.ReadWriter
	once   sync.Once
	closed chan struct{}
}

func (w *watched) Read(b []byte) (int, error) {
	n, err := w.rw.Read(b)
	if err != nil {
		w.once.Do(func() { close(w.closed) })
	}
	return n, err
}

func (w *watched) Write(b []byte) (int, error) { return w.rw.Write(b) }

// Close closes the line when it can be closed.
func (w *watched) Close() error {
	if c, ok := w.rw.(io.Closer); ok {
		return c.Close()
	}
	return nil
}

// feedInterval is how often the class 2 queue is looked at.
const feedInterval = 5 * time.Millisecond

// feed keeps the next measurand of the model queued as class 2 data, cyclically (cause 2).
func (s *Station) feed(srv *server.Server, stop <-chan struct{}) {
	if len(s.m.Measurands) == 0 {
		return
	}
	every := s.opts.FeedEvery
	if every <= 0 {
		every = feedInterval
	}
	tick := time.NewTicker(every)
	defer tick.Stop()
	for i := 0; ; {
		if srv.Pending(server.Class2) == 0 {
			me := s.m.Measurands[i%len(s.m.Measurands)]
			vals := make([]iec103.MEA, 0, len(me.Values))
			for _, v := range me.Values {
				vals = append(vals, iec103.MEA{Raw: clamp16(v.Value), Overflow: v.OV, Error: v.ER})
			}
			a := iec103.New(iec103.CauseCyclic, s.ca, iec103.FunctionType(me.Fun&0xFF), iec103.InfoNumber(me.Inf&0xFF),
				iec103.Measurands{Set2: me.Type == 9, Values: vals})
			if srv.Send(a, server.Class2) == nil {
				i++
			}
		}
		select {
		case <-stop:
			return
		case <-tick.C:
		}
	}
}

// simulate toggles an indication at its period and reports the change (cause 1).
func (s *Station) simulate(srv *server.Server, ind Indication, stop <-chan struct{}) {
	tick := time.NewTicker(time.Duration(ind.Simulate) * time.Millisecond)
	defer tick.Stop()
	k := funInf{ind.Fun, ind.Inf}
	for {
		select {
		case <-stop:
			return
		case <-tick.C:
		}
		s.mu.Lock()
		next := iec103.DPIOn
		if s.dpi[k] == iec103.DPIOn {
			next = iec103.DPIOff
		}
		s.dpi[k] = next
		s.mu.Unlock()
		_ = srv.Event(iec103.CauseSpontaneous, iec103.FunctionType(ind.Fun&0xFF), iec103.InfoNumber(ind.Inf&0xFF), next)
		s.event(types.ServerEvent{Event: "spontaneous", Fun: u8p(ind.Fun), Inf: u8p(ind.Inf), Value: next.String()})
	}
}

// HandleASDU answers the control-direction ASDUs the protection equipment station leaves
// to the application (server.Handler). It runs on the station's goroutine.
func (s *Station) HandleASDU(srv *server.Server, a *iec103.ASDU) {
	switch b := a.Body.(type) {
	case iec103.GIInit:
		s.interrogation(srv, a, b)
	case iec103.GeneralCommand:
		s.command(srv, a, b)
	case iec103.GenericCommand:
		s.genericRead(srv, a, b)
	case iec103.GenericData:
		s.genericWrite(srv, a, b)
	}
}

// interrogation answers a general interrogation (clause 7.4.3): ASDU 1, cause 9, for
// every indication that takes part, in the model's order, then the termination.
func (s *Station) interrogation(srv *server.Server, a *iec103.ASDU, b iec103.GIInit) {
	s.mu.Lock()
	var data []*iec103.ASDU
	for _, ind := range s.m.Indications {
		if ind.GI != nil && !*ind.GI {
			continue
		}
		data = append(data, iec103.New(iec103.CauseGI, s.ca, iec103.FunctionType(ind.Fun&0xFF), iec103.InfoNumber(ind.Inf&0xFF),
			iec103.TimeTagged{DPI: s.dpi[funInf{ind.Fun, ind.Inf}], Time: srv.Now()}))
	}
	s.mu.Unlock()
	_ = srv.AnswerGI(a, data...)
	s.event(types.ServerEvent{Event: "interrogation", Value: len(data), Detail: fmt.Sprintf("scn=%d", b.SCN)})
}

// command carries out a general command of the model (clause 7.4.4): the indication takes
// the DCO, the command is acknowledged with cause 20 and a change follows with cause 12;
// any other command is refused with cause 21.
func (s *Station) command(srv *server.Server, a *iec103.ASDU, b iec103.GeneralCommand) {
	k := funInf{int(a.Fun), int(a.Inf)}
	ok := slices.Contains(s.m.Commands, CommandDef{k.fun, k.inf})
	e := types.ServerEvent{Event: "command", Fun: u8p(k.fun), Inf: u8p(k.inf), Value: b.DCO.String(), Detail: fmt.Sprintf("rii=%d outcome=rejected", b.RII)}
	if !ok {
		_ = srv.AckCommand(a, false)
		s.event(e)
		return
	}
	s.mu.Lock()
	changed := s.dpi[k] != b.DCO
	s.dpi[k] = b.DCO
	s.mu.Unlock()
	_ = srv.AckCommand(a, true)
	if changed {
		_ = srv.Event(iec103.CauseRemoteOperation, a.Fun, a.Inf, b.DCO)
	}
	e.Detail = fmt.Sprintf("rii=%d outcome=executed", b.RII)
	s.event(e)
}

// List, Record and Transmitted serve the recorded disturbances of the model
// (server.DisturbanceSource); the records stay after a transfer.
func (s *Station) List() []iec103.RecordedFault {
	var l []iec103.RecordedFault
	for _, d := range s.m.Disturbances {
		t, _ := time.Parse(faultTime, d.Time)
		l = append(l, iec103.RecordedFault{FAN: uint16(d.FAN & 0xFFFF), SOF: d.sof(), Time: cp.At(t.In(loc(s.opts.Location)))})
	}
	return l
}

func (d Record) sof() iec103.SOF {
	var v iec103.SOF
	if d.SOF.TP {
		v |= iec103.SOFTrip
	}
	if d.SOF.TM {
		v |= iec103.SOFTM
	}
	if d.SOF.Test {
		v |= iec103.SOFTest
	}
	if d.SOF.OTEV {
		v |= iec103.SOFOTEV
	}
	return v
}

// start is the time of the first value: the record's time of day on the date of the fault
// (ASDU 26 carries a CP32Time2a, no date).
func (d Record) start() time.Time {
	ft, _ := time.Parse(faultTime, d.Time)
	tod, _ := time.Parse(startTime, d.Start)
	day := time.Date(ft.Year(), ft.Month(), ft.Day(), 0, 0, 0, 0, ft.Location())
	return day.Add(time.Duration(tod.Hour())*time.Hour + time.Duration(tod.Minute())*time.Minute +
		time.Duration(tod.Second())*time.Second + time.Duration(tod.Nanosecond()))
}

// Record returns the record of fault number fan.
func (s *Station) Record(fan uint16) (*iec103.Disturbance, bool) {
	for _, d := range s.m.Disturbances {
		if d.FAN != int(fan) {
			continue
		}
		rec := &iec103.Disturbance{
			Fun: s.fun, TOV: iec103.TOVInstantaneous, FAN: fan, NOF: uint16(d.NOF & 0xFFFF),
			Interval: uint16(d.Interval & 0xFFFF), Start: cp.At(d.start().In(loc(s.opts.Location))),
		}
		for _, t := range d.Tags {
			rec.Tags = append(rec.Tags, iec103.TagChange{Pos: uint16(t.Pos & 0xFFFF),
				Tag: iec103.Tag{Fun: iec103.FunctionType(t.Fun & 0xFF), Inf: iec103.InfoNumber(t.Inf & 0xFF), DPI: iec103.DPI(t.DPI & 3)}})
		}
		for _, c := range d.Channels {
			vals := make([]int16, len(c.Values))
			for i, v := range c.Values {
				vals[i] = clamp16(v)
			}
			rec.Channels = append(rec.Channels, iec103.Channel{ACC: uint8(c.ACC & 0xFF), RPV: float32(c.RPV), RSV: float32(c.RSV), RFA: float32(c.RFA), Values: vals})
		}
		return rec, true
	}
	return nil, false
}

// Transmitted logs the final acknowledgement of a transfer.
func (s *Station) Transmitted(fan uint16, ok bool) {
	s.event(types.ServerEvent{Event: "disturbance-transmitted", Value: fan, Detail: fmt.Sprintf("positive=%t", ok)})
}
