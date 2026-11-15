// SPDX-License-Identifier: MIT

package cmd

import (
	"context"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/otfabric/go-iec60870-5/iec103"
	"github.com/otfabric/iec60870ctl/internal/iec103ctl/stationtest"
)

// genericWrite reports whether a frame carries an ASDU 10 with cause 40 (generic write),
// FUN 254 and inf.
func genericWrite(frame []byte, inf iec103.InfoNumber) bool {
	for i := 0; i+6 <= len(frame); i++ {
		if frame[i] == 10 && frame[i+1] == 0x81 && frame[i+2] == byte(iec103.CauseGenericWrite) &&
			frame[i+4] == byte(iec103.FunGeneric) && frame[i+5] == byte(inf) {
			return true
		}
	}
	return false
}

// dropProxy forwards the lines of a controlling system to st, except the first frame that
// carries a generic write with drop: that one the device never sees, so the controlling
// system has no link confirmation until its link layer repeats the frame after
// --link-timeout. dropped is called when it was held back. The proxy counts the frames
// with a write abort (INF 251) it forwarded.
func dropProxy(t *testing.T, st *stationtest.Station, drop iec103.InfoNumber, dropped func()) (*stationtest.Station, *atomic.Int32) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	var aborts atomic.Int32
	var once sync.Once
	var wg sync.WaitGroup
	wg.Go(func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			d, err := net.Dial("tcp", st.Addr)
			if err != nil {
				_ = c.Close()
				t.Error(err)
				return
			}
			end := func() { _ = c.Close(); _ = d.Close() }
			wg.Go(func() { defer end(); _, _ = io.Copy(c, d) })
			wg.Go(func() {
				defer end()
				buf := make([]byte, 512)
				for {
					n, err := c.Read(buf)
					if err != nil {
						return
					}
					held := false
					if genericWrite(buf[:n], drop) {
						once.Do(func() { held = true })
					}
					if held {
						dropped()
						continue
					}
					if genericWrite(buf[:n], iec103.InfWriteAbort) {
						aborts.Add(1)
					}
					if _, err := d.Write(buf[:n]); err != nil {
						return
					}
				}
			})
		}
	})
	t.Cleanup(func() {
		_ = ln.Close()
		wg.Wait()
	})
	return &stationtest.Station{Addr: ln.Addr().String()}, &aborts
}

func abortWrite(t *testing.T, ctx context.Context, px *stationtest.Station, timeout, linkTimeout string) result {
	t.Helper()
	return runCtx(t, ctx, "client", "generic", "write", "--url", px.URL(), "--timeout", timeout, "--link-timeout", linkTimeout,
		"--retries", "3", "--poll-interval", "10", "--gin", "1.2", "--type", "float32", "--value", "3", "--mode", "abort")
}

// An interrupt while the write entry (INF 248) waits for its link confirmation still sends
// the write abort (INF 251), and the error says the write was aborted.
func TestGenericWriteAbortAfterAnInterrupt(t *testing.T) {
	st := stationtest.Start(t, stationtest.Model)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	px, aborts := dropProxy(t, st, iec103.InfWriteEntry, cancel)
	r := abortWrite(t, ctx, px, "5000", "300").want(t, 130)
	if aborts.Load() == 0 {
		t.Fatalf("no write abort (INF 251) after the interrupt\nstderr:\n%s", r.stderr)
	}
	r.errHas(t, "interrupted", "the write was aborted (INF 251 acknowledged)")
}

// A write entry whose link confirmation comes after --timeout is still aborted: INF 251
// waits for the link layer on a --timeout of its own.
func TestGenericWriteAbortAfterATimeout(t *testing.T) {
	st := stationtest.Start(t, stationtest.Model)
	// The repetition of the write entry comes 1200 ms after it, past --timeout; the abort
	// after it is within its own --timeout.
	px, aborts := dropProxy(t, st, iec103.InfWriteEntry, func() {})
	r := abortWrite(t, context.Background(), px, "1000", "1200").want(t, 5)
	if aborts.Load() == 0 {
		t.Fatalf("no write abort (INF 251) after the timeout\nstderr:\n%s", r.stderr)
	}
	r.errHas(t, "timeout", "the write was aborted (INF 251 acknowledged)")
}

// A write abort that is not answered within --timeout is sent once more, and the error
// says that the write may still be staged.
func TestGenericWriteAbortSentAgain(t *testing.T) {
	st := stationtest.Start(t, stationtest.Model)
	// The first abort is held back; the link layer repeats it after 1200 ms, past --timeout.
	px, aborts := dropProxy(t, st, iec103.InfWriteAbort, func() {})
	r := abortWrite(t, context.Background(), px, "1000", "1200").want(t, 5)
	if aborts.Load() == 0 {
		t.Fatalf("the write abort (INF 251) never reached the device\nstderr:\n%s", r.stderr)
	}
	r.errHas(t, "timeout", "the write may still be staged on the device")
}
