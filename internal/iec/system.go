// SPDX-License-Identifier: MIT

package iec

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/otfabric/go-iec104/asdu"
	"github.com/otfabric/iec104ctl/internal/config"
	"github.com/otfabric/iec104ctl/internal/types"
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
	if err := Validate(&cfg.ConnConfig); err != nil {
		return err
	}
	if err := CheckCommonAddr(cfg.CommonAddr, true); err != nil {
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
	s, err := Dial(ctx, cfg.Conn(), trace)
	if err != nil {
		return nil, err
	}
	defer func() { _ = s.Close() }()
	// "now" is taken after connecting, as close to sending as possible.
	t, err := parseClockTime(cfg.Time, time.Now(), s.Params.Location)
	if err != nil {
		return nil, err
	}
	begin := time.Now()
	if err := s.ClockSync(ctx, asdu.CommonAddr(cfg.CommonAddr), t); err != nil {
		return nil, WrapError(err)
	}
	loc := s.Params.Location
	if loc == nil {
		loc = time.UTC
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
	if err := Validate(&cfg.ConnConfig); err != nil {
		return err
	}
	if err := CheckCommonAddr(cfg.CommonAddr, true); err != nil {
		return err
	}
	return oneOf("qualifier", cfg.Qualifier, "process", "events")
}

// Reset sends a reset process command (C_RP_NA_1).
func Reset(ctx context.Context, cfg *config.ResetConfig, trace io.Writer) (*types.ConfirmedResult, error) {
	s, err := Dial(ctx, cfg.Conn(), trace)
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

// Ping methods.
const (
	PingTestFR  = "testfr"
	PingCommand = "command"
)

// PingMethods lists the values of --method of `client ping`.
func PingMethods() map[string]string {
	return map[string]string{
		PingTestFR:  "link test frame (TESTFR act/con)",
		PingCommand: "test command C_TS_TA_1",
	}
}

// CheckPing validates the configuration of `client ping`.
func CheckPing(cfg *config.PingConfig) error {
	if err := Validate(&cfg.ConnConfig); err != nil {
		return err
	}
	if err := oneOf("method", cfg.Method, PingTestFR, PingCommand); err != nil {
		return err
	}
	if cfg.Method == PingCommand {
		if err := CheckCommonAddr(cfg.CommonAddr, false); err != nil {
			return err
		}
	}
	if cfg.Interval == 0 {
		return fmt.Errorf("invalid --interval 0: use at least 1 ms")
	}
	return nil
}

// Ping tests the connection to a station at an interval and reports every test through
// emit. A failed test is reported and testing goes on while the connection is up; an error
// that ends the connection ends the command after it has been reported.
func Ping(ctx context.Context, cfg *config.PingConfig, trace io.Writer, emit func(*types.PingSample) error) (types.PingSummary, error) {
	sum := types.PingSummary{MethodName: cfg.Method}
	s, err := Dial(ctx, cfg.Conn(), trace)
	if err != nil {
		return sum, err
	}
	defer func() { _ = s.Close() }()
	sum.Target = s.Target.String()

	interval := time.Duration(cfg.Interval) * time.Millisecond
	var total float64
	for seq := uint32(1); cfg.Count == 0 || seq <= cfg.Count; seq++ {
		begin := time.Now()
		var err error
		if cfg.Method == PingCommand {
			err = s.TestCommand(ctx, asdu.CommonAddr(cfg.CommonAddr))
		} else {
			tctx, cancel := context.WithTimeout(ctx, s.Timeout)
			err = s.TestLink(tctx)
			cancel()
		}
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
		if err != nil && Fatal(err) {
			return sum, WrapError(err)
		}
		if cfg.Count != 0 && seq == cfg.Count {
			break
		}
		select {
		case <-ctx.Done():
			return sum, nil
		case <-time.After(interval):
		}
	}
	return sum, nil
}

// CheckTypes validates the configuration of `client types`.
func CheckTypes(cfg *config.TypesConfig) error {
	return oneOf("direction", cfg.Direction, "", "monitor", "control")
}

// Types lists the type identifications go-iec104 knows.
func Types(cfg *config.TypesConfig) *types.TypesResult {
	res := &types.TypesResult{}
	for _, name := range TypeNames() {
		t := typeNames[strings.ToUpper(name)]
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
