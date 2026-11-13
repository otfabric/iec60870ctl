// SPDX-License-Identifier: MIT

package iec

import (
	"context"
	"io"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/otfabric/go-iec60870-5/ft12"
	"github.com/otfabric/go-iec60870-5/link"
	"github.com/otfabric/iec60870ctl/internal/errs"
	"github.com/otfabric/iec60870ctl/internal/iec101ctl/config"
	"github.com/otfabric/iec60870ctl/internal/iec101ctl/types"
)

// refused returns a connection configuration of a port nothing listens on.
func refused(t *testing.T) config.ConnConfig {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := portOf(ln.Addr())
	_ = ln.Close()
	return config.ConnConfig{LinkConfig: config.DefaultLink(), IP: "127.0.0.1", Port: port, Timeout: 500}
}

// Every command reports a line that cannot be opened as a transport error, and an
// interrupted one as interrupted.
func TestEveryCommandOnARefusedLine(t *testing.T) {
	c := refused(t)
	st := config.StationConfig{ConnConfig: c, CommonAddr: 1}
	ctx := context.Background()
	calls := map[string]func(context.Context) error{
		"interrogate": func(ctx context.Context) error {
			_, err := Interrogate(ctx, &config.InterrogateConfig{StationConfig: st}, nil)
			return err
		},
		"counters": func(ctx context.Context) error {
			_, err := Counters(ctx, &config.CountersConfig{StationConfig: st, Freeze: "read"}, nil)
			return err
		},
		"read": func(ctx context.Context) error {
			_, err := Read(ctx, &config.ReadConfig{StationConfig: st, IOAs: "1"}, nil)
			return err
		},
		"command": func(ctx context.Context) error {
			_, err := Command(ctx, &config.CommandConfig{StationConfig: st, Type: "single", IOA: 1, Value: "on", Mode: ModeExecute}, nil)
			return err
		},
		"clock": func(ctx context.Context) error {
			_, err := Clock(ctx, &config.ClockConfig{StationConfig: st}, nil)
			return err
		},
		"reset": func(ctx context.Context) error {
			_, err := Reset(ctx, &config.ResetConfig{StationConfig: st, Qualifier: "events"}, nil)
			return err
		},
		"delay": func(ctx context.Context) error {
			_, err := Delay(ctx, &config.DelayConfig{StationConfig: st}, nil)
			return err
		},
		"scan": func(ctx context.Context) error {
			_, err := Scan(ctx, &config.ScanConfig{ConnConfig: c, Method: ScanCA, Start: 1, End: 1}, nil, nil)
			return err
		},
		"broadcast": func(ctx context.Context) error {
			_, err := Scan(ctx, &config.ScanConfig{ConnConfig: c, Method: ScanBroadcast}, nil, nil)
			return err
		},
		"monitor": func(ctx context.Context) error {
			_, err := Monitor(ctx, &config.MonitorConfig{ConnConfig: c}, io.Discard, nil)
			return err
		},
	}
	for name, call := range calls {
		e, ok := errs.As(call(ctx))
		if !ok || e.Code != errs.CodeTransportConnectFailed {
			t.Errorf("%s: %+v", name, e)
		}
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	for name, call := range calls {
		if e, ok := errs.As(call(cancelled)); !ok || e.Kind != errs.KindInterrupted {
			t.Errorf("%s interrupted: %+v", name, e)
		}
	}
	// Invalid parameters that only Check would catch reach the commands as errors too.
	bad := st
	bad.IOASize = 9
	if _, err := Interrogate(ctx, &config.InterrogateConfig{StationConfig: bad}, nil); err == nil {
		t.Error("interrogate with --ioa-size 9")
	}
	if _, err := Read(ctx, &config.ReadConfig{StationConfig: bad}, nil); err == nil {
		t.Error("read with --ioa-size 9")
	}
	if _, err := Command(ctx, &config.CommandConfig{StationConfig: bad}, nil); err == nil {
		t.Error("command with --ioa-size 9")
	}
	if _, err := Monitor(ctx, &config.MonitorConfig{ConnConfig: bad.ConnConfig}, nil, nil); err == nil {
		t.Error("monitor with --ioa-size 9")
	}
	if _, err := Delay(ctx, &config.DelayConfig{StationConfig: st, Set: "x"}, nil); err == nil {
		t.Error("delay --set x")
	}
	if _, err := Scan(ctx, &config.ScanConfig{ConnConfig: bad.ConnConfig, Method: ScanLink}, nil, nil); err == nil {
		t.Error("link scan with --ioa-size 9")
	}
	if _, _, err := openPinger(ctx, &config.PingConfig{StationConfig: bad, Method: PingLinkStatus}, nil); err == nil {
		t.Error("ping with --ioa-size 9")
	}
	if _, err := Discover(ctx, &config.DiscoverConfig{Subnets: []string{"10.0.0.0/8"}}, nil); err == nil {
		t.Error("discover of a /8")
	}
	if _, err := Discover(ctx, &config.DiscoverConfig{Subnets: []string{"10.0.0.1"}}, nil); err == nil {
		t.Error("discover without ports")
	}
	if _, err := Discover(ctx, &config.DiscoverConfig{Subnets: []string{"10.0.0.1"}, Ports: []string{"1"}, LinkAddrSize: 9}, nil); err == nil {
		t.Error("discover with --link-address-size 9")
	}
	if _, err := Discover(cancelled, &config.DiscoverConfig{Subnets: []string{"10.0.0.1"}, Ports: []string{"1"}, LinkAddrSize: 1, Parallel: 1, ForceLarge: true}, nil); err == nil {
		t.Error("an interrupted discover")
	}
}

// A line that closes again at once is opened again by the monitor until its duration ends.
// TestMonitorOpensTheLineAgainAfterCloseDuringStartUp is the regression test of a line
// opened again that keeps closing while its link is being initialized, until Dial gives
// up: that line is lost like any other and is opened once more; it does not end the
// monitor with "the line closed before the link came up". After the first line was lost,
// every line closes on the first frame of the link start-up until the monitor has
// reported the failed reopen.
func TestMonitorOpensTheLineAgainAfterCloseDuringStartUp(t *testing.T) {
	old := reopenDelay
	reopenDelay = 20 * time.Millisecond
	t.Cleanup(func() { reopenDelay = old })
	status := &countLost{}
	var (
		mu    sync.Mutex
		index = map[*rogueConn]int{}
	)
	r := startRogue(t, func(c *rogueConn, f ft12.Frame) ([]byte, error) {
		mu.Lock()
		n, ok := index[c]
		if !ok {
			n = len(index) + 1
			index[c] = n
		}
		mu.Unlock()
		switch {
		case n == 1 && link.Control(f.Control).Func() == link.FuncRequestClass2:
			return nil, io.EOF // the first line closes after the link came up
		case n > 1 && status.lost.Load() < 2:
			return nil, io.EOF // the lines opened again close during the link start-up
		}
		return proper(c, f), nil
	})
	cfg := config.MonitorConfig{ConnConfig: r.conn(200 * time.Millisecond), Duration: 1}
	sum, err := within(t, 5*time.Second, "monitor", func() (MonitorSummary, error) {
		return Monitor(context.Background(), &cfg, status, func(*types.Point) error { return nil })
	})
	mu.Lock()
	lines := len(index)
	mu.Unlock()
	if err != nil || sum.Reopens < 2 || sum.LinkUps < 2 || lines < 3 {
		t.Fatalf("monitor: %+v, %v (%d lines)", sum, err, lines)
	}
}

// countLost is a status writer that counts the "line lost" reports.
type countLost struct{ lost atomic.Int64 }

func (w *countLost) Write(p []byte) (int, error) {
	if strings.Contains(string(p), "line lost") {
		w.lost.Add(1)
	}
	return len(p), nil
}

// TestDialOpensABusyLineAgain: a TCP station that closes a connection during the link
// start-up (a terminal server still serving the previous one) is opened again within the
// timeout; a station that keeps closing ends Dial with a connection error.
func TestDialOpensABusyLineAgain(t *testing.T) {
	var lines atomic.Int64
	var mu sync.Mutex
	seen := map[*rogueConn]bool{}
	r := startRogue(t, func(c *rogueConn, f ft12.Frame) ([]byte, error) {
		mu.Lock()
		if !seen[c] {
			seen[c] = true
			lines.Add(1)
		}
		first := len(seen) == 1
		mu.Unlock()
		if first {
			return nil, io.EOF // busy: the first connection is closed at once
		}
		return proper(c, f), nil
	})
	cfg := r.conn(time.Second)
	s, err := within(t, 5*time.Second, "dial", func() (*Session, error) {
		return Dial(context.Background(), &cfg, nil, Hooks{})
	})
	if err != nil || lines.Load() != 2 {
		t.Fatalf("dial: %v (%d lines)", err, lines.Load())
	}
	_ = s.Close()

	closing := startRogue(t, func(*rogueConn, ft12.Frame) ([]byte, error) { return nil, io.EOF })
	cfg = closing.conn(300 * time.Millisecond)
	_, err = within(t, 5*time.Second, "dial", func() (*Session, error) {
		return Dial(context.Background(), &cfg, nil, Hooks{})
	})
	if e, ok := errs.As(err); !ok || e.Code != errs.CodeConnectionLost {
		t.Fatalf("always closed: %v", err)
	}
}

// TestMonitorOpensTheLineAgainAfterUpBeforeDial is the regression test of a first line
// whose link came up (OnState was told) but that failed before Dial returned: the link
// did come up, so the configuration is right and the line is lost like any other, opened
// again; it does not end the monitor. The status writer holds the "link up" report until
// the first line has ended, so Dial deterministically fails after the link came up.
func TestMonitorOpensTheLineAgainAfterUpBeforeDial(t *testing.T) {
	old := reopenDelay
	reopenDelay = 20 * time.Millisecond
	t.Cleanup(func() { reopenDelay = old })
	sum, err := monitorUpBeforeDial(t, false)
	if err != nil || sum.Reopens < 1 || sum.LinkUps < 2 {
		t.Fatalf("monitor: %+v, %v", sum, err)
	}
	// With --no-reconnect the same first line ends the monitor with an error.
	if _, err := monitorUpBeforeDial(t, true); err == nil {
		t.Fatal("--no-reconnect: no error")
	}
}

// monitorUpBeforeDial runs Monitor against a proper station whose first line ends while
// the "link up" report is held, so Dial fails after the link came up.
func monitorUpBeforeDial(t *testing.T, noReconnect bool) (MonitorSummary, error) {
	t.Helper()
	firstClosed := make(chan struct{})
	var once sync.Once
	r := startRogueClosed(t, func(c *rogueConn, f ft12.Frame) ([]byte, error) {
		return proper(c, f), nil
	}, func(*rogueConn) { once.Do(func() { close(firstClosed) }) })
	status := &holdFirstUp{release: firstClosed}
	// The timeout ends the first Dial while its "link up" report is held; it is long
	// enough for the link to come up first on a loaded machine.
	cfg := config.MonitorConfig{ConnConfig: r.conn(time.Second), Duration: 2, NoReconnect: noReconnect}
	return within(t, 5*time.Second, "monitor", func() (MonitorSummary, error) {
		return Monitor(context.Background(), &cfg, status, func(*types.Point) error { return nil })
	})
}

// holdFirstUp is a status writer that holds the first "link up" report until release is
// closed.
type holdFirstUp struct {
	release <-chan struct{}
	held    atomic.Bool
}

func (w *holdFirstUp) Write(p []byte) (int, error) {
	if strings.Contains(string(p), "link up") && w.held.CompareAndSwap(false, true) {
		<-w.release
	}
	return len(p), nil
}

func TestMonitorOpensTheLineAgain(t *testing.T) {
	old := reopenDelay
	reopenDelay = 20 * time.Millisecond
	t.Cleanup(func() { reopenDelay = old })
	var ups atomic.Int64
	r := startRogue(t, func(c *rogueConn, f ft12.Frame) ([]byte, error) {
		switch link.Control(f.Control).Func() {
		case link.FuncResetLink:
			ups.Add(1)
		case link.FuncRequestClass2:
			if ups.Load() < 3 {
				return nil, io.EOF // the first two lines close after the link came up
			}
		}
		return proper(c, f), nil
	})
	cfg := config.MonitorConfig{ConnConfig: r.conn(time.Second), Duration: 2, Interrogate: true}
	sum, err := within(t, 5*time.Second, "monitor", func() (MonitorSummary, error) {
		return Monitor(context.Background(), &cfg, io.Discard, func(*types.Point) error { return nil })
	})
	// Three lines came up. The second loss may be met inside Dial, which opens a TCP line
	// that closes before it returns again itself (busy line), so the monitor itself opens
	// the line again at least once.
	if err != nil || sum.Reopens < 1 || sum.LinkUps < 3 || ups.Load() < 3 {
		t.Fatalf("monitor: %+v, %v", sum, err)
	}
	if s := sum.String(); s == "" {
		t.Error("summary")
	}
	// An emit that fails ends the monitor with an output error.
	r2 := startRogue(t, func(c *rogueConn, f ft12.Frame) ([]byte, error) {
		if fn := link.Control(f.Control).Func(); fn == link.FuncRequestClass1 || fn == link.FuncRequestClass2 {
			return secondary(link.FuncUserData, false, encodeASDU(endOfInit())), nil
		}
		return proper(c, f), nil
	})
	cfg = config.MonitorConfig{ConnConfig: r2.conn(time.Second)}
	_, err = within(t, 5*time.Second, "monitor", func() (MonitorSummary, error) {
		return Monitor(context.Background(), &cfg, io.Discard, func(*types.Point) error { return io.ErrClosedPipe })
	})
	if e, ok := errs.As(err); !ok || e.Code != errs.CodeOutputWriteFailed {
		t.Fatalf("emit failure: %v", err)
	}
}
