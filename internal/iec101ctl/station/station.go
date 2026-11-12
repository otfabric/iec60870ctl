// SPDX-License-Identifier: MIT

package station

import (
	"context"
	"fmt"
	"io"
	"math"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/otfabric/go-iec60870-5/asdu"
	"github.com/otfabric/go-iec60870-5/iec101"
	"github.com/otfabric/go-iec60870-5/iec101/server"
	"github.com/otfabric/iec60870ctl/internal/iec101ctl/iec"
	"github.com/otfabric/iec60870ctl/internal/iec101ctl/types"
)

// Options configure a [Station] beyond its model.
type Options struct {
	// Link holds the system parameters and the link addresses of the line.
	Link iec.Link
	// NoEndOfInit suppresses end of initialization after the first reset of the link.
	NoEndOfInit bool
	// RetryInterval is the pause before a balanced station initializes its direction of
	// the link again; zero is the library's default.
	RetryInterval time.Duration
	// Log receives every event of the station. It is called from several goroutines, one
	// call at a time.
	Log func(types.ServerEvent)
	// Trace, when not nil, receives every frame, for --debug.
	Trace io.Writer
	// Now is the clock of the station; time.Now when nil.
	Now func() time.Time
}

type point struct {
	spec    PointSpec
	typ     asdu.TypeID
	quality asdu.Quality
	timeTag bool
	initial value
	val     value
	seq     uint8 // sequence number of an integrated total
	tick    int   // simulation step
}

type command struct {
	spec    CommandSpec
	typ     asdu.TypeID
	timeout time.Duration
	report  asdu.Cause
}

type selection struct {
	conn   uint64
	object string // the selected command, without the select bit
	until  time.Time
}

// maxBacklog bounds the events kept while no controlling station is connected.
const maxBacklog = 1024

// Station is a simulated controlled station. Its process state (values, selections, clock)
// lives as long as the station; each line it is served on gets a controlled station of
// go-iec60870-5 of its own, as a line that has just come up.
type Station struct {
	model  *Model
	opts   Options
	ca     asdu.CommonAddr
	freeze bool

	mu       sync.Mutex
	points   map[asdu.IOA]*point
	order    []asdu.IOA
	commands map[asdu.IOA]*command
	selected map[asdu.IOA]selection
	// offset is what a clock synchronization moved the station's clock by; delay is the
	// transmission delay the controlling station sent (C_CD_NA_1, cause spontaneous).
	offset, delay time.Duration
	// cur is the controlled station of the line being served, nil between lines.
	cur     *server.Server
	conns   uint64
	backlog []*asdu.ASDU

	logMu sync.Mutex
	wg    sync.WaitGroup
}

// New builds a station from a validated model. Every address of the model must fit the
// field sizes of the options.
func New(m *Model, opts Options) (*Station, error) {
	if err := m.Validate(); err != nil {
		return nil, err
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	if opts.Link.Params.ASDU.IOASize == 0 {
		l, err := iec.LinkParams(defaultLink())
		if err != nil {
			return nil, err
		}
		opts.Link = l
	}
	ap := opts.Link.Params.ASDU
	if bc := iec.BroadcastCA(ap.CommonAddrSize); m.Station.CommonAddress >= bc {
		return nil, fmt.Errorf("station.common_address %d does not fit a %d-octet common address (1-%d)", m.Station.CommonAddress, ap.CommonAddrSize, bc-1)
	}
	maxIOA := iec.MaxIOAFor(ap.IOASize)
	var tooLarge []string
	for _, p := range m.Points {
		if p.IOA > maxIOA {
			tooLarge = append(tooLarge, fmt.Sprint(p.IOA))
		}
	}
	for _, c := range m.Commands {
		if c.IOA > maxIOA {
			tooLarge = append(tooLarge, fmt.Sprint(c.IOA))
		}
	}
	if len(tooLarge) > 0 {
		return nil, fmt.Errorf("information object addresses %s do not fit a %d-octet address (1-%d)", strings.Join(tooLarge, ", "), ap.IOASize, maxIOA)
	}
	st := &Station{
		model:    m,
		opts:     opts,
		ca:       asdu.CommonAddr(m.Station.CommonAddress),
		freeze:   m.Station.CounterFreeze == nil || *m.Station.CounterFreeze,
		points:   map[asdu.IOA]*point{},
		commands: map[asdu.IOA]*command{},
		selected: map[asdu.IOA]selection{},
	}
	for _, spec := range m.Points {
		typ := monitorTypes[strings.ToUpper(strings.TrimSpace(spec.Type))]
		v, _ := parseValue(typ, spec.Value)
		p := &point{spec: spec, typ: typ, initial: v, val: v, seq: spec.Sequence, timeTag: spec.TimeTag == nil || *spec.TimeTag}
		for _, q := range spec.Quality {
			p.quality |= qualityFlags[strings.ToUpper(strings.TrimSpace(q))]
		}
		st.points[asdu.IOA(spec.IOA)] = p
		st.order = append(st.order, asdu.IOA(spec.IOA))
	}
	slices.Sort(st.order)
	for _, spec := range m.Commands {
		c := &command{spec: spec, typ: commandTypes[strings.ToUpper(strings.TrimSpace(spec.Type))], timeout: time.Duration(spec.SelectTimeout), report: asdu.Cause(spec.ReportCause)}
		if c.timeout == 0 {
			c.timeout = defaultSelectTimeout
		}
		if c.report == 0 {
			c.report = asdu.CauseReturnRemote
		}
		st.commands[asdu.IOA(spec.IOA)] = c
	}
	return st, nil
}

// CommonAddr returns the common address of the station.
func (st *Station) CommonAddr() uint16 { return uint16(st.ca) }

// Describe returns a line that says what the station serves.
func (st *Station) Describe() string {
	l := st.opts.Link
	return fmt.Sprintf("station %d (%s), %d points, %d commands; %s, link address %d (%d octets), cot %d, ca %d, ioa %d octets",
		st.ca, st.name(), len(st.points), len(st.commands), l.Params.Mode, l.Addr, l.Params.Link.FT12.AddrSize,
		l.Params.ASDU.CauseSize, l.Params.ASDU.CommonAddrSize, l.Params.ASDU.IOASize)
}

func (st *Station) name() string {
	if n := st.model.Station.Name; n != "" {
		return n
	}
	return "unnamed"
}

// now is the station's clock: the system clock, moved by clock synchronization.
func (st *Station) now() time.Time {
	st.mu.Lock()
	defer st.mu.Unlock()
	return st.opts.Now().Add(st.offset)
}

func (st *Station) event(e types.ServerEvent) {
	if st.opts.Log == nil {
		return
	}
	e.Timestamp = st.opts.Now().UTC().Format("2006-01-02T15:04:05.000Z07:00")
	st.logMu.Lock()
	defer st.logMu.Unlock()
	st.opts.Log(e)
}

// Event logs an event of the station.
func (st *Station) Event(e types.ServerEvent) { st.event(e) }

// conn is one line the station is served on: the handler of its controlled station.
type conn struct {
	st     *Station
	id     uint64
	remote string
}

// ServeLine runs a controlled station on rw, a line that has just come up, until ctx ends
// or the line fails, and closes rw. It returns the error that ended the line, nil when ctx
// ended it.
func (st *Station) ServeLine(ctx context.Context, rw io.ReadWriteCloser, remote string) error {
	st.mu.Lock()
	st.conns++
	c := &conn{st: st, id: st.conns, remote: remote}
	st.mu.Unlock()

	l := st.opts.Link
	p := l.Params
	if st.opts.Trace != nil {
		iec.NewTracer(st.opts.Trace, p).Attach(&p.Link)
	}
	srv, err := server.New(rw, server.Config{
		Params:        p,
		LinkAddr:      l.Addr,
		Remote:        l.Remote,
		CommonAddr:    st.ca,
		Handler:       c,
		NoEndOfInit:   st.opts.NoEndOfInit,
		RetryInterval: st.opts.RetryInterval,
		OnState:       c.state,
	})
	if err != nil {
		_ = rw.Close()
		return err
	}
	st.mu.Lock()
	st.cur = srv
	backlog := st.backlog
	st.backlog = nil
	st.mu.Unlock()
	for _, a := range backlog {
		_ = srv.Send(a)
	}
	st.event(types.ServerEvent{Event: "connected", Remote: remote})

	select {
	case <-ctx.Done():
	case <-srv.Done():
	}
	lineErr := srv.Err()
	_ = srv.Close()
	st.mu.Lock()
	st.cur = nil
	for ioa, sel := range st.selected {
		if sel.conn == c.id {
			delete(st.selected, ioa)
		}
	}
	st.mu.Unlock()
	e := types.ServerEvent{Event: "disconnected", Remote: remote}
	if lineErr != nil {
		e.Detail = lineErr.Error()
	}
	st.event(e)
	return lineErr
}

func (c *conn) state(s iec101.State, err error) {
	e := types.ServerEvent{Remote: c.remote}
	switch s {
	case iec101.StateUp:
		e.Event = "link-up"
	case iec101.StateDown:
		e.Event = "link-down"
		if err != nil {
			e.Detail = err.Error()
		}
	case iec101.StateClosed:
		return
	}
	c.st.event(e)
}

// send queues a monitor-direction ASDU of the station's own initiative on the line being
// served. Between lines an event (class 1) is kept, up to a bound, for the next line;
// class 2 data is dropped, since the next line asks for current values anyway.
func (st *Station) send(a *asdu.ASDU, class server.Class) {
	st.mu.Lock()
	cur := st.cur
	if cur == nil {
		if class == server.Class1 && len(st.backlog) < maxBacklog {
			st.backlog = append(st.backlog, a)
		}
		st.mu.Unlock()
		return
	}
	st.mu.Unlock()
	_ = cur.SendClass(a, class)
}

// HandleASDU implements server.Handler.
func (c *conn) HandleASDU(s *server.Server, req *asdu.ASDU) {
	st := c.st
	if req.CommonAddr != st.ca && !s.Params().ASDU.IsBroadcast(req.CommonAddr) {
		_ = s.Reject(req, asdu.CauseUnknownCommonAddr)
		st.event(types.ServerEvent{Event: "refused", Remote: c.remote, CommonAddr: uint16(req.CommonAddr), Type: req.Type.String(), Detail: "unknown common address"})
		return
	}
	switch o := req.First().(type) {
	case asdu.Interrogation:
		c.interrogation(s, req, o)
	case asdu.CounterInterrogation:
		c.counterInterrogation(s, req, o)
	case asdu.Read:
		c.read(s, req, o)
	case asdu.ClockSync:
		c.clockSync(s, req, o)
	case asdu.DelayAcquisition:
		c.delayAcquisition(s, req, o)
	case asdu.TestCommand:
		c.testCommand(s, req)
	case asdu.ResetProcess:
		c.resetProcess(s, req, o)
	default:
		if _, ok := commandTypes[req.Type.String()]; ok && req.First() != nil {
			c.command(s, req)
			return
		}
		_ = s.Reject(req, asdu.CauseUnknownType)
		st.event(types.ServerEvent{Event: "refused", Remote: c.remote, CommonAddr: uint16(st.ca), Type: req.Type.String(), Detail: "unknown type identification"})
	}
}

// object builds the information object of a point. The caller holds st.mu.
func (p *point) object(tag asdu.Timestamp) asdu.InformationObject {
	ioa := asdu.IOA(p.spec.IOA)
	switch p.typ {
	case asdu.M_SP_NA_1:
		return asdu.SinglePoint{IOA: ioa, Value: p.val.b, Quality: p.quality, Time: tag}
	case asdu.M_DP_NA_1:
		return asdu.DoublePoint{IOA: ioa, Value: p.val.d, Quality: p.quality, Time: tag}
	case asdu.M_ST_NA_1:
		return asdu.StepPosition{IOA: ioa, Value: int8(p.val.f), Quality: p.quality, Time: tag}
	case asdu.M_BO_NA_1:
		return asdu.Bitstring32{IOA: ioa, Value: uint32(p.val.f), Quality: p.quality, Time: tag}
	case asdu.M_ME_NA_1:
		return asdu.MeasuredNormalized{IOA: ioa, Value: asdu.NormalizedFromFloat(p.val.f), Quality: p.quality, Time: tag}
	case asdu.M_ME_NB_1:
		return asdu.MeasuredScaled{IOA: ioa, Value: int16(p.val.f), Quality: p.quality, Time: tag}
	case asdu.M_ME_NC_1:
		return asdu.MeasuredFloat{IOA: ioa, Value: float32(p.val.f), Quality: p.quality, Time: tag}
	default:
		return asdu.IntegratedTotal{IOA: ioa, Value: int32(p.val.f), Sequence: p.seq, Invalid: p.quality&asdu.QualityInvalid != 0, Time: tag}
	}
}

// shown is the value of a point as the event log shows it. The caller holds st.mu.
func (p *point) shown() any {
	switch p.typ {
	case asdu.M_SP_NA_1:
		return p.val.b
	case asdu.M_DP_NA_1:
		return p.val.d.String()
	case asdu.M_ME_NA_1, asdu.M_ME_NC_1:
		return math.Round(p.val.f*1e6) / 1e6
	default:
		return int64(p.val.f)
	}
}

// perASDU is how many objects go into one ASDU of an interrogation: the largest object
// without time tag takes 8 octets with a three-octet address, and a frame carries at least
// 249 octets of ASDU.
const perASDU = 29

// report builds the ASDUs that hold the points sel selects, in ascending address order:
// consecutive points of one type share an ASDU, a change of type starts a new one.
func (st *Station) report(cause asdu.Cause, originator uint8, sel func(*point) bool) []*asdu.ASDU {
	st.mu.Lock()
	defer st.mu.Unlock()
	var out []*asdu.ASDU
	var cur *asdu.ASDU
	for _, ioa := range st.order {
		p := st.points[ioa]
		if !sel(p) {
			continue
		}
		if cur == nil || cur.Type != p.typ || len(cur.Objects) >= perASDU {
			cur = &asdu.ASDU{Type: p.typ, Cause: cause, Originator: originator, CommonAddr: st.ca}
			out = append(out, cur)
		}
		cur.Objects = append(cur.Objects, p.object(asdu.Timestamp{}))
	}
	return out
}

// own addresses the mirror of a broadcast request with the station's own common address.
func (st *Station) own(req *asdu.ASDU) *asdu.ASDU {
	if req.CommonAddr == st.ca {
		return req
	}
	c := *req
	c.CommonAddr = st.ca
	return &c
}

func (c *conn) ev(kind string, detail string) types.ServerEvent {
	return types.ServerEvent{Event: kind, Remote: c.remote, CommonAddr: uint16(c.st.ca), Detail: detail}
}

func (c *conn) interrogation(s *server.Server, req *asdu.ASDU, o asdu.Interrogation) {
	st := c.st
	req = st.own(req)
	group := int(o.Qualifier) - int(asdu.QOIStation)
	if req.Cause != asdu.CauseActivation || group < 0 || group > 16 {
		_ = s.Negative(req)
		st.event(c.ev("interrogation", fmt.Sprintf("refused: qualifier %d, cause %s", o.Qualifier, req.Cause)))
		return
	}
	data := st.report(o.Qualifier.Cause(), req.Originator, func(p *point) bool {
		if p.typ == asdu.M_IT_NA_1 {
			return false // counters answer a counter interrogation
		}
		return group == 0 || int(p.spec.Group) == group
	})
	if group != 0 && len(data) == 0 {
		_ = s.Negative(req)
		st.event(c.ev("interrogation", fmt.Sprintf("refused: group %d has no points", group)))
		return
	}
	_ = s.Confirm(req)
	n := 0
	for _, a := range data {
		n += len(a.Objects)
		_ = s.Send(a)
	}
	_ = s.Terminate(req)
	detail := "station"
	if group != 0 {
		detail = fmt.Sprintf("group %d", group)
	}
	st.event(c.ev("interrogation", fmt.Sprintf("%s, %d objects", detail, n)))
}

func (c *conn) counterInterrogation(s *server.Server, req *asdu.ASDU, ci asdu.CounterInterrogation) {
	st := c.st
	req = st.own(req)
	group := 0
	switch {
	case ci.Request >= asdu.CounterGroup1 && ci.Request <= asdu.CounterGroup4:
		group = int(ci.Request)
	case ci.Request != asdu.CounterGeneral:
		_ = s.Negative(req)
		st.event(c.ev("counter-interrogation", fmt.Sprintf("refused: request %d", ci.Request)))
		return
	}
	if req.Cause != asdu.CauseActivation || ci.Freeze > asdu.FreezeWithReset || (ci.Freeze != asdu.FreezeRead && !st.freeze) {
		_ = s.Negative(req)
		st.event(c.ev("counter-interrogation", fmt.Sprintf("refused: freeze %d, cause %s", ci.Freeze, req.Cause)))
		return
	}
	sel := func(p *point) bool {
		return p.typ == asdu.M_IT_NA_1 && (group == 0 || int(p.spec.Group) == group)
	}
	if ci.Freeze != asdu.FreezeRead {
		// A counter freeze command: confirmed only (IEC 60870-5-101:2003 7.4.8, mode C).
		st.mu.Lock()
		n := 0
		for _, p := range st.points {
			if !sel(p) {
				continue
			}
			n++
			p.seq = (p.seq + 1) % 32
			if ci.Freeze == asdu.FreezeWithReset || ci.Freeze == asdu.FreezeResetOnly {
				p.val.f = 0
			}
		}
		st.mu.Unlock()
		_ = s.Confirm(req)
		st.event(c.ev("counter-interrogation", fmt.Sprintf("request %d, freeze %d, %d counters", ci.Request, ci.Freeze, n)))
		return
	}
	data := st.report(ci.Cause(), req.Originator, sel)
	_ = s.Confirm(req)
	n := 0
	for _, a := range data {
		n += len(a.Objects)
		_ = s.Send(a)
	}
	_ = s.Terminate(req)
	st.event(c.ev("counter-interrogation", fmt.Sprintf("request %d, %d counters", ci.Request, n)))
}

func (c *conn) read(s *server.Server, req *asdu.ASDU, o asdu.Read) {
	st := c.st
	st.mu.Lock()
	p := st.points[o.IOA]
	var obj asdu.InformationObject
	if p != nil {
		obj = p.object(asdu.Timestamp{})
	}
	st.mu.Unlock()
	e := c.ev("read", "")
	e.IOA = uint32(o.IOA)
	if p == nil {
		_ = s.Reject(req, asdu.CauseUnknownIOA)
		e.Detail = "refused: unknown information object address"
		st.event(e)
		return
	}
	a := asdu.New(asdu.CauseRequest, st.ca, obj)
	a.Originator = req.Originator
	_ = s.Send(a)
	e.Type = a.Type.String()
	st.event(e)
}

func (c *conn) clockSync(s *server.Server, req *asdu.ASDU, o asdu.ClockSync) {
	st := c.st
	req = st.own(req)
	if req.Cause != asdu.CauseActivation || o.Time.IsZero() || o.Time.Invalid {
		_ = s.Negative(req)
		st.event(c.ev("clock", "refused: invalid time or cause"))
		return
	}
	st.mu.Lock()
	// The time is corrected by the transmission delay the controlling station sent
	// (IEC 60870-5-101:2003 7.4.12).
	t := o.Time.Add(st.delay)
	st.offset = t.Sub(st.opts.Now())
	offset := st.offset
	st.mu.Unlock()
	_ = s.Confirm(req)
	st.event(c.ev("clock", fmt.Sprintf("set to %s (%+.3fs)", t.UTC().Format(time.RFC3339Nano), offset.Seconds())))
}

func (c *conn) delayAcquisition(s *server.Server, req *asdu.ASDU, o asdu.DelayAcquisition) {
	st := c.st
	begin := st.opts.Now()
	switch req.Cause {
	case asdu.CauseSpontaneous:
		st.mu.Lock()
		st.delay = time.Duration(o.Delay) * time.Millisecond
		st.mu.Unlock()
		st.event(c.ev("delay", fmt.Sprintf("transmission delay set to %d ms", o.Delay)))
	case asdu.CauseActivation:
		// The confirmation carries the sending time plus the reaction time of the station.
		reaction := st.opts.Now().Sub(begin)
		r := req.Reply(asdu.CauseActivationCon, false)
		r.CommonAddr = st.ca
		r.Objects = []asdu.InformationObject{asdu.DelayAcquisition{Delay: delayAfter(o.Delay, reaction)}}
		_ = s.Send(r)
		st.event(c.ev("delay", "delay acquisition confirmed"))
	default:
		_ = s.Negative(req)
		st.event(c.ev("delay", "refused: cause "+req.Cause.String()))
	}
}

func (c *conn) testCommand(s *server.Server, req *asdu.ASDU) {
	if req.Type != asdu.C_TS_NA_1 {
		// C_TS_TA_1 is IEC 60870-5-104's.
		_ = s.Reject(req, asdu.CauseUnknownType)
		c.st.event(c.ev("test", "refused: "+req.Type.String()+" is not a test command of IEC 60870-5-101"))
		return
	}
	if req.Cause != asdu.CauseActivation {
		_ = s.Negative(req)
		return
	}
	_ = s.Confirm(req)
	c.st.event(c.ev("test", "test command confirmed"))
}

// resetProcess puts every point back to the value of the model and announces the end of
// initialization.
func (c *conn) resetProcess(s *server.Server, req *asdu.ASDU, o asdu.ResetProcess) {
	st := c.st
	req = st.own(req)
	q := o.Qualifier
	if req.Cause != asdu.CauseActivation || (q != asdu.ResetProcessGeneral && q != asdu.ResetPendingEvents) {
		_ = s.Negative(req)
		return
	}
	detail := "pending events"
	st.mu.Lock()
	if q == asdu.ResetProcessGeneral {
		// The values are back before the confirmation: whoever acts on it sees them.
		detail = "process: values back to the model"
		for _, p := range st.points {
			p.val, p.seq, p.tick = p.initial, p.spec.Sequence, 0
		}
		st.selected = map[asdu.IOA]selection{}
	} else {
		st.backlog = nil
	}
	st.mu.Unlock()
	_ = s.Confirm(req)
	if q == asdu.ResetProcessGeneral {
		_ = s.Send(asdu.New(asdu.CauseInitialized, st.ca, asdu.EndOfInitialization{Cause: asdu.InitRemoteReset}))
	}
	st.event(c.ev("reset", detail))
}

// tag returns the time tag of an event of p: the station's time, or none.
func (st *Station) tag(p *point) asdu.Timestamp {
	if !p.timeTag {
		return asdu.Timestamp{}
	}
	return asdu.At(st.now())
}

// eventOf builds the report of the current value of the point at ioa, or nil.
func (st *Station) eventOf(ioa asdu.IOA, cause asdu.Cause) (*asdu.ASDU, *point) {
	st.mu.Lock()
	p := st.points[ioa]
	st.mu.Unlock()
	if p == nil {
		return nil, nil
	}
	tag := st.tag(p)
	st.mu.Lock()
	obj := p.object(tag)
	st.mu.Unlock()
	return asdu.New(cause, st.ca, obj), p
}

// announce reports the current value of the point at ioa as a spontaneous event: class 1
// data in unbalanced transmission. A change of state (single and double points) is kept
// for the next line while none is served; a change of a measured value, a step position, a
// bitstring or a counter is not: a station that comes back gets the current values from
// its general interrogation, not a backlog of old measurements.
func (st *Station) announce(ioa asdu.IOA) {
	a, p := st.eventOf(ioa, asdu.CauseSpontaneous)
	if a == nil {
		return
	}
	if p.typ != asdu.M_SP_NA_1 && p.typ != asdu.M_DP_NA_1 {
		st.mu.Lock()
		connected := st.cur != nil
		st.mu.Unlock()
		if !connected {
			return
		}
	}
	st.send(a, server.Class1)
}

// commanded describes a received command: its value, whether it is a select, and a key
// that identifies the command without the select bit.
func commanded(obj asdu.InformationObject) (v any, sel bool, key string) {
	switch o := obj.(type) {
	case asdu.SingleCommand:
		return o.Value, o.Select, fmt.Sprintf("sc %v %d", o.Value, o.Qualifier)
	case asdu.DoubleCommand:
		return o.Value.String(), o.Select, fmt.Sprintf("dc %d %d", o.Value, o.Qualifier)
	case asdu.StepCommand:
		dir := "lower"
		if o.Value == asdu.StepHigher {
			dir = "higher"
		}
		return dir, o.Select, fmt.Sprintf("rc %d %d", o.Value, o.Qualifier)
	case asdu.SetpointNormalized:
		return o.Value.Float64(), o.Select, fmt.Sprintf("se-na %d %d", o.Value, o.Qualifier)
	case asdu.SetpointScaled:
		return int64(o.Value), o.Select, fmt.Sprintf("se-nb %d %d", o.Value, o.Qualifier)
	case asdu.SetpointFloat:
		return float64(o.Value), o.Select, fmt.Sprintf("se-nc %v %d", o.Value, o.Qualifier)
	case asdu.BitstringCommand:
		return int64(o.Value), false, fmt.Sprintf("bo %d", o.Value)
	}
	return nil, false, ""
}

// valid reports whether the station can carry out the command.
func valid(obj asdu.InformationObject) bool {
	switch o := obj.(type) {
	case asdu.DoubleCommand:
		return o.Value == asdu.DoubleOff || o.Value == asdu.DoubleOn
	case asdu.StepCommand:
		return o.Value == asdu.StepLower || o.Value == asdu.StepHigher
	}
	return true
}

// atLimit reports whether a step command would move a step position beyond its range.
// The caller holds st.mu.
func (p *point) atLimit(obj asdu.InformationObject) bool {
	o, ok := obj.(asdu.StepCommand)
	if !ok || p == nil || p.typ != asdu.M_ST_NA_1 {
		return false
	}
	lo, hi := limits(p.typ)
	return (o.Value == asdu.StepLower && p.val.f <= lo) || (o.Value == asdu.StepHigher && p.val.f >= hi)
}

// apply sets the target point from a command. The caller holds st.mu. It reports whether
// the value changed.
func (p *point) apply(obj asdu.InformationObject) bool {
	old := p.val
	lo, hi := limits(p.typ)
	set := func(f float64) {
		if p.typ != asdu.M_ME_NA_1 && p.typ != asdu.M_ME_NC_1 {
			f = math.Round(f)
		}
		p.val.f = min(max(f, lo), hi)
	}
	switch o := obj.(type) {
	case asdu.SingleCommand:
		p.val.b = o.Value
	case asdu.DoubleCommand:
		p.val.d = o.Value
	case asdu.StepCommand:
		if o.Value == asdu.StepHigher {
			set(p.val.f + 1)
		} else {
			set(p.val.f - 1)
		}
	case asdu.SetpointNormalized:
		set(o.Value.Float64())
	case asdu.SetpointScaled:
		set(float64(o.Value))
	case asdu.SetpointFloat:
		set(float64(o.Value))
	case asdu.BitstringCommand:
		set(float64(o.Value))
	}
	return p.val != old
}

func (c *conn) command(s *server.Server, req *asdu.ASDU) {
	st := c.st
	obj := req.First()
	ioa := obj.Address()
	e := types.ServerEvent{Event: "command", Remote: c.remote, CommonAddr: uint16(st.ca), IOA: uint32(ioa), Type: req.Type.String()}
	refuse := func(why string, reject asdu.Cause) {
		if reject != 0 {
			_ = s.Reject(req, reject)
		} else {
			_ = s.Negative(req)
		}
		e.Detail = "refused: " + why
		st.event(e)
	}

	st.mu.Lock()
	cmd := st.commands[ioa]
	st.mu.Unlock()
	if cmd == nil || req.Type != cmd.typ {
		refuse("unknown information object address", asdu.CauseUnknownIOA)
		return
	}
	if req.Cause != asdu.CauseActivation && req.Cause != asdu.CauseDeactivation {
		refuse("cause "+req.Cause.String(), asdu.CauseUnknownCause)
		return
	}
	v, sel, key := commanded(obj)
	e.Value = v

	now := st.opts.Now()
	st.mu.Lock()
	current, selected := st.selected[ioa]
	if selected && now.After(current.until) {
		delete(st.selected, ioa)
		selected = false
	}
	st.mu.Unlock()

	if req.Cause == asdu.CauseDeactivation {
		st.mu.Lock()
		delete(st.selected, ioa)
		st.mu.Unlock()
		_ = s.Confirm(req)
		e.Detail = "selection cancelled"
		if !selected {
			e.Detail = "deactivated, nothing was selected"
		}
		st.event(e)
		return
	}
	if !valid(obj) {
		refuse("value not permitted", 0)
		return
	}
	if sel {
		if selected && current.conn != c.id {
			refuse("selected by another controlling station", 0)
			return
		}
		st.mu.Lock()
		st.selected[ioa] = selection{conn: c.id, object: key, until: now.Add(cmd.timeout)}
		st.mu.Unlock()
		_ = s.Confirm(req)
		e.Detail = "selected"
		st.event(e)
		return
	}

	// Execute.
	switch {
	case selected && (current.conn != c.id || current.object != key):
		st.mu.Lock()
		delete(st.selected, ioa)
		st.mu.Unlock()
		refuse("does not match the selection", 0)
		return
	case !selected && cmd.spec.SelectRequired:
		refuse("select required", 0)
		return
	}
	st.mu.Lock()
	delete(st.selected, ioa)
	target := st.points[asdu.IOA(cmd.spec.Target)]
	if target.atLimit(obj) {
		st.mu.Unlock()
		refuse("step position at its limit", 0)
		return
	}
	changed := false
	if target != nil {
		changed = target.apply(obj)
	}
	st.mu.Unlock()

	// Confirmation, report, termination, in this order, on the line of the command.
	_ = s.Confirm(req)
	if target != nil {
		if a, _ := st.eventOf(asdu.IOA(cmd.spec.Target), cmd.report); a != nil {
			_ = s.Send(a)
		}
	}
	_ = s.Terminate(req)
	e.Detail = "executed"
	if target != nil {
		e.Detail = fmt.Sprintf("executed, point %d set", cmd.spec.Target)
		if !changed {
			e.Detail += " (unchanged)"
		}
	}
	st.event(e)
}

// Value returns the current value of a point as the event log shows it.
func (st *Station) Value(ioa uint32) (any, bool) {
	st.mu.Lock()
	defer st.mu.Unlock()
	p := st.points[asdu.IOA(ioa)]
	if p == nil {
		return nil, false
	}
	return p.shown(), true
}

// Set changes the value of a point and reports it as a spontaneous event, as the simulation
// does. It returns false for an unknown point or a value the point cannot hold.
func (st *Station) Set(ioa uint32, v any) bool {
	st.mu.Lock()
	p := st.points[asdu.IOA(ioa)]
	if p == nil {
		st.mu.Unlock()
		return false
	}
	nv, err := parseValue(p.typ, v)
	if err != nil {
		st.mu.Unlock()
		return false
	}
	p.val = nv
	st.mu.Unlock()
	st.announce(asdu.IOA(ioa))
	return true
}

// step advances the simulation of p by one interval. The caller holds st.mu.
func (p *point) step(rng *splitMix) {
	s := p.spec.Simulate
	p.tick++
	lo, hi := limits(p.typ)
	clamp := func(f float64) float64 {
		if p.typ != asdu.M_ME_NA_1 && p.typ != asdu.M_ME_NC_1 {
			f = math.Round(f)
		}
		return min(max(f, lo), hi)
	}
	stepSize := s.Step
	switch strings.ToLower(s.Mode) {
	case "toggle":
		if p.typ == asdu.M_SP_NA_1 {
			p.val.b = !p.val.b
		} else if p.val.d == asdu.DoubleOn {
			p.val.d = asdu.DoubleOff
		} else {
			p.val.d = asdu.DoubleOn
		}
	case "ramp":
		if stepSize == 0 {
			stepSize = 1
		}
		next := p.val.f + stepSize
		if next > s.Max {
			next = s.Min
		}
		p.val.f = clamp(next)
	case "sine":
		period := stepSize
		if period == 0 {
			period = 60
		}
		mid, amp := (s.Max+s.Min)/2, (s.Max-s.Min)/2
		p.val.f = clamp(mid + amp*math.Sin(2*math.Pi*float64(p.tick)/period))
	case "random":
		p.val.f = clamp(s.Min + rng.Float64()*(s.Max-s.Min))
	case "count":
		if stepSize == 0 {
			stepSize = 1
		}
		next := p.val.f + stepSize
		if next > hi {
			next = 0 // the counter overflows
		}
		p.val.f = clamp(next)
	}
}

// Simulate starts the goroutines that change values by themselves and send the cyclic data,
// until ctx ends. Wait waits for them.
func (st *Station) Simulate(ctx context.Context) {
	for _, ioa := range st.order {
		p := st.points[ioa]
		if p.spec.Simulate == nil {
			continue
		}
		st.wg.Go(func() {
			rng := &splitMix{state: uint64(ioa)<<8 | 101}
			tick := time.NewTicker(time.Duration(p.spec.Simulate.Interval))
			defer tick.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-tick.C:
				}
				st.mu.Lock()
				old := p.val
				p.step(rng)
				changed := p.val != old
				st.mu.Unlock()
				if changed {
					st.announce(ioa)
				}
			}
		})
	}
	if d := time.Duration(st.model.Station.CyclicInterval); d > 0 {
		st.wg.Go(func() {
			tick := time.NewTicker(d)
			defer tick.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-tick.C:
				}
				measured := st.report(asdu.CausePeriodic, 0, func(p *point) bool {
					return p.typ == asdu.M_ME_NA_1 || p.typ == asdu.M_ME_NB_1 || p.typ == asdu.M_ME_NC_1
				})
				for _, a := range measured {
					st.send(a, server.Class2)
				}
			}
		})
	}
}

// Wait waits for the goroutines of [Station.Simulate] after their context ended.
func (st *Station) Wait() { st.wg.Wait() }

// splitMix is the SplitMix64 generator: the deterministic, seeded source of the "random"
// simulation mode. A simulated measurement needs no unpredictability, and a fixed seed per
// point makes a run repeatable.
type splitMix struct{ state uint64 }

// Float64 returns a number in [0, 1).
func (s *splitMix) Float64() float64 {
	s.state += 0x9E3779B97F4A7C15
	z := s.state
	z = (z ^ (z >> 30)) * 0xBF58476D1CE4E5B9
	z = (z ^ (z >> 27)) * 0x94D049BB133111EB
	z ^= z >> 31
	return float64(z>>11) / (1 << 53)
}

// delayAfter is the time of a delay acquisition (milliseconds of the minute, CP16Time2a)
// plus the reaction time of the station.
func delayAfter(sent uint16, reaction time.Duration) uint16 {
	var r uint32
	if ms := reaction.Milliseconds(); ms > 0 && ms < 60000 {
		r = uint32(ms)
	}
	if v := (uint32(sent) + r) % 60000; v <= math.MaxUint16 {
		return uint16(v)
	}
	return sent
}
