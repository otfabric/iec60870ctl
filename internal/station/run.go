// SPDX-License-Identifier: MIT

package station

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io"
	"net"
	"net/netip"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/otfabric/iec104ctl/internal/config"
	"github.com/otfabric/iec104ctl/internal/errs"
	"github.com/otfabric/iec104ctl/internal/iec"
	"github.com/otfabric/iec104ctl/internal/types"
)

// serverTLS builds the TLS configuration of the listener, or nil without a certificate.
func serverTLS(cfg *config.ServerConfig) (*tls.Config, error) {
	if (cfg.Cert == "") != (cfg.Key == "") {
		return nil, fmt.Errorf("--tls-cert and --tls-key must be given together")
	}
	if cfg.Cert == "" {
		if cfg.ClientCA != "" {
			return nil, fmt.Errorf("--tls-client-ca needs --tls-cert and --tls-key")
		}
		return nil, nil
	}
	pair, err := tls.LoadX509KeyPair(cfg.Cert, cfg.Key)
	if err != nil {
		return nil, fmt.Errorf("loading the certificate and key: %w", err)
	}
	out := &tls.Config{MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{pair}}
	if cfg.ClientCA != "" {
		pem, err := os.ReadFile(cfg.ClientCA)
		if err != nil {
			return nil, fmt.Errorf("reading the client CA certificates: %w", err)
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(pem) {
			return nil, fmt.Errorf("no certificates found in %s", cfg.ClientCA)
		}
		out.ClientCAs, out.ClientAuth = pool, tls.RequireAndVerifyClientCert
	}
	return out, nil
}

func parseAllow(list []string) ([]netip.Prefix, error) {
	var out []netip.Prefix
	for _, s := range list {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		if p, err := netip.ParsePrefix(s); err == nil {
			out = append(out, p.Masked())
			continue
		}
		addr, err := netip.ParseAddr(s)
		if err != nil {
			return nil, fmt.Errorf("invalid --allow %q: use an address or a CIDR prefix", s)
		}
		out = append(out, netip.PrefixFrom(addr, addr.BitLen()))
	}
	return out, nil
}

// LoadModel returns the model of cfg: the file --config names, or the built-in demo.
func LoadModel(cfg *config.ServerConfig) (*Model, error) {
	if cfg.Config == "" {
		return Demo(), nil
	}
	return Load(cfg.Config)
}

// Check validates the configuration of `server start`, the model included.
func Check(cfg *config.ServerConfig) error {
	_, err := build(cfg, nil, nil)
	return err
}

func build(cfg *config.ServerConfig, log func(types.ServerEvent), trace io.Writer) (*Station, error) {
	m, err := LoadModel(cfg)
	if err != nil {
		return nil, err
	}
	link, err := iec.LinkParams(cfg.LinkConfig)
	if err != nil {
		return nil, err
	}
	ap, err := iec.ASDUParams(cfg.LinkConfig)
	if err != nil {
		return nil, err
	}
	tc, err := serverTLS(cfg)
	if err != nil {
		return nil, err
	}
	allow, err := parseAllow(cfg.Allow)
	if err != nil {
		return nil, err
	}
	security, err := iec.ServerSecurity(cfg.ServerSecurityConfig)
	if err != nil {
		return nil, err
	}
	opts := Options{Link: link, ASDU: ap, TLS: tc, Allow: allow, MaxSessions: int(cfg.MaxSessions), Log: log, Security: security}
	if cfg.Debug && trace != nil {
		opts.Metrics = &iec.Tracer{W: trace, Params: ap}
	}
	return New(m, opts)
}

// listenAddr returns the address `server start` listens on.
func listenAddr(cfg *config.ServerConfig, withTLS bool) string {
	port := cfg.Port
	if port == 0 {
		port = config.DefaultPort
		if withTLS {
			port = config.DefaultTLSPort
		}
	}
	return net.JoinHostPort(strings.TrimSpace(cfg.Listen), strconv.Itoa(int(port)))
}

// Run serves the station of cfg until ctx ends or the duration of cfg has passed. Every
// event goes to log and, with --debug, every frame to trace; ready, when not nil, is called
// with the address once the station listens.
func Run(ctx context.Context, cfg *config.ServerConfig, log func(types.ServerEvent), trace io.Writer, ready func(net.Addr)) error {
	st, err := build(cfg, log, trace)
	if err != nil {
		return errs.InvalidInput(errs.CodeInvalidConfig, err.Error(), err)
	}
	ln, err := st.Listen(listenAddr(cfg, st.opts.TLS != nil))
	if err != nil {
		return errs.New(errs.KindTransport, errs.CodeListenFailed, fmt.Sprintf("cannot listen: %v", err), err)
	}
	if ready != nil {
		ready(ln.Addr())
	}
	if cfg.Duration != 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(cfg.Duration)*time.Second)
		defer cancel()
	}
	served := make(chan error, 1)
	go func() { served <- st.Serve(ln) }()
	select {
	case err := <-served:
		if err != nil {
			return errs.New(errs.KindTransport, errs.CodeListenFailed, fmt.Sprintf("server stopped: %v", err), err)
		}
		return nil
	case <-ctx.Done():
	}
	st.event(types.ServerEvent{Event: "stopping", Detail: fmt.Sprintf("%d connections", st.Sessions())})
	_ = st.Close()
	if err := <-served; err != nil {
		return errs.New(errs.KindTransport, errs.CodeListenFailed, fmt.Sprintf("server stopped: %v", err), err)
	}
	return nil
}
