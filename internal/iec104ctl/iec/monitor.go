// SPDX-License-Identifier: MIT

package iec

import (
	"context"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/otfabric/go-iec60870-5/asdu"
	iec104 "github.com/otfabric/go-iec60870-5/iec104"
	"github.com/otfabric/go-iec60870-5/iec104/client"
	"github.com/otfabric/iec60870ctl/internal/errs"
	"github.com/otfabric/iec60870ctl/internal/iec104ctl/config"
	"github.com/otfabric/iec60870ctl/internal/iec104ctl/types"
)

// monitorDial is how Monitor connects; a test replaces it to order the connection after
// the --count values.
var monitorDial = Dial

// monitorReconnect is the backoff of the reconnection after a lost connection; the zero
// value is the library's (1 s, doubling up to 30 s). A test shortens or lengthens it.
var monitorReconnect = client.Reconnect{}

// MonitorSummary is printed to stderr when `client monitor` ends.
type MonitorSummary struct {
	Target     string
	Values     int
	ByType     map[string]int
	Reconnects int
	// Switchovers counts how often another connection of the redundancy group of
	// --standby-url took over data transfer.
	Switchovers int
	Duration    time.Duration
}

func (s MonitorSummary) String() string {
	names := make([]string, 0, len(s.ByType))
	for n := range s.ByType {
		names = append(names, n)
	}
	sort.Strings(names)
	parts := make([]string, 0, len(names))
	for _, n := range names {
		parts = append(parts, fmt.Sprintf("%s×%d", n, s.ByType[n]))
	}
	out := fmt.Sprintf("--- %s: %d values in %s", s.Target, s.Values, s.Duration.Round(time.Millisecond))
	if s.Reconnects > 0 {
		out += fmt.Sprintf(", %d reconnects", s.Reconnects)
	}
	if s.Switchovers > 0 {
		out += fmt.Sprintf(", %d switchovers", s.Switchovers)
	}
	if len(parts) > 0 {
		out += " (" + strings.Join(parts, " ") + ")"
	}
	return out
}

// monitorCA parses --ca of `client monitor`: empty for all stations.
func monitorCA(s string) (ca asdu.CommonAddr, set bool, err error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return asdu.Broadcast, false, nil
	}
	n, perr := strconv.ParseUint(s, 10, 16)
	if perr != nil {
		return 0, false, fmt.Errorf("invalid --ca %q: use a common address 1-65534", s)
	}
	if err := CheckCommonAddr(uint16(n), false); err != nil {
		return 0, false, err
	}
	return asdu.CommonAddr(n), true, nil
}

// countReached reports whether values has reached --count; a count of 0 is no count. The
// comparison is made in 64 bits, so that every count of the uint32 flag holds on a
// platform with a 32-bit int as well.
func countReached(values int, count uint32) bool {
	return count != 0 && int64(values) >= int64(count)
}

// CheckMonitor validates the configuration of `client monitor`.
func CheckMonitor(cfg *config.MonitorConfig) error {
	if err := Validate(&cfg.ConnConfig); err != nil {
		return err
	}
	if _, _, err := monitorCA(cfg.CommonAddr); err != nil {
		return err
	}
	_, err := NewFilter(cfg.Types, cfg.IOAs)
	return err
}

// Monitor connects to a station, starts data transfer and reports every information object
// the station sends through emit, until ctx ends, the duration has passed or the count is
// reached. State changes of the connection are written to status. Unless reconnecting is
// turned off, a lost connection is established again and monitoring goes on.
func Monitor(ctx context.Context, cfg *config.MonitorConfig, status io.Writer, emit func(*types.Point) error) (MonitorSummary, error) {
	sum := MonitorSummary{ByType: map[string]int{}}
	filter, err := NewFilter(cfg.Types, cfg.IOAs)
	if err != nil {
		return sum, err
	}
	ca, hasCA, err := monitorCA(cfg.CommonAddr)
	if err != nil {
		return sum, err
	}
	filter.CommonAddr, filter.HasCommonAddr = ca, hasCA
	if hasCA {
		cfg.CommonAddrHint = uint16(ca)
	}

	// With --standby-url the connections form a redundancy group: the group reconnects
	// every connection, and a switchover is not a reconnect.
	names := map[string]string{} // host:port of each connection of the group → its URL
	if t, terr := cfg.ResolveTarget(); terr == nil {
		standby, _ := cfg.ResolveStandby(t)
		for _, c := range append([]config.Target{t}, standby...) {
			names[c.Addr()] = c.String()
		}
		if len(standby) == 0 {
			names = nil
		}
	}
	grouped := len(names) > 0

	parent := ctx
	ctx, stop := context.WithCancel(ctx)
	defer stop()
	if cfg.Duration != 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(cfg.Duration)*time.Second)
		defer cancel()
	}

	var (
		mu       sync.Mutex
		emitErr  error
		lost     = make(chan error, 1)
		started  int
		switches int // calls of the switch handler with a connection
		// interrogations are the general interrogations of --interrogate on the connections
		// of a group that took over.
		interrogations sync.WaitGroup
		statusMu       sync.Mutex
	)
	say := func(format string, args ...any) {
		statusMu.Lock()
		defer statusMu.Unlock()
		_, _ = fmt.Fprintf(status, format+"\n", args...)
	}
	handler := client.HandlerFunc(func(a *asdu.ASDU) {
		if !cfg.All && (a.Type.InControlDirection() || a.Type.IsFileTransfer()) {
			return
		}
		now := FormatTime(time.Now())
		for _, p := range Points(a) {
			if !filter.Match(p) {
				continue
			}
			mu.Lock()
			if emitErr != nil || countReached(sum.Values, cfg.Count) {
				mu.Unlock()
				return
			}
			p.ReceivedAt = now
			sum.Values++
			sum.ByType[p.Type]++
			done := countReached(sum.Values, cfg.Count)
			if err := emit(&p); err != nil {
				emitErr = err
				done = true
			}
			mu.Unlock()
			if done {
				stop()
				return
			}
		}
	})
	opts := []client.Option{
		client.WithHandler(handler),
		client.WithStateHandler(func(st iec104.State, err error) {
			switch {
			case st == iec104.StateStarted:
				mu.Lock()
				started++
				if started > 1 && !grouped {
					sum.Reconnects++
				}
				mu.Unlock()
				say("monitor: data transfer started")
			case err != nil && ctx.Err() == nil:
				say("monitor: %s: %v", st, err)
				if cfg.NoReconnect && !grouped && st == iec104.StateDisconnected {
					select {
					case lost <- err:
					default:
					}
				}
			}
		}),
	}
	if !cfg.NoReconnect || grouped {
		opts = append(opts, client.WithReconnect(monitorReconnect))
	}
	interrogate := func(ictx context.Context, c *client.Client) {
		if _, err := c.Interrogate(ictx, ca, asdu.QOIStation); err != nil && ictx.Err() == nil {
			say("monitor: general interrogation failed: %v", err)
		}
	}
	if grouped {
		// IEC 60870-5-104 clause 10: one connection of the group has data transfer
		// started; when it is lost, another one takes over.
		opts = append(opts, client.WithSwitchHandler(func(active *client.Client) {
			if ctx.Err() != nil {
				return
			}
			if active == nil {
				say("monitor: switchover: no connection of the redundancy group is available")
				if cfg.NoReconnect {
					select {
					case lost <- fmt.Errorf("%w: no connection of the redundancy group has data transfer started", iec104.ErrConnectionLost):
					default:
					}
				}
				return
			}
			mu.Lock()
			switches++
			first := switches == 1
			if !first {
				sum.Switchovers++
			}
			mu.Unlock()
			if !first {
				say("monitor: switchover: data transfer on %s", names[active.Addr()])
			}
			if cfg.Interrogate {
				// A group starts data transfer itself, without the start handler of a
				// connection: interrogate the one that took over, outside the handler.
				interrogations.Add(1)
				go func() {
					defer interrogations.Done()
					interrogate(ctx, active)
				}()
			}
		}))
	}
	if cfg.Interrogate && !grouped {
		opts = append(opts, client.WithStartHandler(func(sctx context.Context, c *client.Client) { interrogate(sctx, c) }))
	}

	s, err := monitorDial(ctx, &cfg.ConnConfig, status, opts...)
	if err != nil {
		// A station that floods may deliver --count values before the connection is fully
		// set up: the handler then stops ctx, and Dial fails only because of that. That is
		// the end the user asked for, not an interruption.
		mu.Lock()
		reached := emitErr != nil || countReached(sum.Values, cfg.Count)
		eerr := emitErr
		mu.Unlock()
		if !reached || parent.Err() != nil {
			return sum, err
		}
		if t, terr := cfg.ResolveTarget(); terr == nil {
			sum.Target = t.String()
		}
		if eerr != nil {
			return sum, errs.Output(errs.CodeOutputWriteFailed, eerr)
		}
		return sum, nil
	}
	sum.Target = s.Target.String()
	begin := time.Now()
	var fatal error
	select {
	case <-ctx.Done():
	case err := <-lost:
		fatal = WrapError(err)
	}
	stop()
	_ = s.Close()
	interrogations.Wait()
	sum.Duration = time.Since(begin)

	mu.Lock()
	defer mu.Unlock()
	if emitErr != nil {
		return sum, errs.Output(errs.CodeOutputWriteFailed, emitErr)
	}
	return sum, fatal
}
