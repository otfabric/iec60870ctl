// SPDX-License-Identifier: MIT

package cmd

import (
	"context"
	"math/rand/v2"
	"net"
	"os"
	"runtime"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/otfabric/iec60870ctl/internal/errs"
	"github.com/otfabric/iec60870ctl/internal/iec104ctl/stationtest"
)

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

// chaosProxy forwards one connection at a time between a client and the station, and on
// the way drops, corrupts, splits, delays or cuts what passes, as decided by its seed.
type chaosProxy struct {
	l    net.Listener
	to   string
	rng  *rand.Rand
	mu   sync.Mutex
	wg   sync.WaitGroup
	done chan struct{}
	once sync.Once
}

// close stops the proxy and waits for its connections to end.
func (p *chaosProxy) close() {
	p.once.Do(func() {
		close(p.done)
		_ = p.l.Close()
	})
	p.wg.Wait()
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

func (p *chaosProxy) port() string { return strconv.Itoa(p.l.Addr().(*net.TCPAddr).Port) }

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
		s, err := net.Dial("tcp", p.to)
		if err != nil {
			_ = c.Close()
			continue
		}
		p.wg.Add(2)
		go p.pipe(c, s)
		go p.pipe(s, c)
	}
}

// pipe copies from src to dst with faults. Either side closing ends both.
func (p *chaosProxy) pipe(src, dst net.Conn) {
	defer p.wg.Done()
	defer func() { _ = src.Close(); _ = dst.Close() }()
	buf := make([]byte, 512)
	for {
		select {
		case <-p.done:
			return
		default:
		}
		_ = src.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
		n, err := src.Read(buf)
		if n > 0 {
			chunk := append([]byte(nil), buf[:n]...)
			switch r := p.intn(100); {
			case r < 4: // cut the connection
				return
			case r < 10: // drop the chunk
				continue
			case r < 18: // flip a bit
				i := p.intn(len(chunk))
				chunk[i] ^= byte(1 << p.intn(8))
			case r < 24: // delay
				time.Sleep(time.Duration(p.intn(150)) * time.Millisecond)
			case r < 30: // duplicate
				chunk = append(chunk, chunk...)
			}
			// Write in pieces: framing must not depend on read boundaries.
			for len(chunk) > 0 {
				k := 1 + p.intn(len(chunk))
				if _, werr := dst.Write(chunk[:k]); werr != nil {
					return
				}
				chunk = chunk[k:]
			}
		}
		if err != nil {
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				continue
			}
			return // io.EOF or a failed read
		}
	}
}

// TestChaosClientThroughFaultyLink runs client commands through a link that loses,
// corrupts, splits, duplicates, delays and cuts traffic. Every run ends within its time-out
// with an exit code of the documented set (0, or a transport, time-out or protocol error),
// never a panic or an internal error, and leaves no goroutines behind.
func TestChaosClientThroughFaultyLink(t *testing.T) {
	st := stationtest.Start(t, stationtest.Model)
	before := runtime.NumGoroutine()
	allowed := map[int]bool{errs.ExitOK: true, errs.ExitTransport: true, errs.ExitTimeout: true, errs.ExitProtocol: true, errs.ExitPartial: true}
	ops := [][]string{
		{"interrogate"},
		{"counters"},
		{"read", "--ioa", "100,1000"},
		{"clock", "--time", "now"},
		{"ping", "--count", "2", "--interval", "50"},
		{"command", "--type", "single", "--ioa", "5000", "--value", "on"},
	}
	for seed := range chaosSeeds(t) {
		p := newChaosProxy(t, st.Addr(), uint64(seed)+1)
		for i, op := range ops {
			args := append([]string{"--debug", "client", op[0], "--ip", "127.0.0.1", "--port", p.port(), "--t1", "2", "--t2", "1", "--timeout", "1500", "--format", "json"}, op[1:]...)
			begin := time.Now()
			r := runUnbounded(t, args...)
			if !allowed[r.code] {
				t.Fatalf("seed %d, %v: exit %d\nstdout:\n%s\nstderr:\n%s", seed, op, r.code, r.stdout, r.stderr)
			}
			if d := time.Since(begin); d > 20*time.Second {
				t.Errorf("seed %d, op %d: took %s, exit %d\nstderr:\n%s", seed, i, d, r.code, tail(r.stderr, 3000))
			}
		}
		p.close()
	}
	eventually(t, "the goroutines of the runs to end", func() bool { return runtime.NumGoroutine() <= before+8 })
}

// tail returns the last n bytes of s.
func tail(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[len(s)-n:]
}

// runUnbounded runs iec104ctl as the process does, with a context that has no deadline
// (go-iec60870-5 applies the request timeout only to such a context), and fails the test
// when a run outlives a minute.
func runUnbounded(t *testing.T, args ...string) result {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	watchdog := time.AfterFunc(time.Minute, func() {
		t.Errorf("iec104ctl %v still running after a minute", args)
		cancel()
	})
	defer watchdog.Stop()
	var stdout, stderr syncBuffer
	code := Run(ctx, args, &stdout, &stderr)
	return result{stdout.String(), stderr.String(), code}
}
