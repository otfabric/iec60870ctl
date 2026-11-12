// SPDX-License-Identifier: MIT

package meter_test

// The client commands against a hostile integrated total station: one that never answers,
// answers with garbage, wrong checksums, truncated or oversized frames, closes in the middle
// of an exchange, refuses, or floods the controlling station with data that answers nothing.
// Every case checks that the command returns within a bound, with the error kind (and so the
// exit code) a script relies on, and that it leaves no goroutine behind.

import (
	"context"
	"errors"
	"math/rand/v2"
	"net"
	"os"
	"runtime"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/otfabric/go-iec60870-5/ft12"
	"github.com/otfabric/go-iec60870-5/iec102"
	"github.com/otfabric/iec60870ctl/internal/errs"
	"github.com/otfabric/iec60870ctl/internal/iec102ctl/config"
	"github.com/otfabric/iec60870ctl/internal/iec102ctl/meter"
	"github.com/otfabric/iec60870ctl/internal/iec102ctl/stationtest"
)

var p2 = ft12.Params{AddrSize: 2}

// rogue is a station on a loopback port whose answers a script decides: for each frame of
// the controlling station, answer returns the octets to send back (nil for none) and
// whether to close the connection.
type rogue struct {
	ln   net.Listener
	wg   sync.WaitGroup
	stop chan struct{}
}

type answerFunc func(f ft12.Frame, n int) (out []byte, hangup bool)

func startRogue(t *testing.T, answer answerFunc) *rogue {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	r := &rogue{ln: ln, stop: make(chan struct{})}
	r.wg.Add(1)
	go func() {
		defer r.wg.Done()
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			r.wg.Add(1)
			go func() {
				defer r.wg.Done()
				r.serve(c, answer)
			}()
		}
	}()
	t.Cleanup(func() {
		close(r.stop)
		_ = ln.Close()
		r.wg.Wait()
	})
	return r
}

func (r *rogue) url() string { return "tcp://" + r.ln.Addr().String() }

func (r *rogue) serve(c net.Conn, answer answerFunc) {
	ended := make(chan struct{})
	defer func() { close(ended); _ = c.Close() }()
	go func() {
		select {
		case <-r.stop:
			_ = c.Close()
		case <-ended:
		}
	}()
	dec := ft12.NewDecoder(c, p2)
	for n := 0; ; n++ {
		f, err := dec.Next()
		if err != nil {
			if errors.Is(err, ft12.ErrChecksum) || errors.Is(err, ft12.ErrMalformed) {
				continue
			}
			return
		}
		f.Data = append([]byte(nil), f.Data...)
		out, hangup := answer(f, n)
		if len(out) > 0 {
			_ = c.SetWriteDeadline(time.Now().Add(5 * time.Second))
			if _, err := c.Write(out); err != nil {
				return
			}
		}
		if hangup {
			return
		}
	}
}

func fixed(c byte) []byte {
	b, _ := ft12.Frame{Kind: ft12.KindFixed, Control: c, Addr: 1}.Encode(p2)
	return b
}

func variable(data []byte) []byte {
	b, _ := ft12.Frame{Kind: ft12.KindVariable, Control: 0x08, Addr: 1}.Encode(p2)
	if len(data) > 0 {
		b, _ = ft12.Frame{Kind: ft12.KindVariable, Control: 0x08, Addr: 1, Data: data}.Encode(p2)
	}
	return b
}

// linkUp answers the link procedures as a station does: status of link, ACK of a reset and
// of user data, and data by poll from next (nil: no data).
func linkUp(f ft12.Frame, next func() []byte) []byte {
	switch f.Control & 0x0F {
	case 9:
		return fixed(0x0B)
	case 0, 3:
		return fixed(0x00)
	case 10, 11:
		if d := next(); d != nil {
			return variable(d)
		}
		return fixed(0x09)
	}
	return nil
}

func asduOf(t *testing.T, a *iec102.ASDU) []byte {
	t.Helper()
	b, err := a.Encode(iec102.Params{})
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// bounded runs the command and checks that it ends within limit with the kind and code.
func bounded(t *testing.T, limit time.Duration, kind errs.Kind, code string, run func(ctx context.Context) error) {
	t.Helper()
	before := runtime.NumGoroutine()
	start := time.Now()
	c, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	err := run(c)
	if d := time.Since(start); d > limit {
		t.Errorf("took %s, more than %s", d, limit)
	}
	e, ok := errs.As(err)
	if !ok || e.Kind != kind || (code != "" && e.Code != code) {
		t.Errorf("error %v (%+v), want %s/%s", err, e, kind, code)
	}
	noLeak(t, before)
}

// noLeak waits for the goroutines of a command to end.
func noLeak(t *testing.T, before int) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for runtime.NumGoroutine() > before+2 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if n := runtime.NumGoroutine(); n > before+2 {
		buf := make([]byte, 1<<16)
		t.Errorf("%d goroutines left, %d before:\n%s", n, before, buf[:runtime.Stack(buf, true)])
	}
}

func quick(url string) config.InfoConfig {
	c := config.InfoConfig{ConnConfig: config.Defaults()}
	c.URL, c.Timeout, c.LinkTimeout, c.LinkRetries = url, 1500, 100, 1
	return c
}

func TestHostileSilentStation(t *testing.T) {
	r := startRogue(t, func(ft12.Frame, int) ([]byte, bool) { return nil, false })
	c := quick(r.url())
	bounded(t, 3*time.Second, errs.KindTimeout, errs.CodeLinkTimeout, func(ctx context.Context) error { _, err := meter.Info(ctx, &c, nil); return err })
}

func TestHostileStationHangsUp(t *testing.T) {
	r := startRogue(t, func(ft12.Frame, int) ([]byte, bool) { return nil, true })
	c := quick(r.url())
	bounded(t, 3*time.Second, errs.KindTransport, errs.CodeConnectionLost, func(ctx context.Context) error { _, err := meter.Info(ctx, &c, nil); return err })
}

// The link comes up, then the station closes the line in the middle of the request.
func TestHostileStationClosesMidExchange(t *testing.T) {
	r := startRogue(t, func(f ft12.Frame, _ int) ([]byte, bool) {
		if f.Control&0x0F == 3 {
			return nil, true
		}
		return linkUp(f, func() []byte { return nil }), false
	})
	c := quick(r.url())
	bounded(t, 3*time.Second, errs.KindTransport, errs.CodeConnectionLost, func(ctx context.Context) error { _, err := meter.Info(ctx, &c, nil); return err })
}

// The link comes up and the request is acknowledged, but no answer ever comes: the polls
// are answered with "no data" until --timeout.
func TestHostileStationNeverAnswers(t *testing.T) {
	r := startRogue(t, func(f ft12.Frame, _ int) ([]byte, bool) { return linkUp(f, func() []byte { return nil }), false })
	c := quick(r.url())
	bounded(t, 3*time.Second, errs.KindTimeout, errs.CodeRequestTimeout, func(ctx context.Context) error { _, err := meter.Info(ctx, &c, nil); return err })
}

// Garbage, wrong checksums, truncated frames and data of another measuring point flood the
// client; the request still ends at --timeout, and an REE session is still closed.
func TestHostileStationFloods(t *testing.T) {
	for name, poll := range map[string]func(t *testing.T) []byte{
		"other common address": func(t *testing.T) []byte {
			return asduOf(t, iec102.New(iec102.CauseRequest, 99, 0, iec102.SystemTime{}))
		},
		"undecodable ASDU": func(*testing.T) []byte { return []byte{72, 1, 5} },
		"unrelated data": func(t *testing.T) []byte {
			return asduOf(t, iec102.New(iec102.CauseRequest, 1, 0, iec102.SystemTime{}))
		},
	} {
		t.Run(name, func(t *testing.T) {
			data := poll(t)
			r := startRogue(t, func(f ft12.Frame, _ int) ([]byte, bool) { return linkUp(f, func() []byte { return data }), false })
			c := config.TotalsConfig{ConnConfig: quick(r.url()).ConnConfig, Record: 11, Oldest: true}
			bounded(t, 4*time.Second, errs.KindTimeout, "", func(ctx context.Context) error { _, err := meter.Totals(ctx, &c, nil); return err })
		})
	}
}

// Octets that are no frame at all: noise, wrong checksums, frames cut short, a length
// field that promises more than comes. The link start-up times out.
func TestHostileStationSendsGarbage(t *testing.T) {
	good := fixed(0x0B)
	bad := append([]byte(nil), good...)
	bad[len(bad)-2] ^= 0xFF
	for name, out := range map[string][]byte{
		"noise":           {0x00, 0xFF, 0x13, 0x37, 0xE4},
		"wrong checksum":  bad,
		"truncated":       good[:3],
		"oversized":       {0x68, 0xFF, 0xFF, 0x68, 0x08, 0x01, 0x00},
		"length mismatch": {0x68, 0x05, 0x06, 0x68, 0x08, 0x01, 0x00, 0x00, 0x00, 0x09, 0x16},
	} {
		t.Run(name, func(t *testing.T) {
			r := startRogue(t, func(ft12.Frame, int) ([]byte, bool) { return out, false })
			c := quick(r.url())
			bounded(t, 4*time.Second, errs.KindTimeout, "", func(ctx context.Context) error { _, err := meter.Info(ctx, &c, nil); return err })
		})
	}
}

// A station that answers C_AC_NA_2 with nothing: the login times out and nothing else is
// sent.
func TestHostileStationIgnoresLogin(t *testing.T) {
	r := startRogue(t, func(f ft12.Frame, _ int) ([]byte, bool) { return linkUp(f, func() []byte { return nil }), false })
	c := quick(r.url())
	c.AccessKey = "7"
	bounded(t, 3*time.Second, errs.KindTimeout, errs.CodeRequestTimeout, func(ctx context.Context) error { _, err := meter.Info(ctx, &c, nil); return err })
}

// A station that confirms the login but not the logout: the result is there, and partial.
func TestHostileStationDropsLogout(t *testing.T) {
	var mu sync.Mutex
	var queue [][]byte
	r := startRogue(t, func(f ft12.Frame, _ int) ([]byte, bool) {
		if f.Control&0x0F == 3 && len(f.Data) > 0 {
			a, err := iec102.Decode(f.Data, iec102.Params{})
			if err == nil {
				mu.Lock()
				switch a.Type {
				case 183:
					m := *a
					m.Cause = iec102.CauseActivationCon
					queue = append(queue, asduOf(t, &m))
				case iec102.C_RD_NA_2:
					queue = append(queue, asduOf(t, iec102.New(5, 1, 0, iec102.ProductSpec{Standard: 1})))
				default: // the logout, and nothing else, goes unanswered
				}
				mu.Unlock()
			}
		}
		return linkUp(f, func() []byte {
			mu.Lock()
			defer mu.Unlock()
			if len(queue) == 0 {
				return nil
			}
			d := queue[0]
			queue = queue[1:]
			return d
		}), false
	})
	c := quick(r.url())
	c.AccessKey = "7"
	res, err := meter.Info(context.Background(), &c, nil)
	if err != nil || res.Standard != 1 || !res.Session.Login || res.Session.Logout || !meter.SessionPartial(res.Meter) {
		t.Fatalf("%+v %v", res, err)
	}
}

// chaosSeeds is the number of seeds of a chaos test: CHAOS_SEEDS (make chaos), or a few in
// an ordinary test run.
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

// TestChaosLine puts a line between the client and the simulated meter that, as its seed
// decides, drops, corrupts, splits, delays or cuts what passes. Every command must end
// within its bound, either with the right result or with a typed error, and leave no
// goroutine behind.
func TestChaosLine(t *testing.T) {
	s := stationtest.Start(t)
	for seed := range chaosSeeds(t) {
		t.Run("seed"+strconv.Itoa(seed), func(t *testing.T) {
			before := runtime.NumGoroutine()
			p := newChaosProxy(t, s.Addr, uint64(seed))
			c := config.TotalsConfig{ConnConfig: quick("tcp://" + p.l.Addr().String()).ConnConfig, Record: 11, First: 1, Last: 8, From: "2026-01-01", To: "2026-01-02"}
			c.Timeout = 2000
			// FT 1.2 cannot tell a late answer from the answer to the next poll (one FCB bit):
			// the link time-out must exceed the worst delay of the line, 150 ms here, or an
			// answer is taken for the next one and a period is lost (IEC 60870-5-2).
			c.LinkTimeout = 400
			start := time.Now()
			r, err := meter.Totals(context.Background(), &c, nil)
			if d := time.Since(start); d > 8*time.Second {
				t.Errorf("took %s", d)
			}
			if err == nil {
				if r.Count != 3 {
					t.Errorf("a result with %d periods", r.Count)
				}
			} else if _, ok := errs.As(err); !ok {
				t.Errorf("untyped error %v", err)
			} else if e, _ := errs.As(err); e.Kind == errs.KindInternal {
				t.Errorf("internal error %v", err)
			}
			p.close()
			noLeak(t, before)
		})
	}
}

// chaosProxy forwards one connection at a time between a client and the station, and on
// the way drops, corrupts, splits, delays or cuts what passes, as decided by its seed.
type chaosProxy struct {
	l    net.Listener
	to   string
	mu   sync.Mutex
	rng  *rand.Rand
	wg   sync.WaitGroup
	done chan struct{}
	once sync.Once
}

func newChaosProxy(t *testing.T, to string, seed uint64) *chaosProxy {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	p := &chaosProxy{l: l, to: to, rng: rand.New(rand.NewPCG(seed, seed^0x9e3779b97f4a7c15)), done: make(chan struct{})}
	p.wg.Add(1)
	go p.accept()
	t.Cleanup(p.close)
	return p
}

func (p *chaosProxy) close() {
	p.once.Do(func() {
		close(p.done)
		_ = p.l.Close()
	})
	p.wg.Wait()
}

func (p *chaosProxy) intn(n int) int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.rng.IntN(n)
}

func (p *chaosProxy) accept() {
	defer p.wg.Done()
	for {
		c, err := p.l.Accept()
		if err != nil {
			return
		}
		up, err := net.Dial("tcp", p.to)
		if err != nil {
			_ = c.Close()
			continue
		}
		p.wg.Add(2)
		go p.pipe(c, up)
		go p.pipe(up, c)
		go func() {
			<-p.done
			_ = c.Close()
			_ = up.Close()
		}()
	}
}

// pipe copies from a to b, mangling as it goes.
func (p *chaosProxy) pipe(a, b net.Conn) {
	defer p.wg.Done()
	defer func() { _ = a.Close(); _ = b.Close() }()
	buf := make([]byte, 512)
	for {
		n, err := a.Read(buf)
		if err != nil {
			return
		}
		chunk := append([]byte(nil), buf[:n]...)
		switch p.intn(20) {
		case 0:
			continue // drop
		case 1:
			chunk[p.intn(len(chunk))] ^= byte(1 + p.intn(255)) // corrupt
		case 2:
			time.Sleep(time.Duration(p.intn(150)) * time.Millisecond) // delay
		case 3:
			if p.intn(4) == 0 {
				return // cut
			}
		case 4:
			k := p.intn(len(chunk)) // split
			if _, err := b.Write(chunk[:k]); err != nil {
				return
			}
			chunk = chunk[k:]
		}
		if _, err := b.Write(chunk); err != nil {
			return
		}
	}
}
