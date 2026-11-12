// SPDX-License-Identifier: MIT

// Package line is the transport of the FT 1.2 tools (iec101ctl, iec102ctl, iec103ctl): the
// byte stream of IEC 60870-5-1 FT 1.2 on a serial port, or over one TCP connection to a
// terminal server (serial device server) that passes the line through unchanged, as the
// interop images of otfabric/iec60870-5-interop carry it (port 2401).
package line

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	serial "github.com/otfabric/go-serial"
)

// DefaultPort is the TCP port of a line carried over TCP, the one of the interop images of
// otfabric/iec60870-5-interop. A terminal server may use any other.
const DefaultPort = 2401

// URL schemes accepted by --url.
const (
	SchemeTCP    = "tcp"
	SchemeSerial = "serial"
)

// Target is a resolved line: a TCP address or a serial device.
type Target struct {
	Host   string
	Port   uint16
	Device string
}

// Serial reports whether t is a serial port.
func (t Target) Serial() bool { return t.Device != "" }

// Addr returns host:port of a TCP target, or the device of a serial one.
func (t Target) Addr() string {
	if t.Serial() {
		return t.Device
	}
	return net.JoinHostPort(t.Host, strconv.Itoa(int(t.Port)))
}

// String returns the target as a URL, the form used in output ("tcp://host:2401",
// "serial:///dev/ttyUSB0", "serial://COM3").
func (t Target) String() string {
	if t.Serial() {
		return SchemeSerial + "://" + t.Device
	}
	return SchemeTCP + "://" + t.Addr()
}

// ParseURL parses tcp://host[:port] or serial:///dev/ttyUSB0 (serial://COM3 on Windows). A
// missing port is 2401.
func ParseURL(raw string) (Target, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return Target{}, fmt.Errorf("invalid URL %q: %w", raw, err)
	}
	switch strings.ToLower(u.Scheme) {
	case SchemeTCP:
	case SchemeSerial:
		if u.RawQuery != "" || u.Fragment != "" || u.User != nil || u.Port() != "" {
			return Target{}, fmt.Errorf("invalid URL %q: a serial URL names the device only (serial:///dev/ttyUSB0); set the line with --baud, --data-bits, --parity and --stop-bits", raw)
		}
		dev := u.Host + u.Path
		if dev == "" || dev == "/" {
			return Target{}, fmt.Errorf("invalid URL %q: missing device (use serial:///dev/ttyUSB0 or serial://COM3)", raw)
		}
		return Target{Device: dev}, nil
	case "":
		return Target{}, fmt.Errorf("invalid URL %q: missing scheme (use tcp://host:port or serial:///dev/ttyUSB0)", raw)
	default:
		return Target{}, fmt.Errorf("invalid URL %q: unsupported scheme %q (use tcp or serial)", raw, u.Scheme)
	}
	if u.Path != "" && u.Path != "/" || u.RawQuery != "" || u.Fragment != "" || u.User != nil {
		return Target{}, fmt.Errorf("invalid URL %q: only scheme, host and port are allowed", raw)
	}
	t := Target{Host: u.Hostname(), Port: DefaultPort}
	if t.Host == "" {
		return Target{}, fmt.Errorf("invalid URL %q: missing host", raw)
	}
	if p := u.Port(); p != "" {
		n, err := strconv.ParseUint(p, 10, 16)
		if err != nil || n == 0 {
			return Target{}, fmt.Errorf("invalid URL %q: port must be 1-65535", raw)
		}
		t.Port = uint16(n)
	}
	return t, nil
}

// Resolve returns the line a client command uses: --url, or --ip with --port (2401 when
// 0). The two are mutually exclusive and one is required; what names the station is the
// noun of the error ("station", "meter", "device").
func Resolve(rawURL, ip string, port uint16, what string) (Target, error) {
	rawURL, ip = strings.TrimSpace(rawURL), strings.TrimSpace(ip)
	switch {
	case rawURL != "" && ip != "":
		return Target{}, errors.New("--url and --ip are mutually exclusive")
	case rawURL != "":
		return ParseURL(rawURL)
	case ip != "":
		if port == 0 {
			port = DefaultPort
		}
		return Target{Host: ip, Port: port}, nil
	default:
		return Target{}, fmt.Errorf("no %s given: set --url (tcp://host:port or serial:///dev/ttyUSB0) or --ip", what)
	}
}

// Settings are the character format of a serial line. Zero values select FT 1.2's: 9600
// baud, 8 data bits, even parity, 1 stop bit (IEC 60870-5-1 6.2.4.2). On a TCP target they
// do not apply: a terminal server sets its own line.
type Settings struct {
	Baud     uint32
	DataBits uint8
	Parity   string
	StopBits uint8
}

// ParityValues lists the values of --parity.
func ParityValues() []string { return []string{"even", "odd", "none"} }

// SerialConfig returns the go-serial configuration of device with the line settings s.
func SerialConfig(device string, s Settings) (serial.Config, error) {
	out := serial.Config{Address: device, BaudRate: serial.BaudRate(s.Baud), DataBits: serial.DataBits(s.DataBits), StopBits: serial.StopBits(s.StopBits)}
	switch strings.ToLower(strings.TrimSpace(s.Parity)) {
	case "", "even", "e":
		out.Parity = serial.ParityEven
	case "odd", "o":
		out.Parity = serial.ParityOdd
	case "none", "n":
		out.Parity = serial.ParityNone
	default:
		return out, fmt.Errorf("invalid --parity %q: use even, odd or none", s.Parity)
	}
	if err := out.Validate(); err != nil {
		return out, fmt.Errorf("serial line: %w", err)
	}
	return out, nil
}

// CheckSettings validates s without opening anything.
func CheckSettings(s Settings) error {
	_, err := SerialConfig("check", s)
	return err
}

// OpenSerial opens a serial port; tests replace it.
var OpenSerial = func(c *serial.Config) (io.ReadWriteCloser, error) { return serial.Open(c) }

// Dial opens the line of t: a TCP connection within timeout, or the serial port with s.
func Dial(ctx context.Context, t Target, s Settings, timeout time.Duration) (io.ReadWriteCloser, error) {
	if t.Serial() {
		sc, err := SerialConfig(t.Device, s)
		if err != nil {
			return nil, err
		}
		p, err := OpenSerial(&sc)
		if err != nil {
			return nil, fmt.Errorf("open %s: %w", t.Device, err)
		}
		return p, nil
	}
	d := net.Dialer{Timeout: timeout}
	return d.DialContext(ctx, "tcp", t.Addr())
}

// Conn is one line a server serves: its stream and the name of the peer.
type Conn struct {
	io.ReadWriteCloser
	Remote string
}

// BusyGrace is how long a connection that arrives while another is served waits for that
// one to end before it is refused. A controlling station that closes its connection and
// opens the next one at once (one command after another) must not find the line busy
// while the station is still tearing the first one down.
var BusyGrace = 500 * time.Millisecond

// Serve accepts connections on l and runs handle for one at a time, as a serial line has
// one controlling station: a connection that arrives while one is served waits up to
// BusyGrace for it to end, and is then closed and reported to busy (which may be nil). It
// returns when ctx ends or l fails, after the connection being served has ended; it closes
// l.
func Serve(ctx context.Context, l net.Listener, handle func(ctx context.Context, c Conn), busy func(remote string)) error {
	// free holds one token while no connection is served.
	free := make(chan struct{}, 1)
	free <- struct{}{}
	var wg sync.WaitGroup
	stop := context.AfterFunc(ctx, func() { _ = l.Close() })
	defer stop()
	defer wg.Wait()
	defer func() { _ = l.Close() }()
	for {
		nc, err := l.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			remote := nc.RemoteAddr().String()
			wait := time.NewTimer(BusyGrace)
			defer wait.Stop()
			select {
			case <-free:
			case <-wait.C:
				_ = nc.Close()
				if busy != nil {
					busy(remote)
				}
				return
			case <-ctx.Done():
				_ = nc.Close()
				return
			}
			defer func() { free <- struct{}{} }()
			defer func() { _ = nc.Close() }()
			cctx, cancel := context.WithCancel(ctx)
			defer cancel()
			closeOnEnd := context.AfterFunc(cctx, func() { _ = nc.Close() })
			defer closeOnEnd()
			handle(cctx, Conn{ReadWriteCloser: nc, Remote: remote})
		}()
	}
}
