// SPDX-License-Identifier: MIT

package line

import (
	"context"
	"errors"
	"io"
	"net"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	serial "github.com/otfabric/go-serial"
)

func TestParseURL(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{"tcp://10.0.0.1", "tcp://10.0.0.1:2401"},
		{"TCP://host:4001", "tcp://host:4001"},
		{" tcp://[::1]:7 ", "tcp://[::1]:7"},
		{"tcp://host/", "tcp://host:2401"},
		{"serial:///dev/ttyUSB0", "serial:///dev/ttyUSB0"},
		{"serial://COM3", "serial://COM3"},
	} {
		got, err := ParseURL(c.in)
		if err != nil || got.String() != c.want {
			t.Errorf("ParseURL(%q) = %v, %v; want %s", c.in, got, err, c.want)
		}
	}
	for _, in := range []string{"", "host:1", "udp://h", "tcp://", "tcp://h:0", "tcp://h:70000", "tcp://h/x",
		"tcp://u@h", "tcp://h?x=1", "serial://", "serial:///", "serial:///dev/tty?baud=1", "serial://h:1", "%zz"} {
		if got, err := ParseURL(in); err == nil {
			t.Errorf("ParseURL(%q) = %v, want an error", in, got)
		}
	}
	tg, _ := ParseURL("serial:///dev/ttyS1")
	if !tg.Serial() || tg.Addr() != "/dev/ttyS1" {
		t.Errorf("serial target %+v", tg)
	}
}

func TestResolve(t *testing.T) {
	if _, err := Resolve("tcp://h", "h", 0, "station"); err == nil || !strings.Contains(err.Error(), "mutually exclusive") {
		t.Errorf("both: %v", err)
	}
	if _, err := Resolve("", "", 0, "meter"); err == nil || !strings.Contains(err.Error(), "no meter given") {
		t.Errorf("none: %v", err)
	}
	if tg, err := Resolve("", "10.0.0.2", 0, "x"); err != nil || tg.String() != "tcp://10.0.0.2:2401" {
		t.Errorf("ip: %v %v", tg, err)
	}
	if tg, err := Resolve("", "h", 9, "x"); err != nil || tg.Port != 9 {
		t.Errorf("ip port: %v %v", tg, err)
	}
	if tg, err := Resolve("serial://COM1", "", 0, "x"); err != nil || tg.Device != "COM1" {
		t.Errorf("url: %v %v", tg, err)
	}
}

func TestSerialConfig(t *testing.T) {
	sc, err := SerialConfig("/dev/x", Settings{})
	if err != nil || sc.Parity != serial.ParityEven {
		t.Fatalf("defaults: %+v %v", sc, err)
	}
	for p, want := range map[string]serial.Parity{"E": serial.ParityEven, "odd": serial.ParityOdd, "o": serial.ParityOdd, "none": serial.ParityNone, "N": serial.ParityNone} {
		sc, err := SerialConfig("/dev/x", Settings{Parity: p})
		if err != nil || sc.Parity != want {
			t.Errorf("parity %q: %v %v", p, sc.Parity, err)
		}
	}
	if err := CheckSettings(Settings{Parity: "mark"}); err == nil {
		t.Error("parity mark accepted")
	}
	if err := CheckSettings(Settings{DataBits: 9}); err == nil {
		t.Error("9 data bits accepted")
	}
	if err := CheckSettings(Settings{Baud: 19200, DataBits: 7, StopBits: 2, Parity: "none"}); err != nil {
		t.Error(err)
	}
	if len(ParityValues()) != 3 {
		t.Error(ParityValues())
	}
}

type fakePort struct{ io.ReadWriter }

func (fakePort) Close() error { return nil }

func TestDial(t *testing.T) {
	old := OpenSerial
	t.Cleanup(func() { OpenSerial = old })
	var got serial.Config
	OpenSerial = func(c *serial.Config) (io.ReadWriteCloser, error) {
		got = *c
		if c.Address == "/dev/bad" {
			return nil, errors.New("no such device")
		}
		return fakePort{}, nil
	}
	ctx := context.Background()
	if _, err := Dial(ctx, Target{Device: "/dev/ok"}, Settings{Baud: 1200}, time.Second); err != nil || got.BaudRate != 1200 {
		t.Errorf("serial: %v %+v", err, got)
	}
	if _, err := Dial(ctx, Target{Device: "/dev/bad"}, Settings{}, time.Second); err == nil || !strings.Contains(err.Error(), "/dev/bad") {
		t.Errorf("bad device: %v", err)
	}
	if _, err := Dial(ctx, Target{Device: "/dev/ok"}, Settings{Parity: "x"}, time.Second); err == nil {
		t.Error("bad parity accepted")
	}

	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = l.Close() }()
	addr := l.Addr().(*net.TCPAddr)
	c, err := Dial(ctx, Target{Host: "127.0.0.1", Port: uint16(addr.Port)}, Settings{}, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	_ = c.Close()
}

func TestServeOneAtATime(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	var served, busy atomic.Int32
	started := make(chan struct{}, 4)
	done := make(chan error, 1)
	go func() {
		done <- Serve(ctx, l, func(ctx context.Context, c Conn) {
			served.Add(1)
			if c.Remote == "" {
				t.Error("no remote")
			}
			started <- struct{}{}
			<-ctx.Done()
		}, func(string) { busy.Add(1) })
	}()
	first, err := net.Dial("tcp", l.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = first.Close() }()
	<-started
	second, err := net.Dial("tcp", l.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	_ = second.SetReadDeadline(time.Now().Add(2 * time.Second))
	if _, err := second.Read(make([]byte, 1)); err == nil {
		t.Error("second connection was not closed")
	}
	_ = second.Close()
	cancel()
	if err := <-done; err != nil {
		t.Errorf("Serve: %v", err)
	}
	if served.Load() != 1 || busy.Load() != 1 {
		t.Errorf("served %d, busy %d", served.Load(), busy.Load())
	}
}

func TestServeAfterTheFirstEnds(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var served atomic.Int32
	go func() {
		_ = Serve(ctx, l, func(ctx context.Context, c Conn) { served.Add(1) }, nil)
	}()
	for i := 0; i < 3; i++ {
		c, err := net.Dial("tcp", l.Addr().String())
		if err != nil {
			t.Fatal(err)
		}
		_ = c.SetReadDeadline(time.Now().Add(2 * time.Second))
		_, _ = c.Read(make([]byte, 1)) // the handler returned: the connection is closed
		_ = c.Close()
		deadline := time.Now().Add(2 * time.Second)
		for served.Load() != int32(i+1) && time.Now().Before(deadline) {
			time.Sleep(5 * time.Millisecond)
		}
	}
	if served.Load() != 3 {
		t.Errorf("served %d", served.Load())
	}
}

func TestServeListenerFails(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	_ = l.Close()
	if err := Serve(context.Background(), l, func(context.Context, Conn) {}, nil); err == nil {
		t.Error("no error from a closed listener")
	}
}

func FuzzParseURL(f *testing.F) {
	for _, s := range []string{"tcp://h", "tcp://h:2401", "serial:///dev/ttyUSB0", "serial://COM3", "tcp://[::1]:1", "", "%zz", "serial://h:1"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, raw string) {
		tg, err := ParseURL(raw)
		if err != nil {
			if tg != (Target{}) {
				t.Fatalf("ParseURL(%q): %+v with %v", raw, tg, err)
			}
			return
		}
		if !tg.Serial() && (tg.Host == "" || tg.Port == 0) {
			t.Fatalf("ParseURL(%q): %+v", raw, tg)
		}
	})
}
