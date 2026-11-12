// SPDX-License-Identifier: MIT

package iec

import (
	"context"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/otfabric/go-iec60870-5/iec103"
	"github.com/otfabric/iec60870ctl/internal/errs"
	"github.com/otfabric/iec60870ctl/internal/iec103ctl/config"
	"github.com/otfabric/iec60870ctl/internal/iec103ctl/types"
)

// ReconnectDelay is the pause before `client monitor` opens a lost line again.
var ReconnectDelay = time.Second

// CheckMonitor validates `client monitor`.
func CheckMonitor(cfg *config.MonitorConfig) error {
	_, err := CheckConn(&cfg.ConnConfig)
	return err
}

// Monitor polls the equipment and hands every ASDU it sends to emit, until ctx ends,
// --duration passed or --count messages were emitted. A lost line is opened again unless
// --no-reconnect is set; then it ends with a connection error. State changes of the link
// go to stderr.
func Monitor(ctx context.Context, cfg *config.MonitorConfig, stderr io.Writer, emit func(*types.Message) error) (sum types.MonitorSummary, err error) {
	if t, err := CheckConn(&cfg.ConnConfig); err == nil {
		sum.Target = t.String()
	}
	start := time.Now()
	defer func() { sum.DurationMs = since(start) }()
	if cfg.Duration != 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(cfg.Duration)*time.Second)
		defer cancel()
	}
	var errMu sync.Mutex
	logf := func(format string, args ...any) {
		errMu.Lock()
		defer errMu.Unlock()
		_, _ = fmt.Fprintf(stderr, format+"\n", args...)
	}
	msgs := make(chan types.Message, 256)
	for attempt := 0; ; attempt++ {
		done := make(chan struct{})
		handler := func(a *iec103.ASDU) {
			m := MessageOf(a)
			m.Received = time.Now().Format(time.RFC3339Nano)
			select {
			case msgs <- m:
			case <-done:
			}
		}
		onState := func(st iec103.State, err error) {
			if err != nil {
				logf("link %s: %v", st, err)
			} else {
				logf("link %s", st)
			}
		}
		s, err := open(ctx, &cfg.ConnConfig, sessionOptions{handler: handler, onState: onState, trace: traceOf(cfg.Debug, stderr)})
		if err != nil {
			close(done)
			if attempt == 0 {
				return sum, err
			}
			if ctx.Err() != nil {
				return sum, nil
			}
			logf("reconnect failed: %v", err)
			if !pause(ctx) {
				return sum, nil
			}
			continue
		}
		var wg sync.WaitGroup
		sctx, scancel := context.WithCancel(ctx)
		if cfg.Interrogate {
			wg.Go(func() {
				if _, err := s.c.Interrogate(sctx); err != nil && sctx.Err() == nil {
					logf("general interrogation: %v", WrapError(err))
				}
			})
		}
		end, err := s.stream(ctx, msgs, emit, cfg.Count, &sum)
		scancel()
		close(done)
		s.close()
		wg.Wait()
		switch {
		case err != nil:
			return sum, err
		case end:
			return sum, nil
		case cfg.NoReconnect:
			return sum, errs.New(errs.KindTransport, errs.CodeConnectionLost, fmt.Sprintf("connection error: the line %s closed", s.target), nil)
		}
		sum.Reconnects++
		logf("line %s lost, connecting again", s.target)
		if !pause(ctx) {
			return sum, nil
		}
	}
}

// pause waits ReconnectDelay; it reports false when ctx ended first.
func pause(ctx context.Context) bool {
	t := time.NewTimer(ReconnectDelay)
	defer t.Stop()
	select {
	case <-t.C:
		return true
	case <-ctx.Done():
		return false
	}
}

// stream emits the messages of the session until ctx ends or count is reached (end true),
// or the line is lost (end false).
func (s *session) stream(ctx context.Context, msgs chan types.Message, emit func(*types.Message) error, count uint32, sum *types.MonitorSummary) (bool, error) {
	for {
		select {
		case m := <-msgs:
			if err := emit(&m); err != nil {
				return true, errs.Output(errs.CodeOutputWriteFailed, err)
			}
			sum.Messages++
			if count != 0 && int64(sum.Messages) >= int64(count) {
				return true, nil
			}
		case <-s.lost:
			return false, nil
		case <-ctx.Done():
			return true, nil
		}
	}
}
