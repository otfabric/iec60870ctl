// SPDX-License-Identifier: MIT

package iec

import (
	"context"
	"io"
	"time"

	"github.com/otfabric/go-iec60870-5/iec101"
	"github.com/otfabric/go-iec60870-5/link"
)

// rawLink is a link station of its own on an open line, for the link procedures that the
// controlling station of go-iec60870-5 does not expose: request status of link (function
// 9) and the test function for link (function 2, balanced). It sends no user data.
type rawLink struct {
	pri *link.Primary
	bal *link.Balanced
}

// discardUser is the user of a balanced raw link: it takes the peer's user data and
// drops it, so that the peer's primary direction does not stall.
type discardUser struct{}

func (discardUser) UserData([]byte) bool { return true }
func (discardUser) Reset(bool)           {}

// openRaw starts a raw link station on rw with the link parameters of l. Every frame is
// waited for timeout and not repeated: a probe asks once. trace, when not nil, receives
// the frames.
func openRaw(rw io.ReadWriter, l Link, timeout time.Duration, trace *Tracer) (*rawLink, error) {
	cfg := l.Params.Link
	cfg.Timeout = timeout
	cfg.Retries = -1
	if trace != nil {
		trace.Attach(&cfg)
	}
	if l.Params.Mode == iec101.ModeBalanced {
		b, err := link.NewBalanced(rw, cfg, link.BalancedConfig{Addr: l.Remote, Remote: l.Addr, DIR: true}, discardUser{})
		if err != nil {
			return nil, err
		}
		return &rawLink{bal: b}, nil
	}
	p, err := link.NewPrimary(rw, cfg)
	if err != nil {
		return nil, err
	}
	return &rawLink{pri: p}, nil
}

// status sends request status of link to addr (unbalanced) or to the peer (balanced).
func (r *rawLink) status(ctx context.Context, addr uint16) (link.Status, error) {
	if r.bal != nil {
		return r.bal.RequestStatus(ctx)
	}
	return r.pri.RequestStatus(ctx, addr)
}

// done is closed when the station stops, for example when the line closes.
func (r *rawLink) done() <-chan struct{} {
	if r.bal != nil {
		return r.bal.Done()
	}
	return r.pri.Done()
}

// Close stops the station and closes the line.
func (r *rawLink) Close() error {
	if r.bal != nil {
		return r.bal.Close()
	}
	return r.pri.Close()
}
