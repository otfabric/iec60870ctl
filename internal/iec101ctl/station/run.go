// SPDX-License-Identifier: MIT

package station

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/otfabric/iec60870ctl/internal/errs"
	"github.com/otfabric/iec60870ctl/internal/iec101ctl/config"
	"github.com/otfabric/iec60870ctl/internal/iec101ctl/iec"
	"github.com/otfabric/iec60870ctl/internal/iec101ctl/types"
	"github.com/otfabric/iec60870ctl/internal/line"
)

func defaultLink() config.LinkConfig { return config.DefaultLink() }

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
	l, err := iec.LinkParams(cfg.LinkConfig)
	if err != nil {
		return nil, err
	}
	l.Params.Link.SingleCharAck = cfg.SingleCharAck
	if strings.TrimSpace(cfg.Serial) != "" {
		if strings.TrimSpace(cfg.Listen) != "" || cfg.Port != 0 {
			return nil, errors.New("--serial and --listen/--port are mutually exclusive")
		}
		if err := line.CheckSettings(iec.Settings(cfg.LineConfig)); err != nil {
			return nil, err
		}
	}
	opts := Options{Link: l, NoEndOfInit: cfg.NoEndOfInit, Log: log}
	if cfg.Debug && trace != nil {
		opts.Trace = trace
	}
	return New(m, opts)
}

// listenAddr returns the address `server start` listens on.
func listenAddr(cfg *config.ServerConfig) string {
	port := cfg.Port
	if port == 0 {
		port = line.DefaultPort
	}
	return net.JoinHostPort(strings.TrimSpace(cfg.Listen), strconv.Itoa(int(port)))
}

// Run serves the station of cfg until ctx ends or the duration of cfg has passed: on TCP,
// one controlling station at a time as a terminal server carries one line, or on a serial
// port. Every event goes to log and, with --debug, every frame to trace; ready, when not
// nil, is called with the address once the station listens (nil for a serial port).
func Run(ctx context.Context, cfg *config.ServerConfig, log func(types.ServerEvent), trace io.Writer, ready func(net.Addr)) error {
	st, err := build(cfg, log, trace)
	if err != nil {
		return errs.InvalidInput(errs.CodeInvalidConfig, err.Error(), err)
	}
	if cfg.Duration != 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(cfg.Duration)*time.Second)
		defer cancel()
	}
	if dev := strings.TrimSpace(cfg.Serial); dev != "" {
		return runSerial(ctx, st, dev, iec.Settings(cfg.LineConfig), ready)
	}
	ln, err := net.Listen("tcp", listenAddr(cfg))
	if err != nil {
		return errs.New(errs.KindTransport, errs.CodeListenFailed, fmt.Sprintf("cannot listen: %v", err), err)
	}
	if ready != nil {
		ready(ln.Addr())
	}
	sctx, stop := context.WithCancel(ctx)
	st.Simulate(sctx)
	defer func() {
		stop()
		st.Wait()
	}()
	st.event(types.ServerEvent{Event: "listening", Detail: fmt.Sprintf("%s, %s", ln.Addr(), st.Describe())})
	err = line.Serve(ctx, ln,
		func(ctx context.Context, c line.Conn) { _ = st.ServeLine(ctx, c, c.Remote) },
		func(remote string) {
			st.event(types.ServerEvent{Event: "busy", Remote: remote, Detail: "refused: a line has one controlling station"})
		})
	if err != nil {
		return errs.New(errs.KindTransport, errs.CodeListenFailed, fmt.Sprintf("server stopped: %v", err), err)
	}
	st.event(types.ServerEvent{Event: "stopping"})
	return nil
}

// runSerial serves the station on a serial port until ctx ends or the port fails.
func runSerial(ctx context.Context, st *Station, dev string, s line.Settings, ready func(net.Addr)) error {
	t := line.Target{Device: dev}
	port, err := line.Dial(ctx, t, s, 0)
	if err != nil {
		return errs.New(errs.KindTransport, errs.CodeTransportConnectFailed, fmt.Sprintf("connection error: %v", err), err)
	}
	if ready != nil {
		ready(nil)
	}
	sctx, stop := context.WithCancel(ctx)
	st.Simulate(sctx)
	defer func() {
		stop()
		st.Wait()
	}()
	st.event(types.ServerEvent{Event: "listening", Detail: fmt.Sprintf("%s, %s", t, st.Describe())})
	if err := st.ServeLine(ctx, port, t.String()); err != nil {
		return errs.New(errs.KindTransport, errs.CodeConnectionLost, fmt.Sprintf("connection error: %s: %v", t, err), err)
	}
	st.event(types.ServerEvent{Event: "stopping"})
	return nil
}
