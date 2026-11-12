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

// MonitorSummary is printed to stderr when `client monitor` ends.
type MonitorSummary struct {
	Target     string
	Values     int
	ByType     map[string]int
	Reconnects int
	Duration   time.Duration
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
		statusMu sync.Mutex
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
			if emitErr != nil || (cfg.Count != 0 && sum.Values >= int(cfg.Count)) {
				mu.Unlock()
				return
			}
			p.ReceivedAt = now
			sum.Values++
			sum.ByType[p.Type]++
			done := cfg.Count != 0 && sum.Values >= int(cfg.Count)
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
				if started > 1 {
					sum.Reconnects++
				}
				mu.Unlock()
				say("monitor: data transfer started")
			case err != nil && ctx.Err() == nil:
				say("monitor: %s: %v", st, err)
				if cfg.NoReconnect && st == iec104.StateDisconnected {
					select {
					case lost <- err:
					default:
					}
				}
			}
		}),
	}
	if !cfg.NoReconnect {
		opts = append(opts, client.WithReconnect(client.Reconnect{}))
	}
	if cfg.Interrogate {
		opts = append(opts, client.WithStartHandler(func(sctx context.Context, c *client.Client) {
			if _, err := c.Interrogate(sctx, ca, asdu.QOIStation); err != nil && sctx.Err() == nil {
				say("monitor: general interrogation failed: %v", err)
			}
		}))
	}

	s, err := monitorDial(ctx, &cfg.ConnConfig, status, opts...)
	if err != nil {
		// A station that floods may deliver --count values before the connection is fully
		// set up: the handler then stops ctx, and Dial fails only because of that. That is
		// the end the user asked for, not an interruption.
		mu.Lock()
		reached := emitErr != nil || (cfg.Count != 0 && sum.Values >= int(cfg.Count))
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
	_ = s.Close()
	sum.Duration = time.Since(begin)

	mu.Lock()
	defer mu.Unlock()
	if emitErr != nil {
		return sum, errs.Output(errs.CodeOutputWriteFailed, emitErr)
	}
	return sum, fatal
}
