// SPDX-License-Identifier: MIT

package iec

// The client commands against a hostile controlled station: one that answers with garbage,
// refuses, never stops talking or never answers. Every case checks that the command returns
// within a bound, with the error kind (and so the exit code) a script relies on, and that it
// leaves no goroutine behind.

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/otfabric/go-iec104/apci"
	"github.com/otfabric/go-iec104/asdu"
	"github.com/otfabric/iec104ctl/internal/config"
	"github.com/otfabric/iec104ctl/internal/errs"
	"github.com/otfabric/iec104ctl/internal/types"
)

// rawPeer is one end of a connection that speaks raw APDUs.
type rawPeer struct {
	t    testing.TB
	conn net.Conn

	mu     sync.Mutex
	vs, vr uint16

	startOnce sync.Once
	started   chan struct{} // closed when STARTDT is confirmed
}

// waitStarted waits until the client has started data transfer, or the rogue closes.
func (p *rawPeer) waitStarted(r *rogue) bool {
	select {
	case <-p.started:
		return true
	case <-r.stop:
		return false
	case <-time.After(5 * time.Second):
		return false
	}
}

func (p *rawPeer) write(b []byte) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	_ = p.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
	_, err := p.conn.Write(b)
	return err
}

func (p *rawPeer) frame(f apci.Frame) error {
	b, err := f.MarshalBinary()
	if err != nil {
		p.t.Errorf("marshal %s: %v", f, err)
		return err
	}
	return p.write(b)
}

// sendRaw sends raw as the next I frame.
func (p *rawPeer) sendRaw(raw []byte) error {
	p.mu.Lock()
	f := apci.NewI(p.vs, p.vr, raw)
	p.vs = apci.SeqNext(p.vs)
	p.mu.Unlock()
	return p.frame(f)
}

func (p *rawPeer) send(a *asdu.ASDU) error {
	raw, err := a.Encode(asdu.IEC104)
	if err != nil {
		p.t.Errorf("encode %s: %v", a, err)
		return err
	}
	return p.sendRaw(raw)
}

// next returns the next ASDU the client sends, answering STARTDT and test frames and
// acknowledging I frames on the way.
func (p *rawPeer) next() (*asdu.ASDU, error) {
	for {
		_ = p.conn.SetReadDeadline(time.Now().Add(time.Minute))
		f, err := apci.ReadFrame(p.conn)
		if err != nil {
			return nil, err
		}
		switch {
		case f.Format == apci.FormatI:
			p.mu.Lock()
			p.vr = apci.SeqNext(p.vr)
			vr := p.vr
			p.mu.Unlock()
			if err := p.frame(apci.NewS(vr)); err != nil {
				return nil, err
			}
			return asdu.Decode(f.ASDU, asdu.IEC104)
		case f.Format == apci.FormatU && f.Function == apci.TestFRAct:
			if err := p.frame(apci.NewU(apci.TestFRCon)); err != nil {
				return nil, err
			}
		case f.Format == apci.FormatU && f.Function == apci.StartDTAct:
			if err := p.frame(apci.NewU(apci.StartDTCon)); err != nil {
				return nil, err
			}
			p.startOnce.Do(func() { close(p.started) })
		case f.Format == apci.FormatU && f.Function == apci.StopDTAct:
			if err := p.frame(apci.NewU(apci.StopDTCon)); err != nil {
				return nil, err
			}
		}
	}
}

// rogue is a hostile controlled station: onConn, when not nil, runs on every connection as
// soon as it is accepted, for a station that does not wait to misbehave; serve, when not
// nil, runs on every ASDU a client sends.
type rogue struct {
	t       testing.TB
	ln      net.Listener
	wg      sync.WaitGroup
	mu      sync.Mutex
	conns   []net.Conn
	stop    chan struct{}
	stopped sync.Once
}

func startRogue(t testing.TB, onConn func(r *rogue, p *rawPeer), serve func(r *rogue, p *rawPeer, a *asdu.ASDU)) *rogue {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	r := &rogue{t: t, ln: ln, stop: make(chan struct{})}
	r.wg.Go(func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			r.mu.Lock()
			r.conns = append(r.conns, conn)
			r.mu.Unlock()
			p := &rawPeer{t: t, conn: conn, started: make(chan struct{})}
			r.wg.Go(func() {
				defer func() { _ = conn.Close() }()
				if onConn != nil {
					r.wg.Go(func() { onConn(r, p) })
				}
				for {
					a, err := p.next()
					if err != nil {
						return
					}
					if serve != nil {
						serve(r, p, a)
					}
				}
			})
		}
	})
	t.Cleanup(r.close)
	return r
}

// stopping reports whether the rogue is being closed, for loops that flood.
func (r *rogue) stopping() bool {
	select {
	case <-r.stop:
		return true
	default:
		return false
	}
}

func (r *rogue) close() {
	r.stopped.Do(func() { close(r.stop) })
	_ = r.ln.Close()
	r.mu.Lock()
	for _, c := range r.conns {
		_ = c.Close()
	}
	r.mu.Unlock()
	r.wg.Wait()
}

// conn returns the connection settings of a command against the rogue, with a request
// timeout of timeout.
func (r *rogue) conn(timeout time.Duration) config.ConnConfig {
	_, port, _ := net.SplitHostPort(r.ln.Addr().String())
	n, _ := strconv.ParseUint(port, 10, 16)
	return config.ConnConfig{IP: "127.0.0.1", Port: uint16(n), Timeout: uint32(timeout / time.Millisecond),
		LinkConfig: config.LinkConfig{T1: 2, T2: 1}}
}

// leakCheck returns a function that fails t when goroutines started since the call are
// still running a while after everything was closed.
func leakCheck(t testing.TB) func() {
	baseline := runtime.NumGoroutine()
	return func() {
		t.Helper()
		deadline := time.Now().Add(5 * time.Second)
		for runtime.NumGoroutine() > baseline && time.Now().Before(deadline) {
			time.Sleep(10 * time.Millisecond)
		}
		if n := runtime.NumGoroutine(); n > baseline {
			buf := make([]byte, 1<<16)
			buf = buf[:runtime.Stack(buf, true)]
			t.Errorf("%d goroutines before, %d after everything was closed:\n%s", baseline, n, buf)
		}
	}
}

// within runs call and fails t when it does not return within bound.
func within[T any](t *testing.T, bound time.Duration, what string, call func() (T, error)) (T, error) {
	t.Helper()
	type out struct {
		v   T
		err error
	}
	done := make(chan out, 1)
	go func() {
		v, err := call()
		done <- out{v, err}
	}()
	select {
	case o := <-done:
		return o.v, o.err
	case <-time.After(bound):
		t.Fatalf("%s did not return within %s", what, bound)
		var zero T
		return zero, nil
	}
}

// wantKind fails t unless err is a typed error of kind k.
func wantKind(t *testing.T, err error, k errs.Kind) {
	t.Helper()
	e, ok := errs.As(err)
	if !ok || e.Kind != k {
		t.Fatalf("error %v (%T), want kind %s (exit %d)", err, err, k, errs.ExitCodeForKind(k))
	}
}

// A station that confirms an interrogation again and again, and never terminates it: the
// request timeout ends the command with the exit code of a timeout.
func TestHostileEndlessConfirmations(t *testing.T) {
	leaks := leakCheck(t)
	r := startRogue(t, nil, func(r *rogue, p *rawPeer, a *asdu.ASDU) {
		if a.Type != asdu.C_IC_NA_1 {
			return
		}
		for !r.stopping() {
			if p.send(a.Reply(asdu.CauseActivationCon, false)) != nil {
				return
			}
		}
	})
	cfg := &config.InterrogateConfig{StationConfig: config.StationConfig{ConnConfig: r.conn(400 * time.Millisecond), CommonAddr: 1}}
	_, err := within(t, 5*time.Second, "Interrogate", func() (*types.InterrogateResult, error) {
		return Interrogate(context.Background(), cfg, nil)
	})
	wantKind(t, err, errs.KindTimeout)
	r.close()
	leaks()
}

// The command of `client command` is sent exactly once, whatever the station answers: a
// negative confirmation, silence, or the connection dropped.
func TestHostileCommandSentOnce(t *testing.T) {
	for _, c := range []struct {
		name   string
		answer func(p *rawPeer, a *asdu.ASDU)
		kind   errs.Kind
	}{
		{"negative confirmation", func(p *rawPeer, a *asdu.ASDU) { _ = p.send(a.Reply(asdu.CauseActivationCon, true)) }, errs.KindNegative},
		{"unknown object", func(p *rawPeer, a *asdu.ASDU) { _ = p.send(a.Reply(asdu.CauseUnknownIOA, true)) }, errs.KindNegative},
		{"silence", func(*rawPeer, *asdu.ASDU) {}, errs.KindTimeout},
		{"connection dropped", func(p *rawPeer, _ *asdu.ASDU) { _ = p.conn.Close() }, errs.KindTransport},
		{"garbage", func(p *rawPeer, _ *asdu.ASDU) { _ = p.write([]byte{0x68, 0x04, 0x01, 0x00, 0xff, 0xff, 0x00, 0x13}) }, errs.KindTransport},
	} {
		t.Run(c.name, func(t *testing.T) {
			leaks := leakCheck(t)
			var commands atomic.Int32
			r := startRogue(t, nil, func(_ *rogue, p *rawPeer, a *asdu.ASDU) {
				if a.Type == asdu.C_SC_NA_1 {
					commands.Add(1)
					c.answer(p, a)
				}
			})
			cfg := &config.CommandConfig{StationConfig: config.StationConfig{ConnConfig: r.conn(500 * time.Millisecond), CommonAddr: 1},
				Type: "single", IOA: 5000, Value: "on", Mode: ModeExecute}
			_, err := within(t, 5*time.Second, "Command", func() (*types.CommandResult, error) {
				return Command(context.Background(), cfg, nil)
			})
			if c.kind == errs.KindTransport {
				// A connection that breaks may also show as the request not answered in time.
				if e, ok := errs.As(err); !ok || (e.Kind != errs.KindTransport && e.Kind != errs.KindTimeout && e.Kind != errs.KindProtocol) {
					t.Fatalf("error %v, want a transport, protocol or timeout failure", err)
				}
			} else {
				wantKind(t, err, c.kind)
			}
			r.close()
			if n := commands.Load(); n != 1 {
				t.Errorf("the command was sent %d times", n)
			}
			leaks()
		})
	}
}

// A station that floods activation terminations of other objects while the command waits
// for its own: the command finds its termination, within the bound, and keeps none of the
// others.
func TestHostileTerminationFlood(t *testing.T) {
	leaks := leakCheck(t)
	const flood = 2000
	r := startRogue(t, nil, func(r *rogue, p *rawPeer, a *asdu.ASDU) {
		if a.Type != asdu.C_SC_NA_1 {
			return
		}
		_ = p.send(a.Reply(asdu.CauseActivationCon, false))
		other := asdu.New(asdu.CauseActivationTerm, a.CommonAddr, asdu.SingleCommand{IOA: 4999, Value: true})
		other.Type = asdu.C_SC_NA_1
		for range flood {
			if r.stopping() || p.send(other) != nil {
				return
			}
		}
		_ = p.send(a.Reply(asdu.CauseActivationTerm, false))
	})
	cfg := &config.CommandConfig{StationConfig: config.StationConfig{ConnConfig: r.conn(2 * time.Second), CommonAddr: 1},
		Type: "single", IOA: 5000, Value: "on", Mode: ModeExecute, Termination: 10000}
	res, err := within(t, 10*time.Second, "Command", func() (*types.CommandResult, error) {
		return Command(context.Background(), cfg, nil)
	})
	if err != nil {
		t.Fatal(err)
	}
	if last := res.Steps[len(res.Steps)-1]; last.Step != "termination" || last.Result != "received" {
		t.Errorf("steps %+v", res.Steps)
	}
	r.close()
	leaks()
}

// What the wait for a termination keeps does not grow with what the station floods.
func TestTerminationsKeepOnlyTheirOwn(t *testing.T) {
	term := newTerminations(asdu.C_SC_NA_1, 1, 5000)
	other := asdu.New(asdu.CauseActivationTerm, 1, asdu.SingleCommand{IOA: 4999, Value: true})
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	for i := range 200_000 {
		// A new ASDU each time, as the client decodes one per frame.
		a := *other
		a.Objects = []asdu.InformationObject{asdu.SingleCommand{IOA: asdu.IOA(i % 4999), Value: true}}
		term.HandleASDU(&a)
	}
	runtime.GC()
	runtime.ReadMemStats(&after)
	if grown := int64(after.HeapAlloc) - int64(before.HeapAlloc); grown > 4<<20 {
		t.Errorf("200000 foreign terminations left %d octets behind", grown)
	}
	ctx, cancel := context.WithCancel(context.Background())
	if term.wait(ctx, 10*time.Millisecond) {
		t.Error("a foreign termination was taken for the command's own")
	}
	cancel()
	if term.wait(ctx, time.Second) {
		t.Error("a cancelled wait reported the termination")
	}
	own := asdu.New(asdu.CauseActivationTerm, 1, asdu.SingleCommand{IOA: 5000, Value: true})
	term.HandleASDU(own)
	term.HandleASDU(own) // twice: the second changes nothing
	if !term.wait(context.Background(), time.Second) {
		t.Error("the command's own termination was not seen")
	}
	// Not a termination, another station, another type, no object.
	for _, a := range []*asdu.ASDU{
		asdu.New(asdu.CauseActivationCon, 1, asdu.SingleCommand{IOA: 5000}),
		asdu.New(asdu.CauseActivationTerm, 2, asdu.SingleCommand{IOA: 5000}),
		asdu.New(asdu.CauseActivationTerm, 1, asdu.DoubleCommand{IOA: 5000}),
		{Type: asdu.C_SC_NA_1, Cause: asdu.CauseActivationTerm, CommonAddr: 1},
	} {
		fresh := newTerminations(asdu.C_SC_NA_1, 1, 5000)
		fresh.HandleASDU(a)
		if fresh.wait(context.Background(), 0) {
			t.Errorf("%s taken for the termination", a)
		}
	}
	runtime.KeepAlive(term)
}

// A station that serves a file with a wrong checksum: the transfer fails as a protocol error
// and no output file is left behind.
func TestHostileFileChecksum(t *testing.T) {
	leaks := leakCheck(t)
	const ioa, name = 30000, 1
	payload := bytes.Repeat([]byte{0x5A}, 200)
	r := startRogue(t, nil, func(_ *rogue, p *rawPeer, a *asdu.ASDU) {
		call, ok := a.First().(asdu.FileCall)
		if !ok {
			return
		}
		send := func(o asdu.InformationObject) { _ = p.send(asdu.New(asdu.CauseFileTransfer, a.CommonAddr, o)) }
		switch call.Qualifier {
		case asdu.FileSelect:
			send(asdu.FileReady{IOA: ioa, Name: name, Length: 200})
		case asdu.FileRequest:
			send(asdu.SectionReady{IOA: ioa, Name: name, Section: 1, Length: 200})
		case asdu.SectionRequest:
			send(asdu.FileSegment{IOA: ioa, Name: name, Section: 1, Data: payload})
			send(asdu.FileLastSegment{IOA: ioa, Name: name, Section: 1, Qualifier: asdu.LastSectionNoDeactivation, Checksum: 1})
		}
	})
	out := filepath.Join(t.TempDir(), "out.bin")
	cfg := &config.FileGetConfig{StationConfig: config.StationConfig{ConnConfig: r.conn(time.Second), CommonAddr: 1}, IOA: ioa, Name: name, Output: out}
	_, err := within(t, 5*time.Second, "FileGet", func() (*types.FileTransferResult, error) {
		return FileGet(context.Background(), cfg, nil, io.Discard)
	})
	wantKind(t, err, errs.KindProtocol)
	if _, err := os.Stat(out); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("a failed transfer left %s: %v", out, err)
	}
	r.close()
	leaks()
}

// A broadcast scan of a station that never stops sending: the scan ends at its timeout, with
// what it saw.
func TestHostileScanBroadcastFlood(t *testing.T) {
	leaks := leakCheck(t)
	r := startRogue(t, func(r *rogue, p *rawPeer) {
		data := asdu.New(asdu.QOIStation.Cause(), 7, asdu.SinglePoint{IOA: 1, Value: true})
		if !p.waitStarted(r) {
			return
		}
		for !r.stopping() {
			if p.send(data) != nil {
				return
			}
			time.Sleep(time.Millisecond)
		}
	}, nil)
	cfg := &config.ScanConfig{ConnConfig: r.conn(time.Second), Method: ScanBroadcast}
	begin := time.Now()
	res, err := within(t, 5*time.Second, "Scan", func() (*types.ScanResult, error) {
		return Scan(context.Background(), cfg, nil, nil)
	})
	if err != nil {
		t.Fatal(err)
	}
	if took := time.Since(begin); took > 3*time.Second {
		t.Errorf("the scan took %s with a timeout of 1s", took)
	}
	if res.Found != 1 || res.Stations[0].CommonAddr != 7 {
		t.Errorf("scan: %+v", res)
	}
	r.close()
	leaks()
}

// A sweep of a station that never answers: each address takes the request timeout, the
// sweep ends and reports nothing found.
func TestHostileScanSweepSilence(t *testing.T) {
	leaks := leakCheck(t)
	r := startRogue(t, nil, nil)
	cfg := &config.ScanConfig{ConnConfig: r.conn(200 * time.Millisecond), Method: ScanSweep, Start: 10, End: 12}
	res, err := within(t, 5*time.Second, "Scan", func() (*types.ScanResult, error) {
		return Scan(context.Background(), cfg, nil, io.Discard)
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Probed != 3 || res.Found != 0 || len(res.Stations) != 0 {
		t.Errorf("sweep: %+v", res)
	}
	r.close()
	leaks()
}

// A station that sends far more values than --count asks for, many to an ASDU: monitor
// emits exactly --count values and stops.
func TestHostileMonitorCountFlood(t *testing.T) {
	leaks := leakCheck(t)
	r := startRogue(t, func(r *rogue, p *rawPeer) {
		objs := make([]asdu.InformationObject, 10)
		for i := range objs {
			objs[i] = asdu.MeasuredScaled{IOA: asdu.IOA(100 + i), Value: int16(i)}
		}
		data := asdu.New(asdu.CauseSpontaneous, 1, objs...)
		if !p.waitStarted(r) {
			return
		}
		for !r.stopping() {
			if p.send(data) != nil {
				return
			}
		}
	}, nil)
	for _, count := range []uint32{1, 3, 10, 25} {
		var emitted atomic.Int32
		cfg := &config.MonitorConfig{ConnConfig: r.conn(time.Second), Count: count, NoReconnect: true}
		sum, err := within(t, 5*time.Second, "Monitor", func() (MonitorSummary, error) {
			return Monitor(context.Background(), cfg, io.Discard, func(*types.Point) error { emitted.Add(1); return nil })
		})
		if err != nil {
			t.Fatal(err)
		}
		if n := emitted.Load(); n != int32(count) || sum.Values != int(count) {
			t.Errorf("--count %d: %d values emitted, summary %d", count, n, sum.Values)
		}
	}
	r.close()
	leaks()
}

// A station that breaks the APCI while monitor runs without reconnecting: monitor ends with
// a failure of the connection, not a hang.
func TestHostileMonitorGarbage(t *testing.T) {
	leaks := leakCheck(t)
	r := startRogue(t, func(r *rogue, p *rawPeer) {
		if !p.waitStarted(r) {
			return
		}
		_ = p.write([]byte{0x68, 0x04, 0x07, 0x00, 0x00, 0x00}) // STARTDT act from a station
		_ = p.write([]byte{0x68, 0xff})
		_ = p.write(bytes.Repeat([]byte{0xA5}, 300))
	}, nil)
	cfg := &config.MonitorConfig{ConnConfig: r.conn(time.Second), NoReconnect: true}
	_, err := within(t, 5*time.Second, "Monitor", func() (MonitorSummary, error) {
		return Monitor(context.Background(), cfg, io.Discard, func(*types.Point) error { return nil })
	})
	if e, ok := errs.As(err); !ok || (e.Kind != errs.KindTransport && e.Kind != errs.KindProtocol) {
		t.Fatalf("Monitor: %v, want a transport or protocol failure", err)
	}
	r.close()
	leaks()
}

// What a station may send monitor that is not process data: ASDUs without objects, of
// unknown types, negative, with a broadcast address. Nothing makes monitor fail.
func TestHostileMonitorOddASDUs(t *testing.T) {
	leaks := leakCheck(t)
	odd := [][]byte{
		{byte(asdu.M_SP_NA_1), 0x00, byte(asdu.CauseSpontaneous), 0, 1, 0},                          // zero objects
		{200, 0x02, byte(asdu.CauseSpontaneous), 0, 1, 0, 1, 2, 3},                                  // unknown type
		{byte(asdu.M_SP_NA_1), 0x01, byte(asdu.CauseSpontaneous) | 0x40, 0, 0xff, 0xff, 1, 0, 0, 1}, // negative, broadcast
		{byte(asdu.M_SP_NA_1), 0x81, byte(asdu.CauseSpontaneous), 0, 1, 0, 0xff, 0xff, 0xff, 1},     // sequence, last address
	}
	r := startRogue(t, func(r *rogue, p *rawPeer) {
		if !p.waitStarted(r) {
			return
		}
		for _, raw := range odd {
			_ = p.sendRaw(raw)
		}
		_ = p.send(asdu.New(asdu.CauseSpontaneous, 1, asdu.SinglePoint{IOA: 1, Value: true}))
	}, nil)
	var seen []types.Point
	var mu sync.Mutex
	cfg := &config.MonitorConfig{ConnConfig: r.conn(time.Second), Count: 4, NoReconnect: true}
	_, err := within(t, 5*time.Second, "Monitor", func() (MonitorSummary, error) {
		return Monitor(context.Background(), cfg, io.Discard, func(p *types.Point) error {
			mu.Lock()
			seen = append(seen, *p)
			mu.Unlock()
			return nil
		})
	})
	if err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	if len(seen) != 4 {
		t.Errorf("monitor saw %d values: %+v", len(seen), seen)
	}
	mu.Unlock()
	r.close()
	leaks()
}

// discover against what a scanner meets: a port that accepts and says nothing, one that
// answers with garbage, one that sends frames other than the test frame confirmation. Each
// probe ends within its timeout and reports the port open, not a station.
func TestHostileProbe(t *testing.T) {
	leaks := leakCheck(t)
	for _, c := range []struct {
		name  string
		serve func(conn net.Conn)
	}{
		{"silent", func(conn net.Conn) { _, _ = io.Copy(io.Discard, conn) }},
		{"garbage", func(conn net.Conn) { _, _ = conn.Write(bytes.Repeat([]byte{0x68, 0x00}, 100)) }},
		{"slow drip", func(conn net.Conn) {
			for range 50 {
				if _, err := conn.Write([]byte{0x68}); err != nil {
					return
				}
				time.Sleep(20 * time.Millisecond)
			}
		}},
		{"other frames", func(conn net.Conn) {
			s, _ := apci.NewS(0).MarshalBinary()
			for range 10 {
				if _, err := conn.Write(s); err != nil {
					return
				}
			}
			_, _ = io.Copy(io.Discard, conn)
		}},
	} {
		t.Run(c.name, func(t *testing.T) {
			ln, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			var wg sync.WaitGroup
			wg.Go(func() {
				conn, err := ln.Accept()
				if err != nil {
					return
				}
				defer func() { _ = conn.Close() }()
				_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
				c.serve(conn)
			})
			begin := time.Now()
			status, _ := within(t, 3*time.Second, "probe", func() (string, error) {
				s, _ := probe(context.Background(), ln.Addr().String(), 300*time.Millisecond)
				return s, nil
			})
			if status != probeOpen {
				t.Errorf("probe: %q, want %q", status, probeOpen)
			}
			if took := time.Since(begin); took > 2*time.Second {
				t.Errorf("probe took %s with a timeout of 300ms", took)
			}
			_ = ln.Close()
			wg.Wait()
		})
	}
	leaks()
}

// A station that refuses the clock synchronization: the refusal is a negative confirmation.
func TestHostileClockRefused(t *testing.T) {
	leaks := leakCheck(t)
	r := startRogue(t, nil, func(_ *rogue, p *rawPeer, a *asdu.ASDU) {
		if a.Type == asdu.C_CS_NA_1 {
			_ = p.send(a.Reply(asdu.CauseActivationCon, true))
		}
	})
	cfg := &config.ClockConfig{StationConfig: config.StationConfig{ConnConfig: r.conn(time.Second), CommonAddr: 1}}
	_, err := within(t, 5*time.Second, "Clock", func() (*types.ConfirmedResult, error) {
		return Clock(context.Background(), cfg, nil)
	})
	wantKind(t, err, errs.KindNegative)
	r.close()
	leaks()
}
