// SPDX-License-Identifier: MIT

// Package iec is what the iec103ctl client commands do: it opens the line to an item of
// protection equipment with go-iec60870-5's controlling system (iec103/client), runs one
// application function of IEC 60870-5-103 and renders the result. Every error it returns
// is a typed CLI error (WrapError).
package iec

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/otfabric/go-iec60870-5/iec103"
	"github.com/otfabric/go-iec60870-5/iec103/client"
	"github.com/otfabric/go-iec60870-5/link"
	"github.com/otfabric/iec60870ctl/internal/errs"
	"github.com/otfabric/iec60870ctl/internal/iec103ctl/config"
	"github.com/otfabric/iec60870ctl/internal/iec103ctl/linecfg"
	"github.com/otfabric/iec60870ctl/internal/line"
)

// DefaultTimeout bounds connecting, the link start-up and each request when --timeout is 0.
const DefaultTimeout = 10 * time.Second

// MaxRetries is the largest --retries.
const MaxRetries = 10

// Timeout returns the duration of a millisecond flag, def when it is 0.
func Timeout(ms uint32, def time.Duration) time.Duration {
	if ms == 0 {
		return def
	}
	return time.Duration(ms) * time.Millisecond
}

// CheckConn validates what every client command needs before anything is opened, and
// returns the line it names.
func CheckConn(cfg *config.ConnConfig) (line.Target, error) {
	t, err := line.Resolve(cfg.URL, cfg.IP, cfg.Port, "device")
	if err != nil {
		return t, errs.InvalidInput(errs.CodeInvalidURL, err.Error(), err)
	}
	if cfg.LinkAddress >= client.BroadcastAddr {
		return t, fmt.Errorf("--link-address %d: use 0-254 (255 is the broadcast address)", cfg.LinkAddress)
	}
	if cfg.CommonAddress > 255 {
		return t, fmt.Errorf("--common-address %d: use 0-255", cfg.CommonAddress)
	}
	if cfg.Retries < 0 || cfg.Retries > MaxRetries {
		return t, fmt.Errorf("--retries %d: use 0-%d", cfg.Retries, MaxRetries)
	}
	if _, err := linecfg.Location(cfg.TimeZone); err != nil {
		return t, err
	}
	if err := linecfg.CheckSerial(cfg.SerialConfig); err != nil {
		return t, err
	}
	return t, nil
}

// session is one open line with a controlling system on it whose link is up.
type session struct {
	c      *client.Client
	target line.Target
	cfg    *config.ConnConfig
	// lost is closed when the line itself failed (the transport is gone) or the
	// controlling system was closed: the client's Done.
	lost <-chan struct{}
}

// sessionOptions are what a command adds to the controlling system.
type sessionOptions struct {
	handler func(*iec103.ASDU)
	onState func(iec103.State, error)
	trace   io.Writer
	// rw replaces dialling the target (client scan shares one line).
	rw io.ReadWriter
}

// dialLine opens the line of cfg.
func dialLine(ctx context.Context, cfg *config.ConnConfig, t line.Target) (io.ReadWriteCloser, error) {
	rw, err := line.Dial(ctx, t, linecfg.LineSettings(cfg.SerialConfig), Timeout(cfg.Timeout, DefaultTimeout))
	if err != nil {
		if ctx.Err() != nil {
			return nil, WrapError(ctx.Err())
		}
		return nil, errs.New(errs.KindTransport, errs.CodeTransportConnectFailed, fmt.Sprintf("connection error: %s: %v", t, err), err)
	}
	return rw, nil
}

// linkConfig returns the link layer configuration of cfg, with the frame trace.
func linkConfig(cfg *config.ConnConfig, trace io.Writer) link.Config {
	lc := link.Config{Timeout: Timeout(cfg.LinkTimeout, link.DefaultTimeout), Retries: cfg.Retries}
	if cfg.Retries == 0 {
		lc.Retries = -1
	}
	if trace != nil {
		lc.OnFrame, lc.OnDiscard = linecfg.Tracer(trace)
	}
	return lc
}

// busyRetry is the pause before a line that closed during the link start-up is opened
// again: a terminal server (or iec103ctl server start) closes a connection that arrives
// while it still serves the previous one, which a command run right after another meets.
const busyRetry = 50 * time.Millisecond

// open dials the line of cfg, starts the controlling system and waits for the link
// start-up (clause 7.4.1). The automatic time synchronization and general interrogation
// after the start-up are off: a command does only what it is asked. A TCP line that
// closes before the link came up is opened again until the timeout: nothing but the link
// start-up was sent on it.
func open(ctx context.Context, cfg *config.ConnConfig, opts sessionOptions) (*session, error) {
	t, err := CheckConn(cfg)
	if err != nil {
		return nil, errs.WrapValidation(err)
	}
	timeout := Timeout(cfg.Timeout, DefaultTimeout)
	wctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	for attempt := 1; ; attempt++ {
		s, err := openOnce(wctx, cfg, t, opts)
		if err == nil || opts.rw != nil || t.Serial() || !errors.Is(err, errLineClosed) {
			if err != nil && ctx.Err() != nil {
				return nil, WrapError(ctx.Err())
			}
			return s, err
		}
		pause := time.NewTimer(time.Duration(attempt) * busyRetry)
		select {
		case <-wctx.Done():
			pause.Stop()
			return nil, err
		case <-pause.C:
		}
	}
}

// errLineClosed reports a line that closed during the link start-up.
var errLineClosed = errors.New("the line closed during the link start-up")

func openOnce(ctx context.Context, cfg *config.ConnConfig, t line.Target, opts sessionOptions) (*session, error) {
	rw := opts.rw
	if rw == nil {
		conn, err := dialLine(ctx, cfg, t)
		if err != nil {
			return nil, err
		}
		rw = conn
	}
	loc, _ := linecfg.Location(cfg.TimeZone)
	s := &session{target: t, cfg: cfg}
	ccfg := client.Config{
		Link:           linkConfig(cfg, opts.trace),
		LinkAddr:       uint8(cfg.LinkAddress & 0xFF),
		CommonAddr:     uint8(cfg.CommonAddress & 0xFF),
		ResetFCB:       cfg.ResetFCB,
		NoInitTimeSync: true,
		NoInitGI:       true,
		PollInterval:   Timeout(cfg.PollInterval, client.DefaultPollInterval),
		RequestTimeout: Timeout(cfg.Timeout, DefaultTimeout),
		Location:       loc,
		Handler:        opts.handler,
		OnState:        opts.onState,
	}
	c, err := client.New(rw, ccfg)
	if err != nil {
		if cl, ok := rw.(io.Closer); ok {
			_ = cl.Close()
		}
		return nil, WrapError(err)
	}
	s.c, s.lost = c, c.Done()
	if err := s.waitUp(ctx); err != nil {
		_ = c.Close()
		return nil, err
	}
	return s, nil
}

// waitUp waits for the link start-up, or reports why it did not come. go-iec60870-5
// (v0.5.0) returns from WaitUp with the transport error when the line fails first.
func (s *session) waitUp(ctx context.Context) error {
	err := s.c.WaitUp(ctx)
	switch {
	case err == nil:
		return nil
	case errors.Is(err, context.DeadlineExceeded):
		return errs.New(errs.KindTimeout, errs.CodeLinkTimeout,
			fmt.Sprintf("timeout: the device at link address %d on %s did not answer the link start-up", s.cfg.LinkAddress, s.target), err)
	case s.c.Err() != nil:
		return errs.New(errs.KindTransport, errs.CodeConnectionLost,
			fmt.Sprintf("connection error: the line %s closed during the link start-up", s.target), fmt.Errorf("%w: %w", errLineClosed, err))
	}
	return WrapError(err)
}

// close stops the controlling system and closes the line.
func (s *session) close() { _ = s.c.Close() }

// commonAddr is the common address the requests carry.
func (s *session) commonAddr() uint8 { return s.c.CommonAddr() }

// resetCause is the cause of the identification the link start-up makes the equipment
// send: reset CU (4), or reset FCB (3) with --reset-fcb (clause 7.4.1, table 8).
func (s *session) resetCause() iec103.Cause {
	if s.cfg.ResetFCB {
		return iec103.CauseResetFCB
	}
	return iec103.CauseResetCU
}

// pollInterval is how often a wait for an identification looks again.
const pollInterval = 10 * time.Millisecond

// waitIdentification waits for the identification with the given cause, or for any
// identification when cause is 0, until ctx ends; it returns nil then.
func (s *session) waitIdentification(ctx context.Context, cause iec103.Cause) *iec103.ASDU {
	get := func() *iec103.ASDU {
		if cause == 0 {
			return s.c.Identification()
		}
		return s.c.IdentificationFor(cause)
	}
	tick := time.NewTicker(pollInterval)
	defer tick.Stop()
	for {
		if a := get(); a != nil {
			return a
		}
		select {
		case <-ctx.Done():
			return get()
		case <-s.lost:
			return get()
		case <-tick.C:
		}
	}
}

// request runs fn with a context bounded by the request timeout, and ends it early when
// the line is lost.
func (s *session) request(ctx context.Context, d time.Duration, fn func(context.Context) error) error {
	rctx, cancel := context.WithTimeout(ctx, d)
	defer cancel()
	go func() {
		select {
		case <-s.lost:
			cancel()
		case <-rctx.Done():
		}
	}()
	err := fn(rctx)
	select {
	case <-s.lost:
		if err != nil && ctx.Err() == nil {
			return errs.New(errs.KindTransport, errs.CodeConnectionLost, fmt.Sprintf("connection error: the line %s closed", s.target), err)
		}
	default:
	}
	return WrapError(err)
}

// since returns the milliseconds since start.
func since(start time.Time) float64 {
	return float64(time.Since(start).Microseconds()) / 1000
}
