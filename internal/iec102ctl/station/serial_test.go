// SPDX-License-Identifier: MIT

package station

import (
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"

	serial "github.com/otfabric/go-serial"
	"github.com/otfabric/iec60870ctl/internal/errs"
	"github.com/otfabric/iec60870ctl/internal/iec102ctl/config"
	"github.com/otfabric/iec60870ctl/internal/line"
)

// port is a serial port that reads err at once, or, with err nil, waits in Read until it
// is closed; what is written to it is taken.
type port struct {
	err    error
	once   sync.Once
	closed chan struct{}
}

func newPort(err error) *port { return &port{err: err, closed: make(chan struct{})} }

func (p *port) Read([]byte) (int, error) {
	if p.err != nil {
		return 0, p.err
	}
	<-p.closed
	return 0, io.ErrClosedPipe
}

func (p *port) Write(b []byte) (int, error) { return len(b), nil }

func (p *port) Close() error {
	p.once.Do(func() { close(p.closed) })
	return nil
}

// serialPort makes the next serial port that is opened p.
func serialPort(t *testing.T, p *port) {
	t.Helper()
	old := line.OpenSerial
	t.Cleanup(func() { line.OpenSerial = old })
	line.OpenSerial = func(*serial.Config) (io.ReadWriteCloser, error) { return p, nil }
}

// A serial port that fails while the station serves it ends Run with a transport error
// (exit code 4) that carries the error of the port.
func TestRunSerialPortFails(t *testing.T) {
	gone := errors.New("the port is gone")
	serialPort(t, newPort(gone))
	cfg := config.ServerDefaults()
	cfg.Serial = "/dev/ttyTEST"
	var ev events
	err := Run(context.Background(), &cfg, ev.log, nil, nil)
	e, ok := errs.As(err)
	if !ok || e.Kind != errs.KindTransport || e.Code != errs.CodeConnectionLost || errs.ExitCodeForKind(e.Kind) != 4 {
		t.Fatalf("Run: %v, want a transport error (connection_lost, exit code 4)", err)
	}
	if !errors.Is(err, gone) || !strings.Contains(err.Error(), "/dev/ttyTEST") {
		t.Errorf("Run: %v, want the error of the port and its name", err)
	}
	if got := ev.names(); got != "listening stopping" {
		t.Errorf("events %q", got)
	}
}

// A serial port served until the context ends, before or while it is served, ends Run
// without an error, also when the port then fails.
func TestRunSerialContextEnds(t *testing.T) {
	cfg := config.ServerDefaults()
	cfg.Serial = "/dev/ttyTEST"
	t.Run("ended before", func(t *testing.T) {
		serialPort(t, newPort(errors.New("the port is gone")))
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if err := Run(ctx, &cfg, nil, nil, nil); err != nil {
			t.Errorf("Run: %v", err)
		}
	})
	t.Run("ended while serving", func(t *testing.T) {
		serialPort(t, newPort(nil))
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan error, 1)
		go func() { done <- Run(ctx, &cfg, nil, nil, func(string) { cancel() }) }()
		if err := <-done; err != nil {
			t.Errorf("Run: %v", err)
		}
	})
}
