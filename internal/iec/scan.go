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

	iec104 "github.com/otfabric/go-iec104"
	"github.com/otfabric/go-iec104/asdu"
	"github.com/otfabric/go-iec104/client"
	"github.com/otfabric/iec104ctl/internal/config"
	"github.com/otfabric/iec104ctl/internal/types"
)

// Scan methods.
const (
	ScanBroadcast = "broadcast"
	ScanSweep     = "sweep"
)

// ScanMethods lists the values of --method of `client scan`.
func ScanMethods() map[string]string {
	return map[string]string{
		ScanBroadcast: "one general interrogation to the broadcast address",
		ScanSweep:     "one general interrogation per common address",
	}
}

// scanQuiet is how long a broadcast scan waits after the last answer before it concludes
// that every station has finished.
const scanQuiet = 1500 * time.Millisecond

// CheckScan validates the configuration of `client scan`.
func CheckScan(cfg *config.ScanConfig) error {
	if err := Validate(&cfg.ConnConfig); err != nil {
		return err
	}
	if err := oneOf("method", cfg.Method, ScanBroadcast, ScanSweep); err != nil {
		return err
	}
	if cfg.Method == ScanSweep {
		if cfg.Start == 0 || cfg.End == uint16(asdu.Broadcast) || cfg.End < cfg.Start {
			return fmt.Errorf("invalid sweep range %d-%d: use --start and --end within 1-65534, the end not below the start", cfg.Start, cfg.End)
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

// Scan finds the stations (common addresses) behind one connection. progress receives a
// line per station found during a sweep.
func Scan(ctx context.Context, cfg *config.ScanConfig, trace, progress io.Writer) (*types.ScanResult, error) {
	if cfg.Method == ScanSweep {
		return scanSweep(ctx, cfg, trace, progress)
	}
	return scanBroadcast(ctx, cfg, trace)
}

// scanBroadcast sends one general interrogation to the broadcast address and lists every
// station that answers. It collects until the stations have been quiet for a while: the
// number of stations, and so of terminations, is what the scan is to find out.
func scanBroadcast(ctx context.Context, cfg *config.ScanConfig, trace io.Writer) (*types.ScanResult, error) {
	var (
		mu       sync.Mutex
		stations = map[asdu.CommonAddr]*tally{}
		answered = map[asdu.CommonAddr]bool{}
		last     = time.Now()
	)
	handler := client.HandlerFunc(func(a *asdu.ASDU) {
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
	})
	s, err := Dial(ctx, &cfg.ConnConfig, trace, client.WithHandler(handler))
	if err != nil {
		return nil, err
	}
	defer func() { _ = s.Close() }()

	begin := time.Now()
	req := asdu.New(asdu.CauseActivation, asdu.Broadcast, asdu.Interrogation{Qualifier: asdu.QOIStation})
	req.Originator = cfg.Originator
	if err := s.Send(ctx, req); err != nil {
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
		case <-time.After(50 * time.Millisecond):
		}
		if st := s.State(); st != iec104.StateStarted {
			return nil, WrapError(fmt.Errorf("%w during the scan", iec104.ErrConnectionLost))
		}
		mu.Lock()
		quiet := time.Since(last)
		mu.Unlock()
		if quiet >= scanQuiet || time.Now().After(deadline) {
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
	cas := slices.Sorted(maps.Keys(stations))
	res := &types.ScanResult{Target: s.Target.String(), Method: ScanBroadcast, Probed: 1, DurationMs: ms(time.Since(begin)), Stations: []types.ScanStation{}}
	for _, ca := range cas {
		res.Stations = append(res.Stations, stations[ca].station(ca))
	}
	res.Found = len(res.Stations)
	return res, nil
}

// scanSweep sends a general interrogation to every common address of the range. Addresses
// that are answered with "unknown common address", or not at all, are counted and not
// listed.
func scanSweep(ctx context.Context, cfg *config.ScanConfig, trace, progress io.Writer) (*types.ScanResult, error) {
	s, err := Dial(ctx, &cfg.ConnConfig, trace)
	if err != nil {
		return nil, err
	}
	defer func() { _ = s.Close() }()

	begin := time.Now()
	res := &types.ScanResult{Target: s.Target.String(), Method: ScanSweep, Stations: []types.ScanStation{}}
	delay := time.Duration(cfg.Delay) * time.Millisecond
	for v := uint32(cfg.Start); v <= uint32(cfg.End); v++ {
		ca := asdu.CommonAddr(v & 0xFFFF)
		if v != uint32(cfg.Start) && delay > 0 {
			select {
			case <-ctx.Done():
				return nil, WrapError(ctx.Err())
			case <-time.After(delay):
			}
		}
		res.Probed++
		data, err := s.Interrogate(ctx, ca, asdu.QOIStation)
		var st types.ScanStation
		var neg *iec104.NegativeError
		switch {
		case err == nil:
			t := &tally{}
			for _, a := range data {
				t.add(a)
			}
			st = t.station(ca)
			res.Found++
		case errors.As(err, &neg) && neg.Cause() == asdu.CauseUnknownCommonAddr:
			continue
		case errors.As(err, &neg):
			// Something answers for this address, and refuses the interrogation.
			st = types.ScanStation{CommonAddr: uint16(ca), Status: "refused", Error: EmbeddedErrorInfo(err)}
		case Fatal(err):
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
