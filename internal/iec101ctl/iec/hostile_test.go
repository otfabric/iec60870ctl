// SPDX-License-Identifier: MIT

package iec

import (
	"context"
	"errors"
	"io"
	"math"
	"math/rand/v2"
	"net"
	"os"
	"runtime"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/otfabric/go-iec60870-5/asdu"
	"github.com/otfabric/go-iec60870-5/ft12"
	"github.com/otfabric/go-iec60870-5/iec101"
	"github.com/otfabric/go-iec60870-5/link"
	"github.com/otfabric/iec60870ctl/internal/errs"
	"github.com/otfabric/iec60870ctl/internal/iec101ctl/config"
	"github.com/otfabric/iec60870ctl/internal/iec101ctl/types"
)

// The hostile tests put a secondary station that does not follow IEC 60870-5-2 on the
// other end of a TCP line and write its frames as raw octets. Whatever it does, a command
// must end within its timeout with a typed error or a result, never panic, never send a
// command twice, and leave no goroutine behind.

var fp = ft12.Params{AddrSize: 1}

// chaosSeeds is the number of seeds of the chaos tests: CHAOS_SEEDS (make chaos), or 4.
func chaosSeeds() int {
	if n, err := strconv.Atoi(os.Getenv("CHAOS_SEEDS")); err == nil && n > 0 {
		return n
	}
	return 4
}

// rogue is a misbehaving secondary station on a loopback port. answer is called for every
// frame it receives and returns the octets to write back; nil writes nothing, and a
// returned error closes the line.
type rogue struct {
	ln     net.Listener
	answer func(c *rogueConn, f ft12.Frame) ([]byte, error)
	wg     sync.WaitGroup
	mu     sync.Mutex
	conns  []net.Conn
	frames atomic.Int64
}

type rogueConn struct {
	r      *rogue
	reset  bool
	userFC map[bool]int // user data frames received, by FCB
}

func startRogue(t *testing.T, answer func(c *rogueConn, f ft12.Frame) ([]byte, error)) *rogue {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	r := &rogue{ln: ln, answer: answer}
	r.wg.Go(func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			r.mu.Lock()
			r.conns = append(r.conns, c)
			r.mu.Unlock()
			r.wg.Go(func() { r.serve(c) })
		}
	})
	t.Cleanup(r.close)
	return r
}

func (r *rogue) serve(c net.Conn) {
	defer func() { _ = c.Close() }()
	rc := &rogueConn{r: r, userFC: map[bool]int{}}
	dec := ft12.NewDecoder(c, fp)
	for {
		f, err := dec.Next()
		if err != nil {
			return
		}
		r.frames.Add(1)
		f.Data = append([]byte(nil), f.Data...)
		out, err := r.answer(rc, f)
		if err != nil {
			return
		}
		if out != nil {
			if _, err := c.Write(out); err != nil {
				return
			}
		}
	}
}

func (r *rogue) close() {
	_ = r.ln.Close()
	r.mu.Lock()
	for _, c := range r.conns {
		_ = c.Close()
	}
	r.mu.Unlock()
	r.wg.Wait()
}

// conn returns the client configuration that reaches the rogue.
func (r *rogue) conn(timeout time.Duration) config.ConnConfig {
	port := portOf(r.ln.Addr())
	return config.ConnConfig{LinkConfig: config.LinkConfig{Mode: config.ModeUnbalanced, LinkAddr: 1, LinkAddrSize: 1, CauseSize: 1, CASize: 1, IOASize: 2, LinkTimeout: 100, LinkRetries: 1},
		IP: "127.0.0.1", Port: port, Timeout: uint32(timeout / time.Millisecond), PollInterval: 5}
}

// secondary returns the octets of a secondary frame.
func secondary(fn byte, acd bool, data []byte) []byte {
	f := ft12.Frame{Kind: ft12.KindFixed, Control: byte(link.SecondaryControl(fn, acd, false, false)), Addr: 1}
	if data != nil {
		f.Kind, f.Data = ft12.KindVariable, data
	}
	b, err := f.Encode(fp)
	if err != nil {
		panic(err)
	}
	return b
}

// proper answers as a secondary that has no data does; the starting point of the rogues.
func proper(c *rogueConn, f ft12.Frame) []byte {
	switch link.Control(f.Control).Func() {
	case link.FuncRequestStatus:
		return secondary(link.FuncStatus, false, nil)
	case link.FuncResetLink:
		c.reset = true
		return secondary(link.FuncAck, false, nil)
	case link.FuncUserDataConf:
		c.userFC[link.Control(f.Control).FCB()]++
		return secondary(link.FuncAck, false, nil)
	case link.FuncRequestClass1, link.FuncRequestClass2:
		return secondary(link.FuncNoData, false, nil)
	}
	return secondary(link.FuncNotImplemented, false, nil)
}

func encodeASDU(a *asdu.ASDU) []byte {
	raw, err := a.Encode(iec101.DefaultParams().ASDU)
	if err != nil {
		panic(err)
	}
	return raw
}

// leakCheck fails the test when goroutines are left behind after it and its cleanups.
func leakCheck(t testing.TB) {
	baseline := runtime.NumGoroutine()
	t.Cleanup(func() {
		t.Helper()
		deadline := time.Now().Add(5 * time.Second)
		for runtime.NumGoroutine() > baseline && time.Now().Before(deadline) {
			time.Sleep(10 * time.Millisecond)
		}
		if n := runtime.NumGoroutine(); n > baseline {
			buf := make([]byte, 1<<16)
			t.Fatalf("%d goroutines left behind:\n%s", n-baseline, buf[:runtime.Stack(buf, true)])
		}
	})
}

// within runs call and fails the test when it takes longer than bound.
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
	}
	var zero T
	return zero, nil
}

func wantKind(t *testing.T, err error, k errs.Kind) {
	t.Helper()
	e, ok := errs.As(err)
	if !ok || e.Kind != k {
		t.Fatalf("error %v (%T), want kind %s", err, err, k)
	}
}

// A line that answers nothing, or only with noise, never brings the link up.
func TestHostileSilenceAndNoise(t *testing.T) {
	leakCheck(t)
	rng := rand.New(rand.NewPCG(1, 101))
	for name, answer := range map[string]func(*rogueConn, ft12.Frame) ([]byte, error){
		"silent": func(*rogueConn, ft12.Frame) ([]byte, error) { return nil, nil },
		"noise": func(*rogueConn, ft12.Frame) ([]byte, error) {
			b := make([]byte, 1+rng.IntN(300))
			for i := range b {
				b[i] = byte(rng.IntN(256))
			}
			return b, nil
		},
		"bad checksum": func(c *rogueConn, f ft12.Frame) ([]byte, error) {
			b := proper(c, f)
			b[len(b)-2]++
			return b, nil
		},
		"oversized": func(*rogueConn, ft12.Frame) ([]byte, error) {
			return append([]byte{0x68, 0xFF, 0xFF, 0x68}, make([]byte, 40)...), nil
		},
		"wrong address": func(*rogueConn, ft12.Frame) ([]byte, error) {
			b, _ := ft12.Frame{Kind: ft12.KindFixed, Control: byte(link.SecondaryControl(link.FuncStatus, false, false, false)), Addr: 9}.Encode(fp)
			return b, nil
		},
	} {
		t.Run(name, func(t *testing.T) {
			r := startRogue(t, answer)
			cfg := InterrogateConfig(r.conn(600 * time.Millisecond))
			_, err := within(t, 5*time.Second, "interrogate", func() (*types.InterrogateResult, error) {
				return Interrogate(context.Background(), &cfg, io.Discard)
			})
			e, ok := errs.As(err)
			if !ok || e.Code != errs.CodeLinkInitFailed {
				t.Fatalf("error %v, want link_init_failed", err)
			}
		})
	}
}

// InterrogateConfig is a general interrogation of station 1 over c.
func InterrogateConfig(c config.ConnConfig) config.InterrogateConfig {
	return config.InterrogateConfig{StationConfig: config.StationConfig{ConnConfig: c, CommonAddr: 1}}
}

// A station that acknowledges a command at the link layer and never confirms it gets the
// command once: the client reports the timeout and does not send it again.
func TestHostileCommandSentOnce(t *testing.T) {
	leakCheck(t)
	var got *rogueConn
	var mu sync.Mutex
	r := startRogue(t, func(c *rogueConn, f ft12.Frame) ([]byte, error) {
		mu.Lock()
		got = c
		mu.Unlock()
		return proper(c, f), nil
	})
	cfg := config.CommandConfig{StationConfig: config.StationConfig{ConnConfig: r.conn(700 * time.Millisecond), CommonAddr: 1},
		Type: "single", IOA: 5000, Value: "on", Mode: ModeExecute}
	_, err := within(t, 5*time.Second, "command", func() (*types.CommandResult, error) {
		return Command(context.Background(), &cfg, io.Discard)
	})
	wantKind(t, err, errs.KindTimeout)
	mu.Lock()
	defer mu.Unlock()
	if n := got.userFC[true] + got.userFC[false]; n != 1 {
		t.Fatalf("the command was sent %d times", n)
	}
}

// A station that closes the line in the middle of an exchange ends the command with a
// transport error.
func TestHostileCloseMidExchange(t *testing.T) {
	leakCheck(t)
	r := startRogue(t, func(c *rogueConn, f ft12.Frame) ([]byte, error) {
		if link.Control(f.Control).Func() == link.FuncUserDataConf {
			return nil, errors.New("close")
		}
		return proper(c, f), nil
	})
	cfg := InterrogateConfig(r.conn(2 * time.Second))
	_, err := within(t, 5*time.Second, "interrogate", func() (*types.InterrogateResult, error) {
		return Interrogate(context.Background(), &cfg, io.Discard)
	})
	wantKind(t, err, errs.KindTransport)
}

// A station that floods class 1 data (with ACD always set) cannot hold a monitor with
// --count, and an interrogation that never terminates ends at its timeout.
func TestHostileFlood(t *testing.T) {
	leakCheck(t)
	rng := rand.New(rand.NewPCG(2, 101))
	var mu sync.Mutex
	r := startRogue(t, func(c *rogueConn, f ft12.Frame) ([]byte, error) {
		switch link.Control(f.Control).Func() {
		case link.FuncRequestClass1, link.FuncRequestClass2:
			mu.Lock()
			v := rng.Float32()
			mu.Unlock()
			return secondary(link.FuncUserData, true, encodeASDU(asdu.New(asdu.CauseSpontaneous, 1, asdu.MeasuredFloat{IOA: 7, Value: v}))), nil
		}
		return proper(c, f), nil
	})
	mon := config.MonitorConfig{ConnConfig: r.conn(2 * time.Second), Count: 50}
	n := 0
	_, err := within(t, 10*time.Second, "monitor", func() (MonitorSummary, error) {
		return Monitor(context.Background(), &mon, io.Discard, func(*types.Point) error { n++; return nil })
	})
	if err != nil || n != 50 {
		t.Fatalf("monitor: %d values, %v", n, err)
	}
	cfg := InterrogateConfig(r.conn(800 * time.Millisecond))
	_, err = within(t, 5*time.Second, "interrogate", func() (*types.InterrogateResult, error) {
		return Interrogate(context.Background(), &cfg, io.Discard)
	})
	wantKind(t, err, errs.KindTimeout)
}

// Undecodable and odd ASDUs (wrong common address, unknown types, empty objects) are
// dropped or shown, never a panic; a monitor filtered to a type sees the well-formed one.
func TestHostileOddASDUs(t *testing.T) {
	leakCheck(t)
	odd := [][]byte{
		{0x01},                         // truncated header
		{0xFF, 0x01, 0x03, 0x01, 0x00}, // unknown type
		{0x01, 0x85, 0x03, 0x01, 0x64, 0x00, 0x01}, // SQ with too few octets
		encodeASDU(asdu.New(asdu.CauseSpontaneous, 9, asdu.SinglePoint{IOA: 1, Value: true})),
		encodeASDU(asdu.New(asdu.CauseSpontaneous, 1, asdu.SinglePoint{IOA: 2, Value: true})),
	}
	var i atomic.Int64
	r := startRogue(t, func(c *rogueConn, f ft12.Frame) ([]byte, error) {
		switch link.Control(f.Control).Func() {
		case link.FuncRequestClass1, link.FuncRequestClass2:
			n := i.Add(1) - 1
			if n < int64(len(odd)) {
				return secondary(link.FuncUserData, true, odd[n]), nil
			}
		}
		return proper(c, f), nil
	})
	mon := config.MonitorConfig{ConnConfig: r.conn(2 * time.Second), Count: 1, CommonAddr: "1", Types: "M_SP_NA_1"}
	var got []types.Point
	_, err := within(t, 5*time.Second, "monitor", func() (MonitorSummary, error) {
		return Monitor(context.Background(), &mon, io.Discard, func(p *types.Point) error { got = append(got, *p); return nil })
	})
	if err != nil || len(got) != 1 || got[0].IOA != 2 {
		t.Fatalf("monitor: %+v, %v", got, err)
	}
}

// Chaos: a secondary that answers properly, with noise, with a corrupted or truncated
// frame, late, or by closing the line, chosen per frame by a seeded generator. Every
// command ends within its bound with a result or a typed error.
func TestHostileChaos(t *testing.T) {
	leakCheck(t)
	for seed := range chaosSeeds() {
		rng := rand.New(rand.NewPCG(uint64(seed), 101))
		var mu sync.Mutex
		pick := func(n int) int {
			mu.Lock()
			defer mu.Unlock()
			return rng.IntN(n)
		}
		r := startRogue(t, func(c *rogueConn, f ft12.Frame) ([]byte, error) {
			b := proper(c, f)
			switch pick(10) {
			case 0:
				return []byte{0x10, 0x00}, nil
			case 1:
				b[len(b)-2] ^= 0xFF
				return b, nil
			case 2:
				return b[:len(b)/2], nil
			case 3:
				time.Sleep(time.Duration(pick(150)) * time.Millisecond)
				return b, nil
			case 4:
				if pick(4) == 0 {
					return nil, errors.New("close")
				}
			case 5:
				return secondary(link.FuncUserData, true, []byte{byte(pick(256)), byte(pick(256)), 3, 1, 0, 0, 1}), nil
			}
			return b, nil
		})
		cfg := InterrogateConfig(r.conn(700 * time.Millisecond))
		_, err := within(t, 5*time.Second, "interrogate", func() (*types.InterrogateResult, error) {
			return Interrogate(context.Background(), &cfg, io.Discard)
		})
		if _, ok := errs.As(err); err != nil && !ok {
			t.Fatalf("seed %d: untyped error %v", seed, err)
		}
		ping := config.PingConfig{StationConfig: config.StationConfig{ConnConfig: r.conn(400 * time.Millisecond), CommonAddr: 1}, Method: PingLinkStatus, Count: 3, Interval: 5}
		_, err = within(t, 5*time.Second, "ping", func() (types.PingSummary, error) {
			return Ping(context.Background(), &ping, io.Discard, func(*types.PingSample) error { return nil })
		})
		if _, ok := errs.As(err); err != nil && !ok {
			t.Fatalf("seed %d: untyped ping error %v", seed, err)
		}
		r.close()
	}
}

func endOfInit() *asdu.ASDU {
	return asdu.New(asdu.CauseInitialized, 1, asdu.EndOfInitialization{})
}

// portOf returns the TCP port of a loopback listener.
func portOf(a net.Addr) uint16 {
	p := a.(*net.TCPAddr).Port
	if p <= 0 || p > math.MaxUint16 {
		panic("not a TCP port")
	}
	return uint16(p)
}
