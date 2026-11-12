// SPDX-License-Identifier: MIT

package iec

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	iec104 "github.com/otfabric/go-iec104"
	"github.com/otfabric/go-iec104/apci"
	"github.com/otfabric/go-iec104/asdu"
	"github.com/otfabric/go-iec104/client"
	"github.com/otfabric/go-iec104/sec"
	"github.com/otfabric/iec104ctl/internal/config"
	"github.com/otfabric/iec104ctl/internal/errs"
)

// DefaultTimeout bounds connecting and each request when --timeout is 0.
const DefaultTimeout = 10 * time.Second

// Timeout returns the request timeout of cfg.
func Timeout(ms uint32) time.Duration {
	if ms == 0 {
		return DefaultTimeout
	}
	return time.Duration(ms) * time.Millisecond
}

// LinkParams returns the APCI parameters of cfg: the standard values where cfg has zero.
func LinkParams(cfg config.LinkConfig) (apci.Params, error) {
	p := apci.Params{
		K:  int(cfg.K),
		W:  int(cfg.W),
		T0: time.Duration(cfg.T0) * time.Second,
		T1: time.Duration(cfg.T1) * time.Second,
		T2: time.Duration(cfg.T2) * time.Second,
	}.WithDefaults()
	p.T3 = apci.DefaultParams().T3
	if cfg.T3 != 0 {
		p.T3 = time.Duration(cfg.T3) * time.Second
	}
	if err := p.Validate(); err != nil {
		return p, fmt.Errorf("protocol parameters: %w", err)
	}
	return p, nil
}

// ASDUParams returns the ASDU field sizes and the time zone of cfg.
func ASDUParams(cfg config.LinkConfig) (asdu.Params, error) {
	p := asdu.IEC104
	if cfg.CauseSize != 0 {
		p.CauseSize = int(cfg.CauseSize)
	}
	if cfg.CASize != 0 {
		p.CommonAddrSize = int(cfg.CASize)
	}
	if cfg.IOASize != 0 {
		p.IOASize = int(cfg.IOASize)
	}
	if err := p.Validate(); err != nil {
		return p, fmt.Errorf("ASDU field sizes: %w", err)
	}
	loc, err := Location(cfg.TimeZone)
	if err != nil {
		return p, err
	}
	return p.In(loc), nil
}

// Location resolves --timezone: utc (default), local or an IANA zone name.
func Location(name string) (*time.Location, error) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "", "utc":
		return time.UTC, nil
	case "local":
		return time.Local, nil
	}
	loc, err := time.LoadLocation(strings.TrimSpace(name))
	if err != nil {
		return nil, fmt.Errorf("time zone %q: use utc, local or an IANA name such as Europe/Brussels", name)
	}
	return loc, nil
}

// ClientTLS builds the TLS configuration of a client connection.
func ClientTLS(cfg config.TLSConfig) (*tls.Config, error) {
	out := &tls.Config{
		MinVersion:         tls.VersionTLS12,
		ServerName:         cfg.ServerName,
		InsecureSkipVerify: cfg.Insecure, //nolint:gosec // explicit --tls-insecure, for test setups
	}
	if cfg.CACert != "" {
		pool, err := certPool(cfg.CACert)
		if err != nil {
			return nil, err
		}
		out.RootCAs = pool
	}
	if (cfg.Cert == "") != (cfg.Key == "") {
		return nil, fmt.Errorf("--tls-cert and --tls-key must be given together")
	}
	if cfg.Cert != "" {
		pair, err := tls.LoadX509KeyPair(cfg.Cert, cfg.Key)
		if err != nil {
			return nil, fmt.Errorf("loading the certificate and key: %w", err)
		}
		out.Certificates = []tls.Certificate{pair}
	}
	return out, nil
}

func certPool(path string) (*x509.CertPool, error) {
	pem, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return nil, fmt.Errorf("reading CA certificates: %w", err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(pem) {
		return nil, fmt.Errorf("no certificates found in %s", path)
	}
	return pool, nil
}

// Session is a connection to a station, prepared from the configuration of a command.
type Session struct {
	*client.Client
	Target  config.Target
	Params  asdu.Params
	Timeout time.Duration
	// Security is the endpoint of the application-layer security; nil without --secure.
	Security *sec.Endpoint
	events   *secEvents
}

// Validate checks everything about cfg that can be checked without connecting.
func Validate(cfg *config.ConnConfig) error {
	t, err := cfg.ResolveTarget()
	if err != nil {
		return err
	}
	if _, err := LinkParams(cfg.LinkConfig); err != nil {
		return err
	}
	if _, err := ASDUParams(cfg.LinkConfig); err != nil {
		return err
	}
	if t.TLS {
		if _, err := ClientTLS(cfg.TLSConfig); err != nil {
			return err
		}
	}
	return validateSecurity(cfg)
}

// validateSecurity checks the security flags and that an endpoint can be made of them.
func validateSecurity(cfg *config.ConnConfig) error {
	s, err := ClientSecurity(cfg.ClientSecurityConfig)
	if s == nil || err != nil {
		return err
	}
	ap, err := ASDUParams(cfg.LinkConfig)
	if err != nil {
		return err
	}
	_, err = clientEndpoint(s, ap, cfg.CommonAddrHint, cfg.Originator, nil)
	return err
}

// Open prepares a client for the station of cfg without connecting. trace receives the
// frame trace of --debug. extra options are applied last.
func Open(cfg *config.ConnConfig, trace io.Writer, extra ...client.Option) (*Session, error) {
	target, err := cfg.ResolveTarget()
	if err != nil {
		return nil, errs.InvalidInput(errs.CodeInvalidURL, err.Error(), err)
	}
	link, err := LinkParams(cfg.LinkConfig)
	if err != nil {
		return nil, errs.WrapValidation(err)
	}
	ap, err := ASDUParams(cfg.LinkConfig)
	if err != nil {
		return nil, errs.WrapValidation(err)
	}
	timeout := Timeout(cfg.Timeout)
	opts := []client.Option{
		client.WithParams(link),
		client.WithASDUParams(ap),
		client.WithOriginator(cfg.Originator),
		client.WithRequestTimeout(timeout),
	}
	if target.TLS {
		tc, err := ClientTLS(cfg.TLSConfig)
		if err != nil {
			return nil, errs.WrapValidation(err)
		}
		opts = append(opts, client.WithTLS(tc))
	}
	var tracer *Tracer
	if cfg.Debug && trace != nil {
		tracer = &Tracer{W: trace, Params: ap}
		opts = append(opts, client.WithMetrics(tracer))
	}
	s := &Session{Target: target, Params: ap, Timeout: timeout}
	sc, err := ClientSecurity(cfg.ClientSecurityConfig)
	if err != nil {
		return nil, errs.WrapValidation(err)
	}
	if sc != nil {
		s.events = &secEvents{tracer: tracer}
		if s.Security, err = clientEndpoint(sc, ap, cfg.CommonAddrHint, cfg.Originator, s.events.on); err != nil {
			return nil, errs.WrapValidation(err)
		}
		opts = append(opts, client.WithSecurity(s.Security))
	}
	c, err := client.New(target.Addr(), append(opts, extra...)...)
	if err != nil {
		return nil, errs.WrapValidation(err)
	}
	s.Client = c
	return s, nil
}

// Dial is [Open] followed by connecting and starting data transfer, bounded by the timeout
// of cfg.
func Dial(ctx context.Context, cfg *config.ConnConfig, trace io.Writer, extra ...client.Option) (*Session, error) {
	s, err := Open(cfg, trace, extra...)
	if err != nil {
		return nil, err
	}
	cctx, cancel := context.WithTimeout(ctx, s.Timeout)
	defer cancel()
	if err := s.Connect(cctx); err != nil {
		_ = s.Close()
		if s.Security != nil {
			if serr := securityFailed(err, s.Security, s.events); serr != nil {
				return nil, serr
			}
		}
		return nil, WrapError(err)
	}
	if s.Security != nil && s.events.tracer != nil {
		st := s.Security.Status()
		remote := ""
		if st.Remote != nil {
			remote = fmt.Sprintf(" with %q", st.Remote.Subject.CommonName)
		}
		s.events.tracer.line("--", nil, fmt.Sprintf("security: secure session%s, AIM %d, AIS %d, MAC %s, data %s",
			remote, st.AIM, st.AIS, st.MAC, st.Data))
	}
	return s, nil
}

// Tracer prints every APDU sent and received, for --debug. It is a go-iec104 Metrics.
type Tracer struct {
	iec104.NopMetrics
	W      io.Writer
	Params asdu.Params
	mu     sync.Mutex
}

// OnConnect implements iec104.Metrics.
func (t *Tracer) OnConnect(remote net.Addr) { t.line("--", remote, "connected") }

// OnDisconnect implements iec104.Metrics.
func (t *Tracer) OnDisconnect(remote net.Addr, err error) {
	if err != nil {
		t.line("--", remote, "disconnected: "+err.Error())
		return
	}
	t.line("--", remote, "disconnected")
}

// OnFrameSent implements iec104.Metrics.
func (t *Tracer) OnFrameSent(remote net.Addr, f apci.Frame, _ int) {
	t.line("->", remote, t.describe(f))
}

// OnFrameReceived implements iec104.Metrics.
func (t *Tracer) OnFrameReceived(remote net.Addr, f apci.Frame, _ int) {
	t.line("<-", remote, t.describe(f))
}

func (t *Tracer) describe(f apci.Frame) string {
	s := f.String()
	if f.Format != apci.FormatI {
		return s
	}
	a, err := asdu.Decode(f.ASDU, t.Params)
	if err != nil {
		return fmt.Sprintf("%s  undecodable ASDU: %v  [% x]", s, err, f.ASDU)
	}
	return fmt.Sprintf("%s  %s  [% x]", s, a, f.ASDU)
}

func (t *Tracer) line(dir string, remote net.Addr, text string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	addr := ""
	if remote != nil {
		addr = remote.String()
	}
	_, _ = fmt.Fprintf(t.W, "debug: %s %s %s %s\n", time.Now().UTC().Format("15:04:05.000"), dir, addr, text)
}
