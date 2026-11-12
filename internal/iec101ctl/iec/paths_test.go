// SPDX-License-Identifier: MIT

package iec

import (
	"context"
	"io"
	"net"
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
	if err != nil || sum.Reopens < 2 || sum.LinkUps < 3 {
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
