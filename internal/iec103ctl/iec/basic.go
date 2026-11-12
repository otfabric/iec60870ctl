// SPDX-License-Identifier: MIT

package iec

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/otfabric/go-iec60870-5/iec103"
	"github.com/otfabric/iec60870ctl/internal/errs"
	"github.com/otfabric/iec60870ctl/internal/iec103ctl/config"
	"github.com/otfabric/iec60870ctl/internal/iec103ctl/linecfg"
	"github.com/otfabric/iec60870ctl/internal/iec103ctl/types"
)

// traceOf returns where the --debug frame trace goes: w with --debug, nowhere without.
func traceOf(debug bool, w io.Writer) io.Writer {
	if debug {
		return w
	}
	return nil
}

// device names the equipment of a session in a result.
func (s *session) device() types.Device {
	return types.Device{Target: s.target.String(), LinkAddress: uint8(s.cfg.LinkAddress & 0xFF), CA: s.commonAddr()}
}

// timeout is the request timeout of the session.
func (s *session) timeout() time.Duration { return Timeout(s.cfg.Timeout, DefaultTimeout) }

// CheckInterrogate validates `client interrogate`.
func CheckInterrogate(cfg *config.InterrogateConfig) error {
	_, err := CheckConn(&cfg.ConnConfig)
	return err
}

// Interrogate runs a general interrogation (clause 7.4.3) and returns every answer.
func Interrogate(ctx context.Context, cfg *config.InterrogateConfig, stderr io.Writer) (*types.InterrogateResult, error) {
	s, err := open(ctx, &cfg.ConnConfig, sessionOptions{trace: traceOf(cfg.Debug, stderr)})
	if err != nil {
		return nil, err
	}
	defer s.close()
	start := time.Now()
	var asdus []*iec103.ASDU
	if err := s.request(ctx, s.timeout(), func(rctx context.Context) error {
		var e error
		asdus, e = s.c.Interrogate(rctx)
		return e
	}); err != nil {
		return nil, err
	}
	res := &types.InterrogateResult{Device: s.device(), DurationMs: since(start), Messages: []types.Message{}}
	for _, a := range asdus {
		res.Messages = append(res.Messages, MessageOf(a))
	}
	res.Count = len(res.Messages)
	return res, nil
}

// checkCommand validates `client command` and returns what it sends.
func checkCommand(cfg *config.CommandConfig) (iec103.FunctionType, iec103.InfoNumber, iec103.DPI, error) {
	if _, err := CheckConn(&cfg.ConnConfig); err != nil {
		return 0, 0, 0, err
	}
	if cfg.Fun > 255 || cfg.Inf > 255 {
		return 0, 0, 0, fmt.Errorf("--fun and --inf are 0-255")
	}
	dco, err := ParseDCO(cfg.Value)
	if err != nil {
		return 0, 0, 0, err
	}
	fun, inf := iec103.FunctionType(cfg.Fun), iec103.InfoNumber(cfg.Inf)
	if !iec103.CommandAllowed(fun, inf, dco) {
		return 0, 0, 0, fmt.Errorf("FUN %d INF %d %s is no general command of table 17 (clause 7.2.5.2: INF 16-19 and 23-26 with a compatible function type; INF 19 and 23-26 take ON only)", fun, inf, dco)
	}
	return fun, inf, dco, nil
}

// CheckCommand validates `client command`.
func CheckCommand(cfg *config.CommandConfig) error {
	_, _, _, err := checkCommand(cfg)
	return err
}

// Command sends a general command (clause 7.4.4) once and waits for its acknowledgement,
// then, with --wait-report, for the change of state with cause 12.
func Command(ctx context.Context, cfg *config.CommandConfig, stderr io.Writer) (*types.CommandResult, error) {
	fun, inf, dco, err := checkCommand(cfg)
	if err != nil {
		return nil, errs.WrapValidation(err)
	}
	reports := make(chan *iec103.ASDU, 1)
	handler := func(a *iec103.ASDU) {
		if a.Cause != iec103.CauseRemoteOperation || a.Fun != fun || a.Inf != inf {
			return
		}
		if a.Type != iec103.TypeTimeTagged && a.Type != iec103.TypeRelTimeTagged {
			return
		}
		select {
		case reports <- a:
		default:
		}
	}
	s, err := open(ctx, &cfg.ConnConfig, sessionOptions{handler: handler, trace: traceOf(cfg.Debug, stderr)})
	if err != nil {
		return nil, err
	}
	defer s.close()
	start := time.Now()
	// Sent once: Command is never repeated, also not when its acknowledgement does not come.
	if err := s.request(ctx, s.timeout(), func(rctx context.Context) error { return s.c.Command(rctx, fun, inf, dco) }); err != nil {
		return nil, err
	}
	res := &types.CommandResult{
		Device: s.device(), Fun: uint8(fun), Inf: uint8(inf), Name: InfoName(iec103.Control, iec103.TypeGeneralCommand, fun, inf),
		Command: "off", DCO: uint8(dco), Acknowledged: true, DurationMs: since(start),
	}
	if dco == iec103.DPIOn {
		res.Command = "on"
	}
	if cfg.WaitReport > 0 {
		t := time.NewTimer(time.Duration(cfg.WaitReport) * time.Millisecond)
		defer t.Stop()
		select {
		case a := <-reports:
			m := MessageOf(a)
			res.Report = &m
		case <-t.C:
		case <-ctx.Done():
		}
	}
	return res, nil
}

// CheckClock validates `client clock`.
func CheckClock(cfg *config.ClockConfig) error {
	if _, err := CheckConn(&cfg.ConnConfig); err != nil {
		return err
	}
	_, err := ParseTime(cfg.Time)
	return err
}

// Clock synchronizes the clock of the equipment (clause 7.4.2): addressed and confirmed by
// the ASDU 6 the equipment answers with, or broadcast to every device on the line.
func Clock(ctx context.Context, cfg *config.ClockConfig, stderr io.Writer) (*types.ClockResult, error) {
	t, err := ParseTime(cfg.Time)
	if err != nil {
		return nil, errs.WrapValidation(err)
	}
	s, err := open(ctx, &cfg.ConnConfig, sessionOptions{trace: traceOf(cfg.Debug, stderr)})
	if err != nil {
		return nil, err
	}
	defer s.close()
	loc, _ := linecfg.Location(cfg.TimeZone)
	if t.IsZero() {
		t = time.Now()
	}
	start := time.Now()
	res := &types.ClockResult{Device: s.device(), Broadcast: cfg.Broadcast, Sent: timeAt(t, loc)}
	if cfg.Broadcast {
		if err := s.request(ctx, s.timeout(), func(context.Context) error { return s.c.BroadcastClockSync(t) }); err != nil {
			return nil, err
		}
		res.DurationMs = since(start)
		return res, nil
	}
	err = s.request(ctx, s.timeout(), func(rctx context.Context) error {
		got, e := s.c.ClockSync(rctx, t)
		if e == nil {
			res.DeviceTime = timeAt(got.Time, loc)
			off := float64(got.Sub(t).Microseconds()) / 1000
			res.OffsetMs = &off
		}
		return e
	})
	if err != nil {
		return nil, err
	}
	res.DurationMs = since(start)
	return res, nil
}

// CheckIdentify validates `client identify`.
func CheckIdentify(cfg *config.IdentifyConfig) error {
	_, err := CheckConn(&cfg.ConnConfig)
	return err
}

// identificationCauses are the causes of ASDU 5 (clause 7.4.1, table 8), in the order the
// result lists them.
var identificationCauses = []iec103.Cause{iec103.CauseResetFCB, iec103.CauseResetCU, iec103.CauseStartRestart, iec103.CausePowerOn}

// Identify starts the link and reports the identification the reset makes the equipment
// send (clause 7.4.1), with the start/restart and power on identifications that arrive
// within --wait.
func Identify(ctx context.Context, cfg *config.IdentifyConfig, stderr io.Writer) (*types.IdentifyResult, error) {
	s, err := open(ctx, &cfg.ConnConfig, sessionOptions{trace: traceOf(cfg.Debug, stderr)})
	if err != nil {
		return nil, err
	}
	defer s.close()
	wctx, cancel := context.WithTimeout(ctx, s.timeout())
	first := s.waitIdentification(wctx, s.resetCause())
	cancel()
	if ctx.Err() != nil {
		return nil, WrapError(ctx.Err())
	}
	if first == nil {
		return nil, errs.New(errs.KindTimeout, errs.CodeIEC103NoIdentification,
			fmt.Sprintf("timeout: %s sent no identification (ASDU 5, cause %d) after the reset of its link", s.device(), s.resetCause()), nil)
	}
	if cfg.Wait > 0 {
		t := time.NewTimer(time.Duration(cfg.Wait) * time.Millisecond)
		select {
		case <-t.C:
		case <-ctx.Done():
		case <-s.lost:
		}
		t.Stop()
	}
	res := &types.IdentifyResult{Device: s.device(), Identifications: []types.Identification{}}
	for _, c := range identificationCauses {
		if a := s.c.IdentificationFor(c); a != nil {
			if id, ok := IdentificationOf(a); ok {
				res.Identifications = append(res.Identifications, id)
			}
		}
	}
	return res, nil
}
