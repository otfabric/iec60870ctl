// SPDX-License-Identifier: MIT

package station

// The simulated device against a hostile controlling system: noise, frames with wrong
// checksums, truncated and oversized frames, a flood of polls, ASDUs of random content and
// lines closed in the middle of a frame. After each, an honest controlling system on the
// next line must find the device as it was. Seeded: CHAOS_SEEDS seeds (make chaos), 3
// otherwise.

import (
	"context"
	"math/rand/v2"
	"net"
	"os"
	"runtime"
	"strconv"
	"testing"
	"time"

	"github.com/otfabric/go-iec60870-5/ft12"
	"github.com/otfabric/go-iec60870-5/iec103"
	"github.com/otfabric/go-iec60870-5/iec103/client"
	"github.com/otfabric/go-iec60870-5/link"
	"github.com/otfabric/iec60870ctl/internal/iec103ctl/config"
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
			t.Errorf("%d goroutines before, %d after:\n%s", baseline, n, buf)
		}
	}
}

var ftp = ft12.Params{AddrSize: 1}

func primary(fn byte, fcb, fcv bool) byte { return byte(link.PrimaryControl(fn, fcb, fcv, false)) }

// attack writes what a hostile master sends, drawn from rnd, then closes the line.
func attack(t *testing.T, addr string, rnd *rand.Rand) {
	c, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.Close() }()
	go func() { // drain what the device answers
		buf := make([]byte, 512)
		for {
			if _, err := c.Read(buf); err != nil {
				return
			}
		}
	}()
	write := func(b []byte) { _ = c.SetWriteDeadline(time.Now().Add(time.Second)); _, _ = c.Write(b) }
	frame := func(f ft12.Frame) []byte { b, _ := f.Encode(ftp); return b }
	for range 30 + rnd.IntN(100) {
		switch rnd.IntN(8) {
		case 0: // noise
			b := make([]byte, 1+rnd.IntN(300))
			for i := range b {
				b[i] = byte(rnd.IntN(256))
			}
			write(b)
		case 1: // wrong checksum
			b := frame(ft12.Frame{Kind: ft12.KindFixed, Control: primary(0, false, false), Addr: 1})
			b[len(b)-2]++
			write(b)
		case 2: // truncated variable frame
			a, _ := iec103.New(9, 1, 255, 0, iec103.GIInit{SCN: 1}).Encode()
			b := frame(ft12.Frame{Kind: ft12.KindVariable, Control: primary(3, true, true), Addr: 1, Data: a})
			write(b[:1+rnd.IntN(len(b)-1)])
		case 3: // oversized length field
			write([]byte{0x68, 0xFF, 0xFF, 0x68, primary(3, false, true), 1})
		case 4: // flood of polls
			for range 50 {
				write(frame(ft12.Frame{Kind: ft12.KindFixed, Control: primary(byte(10+rnd.IntN(2)), rnd.IntN(2) == 0, true), Addr: 1}))
			}
		case 5: // random ASDU as user data
			d := make([]byte, 1+rnd.IntN(200))
			for i := range d {
				d[i] = byte(rnd.IntN(256))
			}
			write(frame(ft12.Frame{Kind: ft12.KindVariable, Control: primary(3, rnd.IntN(2) == 0, true), Addr: 1, Data: d}))
		case 6: // a reset and a valid request
			write(frame(ft12.Frame{Kind: ft12.KindFixed, Control: primary(0, false, false), Addr: 1}))
			a, _ := iec103.New(24, 1, 160, 0, iec103.DisturbanceOrder{TOO: iec103.TOO(rnd.IntN(100)), FAN: uint16(rnd.IntN(10))}).Encode()
			write(frame(ft12.Frame{Kind: ft12.KindVariable, Control: primary(3, rnd.IntN(2) == 0, true), Addr: 1, Data: a}))
		default: // another link address, broadcast
			write(frame(ft12.Frame{Kind: ft12.KindFixed, Control: primary(9, false, false), Addr: uint16(rnd.IntN(256))}))
		}
	}
}

func TestHostileMaster(t *testing.T) {
	for seed := range chaosSeeds(t) {
		t.Run(strconv.Itoa(seed), func(t *testing.T) {
			defer leakCheck(t)()
			cfg := &config.ServerConfig{Listen: "127.0.0.1", Port: freePort(t)}
			ctx, cancel := context.WithCancel(context.Background())
			addr := make(chan string, 1)
			done := make(chan error, 1)
			go func() { done <- Run(ctx, cfg, nil, nil, func(a string) { addr <- a }) }()
			a := <-addr
			rnd := rand.New(rand.NewPCG(uint64(seed), 103))
			for range 3 {
				attack(t, a, rnd)
			}
			// An honest controlling system finds the device as it was.
			var cl *client.Client
			var err error
			deadline := time.Now().Add(5 * time.Second)
			for time.Now().Before(deadline) {
				c, derr := net.Dial("tcp", a)
				if derr != nil {
					t.Fatal(derr)
				}
				cl, err = client.New(c, client.Config{LinkAddr: 1, NoInitGI: true, NoInitTimeSync: true, Link: link.Config{Timeout: 200 * time.Millisecond}, RetryInterval: 50 * time.Millisecond})
				if err != nil {
					t.Fatal(err)
				}
				wctx, wcancel := context.WithTimeout(ctx, time.Second)
				err = cl.WaitUp(wctx)
				wcancel()
				if err == nil {
					break
				}
				_ = cl.Close()
			}
			if err != nil {
				t.Fatal(err)
			}
			gctx, gcancel := context.WithTimeout(ctx, 5*time.Second)
			gi, err := cl.Interrogate(gctx)
			gcancel()
			if err != nil || len(gi) != 6 {
				t.Fatalf("%v %d", err, len(gi))
			}
			_ = cl.Close()
			cancel()
			if err := <-done; err != nil {
				t.Fatal(err)
			}
		})
	}
}
