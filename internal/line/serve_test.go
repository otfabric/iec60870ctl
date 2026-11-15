// SPDX-License-Identifier: MIT

package line

import (
	"context"
	"errors"
	"net"
	"os"
	"runtime"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

// failingListener fails its first n accepts with err, then accepts as l does.
type failingListener struct {
	net.Listener
	mu  sync.Mutex
	n   int
	err error
}

func (f *failingListener) Accept() (net.Conn, error) {
	f.mu.Lock()
	if f.n > 0 {
		f.n--
		f.mu.Unlock()
		return nil, f.err
	}
	f.mu.Unlock()
	return f.Listener.Accept()
}

// An accept that fails while the listener is open is reported and accepted again: the next
// connection is served, and Serve returns only when the context ends.
func TestServeAcceptsAgainAfterAFailedAccept(t *testing.T) {
	for name, err := range map[string]error{
		"no file descriptor": &net.OpError{Op: "accept", Net: "tcp", Err: os.NewSyscallError("accept", syscall.EMFILE)},
		"any other error":    errors.New("accept: the peer reset the connection"),
	} {
		t.Run(name, func(t *testing.T) {
			tl, lerr := net.Listen("tcp", "127.0.0.1:0")
			if lerr != nil {
				t.Fatal(lerr)
			}
			l := &failingListener{Listener: tl, n: 3, err: err}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			served := make(chan string, 1)
			var failed atomic.Int32
			done := make(chan error, 1)
			go func() {
				done <- Serve(ctx, l, func(_ context.Context, c Conn) { served <- c.Remote }, nil,
					func(got error) {
						if !errors.Is(got, err) {
							t.Errorf("failed reported %v, want %v", got, err)
						}
						failed.Add(1)
					})
			}()
			c, derr := net.Dial("tcp", tl.Addr().String())
			if derr != nil {
				t.Fatal(derr)
			}
			defer func() { _ = c.Close() }()
			select {
			case <-served:
			case err := <-done:
				t.Fatalf("Serve returned after a failed accept: %v", err)
			case <-time.After(10 * time.Second):
				t.Fatal("the connection after the failed accepts was not served")
			}
			if failed.Load() != 3 {
				t.Errorf("failed reported %d times, want 3", failed.Load())
			}
			cancel()
			if err := <-done; err != nil {
				t.Errorf("Serve: %v", err)
			}
		})
	}
}

// Serve also returns when the context ends during the pause after a failed accept.
func TestServeEndsDuringThePauseAfterAFailedAccept(t *testing.T) {
	tl, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	l := &failingListener{Listener: tl, n: 1 << 30, err: errors.New("accept failed")}
	ctx, cancel := context.WithCancel(context.Background())
	failed := make(chan struct{}, 1)
	done := make(chan error, 1)
	go func() {
		done <- Serve(ctx, l, func(context.Context, Conn) {}, nil, func(error) {
			select {
			case failed <- struct{}{}:
			default:
			}
		})
	}()
	<-failed
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("Serve: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Serve did not return when the context ended")
	}
}

// At most MaxWaiting connections wait for the line while one is served; each further one
// is closed at once and reported busy, and the goroutines of Serve stay bounded.
func TestServeBoundsTheConnectionsThatWait(t *testing.T) {
	old := BusyGrace
	BusyGrace = time.Minute // the waiting connections wait for the whole test
	t.Cleanup(func() { BusyGrace = old })

	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	before := runtime.NumGoroutine()
	ctx, cancel := context.WithCancel(context.Background())
	served := make(chan struct{}, 1)
	refused := make(chan string, 64)
	done := make(chan error, 1)
	go func() {
		done <- Serve(ctx, l, func(ctx context.Context, _ Conn) {
			served <- struct{}{}
			<-ctx.Done()
		}, func(remote string) { refused <- remote }, nil)
	}()
	dial := func() net.Conn {
		t.Helper()
		c, err := net.Dial("tcp", l.Addr().String())
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = c.Close() })
		return c
	}
	dial()
	<-served
	for range MaxWaiting {
		dial()
	}
	const extra = 8
	for i := range extra {
		c := dial()
		_ = c.SetReadDeadline(time.Now().Add(10 * time.Second))
		if _, err := c.Read(make([]byte, 1)); err == nil || errors.Is(err, os.ErrDeadlineExceeded) {
			t.Fatalf("connection %d beyond the waiting ones: %v, want closed at once", i+1, err)
		}
		select {
		case <-refused:
		case <-time.After(10 * time.Second):
			t.Fatalf("connection %d beyond the waiting ones was not reported busy", i+1)
		}
	}
	// Serve, the connection served and the ones that wait; a few more for the runtime.
	if n := runtime.NumGoroutine() - before; n > 1+1+MaxWaiting+3 {
		t.Errorf("%d goroutines more than before Serve, want at most %d", n, 1+1+MaxWaiting+3)
	}
	cancel()
	if err := <-done; err != nil {
		t.Errorf("Serve: %v", err)
	}
	if len(refused) != 0 {
		t.Errorf("%d connections refused that waited", len(refused))
	}
}
