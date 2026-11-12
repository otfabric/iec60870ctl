// SPDX-License-Identifier: MIT

package iec

import (
	"context"
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/otfabric/go-iec60870-5/asdu"
	"github.com/otfabric/go-iec60870-5/iec101"
	"github.com/otfabric/iec60870ctl/internal/errs"
	"github.com/otfabric/iec60870ctl/internal/iec101ctl/config"
	"github.com/otfabric/iec60870ctl/internal/iec101ctl/types"
)

// parseClockTime resolves --time: now, or an RFC 3339 time. The time must fit a CP56Time2a
// in loc, the time zone on the wire: its year has two digits, read as 1970 to 2069. Outside
// that window the time would go out flagged invalid and in another century, so it is refused
// before anything is sent.
func parseClockTime(s string, now time.Time, loc *time.Location) (time.Time, error) {
	s = strings.TrimSpace(s)
	t := now
	if s != "" && !strings.EqualFold(s, "now") {
		var err error
		if t, err = time.Parse(time.RFC3339Nano, s); err != nil {
			return time.Time{}, fmt.Errorf("invalid --time %q: use now or an RFC 3339 time such as 2026-01-31T12:00:00Z", s)
		}
	}
	if loc == nil {
		loc = time.UTC
	}
	if y := t.In(loc).Year(); y < 1970 || y > 2069 {
		return time.Time{}, fmt.Errorf("invalid --time %q: a clock synchronization carries the years 1970 to 2069 only", s)
	}
	return t, nil
}

// CheckClock validates the configuration of `client clock`.
func CheckClock(cfg *config.ClockConfig) error {
	if _, err := checkStation(&cfg.StationConfig, true); err != nil {
		return err
	}
	loc, err := Location(cfg.TimeZone)
	if err != nil {
		return err
	}
	_, err = parseClockTime(cfg.Time, time.Now(), loc)
	return err
}

// Clock sets the clock of a station (C_CS_NA_1).
func Clock(ctx context.Context, cfg *config.ClockConfig, trace io.Writer) (*types.ConfirmedResult, error) {
	s, err := Dial(ctx, &cfg.ConnConfig, trace, Hooks{})
	if err != nil {
		return nil, err
	}
	defer func() { _ = s.Close() }()
	loc := s.Params().Location
	if loc == nil {
		loc = time.UTC
	}
	// "now" is taken after the link came up, as close to sending as possible.
	t, err := parseClockTime(cfg.Time, time.Now(), loc)
	if err != nil {
		return nil, err
	}
	begin := time.Now()
	if err := s.ClockSync(ctx, asdu.CommonAddr(cfg.CommonAddr), t); err != nil {
		return nil, WrapError(err)
	}
	return &types.ConfirmedResult{
		Target:     s.Target.String(),
		CommonAddr: cfg.CommonAddr,
		Request:    "clock-synchronization",
		Time:       t.In(loc).Format(TimeLayout),
		RTTMs:      ms(time.Since(begin)),
	}, nil
}

// resetQualifiers maps --qualifier of `client reset` to the QRP.
var resetQualifiers = map[string]uint8{
	"process": asdu.ResetProcessGeneral,
	"events":  asdu.ResetPendingEvents,
}

// ResetQualifiers lists the values of --qualifier of `client reset`.
func ResetQualifiers() map[string]string {
	return map[string]string{
		"process": "general reset of the process",
		"events":  "reset of pending information with time tag",
	}
}

// CheckReset validates the configuration of `client reset`.
func CheckReset(cfg *config.ResetConfig) error {
	if _, err := checkStation(&cfg.StationConfig, true); err != nil {
		return err
	}
	return oneOf("qualifier", cfg.Qualifier, "process", "events")
}

// Reset sends a reset process command (C_RP_NA_1).
func Reset(ctx context.Context, cfg *config.ResetConfig, trace io.Writer) (*types.ConfirmedResult, error) {
	s, err := Dial(ctx, &cfg.ConnConfig, trace, Hooks{})
	if err != nil {
		return nil, err
	}
	defer func() { _ = s.Close() }()
	begin := time.Now()
	if err := s.ResetProcess(ctx, asdu.CommonAddr(cfg.CommonAddr), resetQualifiers[cfg.Qualifier]); err != nil {
		return nil, WrapError(err)
	}
	return &types.ConfirmedResult{
		Target:     s.Target.String(),
		CommonAddr: cfg.CommonAddr,
		Request:    "reset-process",
		Qualifier:  cfg.Qualifier,
		RTTMs:      ms(time.Since(begin)),
	}, nil
}

// MaxDelay is the largest transmission delay C_CD_NA_1 carries, in milliseconds.
const MaxDelay = 59999

// ParseSetDelay parses --set of `client delay`: "" (measure only), auto (measure, then send
// what was measured) or a number of milliseconds 0-59999 (send it without measuring).
func ParseSetDelay(s string) (measure, send bool, d uint16, err error) {
	s = strings.TrimSpace(s)
	switch strings.ToLower(s) {
	case "":
		return true, false, 0, nil
	case "auto":
		return true, true, 0, nil
	}
	n, perr := strconv.ParseUint(s, 10, 16)
	if perr != nil || n > MaxDelay {
		return false, false, 0, fmt.Errorf("invalid --set %q: use auto, or milliseconds 0-%d", s, MaxDelay)
	}
	return false, true, uint16(n), nil
}

// CheckDelay validates the configuration of `client delay`.
func CheckDelay(cfg *config.DelayConfig) error {
	if _, err := checkStation(&cfg.StationConfig, false); err != nil {
		return err
	}
	_, _, _, err := ParseSetDelay(cfg.Set)
	return err
}

// Delay runs the delay acquisition procedure (C_CD_NA_1, IEC 60870-5-101:2003 7.4.12): it
// measures the transmission delay to the station, sends a delay to it, or both.
func Delay(ctx context.Context, cfg *config.DelayConfig, trace io.Writer) (*types.DelayResult, error) {
	measure, send, d, err := ParseSetDelay(cfg.Set)
	if err != nil {
		return nil, err
	}
	s, err := Dial(ctx, &cfg.ConnConfig, trace, Hooks{})
	if err != nil {
		return nil, err
	}
	defer func() { _ = s.Close() }()
	ca := asdu.CommonAddr(cfg.CommonAddr)
	res := &types.DelayResult{Target: s.Target.String(), CommonAddr: cfg.CommonAddr}
	begin := time.Now()
	if measure {
		got, err := s.MeasureDelay(ctx, ca)
		if err != nil {
			return nil, WrapError(err)
		}
		m := ms(got)
		res.MeasuredMs = &m
		d = uint16(min(max(math.Round(m), 0), MaxDelay))
	}
	if send {
		if err := s.SetDelay(ctx, ca, time.Duration(d)*time.Millisecond); err != nil {
			return nil, WrapError(err)
		}
		res.SetMs = &d
	}
	res.RTTMs = ms(time.Since(begin))
	return res, nil
}

// Ping methods.
const (
	PingCommand    = "command"
	PingLinkStatus = "link-status"
	PingLinkTest   = "link-test"
)

// PingMethods lists the values of --method of `client ping`.
func PingMethods() map[string]string {
	return map[string]string{
		PingCommand:    "test command C_TS_NA_1",
		PingLinkStatus: "request status of link (function 9)",
		PingLinkTest:   "test function for link (function 2), balanced only",
	}
}

// CheckPing validates the configuration of `client ping`.
func CheckPing(cfg *config.PingConfig) error {
	if err := Validate(&cfg.ConnConfig); err != nil {
		return err
	}
	if err := oneOf("method", cfg.Method, PingCommand, PingLinkStatus, PingLinkTest); err != nil {
		return err
	}
	l, _ := LinkParams(cfg.LinkConfig)
	if cfg.Method == PingCommand {
		if err := CheckCommonAddr(cfg.CommonAddr, l.Params.ASDU.CommonAddrSize, false); err != nil {
			return err
		}
	}
	if cfg.Method == PingLinkTest && l.Params.Mode != iec101.ModeBalanced {
		return fmt.Errorf("--method link-test needs --link-mode balanced: the test function for link is a procedure of balanced transmission")
	}
	if cfg.Interval == 0 {
		return fmt.Errorf("invalid --interval 0: use at least 1 ms")
	}
	return nil
}

// pinger runs one test.
type pinger struct {
	test  func(ctx context.Context) error
	close func()
	done  <-chan struct{}
}

func openPinger(ctx context.Context, cfg *config.PingConfig, trace io.Writer) (*pinger, string, error) {
	if cfg.Method == PingCommand {
		s, err := Dial(ctx, &cfg.ConnConfig, trace, Hooks{})
		if err != nil {
			return nil, "", err
		}
		ca := asdu.CommonAddr(cfg.CommonAddr)
		return &pinger{
			test:  func(ctx context.Context) error { return s.TestCommand(ctx, ca) },
			close: func() { _ = s.Close() },
			done:  s.Done(),
		}, s.Target.String(), nil
	}
	l, err := LinkParams(cfg.LinkConfig)
	if err != nil {
		return nil, "", err
	}
	target, rw, err := openLine(ctx, &cfg.ConnConfig)
	if err != nil {
		return nil, "", err
	}
	var tr *Tracer
	if cfg.Debug && trace != nil {
		tr = NewTracer(trace, l.Params)
	}
	timeout := Timeout(cfg.Timeout)
	r, err := openRaw(rw, l, l.Params.Link.Timeout, tr)
	if err != nil {
		_ = rw.Close()
		return nil, "", errs.WrapValidation(err)
	}
	p := &pinger{close: func() { _ = r.Close() }, done: r.done()}
	if cfg.Method == PingLinkStatus {
		p.test = func(ctx context.Context) error {
			_, err := r.status(ctx, l.Addr)
			return err
		}
		return p, target.String(), nil
	}
	// The test function needs this station's direction of the link initialized first.
	ictx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	if err := r.bal.Init(ictx); err != nil {
		_ = r.Close()
		if ctx.Err() != nil {
			return nil, "", WrapError(ctx.Err())
		}
		return nil, "", errs.New(errs.KindTimeout, errs.CodeLinkInitFailed, fmt.Sprintf("timeout: the link did not come up: %v", err), err)
	}
	p.test = r.bal.Test
	return p, target.String(), nil
}

// Ping tests a station at an interval and reports every test through emit. A failed test
// is reported and testing goes on while the line is open; a lost line ends the command
// after it has been reported.
func Ping(ctx context.Context, cfg *config.PingConfig, trace io.Writer, emit func(*types.PingSample) error) (types.PingSummary, error) {
	sum := types.PingSummary{MethodName: cfg.Method}
	p, target, err := openPinger(ctx, cfg, trace)
	if err != nil {
		return sum, err
	}
	defer p.close()
	sum.Target = target

	timeout := Timeout(cfg.Timeout)
	interval := time.Duration(cfg.Interval) * time.Millisecond
	var total float64
	for seq := uint32(1); cfg.Count == 0 || seq <= cfg.Count; seq++ {
		begin := time.Now()
		tctx, cancel := context.WithTimeout(ctx, timeout)
		err := p.test(tctx)
		cancel()
		if ctx.Err() != nil {
			break // interrupted: the stream ends normally
		}
		rtt := ms(time.Since(begin))
		sample := &types.PingSample{Seq: seq, Timestamp: FormatTime(begin), Method: cfg.Method, RTTMs: rtt, Error: EmbeddedErrorInfo(err)}
		sum.Sent++
		if err != nil {
			sum.Failed++
		} else {
			if sum.Sent-sum.Failed == 1 || rtt < sum.Min {
				sum.Min = rtt
			}
			sum.Max = max(sum.Max, rtt)
			total += rtt
			sum.Avg = total / float64(sum.Sent-sum.Failed)
		}
		if werr := emit(sample); werr != nil {
			return sum, werr
		}
		select {
		case <-p.done:
			return sum, errs.New(errs.KindTransport, errs.CodeConnectionLost, "connection error: the line closed", err)
		default:
		}
		if cfg.Count != 0 && seq == cfg.Count {
			break
		}
		select {
		case <-ctx.Done():
			return sum, nil
		case <-p.done:
			return sum, errs.New(errs.KindTransport, errs.CodeConnectionLost, "connection error: the line closed", nil)
		case <-time.After(interval):
		}
	}
	return sum, nil
}

// CheckTypes validates the configuration of `client types`.
func CheckTypes(cfg *config.TypesConfig) error {
	return oneOf("direction", cfg.Direction, "", "monitor", "control")
}

// Types lists the type identifications that IEC 60870-5-101 defines and go-iec60870-5
// knows by name.
func Types(cfg *config.TypesConfig) *types.TypesResult {
	res := &types.TypesResult{Types: []types.TypeInfo{}}
	for _, name := range TypeNames() {
		t := typeNames[strings.ToUpper(name)]
		if !iec101.Defined101(t) {
			continue
		}
		dir := "monitor"
		if t.InControlDirection() {
			dir = "control"
		}
		if cfg.Direction != "" && cfg.Direction != dir {
			continue
		}
		res.Types = append(res.Types, types.TypeInfo{
			ID:          uint8(t),
			Name:        name,
			Description: t.Description(),
			Direction:   dir,
			TimeTag:     t.HasTimeTag(),
			Decoded:     t.Supported(),
		})
	}
	res.Count = len(res.Types)
	return res
}
