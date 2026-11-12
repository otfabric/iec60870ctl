// SPDX-License-Identifier: MIT

package iec

import (
	"context"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/otfabric/go-iec60870-5/asdu"
	"github.com/otfabric/go-iec60870-5/ft12"
	"github.com/otfabric/go-iec60870-5/iec101"
	"github.com/otfabric/go-iec60870-5/iec101/client"
	"github.com/otfabric/go-iec60870-5/link"
	"github.com/otfabric/iec60870ctl/internal/errs"
	"github.com/otfabric/iec60870ctl/internal/iec101ctl/config"
	"github.com/otfabric/iec60870ctl/internal/line"
)

// Validate checks everything about cfg that can be checked without opening the line.
func Validate(cfg *config.ConnConfig) error {
	t, err := line.Resolve(cfg.URL, cfg.IP, cfg.Port, "station")
	if err != nil {
		return err
	}
	if t.Serial() {
		if err := line.CheckSettings(Settings(cfg.LineConfig)); err != nil {
			return err
		}
	}
	_, err = LinkParams(cfg.LinkConfig)
	return err
}

// Hooks are what a command adds to the controlling station of a [Session].
type Hooks struct {
	// Handler receives every monitor-direction ASDU that is not a mirror of a request.
	Handler func(a *asdu.ASDU)
	// OnState is told of every change of the link state.
	OnState func(s iec101.State, err error)
}

// Session is a controlling station on an open line, with the link initialized.
type Session struct {
	*client.Client
	Target  line.Target
	Link    Link
	Timeout time.Duration
}

// Params returns the ASDU parameters of the session.
func (s *Session) Params() asdu.Params { return s.Link.Params.ASDU }

// openLine resolves and opens the line of cfg.
func openLine(ctx context.Context, cfg *config.ConnConfig) (line.Target, io.ReadWriteCloser, error) {
	target, err := line.Resolve(cfg.URL, cfg.IP, cfg.Port, "station")
	if err != nil {
		return target, nil, errs.InvalidInput(errs.CodeInvalidURL, err.Error(), err)
	}
	rw, err := line.Dial(ctx, target, Settings(cfg.LineConfig), Timeout(cfg.Timeout))
	if err != nil {
		return target, nil, connectError(err)
	}
	return target, rw, nil
}

// Dial opens the line of cfg, starts a controlling station on it and waits, within the
// timeout of cfg, until the link is initialized. trace receives the frame trace of --debug.
func Dial(ctx context.Context, cfg *config.ConnConfig, trace io.Writer, hooks Hooks) (*Session, error) {
	l, err := LinkParams(cfg.LinkConfig)
	if err != nil {
		return nil, errs.WrapValidation(err)
	}
	target, rw, err := openLine(ctx, cfg)
	if err != nil {
		return nil, err
	}
	timeout := Timeout(cfg.Timeout)
	p := l.Params
	if cfg.Debug && trace != nil {
		NewTracer(trace, p).Attach(&p.Link)
	}
	c, err := client.New(rw, client.Config{
		Params:         p,
		LinkAddr:       l.Addr,
		OwnAddr:        l.Remote,
		PollInterval:   time.Duration(cfg.PollInterval) * time.Millisecond,
		RequestTimeout: timeout,
		Handler:        hooks.Handler,
		OnState:        hooks.OnState,
	})
	if err != nil {
		_ = rw.Close()
		return nil, errs.WrapValidation(err)
	}
	s := &Session{Client: c, Target: target, Link: l, Timeout: timeout}
	wctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	if err := c.WaitUp(wctx); err != nil {
		_ = c.Close()
		return nil, upError(ctx, c, err)
	}
	return s, nil
}

// upError says why the link did not come up.
func upError(ctx context.Context, c *client.Client, err error) error {
	switch {
	case ctx.Err() != nil:
		return WrapError(ctx.Err())
	case c.Err() != nil:
		return errs.New(errs.KindTransport, errs.CodeConnectionLost, fmt.Sprintf("connection error: the line closed before the link came up: %v", c.Err()), c.Err())
	}
	return errs.New(errs.KindTimeout, errs.CodeLinkInitFailed,
		"timeout: the link did not come up: the station did not answer request status of link and reset of remote link (check --link-mode, --link-address and the field sizes)", err)
}

// Tracer prints every frame sent and received, for --debug.
type Tracer struct {
	w      io.Writer
	params asdu.Params
	mu     sync.Mutex
	now    func() time.Time
}

// NewTracer returns a tracer writing to w that decodes ASDUs with the parameters of p.
func NewTracer(w io.Writer, p iec101.Params) *Tracer {
	return &Tracer{w: w, params: p.ASDU, now: time.Now}
}

// Attach makes cfg report its frames and discarded octets to t.
func (t *Tracer) Attach(cfg *link.Config) {
	cfg.OnFrame = t.Frame
	cfg.OnDiscard = t.Discard
}

// Frame traces one frame.
func (t *Tracer) Frame(tx bool, f ft12.Frame) {
	dir := "<-"
	if tx {
		dir = "->"
	}
	text := f.String()
	if f.Kind != ft12.KindSingle {
		text = fmt.Sprintf("%s  %s", link.Control(f.Control), text)
	}
	if f.Kind == ft12.KindVariable {
		if a, err := asdu.Decode(f.Data, t.params); err != nil {
			text += fmt.Sprintf("  undecodable ASDU: %v  [% x]", err, f.Data)
		} else {
			text += fmt.Sprintf("  %s  [% x]", a, f.Data)
		}
	}
	t.Line(dir, text)
}

// Discard traces octets that did not form a frame.
func (t *Tracer) Discard(b []byte, err error) {
	t.Line("!!", fmt.Sprintf("discarded %d octets: %v  [% x]", len(b), err, b))
}

// Line writes one trace line.
func (t *Tracer) Line(dir, text string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	_, _ = fmt.Fprintf(t.w, "debug: %s %s %s\n", t.now().UTC().Format("15:04:05.000"), dir, text)
}
