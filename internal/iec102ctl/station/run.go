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

	"github.com/otfabric/iec60870ctl/internal/errs"
	"github.com/otfabric/iec60870ctl/internal/iec102ctl/config"
	"github.com/otfabric/iec60870ctl/internal/iec102ctl/meter"
	"github.com/otfabric/iec60870ctl/internal/iec102ctl/types"
	"github.com/otfabric/iec60870ctl/internal/line"
)

// LoadModel returns the model of cfg: the file --config names, or the built-in demo.
func LoadModel(cfg *config.ServerConfig) (*Model, error) {
	if cfg.Config == "" {
		return Demo(), nil
	}
	return Load(cfg.Config)
}

// optional returns the address a flag sets, or nil for -1 (the model's).
func optional(v int32, name string) (*uint16, error) {
	switch {
	case v == -1:
		return nil, nil
	case v < 0 || v > 65535:
		return nil, fmt.Errorf("%s %d: 0-65535, or -1 for the model's", name, v)
	}
	a := uint16(v)
	return &a, nil
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
	size, err := meter.LinkAddrSize(cfg.LinkAddressSize)
	if err != nil {
		return nil, err
	}
	loc, err := meter.Location(cfg.TimeZone)
	if err != nil {
		return nil, err
	}
	la, err := optional(cfg.LinkAddress, "--link-address")
	if err != nil {
		return nil, err
	}
	ca, err := optional(cfg.CommonAddress, "--common-address")
	if err != nil {
		return nil, err
	}
	if cfg.Serial != "" {
		if _, err := line.SerialConfig(cfg.Serial, settings(cfg)); err != nil {
			return nil, err
		}
	}
	opts := Options{
		LinkAddrSize: size, CASize: int(cfg.CASize), Location: loc, LinkAddr: la, CommonAddr: ca, Log: log,
		LinkTimeout: time.Duration(cfg.LinkTimeout) * time.Millisecond,
	}
	if cfg.Debug && trace != nil {
		opts.Trace = trace
	}
	return New(m, opts)
}

func settings(cfg *config.ServerConfig) line.Settings {
	return line.Settings{Baud: cfg.Baud, DataBits: cfg.DataBits, Parity: cfg.Parity, StopBits: cfg.StopBits}
}

// listenAddr returns the address `server start` listens on.
func listenAddr(cfg *config.ServerConfig) string {
	port := cfg.Port
	if port == 0 {
		port = line.DefaultPort
	}
	return net.JoinHostPort(strings.TrimSpace(cfg.Listen), strconv.Itoa(int(port)))
}

// Run serves the meter of cfg until ctx ends or the duration of cfg has passed. Every event
// goes to log and, with --debug, every frame to trace; ready, when not nil, is called with
// the address once the station listens (a serial port's device name).
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
	detail := fmt.Sprintf("meter %q, link address %d, measuring point %d", st.Name(), st.LinkAddr(), st.CommonAddr())
	if cfg.Serial != "" {
		return runSerial(ctx, cfg, st, detail, ready)
	}
	var lc net.ListenConfig
	ln, err := lc.Listen(ctx, "tcp", listenAddr(cfg))
	if err != nil {
		return errs.New(errs.KindTransport, errs.CodeListenFailed, fmt.Sprintf("cannot listen: %v", err), err)
	}
	st.event(types.ServerEvent{Event: "listening", Remote: ln.Addr().String(), Detail: detail})
	if ready != nil {
		ready(ln.Addr().String())
	}
	err = line.Serve(ctx, ln, func(ctx context.Context, c line.Conn) {
		st.event(types.ServerEvent{Event: "connected", Remote: c.Remote})
		st.Serve(ctx, c, c.Remote)
		st.event(types.ServerEvent{Event: "disconnected", Remote: c.Remote})
	}, func(remote string) {
		st.event(types.ServerEvent{Event: "busy", Remote: remote, Detail: "the line is in use: connection closed"})
	})
	st.event(types.ServerEvent{Event: "stopping"})
	if err != nil {
		return errs.New(errs.KindTransport, errs.CodeListenFailed, fmt.Sprintf("server stopped: %v", err), err)
	}
	return nil
}

// runSerial serves the serial port of cfg until ctx ends or the port fails.
func runSerial(ctx context.Context, cfg *config.ServerConfig, st *Station, detail string, ready func(string)) error {
	sc, err := line.SerialConfig(cfg.Serial, settings(cfg))
	if err != nil {
		return errs.InvalidInput(errs.CodeInvalidConfig, err.Error(), err)
	}
	port, err := line.OpenSerial(&sc)
	if err != nil {
		return errs.New(errs.KindTransport, errs.CodeListenFailed, fmt.Sprintf("cannot open %s: %v", cfg.Serial, err), err)
	}
	st.event(types.ServerEvent{Event: "listening", Remote: cfg.Serial, Detail: detail})
	if ready != nil {
		ready(cfg.Serial)
	}
	st.Serve(ctx, port, cfg.Serial)
	st.event(types.ServerEvent{Event: "stopping"})
	return nil
}
