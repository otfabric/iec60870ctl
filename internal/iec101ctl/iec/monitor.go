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
	"github.com/otfabric/go-iec60870-5/iec101"
	"github.com/otfabric/iec60870ctl/internal/errs"
	"github.com/otfabric/iec60870ctl/internal/iec101ctl/config"
	"github.com/otfabric/iec60870ctl/internal/iec101ctl/types"
)

// reopenDelay is the pause before a lost line is opened again.
var reopenDelay = time.Second

// MonitorSummary is printed to stderr when `client monitor` ends.
type MonitorSummary struct {
	Target   string
	Values   int
	ByType   map[string]int
	Reopens  int
	LinkUps  int
	Duration time.Duration
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
	if s.LinkUps > 1 {
		out += fmt.Sprintf(", link up %d times", s.LinkUps)
	}
	if s.Reopens > 0 {
		out += fmt.Sprintf(", line opened again %d times", s.Reopens)
	}
	if len(parts) > 0 {
		out += " (" + strings.Join(parts, " ") + ")"
	}
	return out
}

// monitorCA parses --ca of `client monitor`: empty for all stations.
func monitorCA(s string, size int) (ca asdu.CommonAddr, set bool, err error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return asdu.CommonAddr(BroadcastCA(size)), false, nil
	}
	n, perr := strconv.ParseUint(s, 10, 16)
	if perr != nil {
		return 0, false, fmt.Errorf("invalid --ca %q: use a common address 1-%d", s, BroadcastCA(size)-1)
	}
	if err := CheckCommonAddr(uint16(n), size, false); err != nil {
		return 0, false, err
	}
	return asdu.CommonAddr(n), true, nil
}

// CheckMonitor validates the configuration of `client monitor`.
func CheckMonitor(cfg *config.MonitorConfig) error {
	if err := Validate(&cfg.ConnConfig); err != nil {
		return err
	}
	l, _ := LinkParams(cfg.LinkConfig)
	if _, _, err := monitorCA(cfg.CommonAddr, l.Params.ASDU.CommonAddrSize); err != nil {
		return err
	}
	_, err := NewFilter(cfg.Types, cfg.IOAs, MaxIOAFor(l.Params.ASDU.IOASize))
	return err
}

// Monitor opens the line, brings up the link and reports every information object the
// station sends through emit, until ctx ends, the duration has passed or the count is
// reached. Changes of the link state are written to status. Unless reopening is turned
// off, a lost line is opened again and monitoring goes on; the link itself is initialized
// again by the controlling station whenever the station stops answering.
func Monitor(ctx context.Context, cfg *config.MonitorConfig, status io.Writer, emit func(*types.Point) error) (MonitorSummary, error) {
	sum := MonitorSummary{ByType: map[string]int{}}
	l, err := LinkParams(cfg.LinkConfig)
	if err != nil {
		return sum, err
	}
	filter, err := NewFilter(cfg.Types, cfg.IOAs, MaxIOAFor(l.Params.ASDU.IOASize))
	if err != nil {
		return sum, err
	}
	ca, hasCA, err := monitorCA(cfg.CommonAddr, l.Params.ASDU.CommonAddrSize)
	if err != nil {
		return sum, err
	}
	filter.CommonAddr, filter.HasCommonAddr = ca, hasCA

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
		statusMu sync.Mutex
		wg       sync.WaitGroup
	)
	defer func() {
		stop()
		wg.Wait()
	}()
	say := func(format string, args ...any) {
		statusMu.Lock()
		defer statusMu.Unlock()
		_, _ = fmt.Fprintf(status, format+"\n", args...)
	}
	handler := func(a *asdu.ASDU) {
		if !cfg.All && a.Type.InControlDirection() {
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
	}

	begin := time.Now()
	for attempt := 0; ; attempt++ {
		var s *Session
		ready := make(chan struct{})
		onState := func(st iec101.State, err error) {
			switch st {
			case iec101.StateUp:
				mu.Lock()
				sum.LinkUps++
				mu.Unlock()
				say("monitor: link up")
				if cfg.Interrogate {
					// Not from this callback: the request needs the link it reports.
					wg.Go(func() {
						select {
						case <-ready:
						case <-ctx.Done():
							return
						}
						mu.Lock()
						cur := s
						mu.Unlock()
						if _, err := cur.Interrogate(ctx, ca, asdu.QOIStation); err != nil && ctx.Err() == nil {
							say("monitor: general interrogation failed: %v", err)
						}
					})
				}
			case iec101.StateDown:
				if ctx.Err() == nil {
					say("monitor: link down: %v", err)
				}
			case iec101.StateClosed:
			}
		}
		var sess *Session
		sess, err = Dial(ctx, &cfg.ConnConfig, status, Hooks{Handler: handler, OnState: onState})
		if err != nil {
			if ctx.Err() != nil && attempt > 0 {
				break
			}
			sum.Duration = time.Since(begin)
			return sum, err
		}
		mu.Lock()
		s = sess
		mu.Unlock()
		close(ready)
		sum.Target = sess.Target.String()
		var lost bool
		select {
		case <-ctx.Done():
		case <-sess.Done():
			lost = true
		}
		_ = sess.Close()
		if !lost {
			break
		}
		if cfg.NoReconnect {
			sum.Duration = time.Since(begin)
			return sum, errs.New(errs.KindTransport, errs.CodeConnectionLost, fmt.Sprintf("connection error: the line closed: %v", sess.Err()), sess.Err())
		}
		say("monitor: line lost: %v", sess.Err())
		select {
		case <-ctx.Done():
		case <-time.After(reopenDelay):
		}
		if ctx.Err() != nil {
			break
		}
		sum.Reopens++
	}
	sum.Duration = time.Since(begin)
	mu.Lock()
	defer mu.Unlock()
	if emitErr != nil {
		return sum, errs.Output(errs.CodeOutputWriteFailed, emitErr)
	}
	return sum, nil
}
