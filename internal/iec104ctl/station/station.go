// SPDX-License-Identifier: MIT

package station

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"math"
	"math/rand/v2"
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/otfabric/go-iec60870-5/asdu"
	iec104 "github.com/otfabric/go-iec60870-5/iec104"
	"github.com/otfabric/go-iec60870-5/iec104/apci"
	"github.com/otfabric/go-iec60870-5/iec104/server"
	"github.com/otfabric/iec60870ctl/internal/format"
	"github.com/otfabric/iec60870ctl/internal/iec104ctl/iec"
	"github.com/otfabric/iec60870ctl/internal/iec104ctl/types"
)

// Options configure a [Station] beyond its model.
type Options struct {
	Link apci.Params
	ASDU asdu.Params
	// TLS, when not nil, is the configuration of a TLS listener made by [Station.Listen].
	TLS *tls.Config
	// Allow lists the addresses and prefixes clients may connect from; empty for any.
	Allow       []netip.Prefix
	MaxSessions int
	// Log receives every event of the station. It is called from several goroutines, one
	// call at a time.
	Log func(types.ServerEvent)
	// Metrics, when not nil, receives the frames, for --debug.
	Metrics iec104.Metrics
	// Now is the clock of the station; time.Now when nil.
	Now func() time.Time
	// Redundancy, when it declares groups or an event queue, replaces the redundancy section
	// of the model (the flags of `server start`); the zero value keeps the model's.
	Redundancy RedundancySpec
	// Security, when not nil, applies the application-layer security of IEC 62351-5:
	// every controlling station associates with this identity and trust and has these
	// roles.
	Security *iec.Security
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
	typ     asdu.TypeID // without time tag
	timeout time.Duration
}

type selection struct {
	session uint64
	object  string // the selected command, without the select bit
	until   time.Time
}

type file struct {
	ioa  asdu.IOA
	name uint16
	data server.File
	at   time.Time
}

// group is a redundancy group of the station, with its addresses parsed.
type group struct {
	name  string
	allow []netip.Prefix // empty: every address
}

// Station is a simulated controlled station.
type Station struct {
	model *Model
	opts  Options
	ca    asdu.CommonAddr
	srv   *server.Server
	// groups are the redundancy groups the station declares, nil for none; queue is the
	// size of the event queue of each group.
	groups []group
	queue  int

	mu       sync.Mutex
	points   map[asdu.IOA]*point
	order    []asdu.IOA
	commands map[asdu.IOA]*command
	selected map[asdu.IOA]selection
	files    map[asdu.IOA]*file
	uploads  map[asdu.IOA]upload // the delivered files, by address
	// incoming are the files being delivered: accepted, not yet stored or ended. They count
	// against the upload limits as the delivered files do.
	incoming map[incomingKey]incoming
	sessions map[uint64]bool // sessions that have been announced
	// offset is what a clock synchronization moved the station's clock by.
	offset time.Duration

	logMu sync.Mutex
	stop  context.CancelFunc
	wg    sync.WaitGroup
}

// New builds a station from a validated model.
func New(m *Model, opts Options) (*Station, error) {
	if err := m.Validate(); err != nil {
		return nil, err
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	if opts.ASDU == (asdu.Params{}) {
		opts.ASDU = asdu.IEC104
	}
	if opts.Link == (apci.Params{}) {
		opts.Link = apci.DefaultParams()
	}
	st := &Station{
		model:    m,
		opts:     opts,
		ca:       asdu.CommonAddr(m.Station.CommonAddress),
		points:   map[asdu.IOA]*point{},
		commands: map[asdu.IOA]*command{},
		selected: map[asdu.IOA]selection{},
		files:    map[asdu.IOA]*file{},
		uploads:  map[asdu.IOA]upload{},
		incoming: map[incomingKey]incoming{},
		sessions: map[uint64]bool{},
	}
	for _, spec := range m.Points {
		typ := monitorTypes[strings.ToUpper(strings.TrimSpace(spec.Type))]
		v, _ := parseValue(typ, spec.Value)
		p := &point{spec: spec, typ: typ, initial: v, val: v, timeTag: spec.TimeTag == nil || *spec.TimeTag}
		for _, q := range spec.Quality {
			p.quality |= qualityFlags[strings.ToUpper(strings.TrimSpace(q))]
		}
		st.points[asdu.IOA(spec.IOA)] = p
		st.order = append(st.order, asdu.IOA(spec.IOA))
	}
	slices.Sort(st.order)
	for _, spec := range m.Commands {
		c := &command{spec: spec, typ: commandTypes[strings.ToUpper(strings.TrimSpace(spec.Type))], timeout: time.Duration(spec.SelectTimeout)}
		if c.timeout == 0 {
			c.timeout = defaultSelectTimeout
		}
		st.commands[asdu.IOA(spec.IOA)] = c
	}
	started := opts.Now()
	for _, spec := range m.Files {
		data, err := m.fileContent(spec)
		if err != nil {
			return nil, err
		}
		st.files[asdu.IOA(spec.IOA)] = &file{ioa: asdu.IOA(spec.IOA), name: spec.Name, data: server.NewFile(data, sectionSize(spec)), at: started}
	}
	red := m.Redundancy
	if len(opts.Redundancy.Groups) > 0 {
		red.Groups = opts.Redundancy.Groups
	}
	if opts.Redundancy.EventQueue != 0 {
		red.EventQueue = opts.Redundancy.EventQueue
	}
	var problems []string
	red.validate(func(format string, args ...any) { problems = append(problems, fmt.Sprintf(format, args...)) })
	if len(problems) > 0 {
		return nil, fmt.Errorf("redundancy: %s", strings.Join(problems, "; "))
	}
	st.queue = red.EventQueue
	if st.queue == 0 {
		st.queue = DefaultEventQueue
	}
	var groups []server.RedundancyGroup
	for _, g := range red.Groups {
		sg := group{name: g.Name}
		for _, a := range g.Allow {
			p, _ := ParsePrefix(a) // validated
			sg.allow = append(sg.allow, p)
		}
		st.groups = append(st.groups, sg)
		groups = append(groups, server.RedundancyGroup{Name: g.Name, Allow: g.Allow})
	}
	if dir := m.Uploads.Dir; dir != "" {
		if err := os.MkdirAll(m.resolve(dir), 0o750); err != nil {
			return nil, fmt.Errorf("uploads.dir: %w", err)
		}
	}

	mux := server.NewMux()
	mux.HandleFunc(asdu.C_IC_NA_1, st.interrogation)
	mux.HandleFunc(asdu.C_CI_NA_1, st.counterInterrogation)
	mux.HandleFunc(asdu.C_RD_NA_1, st.read)
	mux.HandleFunc(asdu.C_CS_NA_1, st.clockSync)
	mux.HandleFunc(asdu.C_TS_TA_1, st.testCommand)
	mux.HandleFunc(asdu.C_RP_NA_1, st.resetProcess)
	mux.Handle(server.HandlerFunc(st.parameter), asdu.P_ME_NA_1, asdu.P_ME_NB_1, asdu.P_ME_NC_1, asdu.P_AC_NA_1)
	mux.Handle(server.HandlerFunc(st.command), server.ProcessCommands...)
	fs := server.NewFileServer(server.FileSourceFunc(st.openFile))
	fs.Directory = st.directory
	fs.OnDone = st.fileDone
	if m.Uploads.Accept {
		fs.Sink = st
	}
	mux.Handle(fs, server.FileTypes...)

	sopts := []server.Option{
		server.WithParams(opts.Link),
		server.WithASDUParams(opts.ASDU),
		server.WithCommonAddrs(st.ca),
		server.WithStateHandler(st.sessionState),
		server.WithAccept(st.accept),
	}
	if opts.MaxSessions > 0 {
		sopts = append(sopts, server.WithMaxSessions(opts.MaxSessions))
	}
	sopts = append(sopts, server.WithEventQueue(st.queue))
	if len(groups) > 0 {
		// IEC 60870-5-104 clause 10: the connections of a group are redundant; one of them
		// has data transfer started at a time.
		sopts = append(sopts, server.WithRedundancyGroups(groups...))
	}
	if opts.Metrics != nil {
		sopts = append(sopts, server.WithMetrics(opts.Metrics))
	}
	if opts.Security != nil {
		// Fail now, not at the first controlling station, on a configuration package sec
		// does not accept.
		if _, err := st.endpoint("default", 1); err != nil {
			return nil, fmt.Errorf("security: %w", err)
		}
		sopts = append(sopts, server.WithSecurity(st.newEndpoint))
	}
	srv, err := server.New(mux, sopts...)
	if err != nil {
		return nil, err
	}
	st.srv = srv
	return st, nil
}

// CommonAddr returns the common address of the station.
func (st *Station) CommonAddr() uint16 { return uint16(st.ca) }

// Listen opens the listener of the station: a TLS one when the options hold a TLS
// configuration.
func (st *Station) Listen(addr string) (net.Listener, error) {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, err
	}
	if st.opts.TLS != nil {
		return tls.NewListener(ln, st.opts.TLS), nil
	}
	return ln, nil
}

// Serve serves the station on ln until [Station.Close]. It returns nil after Close.
func (st *Station) Serve(ln net.Listener) error {
	ctx, cancel := context.WithCancel(context.Background())
	st.mu.Lock()
	st.stop = cancel
	st.mu.Unlock()
	st.simulate(ctx)
	detail := fmt.Sprintf("%s, station %d (%s), %d points, %d commands, %d files",
		ln.Addr(), st.ca, st.name(), len(st.points), len(st.commands), len(st.files))
	if s := st.opts.Security; s != nil {
		detail += fmt.Sprintf(", secure (AIS %d)", s.ID)
	}
	if len(st.groups) > 0 {
		names := make([]string, len(st.groups))
		for i, g := range st.groups {
			names[i] = g.name
		}
		detail += fmt.Sprintf(", redundancy groups %s", strings.Join(names, ", "))
	}
	if st.queue != DefaultEventQueue {
		detail += fmt.Sprintf(", event queue %d", st.queue)
	}
	st.event(types.ServerEvent{Event: "listening", Detail: detail})
	err := st.srv.Serve(ln)
	cancel()
	st.wg.Wait()
	if errors.Is(err, server.ErrServerClosed) {
		return nil
	}
	return err
}

// Close stops the station.
func (st *Station) Close() error {
	st.mu.Lock()
	stop := st.stop
	st.mu.Unlock()
	if stop != nil {
		stop()
	}
	return st.srv.Close()
}

// Sessions returns the number of connections.
func (st *Station) Sessions() int { return len(st.srv.Sessions()) }

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

func remote(s *server.Session) string {
	if s == nil || s.RemoteAddr() == nil {
		return ""
	}
	return s.RemoteAddr().String()
}

func (st *Station) accept(addr net.Addr) bool {
	if !allows(st.opts.Allow, addr) {
		st.event(types.ServerEvent{Event: "refused", Remote: addr.String(), Detail: "address not allowed"})
		return false
	}
	if len(st.groups) == 0 {
		return true
	}
	for _, g := range st.groups {
		if allows(g.allow, addr) {
			return true
		}
	}
	// The library would close it too; refusing it here puts it in the event log.
	st.event(types.ServerEvent{Event: "refused", Remote: addr.String(), Detail: "no redundancy group allows the address"})
	return false
}

// allows reports whether addr is in one of prefixes; every address is when there are none.
func allows(prefixes []netip.Prefix, addr net.Addr) bool {
	if len(prefixes) == 0 {
		return true
	}
	tcp, ok := addr.(*net.TCPAddr)
	if !ok {
		return false
	}
	ip, ok := netip.AddrFromSlice(tcp.IP)
	if !ok {
		return false
	}
	ip = ip.Unmap()
	for _, p := range prefixes {
		if p.Contains(ip) {
			return true
		}
	}
	return false
}

func (st *Station) sessionState(s *server.Session, state iec104.State, err error) {
	e := types.ServerEvent{Remote: remote(s)}
	if len(st.groups) > 0 && s != nil {
		e.Group = s.Group()
	}
	switch state {
	case iec104.StateStopped:
		// The first notification of a session announces it.
		st.mu.Lock()
		known := st.sessions[s.ID()]
		st.sessions[s.ID()] = true
		st.mu.Unlock()
		e.Event, e.Detail = "connected", "data transfer stopped"
		if known {
			e.Event = "stopped"
		}
	case iec104.StateStarted:
		e.Event = "started"
		e.Detail = "data transfer started"
	case iec104.StateDisconnected:
		e.Event = "disconnected"
		if err != nil {
			e.Detail = strings.TrimPrefix(err.Error(), "iec104: ")
		}
		st.mu.Lock()
		delete(st.sessions, s.ID())
		for ioa, sel := range st.selected {
			if sel.session == s.ID() {
				delete(st.selected, ioa)
			}
		}
		// The files the session was delivering end with it.
		for k := range st.incoming {
			if k.session == s.ID() {
				delete(st.incoming, k)
			}
		}
		st.mu.Unlock()
	default:
		return
	}
	st.event(e)
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
// without time tag takes 8 octets, and 240 are available.
const perASDU = 30

// report builds the ASDUs that hold the points sel selects, grouped by type.
func (st *Station) report(cause asdu.Cause, originator uint8, sel func(*point) bool) []*asdu.ASDU {
	st.mu.Lock()
	defer st.mu.Unlock()
	var out []*asdu.ASDU
	byType := map[asdu.TypeID]*asdu.ASDU{}
	for _, ioa := range st.order {
		p := st.points[ioa]
		if !sel(p) {
			continue
		}
		cur := byType[p.typ]
		if cur == nil || len(cur.Objects) >= perASDU {
			cur = &asdu.ASDU{Type: p.typ, Cause: cause, Originator: originator, CommonAddr: st.ca}
			byType[p.typ] = cur
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

func (st *Station) interrogation(s *server.Session, req *asdu.ASDU) {
	req = st.own(req)
	qoi := req.First().(asdu.Interrogation).Qualifier
	group := int(qoi) - int(asdu.QOIStation)
	if req.Cause != asdu.CauseActivation || group < 0 || group > 16 {
		_ = s.Negative(req)
		return
	}
	data := st.report(qoi.Cause(), req.Originator, func(p *point) bool {
		if p.typ == asdu.M_IT_NA_1 {
			return false // counters answer a counter interrogation
		}
		return group == 0 || int(p.spec.Group) == group
	})
	_ = s.Confirm(req)
	n := 0
	for _, a := range data {
		n += len(a.Objects)
		_ = s.Send(s.Context(), a)
	}
	_ = s.Terminate(req)
	detail := "station"
	if group != 0 {
		detail = fmt.Sprintf("group %d", group)
	}
	st.event(types.ServerEvent{Event: "interrogation", Remote: remote(s), CommonAddr: uint16(st.ca), Detail: fmt.Sprintf("%s, %d objects", detail, n)})
}

func (st *Station) counterInterrogation(s *server.Session, req *asdu.ASDU) {
	req = st.own(req)
	ci := req.First().(asdu.CounterInterrogation)
	group := 0
	if ci.Request >= asdu.CounterGroup1 && ci.Request <= asdu.CounterGroup4 {
		group = int(ci.Request)
	} else if ci.Request != asdu.CounterGeneral {
		_ = s.Negative(req)
		return
	}
	if req.Cause != asdu.CauseActivation {
		_ = s.Negative(req)
		return
	}
	sel := func(p *point) bool {
		return p.typ == asdu.M_IT_NA_1 && (group == 0 || int(p.spec.Group) == group)
	}
	var data []*asdu.ASDU
	if ci.Freeze != asdu.FreezeResetOnly {
		data = st.report(ci.Cause(), req.Originator, sel)
	}
	if ci.Freeze != asdu.FreezeRead {
		st.mu.Lock()
		for _, p := range st.points {
			if !sel(p) {
				continue
			}
			p.seq = (p.seq + 1) % 32
			if ci.Freeze == asdu.FreezeWithReset || ci.Freeze == asdu.FreezeResetOnly {
				p.val.f = 0
			}
		}
		st.mu.Unlock()
	}
	_ = s.Confirm(req)
	n := 0
	for _, a := range data {
		n += len(a.Objects)
		_ = s.Send(s.Context(), a)
	}
	_ = s.Terminate(req)
	st.event(types.ServerEvent{Event: "counter-interrogation", Remote: remote(s), CommonAddr: uint16(st.ca),
		Detail: fmt.Sprintf("request %d, freeze %d, %d counters", ci.Request, ci.Freeze, n)})
}

func (st *Station) read(s *server.Session, req *asdu.ASDU) {
	ioa := req.First().Address()
	st.mu.Lock()
	p := st.points[ioa]
	var obj asdu.InformationObject
	if p != nil {
		obj = p.object(asdu.Timestamp{})
	}
	st.mu.Unlock()
	if p == nil {
		_ = s.Reject(req, asdu.CauseUnknownIOA)
		st.event(types.ServerEvent{Event: "read", Remote: remote(s), CommonAddr: uint16(st.ca), IOA: uint32(ioa), Detail: "refused: unknown information object address"})
		return
	}
	a := asdu.New(asdu.CauseRequest, st.ca, obj)
	a.Originator = req.Originator
	_ = s.Send(s.Context(), a)
	st.event(types.ServerEvent{Event: "read", Remote: remote(s), CommonAddr: uint16(st.ca), IOA: uint32(ioa), Type: a.Type.String()})
}

func (st *Station) clockSync(s *server.Session, req *asdu.ASDU) {
	req = st.own(req)
	if req.Cause != asdu.CauseActivation {
		_ = s.Negative(req)
		return
	}
	t := req.First().(asdu.ClockSync).Time
	if t.IsZero() || t.Invalid {
		_ = s.Negative(req)
		st.event(types.ServerEvent{Event: "clock", Remote: remote(s), CommonAddr: uint16(st.ca), Detail: "refused: invalid time"})
		return
	}
	st.mu.Lock()
	st.offset = t.Sub(st.opts.Now())
	offset := st.offset
	st.mu.Unlock()
	_ = s.Confirm(req)
	st.event(types.ServerEvent{Event: "clock", Remote: remote(s), CommonAddr: uint16(st.ca),
		Detail: fmt.Sprintf("set to %s (%+.3fs)", t.UTC().Format(time.RFC3339Nano), offset.Seconds())})
}

func (st *Station) testCommand(s *server.Session, req *asdu.ASDU) {
	if req.Cause != asdu.CauseActivation {
		_ = s.Negative(req)
		return
	}
	_ = s.Confirm(req)
	st.event(types.ServerEvent{Event: "test", Remote: remote(s), CommonAddr: uint16(st.ca), Detail: "test command confirmed"})
}

// resetProcess puts every point back to the value of the model and announces the end of
// initialization.
func (st *Station) resetProcess(s *server.Session, req *asdu.ASDU) {
	req = st.own(req)
	q := req.First().(asdu.ResetProcess).Qualifier
	if req.Cause != asdu.CauseActivation || (q != asdu.ResetProcessGeneral && q != asdu.ResetPendingEvents) {
		_ = s.Negative(req)
		return
	}
	detail := "pending events"
	if q == asdu.ResetPendingEvents {
		// The events with a time tag that no controlling station has been sent yet go
		// (IEC 60870-5-101:2003 7.2.6.27, QRP 2); one sent and not yet acknowledged stays.
		detail = fmt.Sprintf("pending events: %d with time tag cleared", st.clearPendingEvents())
	}
	if q == asdu.ResetProcessGeneral {
		// The values are back before the confirmation: whoever acts on it sees them.
		detail = "process: values back to the model"
		st.mu.Lock()
		for _, p := range st.points {
			p.val, p.seq, p.tick = p.initial, 0, 0
		}
		st.selected = map[asdu.IOA]selection{}
		st.mu.Unlock()
	}
	_ = s.Confirm(req)
	if q == asdu.ResetProcessGeneral {
		_ = st.srv.Enqueue(asdu.New(asdu.CauseInitialized, st.ca, asdu.EndOfInitialization{Cause: asdu.InitRemoteReset}))
	}
	st.event(types.ServerEvent{Event: "reset", Remote: remote(s), CommonAddr: uint16(st.ca), Detail: detail})
}

// clearPendingEvents removes the queued events with a time tag that no controlling station
// has been sent, in every redundancy group, and returns how many (QRP 2). The events without
// a time tag, and those sent and not yet acknowledged, stay.
func (st *Station) clearPendingEvents() int { return st.srv.ClearEvents("", true) }

// parameter loads a parameter of measured value or activates parameters (IEC
// 60870-5-104:2000 7.9; IEC 60870-5-101:2003 7.4.9, 7.4.10). The station takes a parameter
// of measured value for any measured value of its model, whatever its representation, and
// confirms it; it keeps no parameter and does not apply it to the simulation. The
// previously loaded parameters are activated at address 0, the parameter or the cyclic
// transmission of an object at the address of a point. Anything else is refused.
func (st *Station) parameter(s *server.Session, req *asdu.ASDU) {
	req = st.own(req)
	obj := req.First()
	ioa := obj.Address()
	e := types.ServerEvent{Event: "parameter", Remote: remote(s), CommonAddr: uint16(st.ca), IOA: uint32(ioa), Type: req.Type.String()}
	refuse := func(why string, reject asdu.Cause) {
		if reject != 0 {
			_ = s.Reject(req, reject)
		} else {
			_ = s.Negative(req)
		}
		e.Detail = "refused: " + why
		st.event(e)
	}
	if req.Cause != asdu.CauseActivation {
		refuse("only activation", 0)
		return
	}
	st.mu.Lock()
	p := st.points[ioa]
	st.mu.Unlock()

	if a, ok := obj.(asdu.ParameterActivation); ok {
		switch {
		case a.Qualifier == asdu.ActivateLoadedParameters && ioa == 0:
			e.Detail = "activated: loaded parameters"
		case (a.Qualifier == asdu.ActivateObjectParameter || a.Qualifier == asdu.ActivateCyclic) && p != nil:
			e.Detail = "activated: object parameter"
			if a.Qualifier == asdu.ActivateCyclic {
				e.Detail = "activated: cyclic transmission"
			}
		case a.Qualifier < asdu.ActivateLoadedParameters || a.Qualifier > asdu.ActivateCyclic:
			refuse(fmt.Sprintf("qualifier of parameter activation %d", a.Qualifier), 0)
			return
		case ioa == 0:
			refuse("the object is required", asdu.CauseUnknownIOA)
			return
		case p == nil:
			refuse("unknown information object address", asdu.CauseUnknownIOA)
			return
		default:
			refuse("the loaded parameters are activated at address 0", 0)
			return
		}
		_ = s.Confirm(req)
		st.event(e)
		return
	}

	if p == nil {
		refuse("unknown information object address", asdu.CauseUnknownIOA)
		return
	}
	if !strings.HasPrefix(p.typ.String(), "M_ME_") {
		refuse(fmt.Sprintf("the object is %s, not a measured value", p.typ), 0)
		return
	}
	var v any
	var q uint8
	switch o := obj.(type) {
	case asdu.ParameterNormalized:
		v, q = o.Value.Float64(), o.Qualifier
	case asdu.ParameterScaled:
		v, q = int(o.Value), o.Qualifier
	case asdu.ParameterFloat:
		v, q = o.Value, o.Qualifier
	}
	e.Value, e.NonFinite = format.Finite(v)
	kind, ok := parameterKinds[q&0x3f]
	if !ok {
		refuse(fmt.Sprintf("kind of parameter %d", q&0x3f), 0)
		return
	}
	if e.NonFinite != "" {
		refuse("the value is not a finite number", 0)
		return
	}
	e.Detail = "loaded: " + kind
	_ = s.Confirm(req)
	st.event(e)
}

// parameterKinds names the kinds of parameter of measured value (KPA,
// IEC 60870-5-101:2003 7.2.6.24).
var parameterKinds = map[uint8]string{
	asdu.ParameterThreshold: "threshold",
	asdu.ParameterSmoothing: "smoothing",
	asdu.ParameterLowLimit:  "low-limit",
	asdu.ParameterHighLimit: "high-limit",
}

// tag returns the time tag of an event of p: the station's time, or none.
func (st *Station) tag(p *point) asdu.Timestamp {
	if !p.timeTag {
		return asdu.Timestamp{}
	}
	return asdu.At(st.now())
}

// announce reports the current value of the point at ioa as an event.
//
// A change of state (single and double points) and the result of a command are queued: the
// station keeps them, with their time tag, until a controlling station has acknowledged
// them, also while none is connected. A change of a measured value, a step position, a
// bitstring or a counter that the simulation makes is sent to whoever has data transfer
// started and is not kept: a station that reconnects gets the current values from its
// general interrogation, not a backlog of old measurements.
func (st *Station) announce(ioa asdu.IOA, cause asdu.Cause) {
	st.mu.Lock()
	p := st.points[ioa]
	st.mu.Unlock()
	if p == nil {
		return
	}
	tag := st.tag(p)
	st.mu.Lock()
	obj := p.object(tag)
	st.mu.Unlock()
	a := asdu.New(cause, st.ca, obj)
	if cause != asdu.CauseSpontaneous || p.typ == asdu.M_SP_NA_1 || p.typ == asdu.M_DP_NA_1 {
		_ = st.srv.Enqueue(a)
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_, _ = st.srv.Broadcast(ctx, a)
}

// baseType returns the command type without time tag.
func baseType(t asdu.TypeID) asdu.TypeID {
	if b, ok := commandTypes[t.String()]; ok {
		return b
	}
	return t
}

// commanded describes a received command: its value, whether it is a select, and a key
// that identifies the command without the select bit and the time tag.
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

func (st *Station) command(s *server.Session, req *asdu.ASDU) {
	obj := req.First()
	ioa := obj.Address()
	e := types.ServerEvent{Event: "command", Remote: remote(s), CommonAddr: uint16(st.ca), IOA: uint32(ioa), Type: req.Type.String()}
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
	if cmd == nil {
		refuse("unknown information object address", asdu.CauseUnknownIOA)
		return
	}
	v, sel, key := commanded(obj)
	e.Value, e.NonFinite = format.Finite(v)
	if baseType(req.Type) != cmd.typ {
		refuse(fmt.Sprintf("the object takes %s", cmd.typ), 0)
		return
	}
	if !valid(obj) {
		refuse("value not permitted", 0)
		return
	}
	// No point holds a NaN or an infinite value: such a set point is outside the range of
	// every point type.
	if e.NonFinite != "" {
		refuse("the value is not a finite number", 0)
		return
	}

	now := st.opts.Now()
	st.mu.Lock()
	current, selected := st.selected[ioa]
	if selected && now.After(current.until) {
		delete(st.selected, ioa)
		selected = false
	}
	st.mu.Unlock()

	switch {
	case req.Cause == asdu.CauseDeactivation:
		if !selected || current.session != s.ID() {
			refuse("nothing selected", 0)
			return
		}
		st.mu.Lock()
		delete(st.selected, ioa)
		st.mu.Unlock()
		_ = s.Confirm(req)
		e.Detail = "selection cancelled"
		st.event(e)
		return

	case sel:
		if selected && current.session != s.ID() {
			refuse("selected by another controlling station", 0)
			return
		}
		st.mu.Lock()
		st.selected[ioa] = selection{session: s.ID(), object: key, until: now.Add(cmd.timeout)}
		st.mu.Unlock()
		_ = s.Confirm(req)
		e.Detail = "selected"
		st.event(e)
		return
	}

	// Execute.
	switch {
	case selected && (current.session != s.ID() || current.object != key):
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
	changed := false
	if target != nil {
		changed = target.apply(obj)
	}
	st.mu.Unlock()

	_ = s.Confirm(req)
	_ = s.Terminate(req)
	e.Detail = "executed"
	if target != nil {
		e.Detail = fmt.Sprintf("executed, point %d set", cmd.spec.Target)
	}
	st.event(e)
	if changed {
		st.announce(asdu.IOA(cmd.spec.Target), asdu.CauseReturnRemote)
	}
}

func (st *Station) openFile(_ *server.Session, ca asdu.CommonAddr, ioa asdu.IOA, name uint16) (server.File, bool) {
	st.mu.Lock()
	defer st.mu.Unlock()
	f := st.files[ioa]
	if ca != st.ca || f == nil || f.name != name {
		return server.File{}, false
	}
	return f.data, true
}

func (st *Station) directory(s *server.Session, ca asdu.CommonAddr, ioa asdu.IOA) []asdu.FileDirectoryEntry {
	if ca != st.ca || ioa != 0 {
		return nil
	}
	st.mu.Lock()
	out := make([]asdu.FileDirectoryEntry, 0, len(st.files))
	for _, f := range st.files {
		out = append(out, asdu.FileDirectoryEntry{IOA: f.ioa, Name: f.name, Length: fileLength(f.data.Len()), Time: asdu.At(f.at)})
	}
	st.mu.Unlock()
	sort.Slice(out, func(i, j int) bool { return out[i].IOA < out[j].IOA })
	st.event(types.ServerEvent{Event: "directory", Remote: remote(s), CommonAddr: uint16(st.ca), Detail: fmt.Sprintf("%d entries", len(out))})
	return out
}

func (st *Station) fileDone(s *server.Session, _ asdu.CommonAddr, ioa asdu.IOA, name uint16, err error) {
	// A delivery that ends without being stored gives its place back.
	if s != nil {
		st.mu.Lock()
		k := incomingKey{session: s.ID(), ioa: ioa}
		if in, ok := st.incoming[k]; ok && in.name == name {
			delete(st.incoming, k)
		}
		st.mu.Unlock()
	}
	e := types.ServerEvent{Event: "file-sent", Remote: remote(s), CommonAddr: uint16(st.ca), IOA: uint32(ioa), Detail: fmt.Sprintf("name %d", name)}
	if err != nil {
		e.Detail += ": " + err.Error()
	}
	st.event(e)
}

// AcceptFile implements server.FileSink: a file is taken at an address that is free, or
// that holds a file delivered earlier. An accepted file counts against the upload limits
// from now on, while it is being delivered, until it is stored or its delivery or its
// session ends.
func (st *Station) AcceptFile(s *server.Session, ca asdu.CommonAddr, ioa asdu.IOA, name uint16, length int) bool {
	st.mu.Lock()
	_, isPoint := st.points[ioa]
	_, isCommand := st.commands[ioa]
	reason := ""
	switch {
	case ca != st.ca || isPoint || isCommand:
		reason = "the address is in use"
	case s != nil && s.Context().Err() != nil:
		reason = "the session has ended"
	default:
		reason = st.overLimit(ioa, length, sessionOf(s))
	}
	if reason == "" && s != nil {
		st.incoming[incomingKey{session: s.ID(), ioa: ioa}] = incoming{name: name, size: length}
	}
	st.mu.Unlock()
	if reason != "" {
		st.event(types.ServerEvent{Event: "file-received", Remote: remote(s), CommonAddr: uint16(st.ca), IOA: uint32(ioa), Detail: fmt.Sprintf("refused: %s (%d octets offered)", reason, length)})
	}
	return reason == ""
}

// upload is what the station keeps of a delivered file for its limits.
type upload struct {
	size int
	path string // where it was written, or ""
}

// incomingKey names a file being delivered: by its session and address, as the library
// keeps one delivery per session and address.
type incomingKey struct {
	session uint64
	ioa     asdu.IOA
}

// incoming is a file being delivered: its name and the length its session announced.
type incoming struct {
	name uint16
	size int
}

// sessionOf returns the key of the deliveries of s; nil (a call without a session) has no
// delivery of its own.
func sessionOf(s *server.Session) *uint64 {
	if s == nil {
		return nil
	}
	id := s.ID()
	return &id
}

// overLimit says why a file of length octets delivered at ioa would exceed the upload
// limits of the model, or "" when it fits. The delivered files and the files being
// delivered count, apart from the delivery of session at ioa itself (the file being
// checked). A file at an address that already holds a delivered file replaces it. st.mu is
// held.
func (st *Station) overLimit(ioa asdu.IOA, length int, session *uint64) string {
	maxFiles, maxSize, maxTotal := st.model.Uploads.limits()
	if length > maxSize {
		return fmt.Sprintf("larger than uploads.max_file_size %d", maxSize)
	}
	self := func(k incomingKey) bool { return session != nil && k.session == *session && k.ioa == ioa }
	addresses := make(map[asdu.IOA]bool, len(st.uploads)+len(st.incoming))
	for a := range st.uploads {
		addresses[a] = true
	}
	total := length
	for a, u := range st.uploads {
		if a != ioa {
			total += u.size
		}
	}
	for k, in := range st.incoming {
		if self(k) {
			continue
		}
		addresses[k.ioa] = true
		total += in.size
	}
	if !addresses[ioa] && len(addresses) >= maxFiles {
		return fmt.Sprintf("uploads.max_files %d reached", maxFiles)
	}
	if total > maxTotal {
		return fmt.Sprintf("uploads.max_total_size %d reached", maxTotal)
	}
	return ""
}

// StoreFile implements server.FileSink.
func (st *Station) StoreFile(s *server.Session, _ asdu.CommonAddr, ioa asdu.IOA, name uint16, f server.File) error {
	detail := fmt.Sprintf("name %d, %d octets in %d sections", name, f.Len(), len(f.Sections))
	path := ""
	if dir := st.model.Uploads.Dir; dir != "" {
		path = filepath.Join(st.model.resolve(dir), fmt.Sprintf("ioa-%d-name-%d.bin", ioa, name))
	}
	// Uploads of several sessions may have been accepted at once: the limits are checked
	// again, and the file's place reserved, under one lock.
	st.mu.Lock()
	reason := st.overLimit(ioa, f.Len(), sessionOf(s))
	old, hadOld := st.uploads[ioa]
	if reason == "" {
		st.uploads[ioa] = upload{size: f.Len(), path: path}
	}
	// The delivery ends here: stored, the file counts as delivered; refused, it is gone.
	if s != nil {
		delete(st.incoming, incomingKey{session: s.ID(), ioa: ioa})
	}
	st.mu.Unlock()
	if reason != "" {
		st.event(types.ServerEvent{Event: "file-received", Remote: remote(s), CommonAddr: uint16(st.ca), IOA: uint32(ioa), Detail: "not stored: " + reason})
		return errors.New(reason)
	}
	if path != "" {
		var data []byte
		for _, sec := range f.Sections {
			data = append(data, sec...)
		}
		if err := os.WriteFile(path, data, 0o600); err != nil {
			st.mu.Lock()
			if hadOld {
				st.uploads[ioa] = old
			} else {
				delete(st.uploads, ioa)
			}
			st.mu.Unlock()
			st.event(types.ServerEvent{Event: "file-received", Remote: remote(s), CommonAddr: uint16(st.ca), IOA: uint32(ioa), Detail: "not stored: " + err.Error()})
			return err
		}
		detail += ", stored as " + path
	}
	st.mu.Lock()
	st.files[ioa] = &file{ioa: ioa, name: name, data: f, at: st.opts.Now().Add(st.offset)}
	st.mu.Unlock()
	// The file it replaces is no longer served; left on disk it would escape the limits.
	if old.path != "" && old.path != path && os.Remove(old.path) == nil {
		detail += ", replaces " + old.path
	}
	st.event(types.ServerEvent{Event: "file-received", Remote: remote(s), CommonAddr: uint16(st.ca), IOA: uint32(ioa), Detail: detail})
	return nil
}

// Uploaded returns a file a controlling station delivered, or one of the model.
func (st *Station) Uploaded(ioa uint32) ([]byte, bool) {
	st.mu.Lock()
	defer st.mu.Unlock()
	f := st.files[asdu.IOA(ioa)]
	if f == nil {
		return nil, false
	}
	var data []byte
	for _, sec := range f.data.Sections {
		data = append(data, sec...)
	}
	return data, true
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
	st.announce(asdu.IOA(ioa), asdu.CauseSpontaneous)
	return true
}

// step advances the simulation of p by one interval. The caller holds st.mu.
func (p *point) step(rng *rand.Rand) {
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

// simulate starts the goroutines that change values by themselves and send the cyclic data.
func (st *Station) simulate(ctx context.Context) {
	for _, ioa := range st.order {
		p := st.points[ioa]
		if p.spec.Simulate == nil {
			continue
		}
		st.wg.Go(func() {
			rng := rand.New(rand.NewPCG(uint64(ioa), 104)) //nolint:gosec // simulation, not security
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
					st.announce(ioa, asdu.CauseSpontaneous)
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
					bctx, cancel := context.WithTimeout(ctx, time.Second)
					_, _ = st.srv.Broadcast(bctx, a)
					cancel()
				}
			}
		})
	}
}

// fileLength is the length of a file of the station as the directory reports it. The
// model refuses files longer than asdu.MaxFileLength, so the bound never applies.
func fileLength(n int) uint32 {
	if n < 0 || n > asdu.MaxFileLength {
		return asdu.MaxFileLength
	}
	return uint32(n)
}
