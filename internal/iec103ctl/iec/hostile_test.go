// SPDX-License-Identifier: MIT

package iec

// The client commands against a hostile protection device: one that answers with garbage,
// wrong checksums or half frames, stalls, closes in the middle of an exchange, floods the
// line, or behaves until the link is up and then misbehaves. Every case checks that the
// command returns within a bound, with an error of a kind (and so an exit code) a script
// relies on, and that it leaves no goroutine behind. The behaviour is drawn from a seed:
// CHAOS_SEEDS seeds (make chaos), 3 otherwise.

import (
	"context"
	"io"
	"math/rand/v2"
	"net"
	"os"
	"runtime"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/otfabric/go-iec60870-5/ft12"
	"github.com/otfabric/go-iec60870-5/iec103"
	"github.com/otfabric/go-iec60870-5/link"
	"github.com/otfabric/iec60870ctl/internal/errs"
	"github.com/otfabric/iec60870ctl/internal/iec103ctl/config"
	"github.com/otfabric/iec60870ctl/internal/iec103ctl/stationtest"
	"github.com/otfabric/iec60870ctl/internal/iec103ctl/types"
)

func chaosSeeds(t *testing.T) int {
	t.Helper()
	if s := os.Getenv("CHAOS_SEEDS"); s != "" {
		n, err := strconv.Atoi(s)
		if err != nil || n < 1 {
			t.Fatalf("CHAOS_SEEDS=%q", s)
		}
		return n
	}
	return 3
}

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

var ft = ft12.Params{AddrSize: 1}

// behaviour is what a rogue device does with each frame it receives.
type behaviour int

const (
	garbage        behaviour = iota // answers with random octets
	badChecksum                     // answers with a correct frame whose checksum is wrong
	halfFrame                       // answers with the first half of a frame, then nothing
	closeAtOnce                     // closes the line at the first frame
	flood                           // sends random frames and octets without pause
	upThenClose                     // starts the link, then closes at the first user data
	upThenSilent                    // starts the link, then answers nothing
	upThenNack                      // starts the link, then answers every request with "not implemented"
	upThenJunkASDU                  // starts the link, then answers class 1 polls with random ASDUs
	behaviours
)

// rogue is a hostile device on a TCP line.
type rogue struct {
	ln   net.Listener
	b    behaviour
	rnd  *rand.Rand
	mu   sync.Mutex
	conn []net.Conn
	wg   sync.WaitGroup
}

func newRogue(t *testing.T, b behaviour, seed uint64) *rogue {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	r := &rogue{ln: ln, b: b, rnd: rand.New(rand.NewPCG(seed, uint64(b)))}
	r.wg.Go(func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			r.mu.Lock()
			r.conn = append(r.conn, c)
			r.mu.Unlock()
			r.wg.Go(func() { r.serve(c) })
		}
	})
	return r
}

func (r *rogue) url() string { return "tcp://" + r.ln.Addr().String() }

func (r *rogue) close() {
	_ = r.ln.Close()
	r.mu.Lock()
	for _, c := range r.conn {
		_ = c.Close()
	}
	r.mu.Unlock()
	r.wg.Wait()
}

func (r *rogue) random(n int) []byte {
	r.mu.Lock()
	defer r.mu.Unlock()
	b := make([]byte, n)
	for i := range b {
		b[i] = byte(r.rnd.IntN(256))
	}
	return b
}

func (r *rogue) intn(n int) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.rnd.IntN(n)
}

func fixed(fn byte, acd bool) ft12.Frame {
	return ft12.Frame{Kind: ft12.KindFixed, Control: byte(link.SecondaryControl(fn, acd, false, false)), Addr: 1}
}

// honest answers a link request as a device with an empty class 1 queue does.
func honest(f ft12.Frame) ft12.Frame {
	switch link.Control(f.Control).Func() {
	case 9: // request status of link
		return fixed(11, false)
	case 10, 11: // class 1, class 2
		return fixed(9, false)
	default: // reset CU, reset FCB, send/confirm
		return fixed(0, false)
	}
}

func (r *rogue) serve(c net.Conn) {
	write := func(b []byte) { _ = c.SetWriteDeadline(time.Now().Add(time.Second)); _, _ = c.Write(b) }
	if r.b == flood {
		r.wg.Go(func() {
			for i := 0; i < 2000; i++ {
				if i%2 == 0 {
					a, _ := iec103.New(1, 1, 160, 16, iec103.TimeTagged{DPI: 2}).Encode()
					f, _ := ft12.Frame{Kind: ft12.KindVariable, Control: byte(link.SecondaryControl(8, true, false, false)), Addr: 1, Data: a}.Encode(ft)
					write(f)
				} else {
					write(r.random(1 + r.intn(40)))
				}
			}
		})
	}
	dec := ft12.NewDecoder(c, ft)
	up := false
	for {
		f, err := dec.Next()
		if err != nil {
			return
		}
		fn := link.Control(f.Control).Func()
		if !up && (r.b >= upThenClose) {
			b, _ := honest(f).Encode(ft)
			write(b)
			up = fn == 0 || fn == 7
			continue
		}
		switch r.b {
		case garbage:
			write(r.random(1 + r.intn(64)))
		case badChecksum:
			b, _ := honest(f).Encode(ft)
			b[len(b)-2] ^= 0xFF
			write(b)
		case halfFrame:
			b, _ := honest(f).Encode(ft)
			write(b[:len(b)/2])
			return
		case closeAtOnce, upThenClose:
			if r.b == closeAtOnce || f.Kind == ft12.KindVariable {
				_ = c.Close()
				return
			}
			b, _ := honest(f).Encode(ft)
			write(b)
		case flood:
		case upThenSilent:
		case upThenNack:
			b, _ := fixed(15, false).Encode(ft)
			write(b)
		case upThenJunkASDU:
			if fn == 10 || fn == 11 {
				b, _ := ft12.Frame{Kind: ft12.KindVariable, Control: byte(link.SecondaryControl(8, r.intn(2) == 0, false, false)), Addr: 1, Data: r.random(1 + r.intn(60))}.Encode(ft)
				write(b)
			} else {
				b, _ := honest(f).Encode(ft)
				write(b)
			}
		default:
		}
	}
}

// TestHostileDevice runs commands against every behaviour: each returns within its bound
// with a transport, timeout or protocol error, never success and never an internal error.
func TestHostileDevice(t *testing.T) {
	n := chaosSeeds(t)
	for b := range behaviours {
		for seed := range n {
			t.Run(strconv.Itoa(int(b))+"/"+strconv.Itoa(seed), func(t *testing.T) {
				defer leakCheck(t)()
				r := newRogue(t, b, uint64(seed))
				defer r.close()
				c := conn(r.url())
				c.Timeout, c.LinkTimeout, c.Retries = 400, 50, 1
				start := time.Now()
				var err error
				switch seed % 4 {
				case 0:
					_, err = Interrogate(context.Background(), &config.InterrogateConfig{ConnConfig: c}, nil)
				case 1:
					_, err = Command(context.Background(), &config.CommandConfig{ConnConfig: c, Fun: 160, Inf: 16, Value: "on"}, nil)
				case 2:
					_, err = GenericReadEntry(context.Background(), &config.GenericEntryConfig{ConnConfig: c, GIN: "1.1"}, nil)
				default:
					_, err = DisturbanceGet(context.Background(), &config.DisturbanceGetConfig{ConnConfig: c, Fun: 160, FAN: 1, TransferTimeout: 400}, nil)
				}
				if d := time.Since(start); d > 5*time.Second {
					t.Errorf("returned after %v", d)
				}
				if err == nil {
					t.Fatal("a hostile device answered positively")
				}
				switch k := kindOf(t, err); k {
				case errs.KindTransport, errs.KindTimeout, errs.KindProtocol:
				default:
					t.Fatalf("kind %s: %v", k, err)
				}
			})
		}
	}
}

// TestChaosMonitorFlood: a device that floods the line with frames and noise is monitored
// for a bounded time without blocking, and leaves nothing behind.
func TestChaosMonitorFlood(t *testing.T) {
	for seed := range chaosSeeds(t) {
		t.Run(strconv.Itoa(seed), func(t *testing.T) {
			defer leakCheck(t)()
			r := newRogue(t, upThenJunkASDU, uint64(seed))
			defer r.close()
			c := conn(r.url())
			c.Timeout, c.LinkTimeout = 1000, 50
			ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
			defer cancel()
			start := time.Now()
			_, err := Monitor(ctx, &config.MonitorConfig{ConnConfig: c}, io.Discard, func(m *types.Message) error { _ = m.Summary(); return nil })
			if err != nil {
				t.Fatal(err)
			}
			if time.Since(start) > 5*time.Second {
				t.Fatal("monitor did not stop")
			}
		})
	}
}

// TestChaosLossyLine runs the commands through a proxy that drops, corrupts and delays
// octets as its seed decides: every command ends within its bound, with success or a
// typed error of the line.
func TestChaosLossyLine(t *testing.T) {
	st := stationtest.Start(t, stationtest.Model)
	for seed := range chaosSeeds(t) {
		t.Run(strconv.Itoa(seed), func(t *testing.T) {
			defer leakCheck(t)()
			px := newLossy(t, st.Addr, uint64(seed))
			defer px.close()
			c := conn("tcp://" + px.addr())
			c.Timeout, c.LinkTimeout, c.Retries = 2000, 100, 3
			start := time.Now()
			_, err := Interrogate(context.Background(), &config.InterrogateConfig{ConnConfig: c}, nil)
			if time.Since(start) > 10*time.Second {
				t.Fatal("no end")
			}
			if err != nil {
				switch k := kindOf(t, err); k {
				case errs.KindTransport, errs.KindTimeout, errs.KindProtocol:
				default:
					t.Fatalf("kind %s: %v", k, err)
				}
			}
		})
	}
}

// lossy forwards one connection to a target and damages what the device sends.
type lossy struct {
	ln  net.Listener
	rnd *rand.Rand
	mu  sync.Mutex
	cs  []net.Conn
	wg  sync.WaitGroup
}

func newLossy(t *testing.T, target string, seed uint64) *lossy {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	l := &lossy{ln: ln, rnd: rand.New(rand.NewPCG(seed, 7))}
	l.wg.Go(func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			u, err := net.Dial("tcp", target)
			if err != nil {
				_ = c.Close()
				return
			}
			l.mu.Lock()
			l.cs = append(l.cs, c, u)
			l.mu.Unlock()
			l.wg.Go(func() { _, _ = io.Copy(u, c); _ = u.Close() })
			l.wg.Go(func() {
				defer func() { _ = c.Close() }()
				buf := make([]byte, 256)
				for {
					n, err := u.Read(buf)
					if err != nil {
						return
					}
					l.mu.Lock()
					p := l.rnd.IntN(10)
					if p == 0 && n > 0 {
						buf[l.rnd.IntN(n)] ^= 0x55
					}
					l.mu.Unlock()
					if p == 1 {
						continue // dropped
					}
					if _, err := c.Write(buf[:n]); err != nil {
						return
					}
				}
			})
		}
	})
	return l
}

func (l *lossy) addr() string { return l.ln.Addr().String() }

func (l *lossy) close() {
	_ = l.ln.Close()
	l.mu.Lock()
	for _, c := range l.cs {
		_ = c.Close()
	}
	l.mu.Unlock()
	l.wg.Wait()
}
