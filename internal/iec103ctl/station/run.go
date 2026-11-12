// SPDX-License-Identifier: MIT

package station

import (
	"context"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/otfabric/go-iec60870-5/link"
	"github.com/otfabric/iec60870ctl/internal/errs"
	"github.com/otfabric/iec60870ctl/internal/iec103ctl/config"
	"github.com/otfabric/iec60870ctl/internal/iec103ctl/linecfg"
	"github.com/otfabric/iec60870ctl/internal/iec103ctl/types"
	"github.com/otfabric/iec60870ctl/internal/line"
)

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
	if cfg.Serial != "" && (strings.TrimSpace(cfg.Listen) != "" || cfg.Port != 0) {
		return nil, fmt.Errorf("--serial and --listen/--port are mutually exclusive")
	}
	if err := linecfg.CheckSerial(cfg.SerialConfig); err != nil {
		return nil, err
	}
	loc, err := linecfg.Location(cfg.TimeZone)
	if err != nil {
		return nil, err
	}
	m, err := LoadModel(cfg)
	if err != nil {
		return nil, err
	}
	la, err := parseAddress(cfg.LinkAddress, "--link-address", -1)
	if err != nil {
		return nil, err
	}
	ca, err := parseAddress(cfg.CommonAddress, "--common-address", -1)
	if err != nil {
		return nil, err
	}
	opts := Options{LinkAddr: la, CommonAddr: ca, Location: loc, Log: log}
	if cfg.Debug && trace != nil {
		opts.Link.OnFrame, opts.Link.OnDiscard = linecfg.Tracer(trace)
	}
	// A line over TCP repeats nothing: the secondary only answers. The frame gap stays the
	// library's default.
	opts.Link.Timeout = link.DefaultTimeout
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

// Run serves the device of cfg until ctx ends or --duration passed. Every event goes to
// log and, with --debug, every frame to trace; ready, when not nil, is called with the
// address (or serial device) once the device is served.
func Run(ctx context.Context, cfg *config.ServerConfig, log func(types.ServerEvent), trace io.Writer, ready func(string)) error {
	st, err := build(cfg, log, trace)
	if err != nil {
		return errs.InvalidInput(errs.CodeInvalidConfig, err.Error(), err)
	}
	if cfg.Duration != 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(cfg.Duration)*time.Second)
		defer cancel()
	}
	detail := fmt.Sprintf("link=%d ca=%d fun=%d", st.LinkAddr(), st.CommonAddr(), st.fun)
	if cfg.Serial != "" {
		return runSerial(ctx, st, cfg, detail, ready)
	}
	var lc net.ListenConfig
	ln, err := lc.Listen(ctx, "tcp", listenAddr(cfg))
	if err != nil {
		return errs.New(errs.KindTransport, errs.CodeListenFailed, fmt.Sprintf("cannot listen: %v", err), err)
	}
	addr := ln.Addr().String()
	st.event(types.ServerEvent{Event: "listening", Remote: addr, Detail: detail})
	if ready != nil {
		ready(addr)
	}
	err = line.Serve(ctx, ln, func(ctx context.Context, c line.Conn) {
		st.event(types.ServerEvent{Event: "connected", Remote: c.Remote})
		_ = st.Serve(ctx, c, c.Remote)
		st.event(types.ServerEvent{Event: "disconnected", Remote: c.Remote})
	}, func(remote string) {
		st.event(types.ServerEvent{Event: "busy", Remote: remote, Detail: "refused: another controlling system is connected"})
	})
	st.event(types.ServerEvent{Event: "stopping"})
	if err != nil {
		return errs.New(errs.KindTransport, errs.CodeListenFailed, fmt.Sprintf("server stopped: %v", err), err)
	}
	return nil
}

// runSerial serves the device on a serial port until ctx ends or the port fails.
func runSerial(ctx context.Context, st *Station, cfg *config.ServerConfig, detail string, ready func(string)) error {
	t := line.Target{Device: cfg.Serial}
	port, err := line.Dial(ctx, t, linecfg.LineSettings(cfg.SerialConfig), 0)
	if err != nil {
		return errs.New(errs.KindTransport, errs.CodeTransportConnectFailed, fmt.Sprintf("connection error: %v", err), err)
	}
	st.event(types.ServerEvent{Event: "listening", Remote: t.String(), Detail: detail})
	if ready != nil {
		ready(t.String())
	}
	err = st.Serve(ctx, port, t.String())
	st.event(types.ServerEvent{Event: "stopping"})
	if ctx.Err() == nil {
		return errs.New(errs.KindTransport, errs.CodeConnectionLost, fmt.Sprintf("connection error: the serial port %s failed", cfg.Serial), err)
	}
	return nil
}
