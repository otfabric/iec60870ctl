// SPDX-License-Identifier: MIT

package station

import (
	"context"
	"math/rand/v2"
	"net"
	"os"
	"runtime"
	"strconv"
	"testing"
	"time"

	"github.com/otfabric/go-iec60870-5/asdu"
	"github.com/otfabric/go-iec60870-5/ft12"
	"github.com/otfabric/go-iec60870-5/iec101"
	"github.com/otfabric/go-iec60870-5/iec101/server"
	"github.com/otfabric/go-iec60870-5/link"
	"github.com/otfabric/iec60870ctl/internal/iec101ctl/config"
)

// The hostile tests put a primary station that does not follow IEC 60870-5-2 on the other
// end of an in-memory line and write its frames as raw octets. Whatever it sends, the
// station must not panic, must keep serving a primary that behaves afterwards, must stop
// within a bound and must leave no goroutine behind.

var fp = ft12.Params{AddrSize: 1}

func chaosSeeds() int {
	if n, err := strconv.Atoi(os.Getenv("CHAOS_SEEDS")); err == nil && n > 0 {
		return n
	}
	return 4
}

func leakCheck(t testing.TB) {
	baseline := runtime.NumGoroutine()
	t.Cleanup(func() {
		deadline := time.Now().Add(5 * time.Second)
		for runtime.NumGoroutine() > baseline && time.Now().Before(deadline) {
			time.Sleep(10 * time.Millisecond)
		}
		if n := runtime.NumGoroutine(); n > baseline {
			buf := make([]byte, 1<<16)
			t.Errorf("%d goroutines left behind:\n%s", n-baseline, buf[:runtime.Stack(buf, true)])
		}
	})
}

// rawPrimary writes primary frames to a station and reads its answers.
type rawPrimary struct {
	t    *testing.T
	conn net.Conn
	in   chan ft12.Frame
	fcb  bool
}

func newRawPrimary(t *testing.T, mode string) (*Station, *rawPrimary, func()) {
	t.Helper()
	m, err := Parse([]byte(testModel))
	if err != nil {
		t.Fatal(err)
	}
	st, err := New(m, Options{Link: linkOf(t, mode), RetryInterval: 20 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	a, b := net.Pipe()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- st.ServeLine(ctx, b, "rogue") }()
	p := &rawPrimary{t: t, conn: a, in: make(chan ft12.Frame, 1024)}
	go func() {
		defer close(p.in)
		dec := ft12.NewDecoder(a, fp)
		for {
			f, err := dec.Next()
			if err != nil {
				return
			}
			f.Data = append([]byte(nil), f.Data...)
			select {
			case p.in <- f:
			default: // a flood of answers is dropped, not blocked on
			}
		}
	}()
	stop := func() {
		cancel()
		select {
		case err := <-done:
			_ = err // the line ends with the cancel or with the closed pipe
		case <-time.After(2 * time.Second):
			t.Fatal("the station did not stop within 2 s")
		}
		_ = a.Close()
	}
	return st, p, stop
}

func (p *rawPrimary) write(b []byte) {
	_ = p.conn.SetWriteDeadline(time.Now().Add(time.Second))
	_, _ = p.conn.Write(b)
}

func (p *rawPrimary) frame(fn byte, fcv bool, data []byte) {
	f := ft12.Frame{Kind: ft12.KindFixed, Control: byte(link.PrimaryControl(fn, p.fcb, fcv, false)), Addr: 1}
	if data != nil {
		f.Kind, f.Data = ft12.KindVariable, data
	}
	if fcv {
		p.fcb = !p.fcb
	}
	b, err := f.Encode(fp)
	if err != nil {
		p.t.Fatal(err)
	}
	p.write(b)
}

// answer waits for the next frame of the station.
func (p *rawPrimary) answer() (ft12.Frame, bool) {
	select {
	case f, ok := <-p.in:
		return f, ok
	case <-time.After(time.Second):
		return ft12.Frame{}, false
	}
}

func (p *rawPrimary) drain() {
	for {
		select {
		case <-p.in:
		case <-time.After(50 * time.Millisecond):
			return
		}
	}
}

// legitimate checks that the station serves a primary that behaves: link reset and a
// general interrogation polled to its termination.
func (p *rawPrimary) legitimate() {
	p.t.Helper()
	p.drain()
	p.frame(link.FuncRequestStatus, false, nil)
	if _, ok := p.answer(); !ok {
		p.t.Fatal("no status of link")
	}
	p.fcb = true
	p.frame(link.FuncResetLink, false, nil)
	if _, ok := p.answer(); !ok {
		p.t.Fatal("no answer to the reset")
	}
	raw, _ := asdu.New(asdu.CauseActivation, 5, asdu.Interrogation{Qualifier: asdu.QOIStation}).Encode(iec101.DefaultParams().ASDU)
	p.frame(link.FuncUserDataConf, true, raw)
	p.answer()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		p.frame(link.FuncRequestClass1, true, nil)
		f, ok := p.answer()
		if !ok || f.Kind != ft12.KindVariable {
			continue
		}
		if a, err := asdu.Decode(f.Data, iec101.DefaultParams().ASDU); err == nil && a.Type == asdu.C_IC_NA_1 && a.Cause == asdu.CauseActivationTerm {
			return
		}
	}
	p.t.Fatal("no activation termination of the interrogation")
}

// Noise, broken frames and user data before the reset of the link.
func TestHostileMasterGarbage(t *testing.T) {
	leakCheck(t)
	_, p, stop := newRawPrimary(t, config.ModeUnbalanced)
	defer stop()
	p.write([]byte{0x68, 0xFF, 0xFF, 0x68, 0x53, 0x01})                   // a frame that never ends
	p.write([]byte{0x10, 0x49, 0x01, 0x00, 0x16})                         // wrong checksum
	p.write([]byte{0xE5, 0xE5, 0x16, 0x16, 0x00})                         // single characters and stray stop characters
	p.write([]byte{0x68, 0x03, 0x04, 0x68, 0x53, 0x01, 0x01, 0x55, 0x16}) // length mismatch
	time.Sleep(150 * time.Millisecond)                                    // past the frame gap
	p.frame(link.FuncUserDataConf, true, []byte{0x01, 0x01, 0x06, 0x05, 0x00, 0x00, 0x14})
	p.legitimate()
}

// Whatever ASDU octets arrive after the link is reset, the station answers or drops them.
func TestHostileMasterRandomASDUs(t *testing.T) {
	leakCheck(t)
	for _, mode := range []string{config.ModeUnbalanced} {
		_, p, stop := newRawPrimary(t, mode)
		p.legitimate()
		rng := rand.New(rand.NewPCG(7, 101))
		for range 300 {
			b := make([]byte, 1+rng.IntN(40))
			for i := range b {
				b[i] = byte(rng.IntN(256))
			}
			if rng.IntN(2) == 0 && len(b) >= 6 {
				b[4] = 5 // the station's common address, so that the handler sees more
			}
			p.frame(link.FuncUserDataConf, true, b)
			p.frame(link.FuncRequestClass1, true, nil)
		}
		p.legitimate()
		stop()
	}
}

// Every request type of the station with every cause, in quick succession, and a flood of
// polls: the queues stay bounded and the station stays responsive.
func TestHostileMasterRequestFlood(t *testing.T) {
	leakCheck(t)
	st, p, stop := newRawPrimary(t, config.ModeUnbalanced)
	defer stop()
	p.legitimate()
	ap := iec101.DefaultParams().ASDU
	objs := []asdu.InformationObject{
		asdu.Interrogation{Qualifier: asdu.QOIStation}, asdu.CounterInterrogation{Request: asdu.CounterGeneral},
		asdu.Read{IOA: 100}, asdu.ClockSync{Time: asdu.Now()}, asdu.DelayAcquisition{Delay: 100},
		asdu.TestCommand{Counter: 0x55AA}, asdu.ResetProcess{Qualifier: 1}, asdu.SingleCommand{IOA: 1000, Value: true},
		asdu.SingleCommand{IOA: 1000, Value: true, Select: true}, asdu.StepCommand{IOA: 1001, Value: 3},
	}
	for range 5 {
		for _, cause := range []asdu.Cause{asdu.CauseActivation, asdu.CauseDeactivation, asdu.CauseSpontaneous, asdu.CauseRequest, 0} {
			for _, o := range objs {
				raw, err := asdu.New(cause, 5, o).Encode(ap)
				if err != nil {
					continue
				}
				p.frame(link.FuncUserDataConf, true, raw)
			}
		}
	}
	st.mu.Lock()
	cur := st.cur
	st.mu.Unlock()
	if cur == nil || cur.Pending(server.Class1) > 2048 {
		t.Fatalf("pending class 1 data unbounded")
	}
	p.legitimate()
}

// Chaos: per seed, a primary that interleaves proper polls with noise, truncated frames
// and repetitions of its last frame. The station keeps serving.
func TestHostileMasterChaos(t *testing.T) {
	leakCheck(t)
	for seed := range chaosSeeds() {
		_, p, stop := newRawPrimary(t, config.ModeUnbalanced)
		p.legitimate()
		rng := rand.New(rand.NewPCG(uint64(seed), 1))
		for range 100 {
			switch rng.IntN(5) {
			case 0:
				b := make([]byte, rng.IntN(20))
				for i := range b {
					b[i] = byte(rng.IntN(256))
				}
				p.write(b)
			case 1:
				p.write([]byte{0x10, 0x5B, 0x01})
			case 2:
				p.fcb = !p.fcb // a repetition of the last FCV frame
				p.frame(link.FuncRequestClass2, true, nil)
			default:
				p.frame(link.FuncRequestClass1, true, nil)
			}
		}
		time.Sleep(150 * time.Millisecond)
		p.legitimate()
		stop()
	}
}
