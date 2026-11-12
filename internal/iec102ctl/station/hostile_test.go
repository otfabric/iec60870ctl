// SPDX-License-Identifier: MIT

package station

// The simulated meter against a hostile controlling station: noise, frames with wrong
// checksums, truncated and oversized frames, undecodable and oversized ASDUs, a flood of
// requests that are never polled, and a line that closes in the middle of a frame. The
// station must not panic, must serve a controlling station that behaves afterwards, stop
// within a bound and leave no goroutine behind.

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
	"github.com/otfabric/go-iec60870-5/iec102"
	"github.com/otfabric/go-iec60870-5/iec102/client"
)

var p2 = ft12.Params{AddrSize: 2}

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

func frame(t *testing.T, f ft12.Frame) []byte {
	t.Helper()
	b, err := f.Encode(p2)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// hostileOctets returns what a hostile master sends, as decided by rng.
func hostileOctets(t *testing.T, rng *rand.Rand) []byte {
	t.Helper()
	switch rng.IntN(7) {
	case 0: // noise
		b := make([]byte, 1+rng.IntN(300))
		for i := range b {
			b[i] = byte(rng.IntN(256))
		}
		return b
	case 1: // a frame with a wrong checksum
		b := frame(t, ft12.Frame{Kind: ft12.KindFixed, Control: 0x49, Addr: 1})
		b[len(b)-2] ^= 0x55
		return b
	case 2: // a truncated frame
		b := frame(t, ft12.Frame{Kind: ft12.KindVariable, Control: 0x73, Addr: 1, Data: []byte{103, 0, 5, 1, 0, 0}})
		return b[:rng.IntN(len(b))]
	case 3: // an undecodable ASDU as user data
		return frame(t, ft12.Frame{Kind: ft12.KindVariable, Control: 0x73, Addr: 1, Data: []byte{byte(rng.IntN(256)), 0xFF}})
	case 4: // a length that promises more than comes
		return []byte{0x68, 0xF0, 0xF0, 0x68, 0x73, 0x01, 0x00}
	case 5: // a read of every record, never polled
		data, _ := iec102.New(6, 1, byte(rng.IntN(256)), iec102.Read{T: iec102.C_CI_NI_2}).Encode(iec102.Params{})
		return frame(t, ft12.Frame{Kind: ft12.KindVariable, Control: 0x53 | byte(rng.IntN(2))<<5, Addr: 1, Data: data})
	default: // link functions in any order, to any address
		return frame(t, ft12.Frame{Kind: ft12.KindFixed, Control: 0x40 | byte(rng.IntN(16)) | byte(rng.IntN(4))<<4, Addr: uint16(rng.IntN(3))})
	}
}

func TestHostileMaster(t *testing.T) {
	m, err := Parse([]byte(DemoYAML))
	if err != nil {
		t.Fatal(err)
	}
	st, err := New(m, Options{})
	if err != nil {
		t.Fatal(err)
	}
	for seed := range chaosSeeds(t) {
		t.Run("seed"+strconv.Itoa(seed), func(t *testing.T) {
			before := runtime.NumGoroutine()
			rng := rand.New(rand.NewPCG(uint64(seed), 7))
			a, b := net.Pipe()
			ctx, cancel := context.WithCancel(context.Background())
			done := make(chan struct{})
			go func() { defer close(done); st.Serve(ctx, a, "hostile") }()
			// Read whatever the station answers, so that it never blocks on the pipe.
			drained := make(chan struct{})
			go func() {
				defer close(drained)
				buf := make([]byte, 512)
				for {
					_ = b.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
					if _, err := b.Read(buf); err != nil {
						if ne, ok := err.(net.Error); ok && ne.Timeout() {
							select {
							case <-ctx.Done():
								return
							default:
								continue
							}
						}
						return
					}
				}
			}()
			for range 40 {
				_ = b.SetWriteDeadline(time.Now().Add(time.Second))
				if _, err := b.Write(hostileOctets(t, rng)); err != nil {
					break
				}
			}
			cancel()
			_ = b.Close()
			select {
			case <-done:
			case <-time.After(3 * time.Second):
				t.Fatal("the station did not stop within 3 s")
			}
			<-drained
			// The station serves a controlling station that behaves on the next line.
			c := serveClient(t, st)
			if _, err := c.ReadProductSpec(context.Background()); err != nil {
				t.Fatalf("after the hostile master: %v", err)
			}
			_ = c.Close()
			deadline := time.Now().Add(3 * time.Second)
			for runtime.NumGoroutine() > before+2 && time.Now().Before(deadline) {
				time.Sleep(10 * time.Millisecond)
			}
			if n := runtime.NumGoroutine(); n > before+2 {
				t.Errorf("%d goroutines left, %d before", n, before)
			}
		})
	}
}

// serveClient serves st on a fresh pipe and returns a client of it; closing the client ends
// the line.
func serveClient(t *testing.T, st *Station) *client.Client {
	t.Helper()
	a, b := net.Pipe()
	go st.Serve(context.Background(), a, "pipe")
	c, err := client.New(b, client.Config{LinkAddr: 1, CommonAddr: 1, RequestTimeout: 5 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	return c
}
