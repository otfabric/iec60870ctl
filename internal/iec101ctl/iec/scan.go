// SPDX-License-Identifier: MIT

package iec

import (
	"context"
	"errors"
	"fmt"
	"io"
	"maps"
	"slices"
	"sync"
	"time"

	"github.com/otfabric/go-iec60870-5/asdu"
	"github.com/otfabric/go-iec60870-5/iec101"
	"github.com/otfabric/go-iec60870-5/link"
	"github.com/otfabric/go-iec60870-5/telecontrol"
	"github.com/otfabric/iec60870ctl/internal/errs"
	"github.com/otfabric/iec60870ctl/internal/iec101ctl/config"
	"github.com/otfabric/iec60870ctl/internal/iec101ctl/types"
)

// Scan methods.
const (
	ScanBroadcast = "broadcast"
	ScanCA        = "ca"
	ScanLink      = "link"
)

// ScanMethods lists the values of --method of `client scan`.
func ScanMethods() map[string]string {
	return map[string]string{
		ScanBroadcast: "one general interrogation to the broadcast common address",
		ScanCA:        "one general interrogation per common address",
		ScanLink:      "request status of link per link address (unbalanced)",
	}
}

// ScanQuiet is how long a broadcast scan waits after the last answer before it concludes
// that every station has finished. Tests shorten it.
var ScanQuiet = 1500 * time.Millisecond

// CheckScan validates the configuration of `client scan`.
func CheckScan(cfg *config.ScanConfig) error {
	if err := Validate(&cfg.ConnConfig); err != nil {
		return err
	}
	if err := oneOf("method", cfg.Method, ScanBroadcast, ScanCA, ScanLink); err != nil {
		return err
	}
	l, _ := LinkParams(cfg.LinkConfig)
	switch cfg.Method {
	case ScanCA:
		bc := BroadcastCA(l.Params.ASDU.CommonAddrSize)
		if cfg.Start == 0 || cfg.End >= bc || cfg.End < cfg.Start {
			return fmt.Errorf("invalid sweep range %d-%d: use --start and --end within 1-%d, the end not below the start", cfg.Start, cfg.End, bc-1)
		}
	case ScanLink:
		if l.Params.Mode != iec101.ModeUnbalanced {
			return fmt.Errorf("--method link needs --link-mode unbalanced: a balanced line has one station")
		}
		top := l.Params.Link.FT12.MaxAddr() - 1 // all ones is the broadcast address
		if cfg.End > top || cfg.End < cfg.Start {
			return fmt.Errorf("invalid sweep range %d-%d: use --start and --end within 0-%d, the end not below the start", cfg.Start, cfg.End, top)
		}
	}
	return nil
}

// tally counts the objects a station sent, by type.
type tally struct {
	objects int
	byType  map[asdu.TypeID]int
}

func (t *tally) add(a *asdu.ASDU) {
	if t.byType == nil {
		t.byType = map[asdu.TypeID]int{}
	}
	n := max(len(a.Objects), 1)
	t.objects += n
	t.byType[a.Type] += n
}

func (t *tally) station(ca asdu.CommonAddr) types.ScanStation {
	st := types.ScanStation{CommonAddr: uint16(ca), Status: "found", Objects: t.objects}
	for _, id := range slices.Sorted(maps.Keys(t.byType)) {
		st.Types = append(st.Types, types.TypeCount{Type: id.String(), Count: t.byType[id]})
	}
	return st
}

// Scan finds the stations on one line: by common address (broadcast or sweep) or by link
// address. progress receives a line per station found during a sweep.
func Scan(ctx context.Context, cfg *config.ScanConfig, trace, progress io.Writer) (any, error) {
	switch cfg.Method {
	case ScanCA:
		return scanCA(ctx, cfg, trace, progress)
	case ScanLink:
		return scanLink(ctx, cfg, trace, progress)
	}
	return scanBroadcast(ctx, cfg, trace)
}

// scanBroadcast sends one general interrogation to the broadcast common address and lists
// every station that answers. It collects until the stations have been quiet for a while:
// the number of stations, and so of terminations, is what the scan is to find out.
func scanBroadcast(ctx context.Context, cfg *config.ScanConfig, trace io.Writer) (*types.ScanResult, error) {
	var (
		mu       sync.Mutex
		stations = map[asdu.CommonAddr]*tally{}
		answered = map[asdu.CommonAddr]bool{}
		last     = time.Now()
	)
	handler := func(a *asdu.ASDU) {
		mu.Lock()
		defer mu.Unlock()
		last = time.Now()
		switch {
		case a.Type == asdu.C_IC_NA_1:
			// A confirmation or termination: the station exists, also when it has no data.
			if !a.Negative && !a.Cause.IsUnknown() {
				answered[a.CommonAddr] = true
			}
		case a.Cause == asdu.QOIStation.Cause():
			if stations[a.CommonAddr] == nil {
				stations[a.CommonAddr] = &tally{}
			}
			stations[a.CommonAddr].add(a)
		}
	}
	s, err := Dial(ctx, &cfg.ConnConfig, trace, Hooks{Handler: handler})
	if err != nil {
		return nil, err
	}
	defer func() { _ = s.Close() }()

	begin := time.Now()
	bc := asdu.CommonAddr(BroadcastCA(s.Params().CommonAddrSize))
	req := asdu.New(asdu.CauseActivation, bc, asdu.Interrogation{Qualifier: asdu.QOIStation})
	sctx, cancel := context.WithTimeout(ctx, s.Timeout)
	err = s.Send(sctx, req)
	cancel()
	if err != nil {
		return nil, WrapError(err)
	}
	mu.Lock()
	last = time.Now()
	mu.Unlock()
	deadline := time.Now().Add(s.Timeout)
	for {
		select {
		case <-ctx.Done():
			return nil, WrapError(ctx.Err())
		case <-s.Done():
			return nil, WrapError(fmt.Errorf("%w during the scan", iec101.ErrClosed))
		case <-time.After(50 * time.Millisecond):
		}
		mu.Lock()
		quiet := time.Since(last)
		mu.Unlock()
		if quiet >= ScanQuiet || time.Now().After(deadline) {
			break
		}
	}

	mu.Lock()
	defer mu.Unlock()
	for ca := range answered {
		if stations[ca] == nil {
			stations[ca] = &tally{}
		}
	}
	res := &types.ScanResult{Target: s.Target.String(), Method: ScanBroadcast, Probed: 1, DurationMs: ms(time.Since(begin)), Stations: []types.ScanStation{}}
	for _, ca := range slices.Sorted(maps.Keys(stations)) {
		res.Stations = append(res.Stations, stations[ca].station(ca))
	}
	res.Found = len(res.Stations)
	return res, nil
}

// pause waits d between the requests of a sweep.
func pause(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return nil
	}
	select {
	case <-ctx.Done():
		return WrapError(ctx.Err())
	case <-time.After(d):
		return nil
	}
}

// scanCA sends a general interrogation to every common address of the range. Addresses
// that are answered with "unknown common address", or not at all, are counted and not
// listed.
func scanCA(ctx context.Context, cfg *config.ScanConfig, trace, progress io.Writer) (*types.ScanResult, error) {
	s, err := Dial(ctx, &cfg.ConnConfig, trace, Hooks{})
	if err != nil {
		return nil, err
	}
	defer func() { _ = s.Close() }()

	begin := time.Now()
	res := &types.ScanResult{Target: s.Target.String(), Method: ScanCA, Stations: []types.ScanStation{}}
	delay := time.Duration(cfg.Delay) * time.Millisecond
	for v := uint32(cfg.Start); v <= uint32(cfg.End); v++ {
		ca := asdu.CommonAddr(v & 0xFFFF)
		if v != uint32(cfg.Start) {
			if err := pause(ctx, delay); err != nil {
				return nil, err
			}
		}
		res.Probed++
		rctx, cancel := context.WithTimeout(ctx, s.Timeout)
		data, err := s.Interrogate(rctx, ca, asdu.QOIStation)
		cancel()
		var st types.ScanStation
		neg, isNeg := errors.AsType[*telecontrol.NegativeError](err)
		switch {
		case err == nil:
			t := &tally{}
			for _, a := range data {
				t.add(a)
			}
			st = t.station(ca)
			res.Found++
		case isNeg && neg.Cause() == asdu.CauseUnknownCommonAddr:
			continue
		case isNeg:
			// Something answers for this address, and refuses the interrogation.
			st = types.ScanStation{CommonAddr: uint16(ca), Status: "refused", Error: EmbeddedErrorInfo(err)}
		case ctx.Err() != nil, Fatal(err):
			return nil, WrapError(err)
		default:
			continue // no answer in time: nothing there
		}
		res.Stations = append(res.Stations, st)
		if progress != nil {
			_, _ = fmt.Fprintf(progress, "scan: ca=%d %s\n", ca, st.Status)
		}
	}
	res.DurationMs = ms(time.Since(begin))
	return res, nil
}

// scanLink sends request status of link to every link address of the range, one at a
// time on one line, as a primary station polls a multidrop line. It changes nothing in the
// stations: request status of link is answered before any reset of the link.
func scanLink(ctx context.Context, cfg *config.ScanConfig, trace, progress io.Writer) (*types.LinkScanResult, error) {
	l, err := LinkParams(cfg.LinkConfig)
	if err != nil {
		return nil, err
	}
	target, rw, err := openLine(ctx, &cfg.ConnConfig)
	if err != nil {
		return nil, err
	}
	var tr *Tracer
	if cfg.Debug && trace != nil {
		tr = NewTracer(trace, l.Params)
	}
	r, err := openRaw(rw, l, l.Params.Link.Timeout, tr)
	if err != nil {
		_ = rw.Close()
		return nil, errs.WrapValidation(err)
	}
	defer func() { _ = r.Close() }()

	begin := time.Now()
	res := &types.LinkScanResult{Target: target.String(), Method: ScanLink, Stations: []types.LinkStation{}}
	delay := time.Duration(cfg.Delay) * time.Millisecond
	for v := uint32(cfg.Start); v <= uint32(cfg.End); v++ {
		addr := uint16(v & 0xFFFF)
		if v != uint32(cfg.Start) {
			if err := pause(ctx, delay); err != nil {
				return nil, err
			}
		}
		res.Probed++
		t0 := time.Now()
		st, err := r.status(ctx, addr)
		row := types.LinkStation{LinkAddress: addr, RTTMs: ms(time.Since(t0))}
		switch {
		case err == nil:
			row.Status, row.ACD, row.DFC = "answered", st.ACD, st.DFC
			res.Found++
		case ctx.Err() != nil:
			return nil, WrapError(ctx.Err())
		case errors.Is(err, link.ErrClosed):
			return nil, WrapError(err)
		case errors.Is(err, link.ErrNack), errors.Is(err, link.ErrNotFunctioning), errors.Is(err, link.ErrNotImplemented):
			row.Status, row.Error = "refused", EmbeddedErrorInfo(err)
			res.Found++
		default:
			continue // no answer: nothing there
		}
		res.Stations = append(res.Stations, row)
		if progress != nil {
			_, _ = fmt.Fprintf(progress, "scan: link=%d %s\n", addr, row.Status)
		}
	}
	res.DurationMs = ms(time.Since(begin))
	return res, nil
}
