// SPDX-License-Identifier: MIT

// Package linecfg turns the line flags of iec103ctl into what go-iec60870-5 and the shared
// line package take: the time zone, the serial settings and the --debug frame trace. The
// client commands (package iec) and the simulated device (package station) share it.
package linecfg

import (
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/otfabric/go-iec60870-5/ft12"
	"github.com/otfabric/go-iec60870-5/iec103"
	"github.com/otfabric/iec60870ctl/internal/iec103ctl/config"
	"github.com/otfabric/iec60870ctl/internal/line"
)

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

// Settings returns the serial line settings of cfg.
func Settings(cfg config.SerialConfig) line.Settings {
	return line.Settings{Baud: cfg.Baud, DataBits: cfg.DataBits, Parity: cfg.Parity, StopBits: cfg.StopBits}
}

// CheckSerial validates the serial flags, also on a TCP line: a value no line can have is
// an error wherever it is given.
func CheckSerial(cfg config.SerialConfig) error {
	return line.CheckSettings(LineSettings(cfg))
}

// LineSettings is Settings with the zero values replaced by the FT 1.2 format.
func LineSettings(cfg config.SerialConfig) line.Settings {
	s := Settings(cfg)
	if s.Baud == 0 {
		s.Baud = 9600
	}
	if s.DataBits == 0 {
		s.DataBits = 8
	}
	if s.StopBits == 0 {
		s.StopBits = 1
	}
	return s
}

// Tracer returns the frame and discard hooks that write the --debug trace to w.
func Tracer(w io.Writer) (func(bool, ft12.Frame), func([]byte, error)) {
	var mu sync.Mutex
	onFrame := func(tx bool, f ft12.Frame) {
		dir := "rx"
		if tx {
			dir = "tx"
		}
		mu.Lock()
		defer mu.Unlock()
		if len(f.Data) == 0 {
			_, _ = fmt.Fprintf(w, "%s %s %s\n", time.Now().Format("15:04:05.000"), dir, f)
			return
		}
		desc := ""
		if a, err := iec103.Decode(f.Data); err == nil {
			desc = "  " + a.String()
		}
		_, _ = fmt.Fprintf(w, "%s %s %s  % X%s\n", time.Now().Format("15:04:05.000"), dir, f, f.Data, desc)
	}
	onDiscard := func(b []byte, err error) {
		mu.Lock()
		defer mu.Unlock()
		_, _ = fmt.Fprintf(w, "%s discarded % X: %v\n", time.Now().Format("15:04:05.000"), b, err)
	}
	return onFrame, onDiscard
}
