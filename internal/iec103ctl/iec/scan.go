// SPDX-License-Identifier: MIT

package iec

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/otfabric/go-iec60870-5/iec103/client"
	"github.com/otfabric/iec60870ctl/internal/errs"
	"github.com/otfabric/iec60870ctl/internal/iec103ctl/config"
	"github.com/otfabric/iec60870ctl/internal/iec103ctl/types"
)

// sharedLine lets several controlling systems use one line, one after the other: a single
// reader passes what arrives to the one attached, and every one writes to the line. The
// octets that arrive while none is attached are dropped.
type sharedLine struct {
	rw   io.ReadWriteCloser
	mu   sync.Mutex
	cur  *io.PipeWriter
	err  error
	done chan struct{}
}

func newSharedLine(rw io.ReadWriteCloser) *sharedLine {
	l := &sharedLine{rw: rw, done: make(chan struct{})}
	go l.read()
	return l
}

func (l *sharedLine) read() {
	defer close(l.done)
	buf := make([]byte, 512)
	for {
		n, err := l.rw.Read(buf)
		l.mu.Lock()
		w := l.cur
		l.mu.Unlock()
		if n > 0 && w != nil {
			_, _ = w.Write(buf[:n])
		}
		if err != nil {
			l.mu.Lock()
			l.err = err
			if l.cur != nil {
				_ = l.cur.CloseWithError(err)
			}
			l.mu.Unlock()
			return
		}
	}
}

// attach returns the line for the next controlling system; closing it detaches it.
func (l *sharedLine) attach() io.ReadWriteCloser {
	pr, pw := io.Pipe()
	l.mu.Lock()
	if l.err != nil {
		_ = pw.CloseWithError(l.err)
	}
	l.cur = pw
	l.mu.Unlock()
	return &attached{l: l, pr: pr, pw: pw}
}

// close closes the line and waits for its reader.
func (l *sharedLine) close() {
	_ = l.rw.Close()
	<-l.done
}

type attached struct {
	l  *sharedLine
	pr *io.PipeReader
	pw *io.PipeWriter
}

func (a *attached) Read(b []byte) (int, error)  { return a.pr.Read(b) }
func (a *attached) Write(b []byte) (int, error) { return a.l.rw.Write(b) }

func (a *attached) Close() error {
	a.l.mu.Lock()
	if a.l.cur == a.pw {
		a.l.cur = nil
	}
	a.l.mu.Unlock()
	_ = a.pw.Close()
	return a.pr.Close()
}

// DefaultScanTimeout bounds the link start-up at each address of `client scan`.
const DefaultScanTimeout = time.Second

func checkScan(cfg *config.ScanConfig) error {
	if _, err := CheckConn(&cfg.ConnConfig); err != nil {
		return err
	}
	if cfg.Start >= client.BroadcastAddr || cfg.End >= client.BroadcastAddr {
		return fmt.Errorf("--start and --end are link addresses 0-254")
	}
	if cfg.Start > cfg.End {
		return fmt.Errorf("--start %d is after --end %d", cfg.Start, cfg.End)
	}
	return nil
}

// CheckScan validates `client scan`.
func CheckScan(cfg *config.ScanConfig) error { return checkScan(cfg) }

// Scan tries every link address from --start to --end on one line: the link start-up
// (request status of link, reset), and, where it is answered, the identification the
// reset makes the equipment send, which names its common address and function type.
// Nothing else is sent.
func Scan(ctx context.Context, cfg *config.ScanConfig, stderr io.Writer) (*types.ScanResult, error) {
	if err := checkScan(cfg); err != nil {
		return nil, errs.WrapValidation(err)
	}
	t, _ := CheckConn(&cfg.ConnConfig)
	timeout := Timeout(cfg.Timeout, DefaultScanTimeout)
	dctx, cancel := context.WithTimeout(ctx, Timeout(cfg.Timeout, DefaultTimeout))
	rw, err := dialLine(dctx, &cfg.ConnConfig, t)
	cancel()
	if err != nil {
		return nil, err
	}
	shared := newSharedLine(rw)
	defer shared.close()
	start := time.Now()
	res := &types.ScanResult{Target: t.String(), Start: uint8(cfg.Start & 0xFF), End: uint8(cfg.End & 0xFF), Found: []types.ScanRow{}}
	for addr := cfg.Start; addr <= cfg.End; addr++ {
		c := cfg.ConnConfig
		c.LinkAddress = addr
		if c.Timeout == 0 {
			c.Timeout = uint32(DefaultScanTimeout / time.Millisecond)
		}
		res.Tried++
		s, err := open(ctx, &c, sessionOptions{rw: shared.attach(), trace: traceOf(cfg.Debug, stderr)})
		if err != nil {
			if ctx.Err() != nil {
				return nil, WrapError(ctx.Err())
			}
			if e, ok := errs.As(err); ok && e.Kind == errs.KindTimeout {
				continue
			}
			return nil, err
		}
		row := types.ScanRow{LinkAddress: uint8(addr & 0xFF)}
		wctx, wcancel := context.WithTimeout(ctx, timeout)
		a := s.waitIdentification(wctx, s.resetCause())
		wcancel()
		if a != nil {
			if id, ok := IdentificationOf(a); ok {
				row.Identified, row.CA, row.Fun, row.FunName = true, id.CA, id.Fun, id.FunName
				row.COL, row.Manufacturer, row.Software = id.COL, id.Manufacturer, id.Software
			}
		}
		lost := false
		select {
		case <-s.lost:
			lost = true
		default:
		}
		s.close()
		if lost {
			return nil, errs.New(errs.KindTransport, errs.CodeConnectionLost, fmt.Sprintf("connection error: the line %s closed", t), errors.New("line closed"))
		}
		res.Found = append(res.Found, row)
	}
	res.DurationMs = since(start)
	return res, nil
}
