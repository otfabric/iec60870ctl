// SPDX-License-Identifier: MIT

package cmd

import (
	"errors"
	"io"
	"strings"
	"sync"
	"testing"

	serial "github.com/otfabric/go-serial"
	"github.com/otfabric/iec60870ctl/internal/line"
)

// port is a serial port that reads err at once, or, with err nil, waits in Read until it
// is closed; what is written to it is taken.
type port struct {
	err    error
	once   sync.Once
	closed chan struct{}
}

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

// `server start --serial` exits with 4 (transport) when the port fails while it serves, and
// with 0 when it served until --duration ended.
func TestServerStartSerialExitCode(t *testing.T) {
	old := line.OpenSerial
	t.Cleanup(func() { line.OpenSerial = old })
	for _, c := range []struct {
		name string
		err  error
		code int
		want string
	}{
		{"the port fails", errors.New("the port is gone"), 4, `"code": "connection_lost"`},
		{"--duration ends", nil, 0, `"event":"stopping"`},
	} {
		t.Run(c.name, func(t *testing.T) {
			p := &port{err: c.err, closed: make(chan struct{})}
			line.OpenSerial = func(*serial.Config) (io.ReadWriteCloser, error) { return p, nil }
			r := run(t, "server", "start", "--serial", "/dev/ttyTEST", "--duration", "1", "--format", "json").want(t, c.code)
			if !strings.Contains(r.stdout, c.want) {
				t.Errorf("stdout %q, want %q", r.stdout, c.want)
			}
		})
	}
}
