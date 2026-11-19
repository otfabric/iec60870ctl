// SPDX-License-Identifier: MIT

package iec

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/otfabric/go-iec60870-5/iec103"
	"github.com/otfabric/go-iec60870-5/iec103/client"
	"github.com/otfabric/iec60870ctl/internal/errs"
	"github.com/otfabric/iec60870ctl/internal/iec103ctl/config"
	"github.com/otfabric/iec60870ctl/internal/iec103ctl/types"
)

// Generic services of `client generic`.
const (
	GenericHeadings    = "headings"
	GenericRead        = "read"
	GenericEntry       = "entry"
	GenericGroup       = "group"
	GenericDirectory   = "directory"
	GenericInterrogate = "interrogate"
	GenericWrite       = "write"
)

// Procedures of `client generic write`.
const (
	WriteExecute = "execute"
	WriteAbort   = "abort"
)

// WriteModes describes the values of --mode of `client generic write`.
func WriteModes() map[string]string {
	return map[string]string{
		WriteExecute: "write with execution (INF 250): the device activates the value",
		WriteAbort:   "write entry (INF 248), then write abort (INF 251): nothing is activated",
	}
}

// genericResult renders the answer to a generic service.
func genericResult(s *session, service string, inf iec103.InfoNumber, r *client.GenericResult, start time.Time) *types.GenericResult {
	res := &types.GenericResult{Device: s.device(), Service: service, Inf: uint8(inf), Sets: []types.DataSet{}, DurationMs: since(start)}
	if r == nil {
		return res
	}
	res.Cause, res.CauseName = uint8(r.Cause), CauseName(r.Cause)
	for _, d := range r.Sets {
		res.Sets = append(res.Sets, DataSetOf(d))
	}
	if len(r.Elements) > 0 {
		res.GIN = GINText(r.GIN)
		for _, e := range r.Elements {
			res.Elements = append(res.Elements, DescElementOf(e))
		}
	}
	return res
}

// InvalidData reports a generic read answered with cause 43 (invalid data): the result was
// written, and the command ends with the partial exit code.
func InvalidData(v any) int {
	if r, ok := v.(*types.GenericResult); ok && r.Cause == uint8(iec103.CauseGenericReadInvalid) {
		return errs.ExitPartial
	}
	return errs.ExitOK
}

// runGeneric opens a session, runs one generic service and renders the answer.
func runGeneric(ctx context.Context, cfg *config.ConnConfig, trace io.Writer, service string, inf iec103.InfoNumber,
	fn func(context.Context, *client.Client) (*client.GenericResult, error)) (*types.GenericResult, error) {
	s, err := open(ctx, cfg, sessionOptions{trace: trace})
	if err != nil {
		return nil, err
	}
	defer s.close()
	start := time.Now()
	var r *client.GenericResult
	if err := s.request(ctx, s.timeout(), func(rctx context.Context) error {
		var e error
		r, e = fn(rctx, s.c)
		return e
	}); err != nil {
		return nil, err
	}
	return genericResult(s, service, inf, r, start), nil
}

// CheckGeneric validates `client generic headings` and `client generic interrogate`.
func CheckGeneric(cfg *config.GenericConfig) error {
	_, err := CheckConn(&cfg.ConnConfig)
	return err
}

// Headings reads the headings of all groups (INF 240, clause 7.4.8.5.1).
func Headings(ctx context.Context, cfg *config.GenericConfig, stderr io.Writer) (*types.GenericResult, error) {
	return runGeneric(ctx, &cfg.ConnConfig, traceOf(cfg.Debug, stderr), GenericHeadings, iec103.InfReadHeadings,
		func(rctx context.Context, c *client.Client) (*client.GenericResult, error) {
			return c.ReadHeadings(rctx)
		})
}

// GenericGI runs the general interrogation of generic data (INF 245, clause 7.4.8.7).
func GenericGI(ctx context.Context, cfg *config.GenericConfig, stderr io.Writer) (*types.GenericResult, error) {
	return runGeneric(ctx, &cfg.ConnConfig, traceOf(cfg.Debug, stderr), GenericInterrogate, iec103.InfGenericGI,
		func(rctx context.Context, c *client.Client) (*client.GenericResult, error) {
			sets, err := c.GenericInterrogation(rctx)
			if err != nil {
				return nil, err
			}
			return &client.GenericResult{Cause: iec103.CauseGITermination, Sets: sets}, nil
		})
}

// readInfs are the information numbers of a generic read (table 18).
var readInfs = map[uint16]bool{240: true, 241: true, 243: true, 244: true}

func checkGenericRead(cfg *config.GenericReadConfig) ([]iec103.Item, error) {
	if _, err := CheckConn(&cfg.ConnConfig); err != nil {
		return nil, err
	}
	if !readInfs[cfg.Inf] {
		return nil, fmt.Errorf("--inf %d: a generic read is INF 240, 241, 243 or 244 (table 18)", cfg.Inf)
	}
	items, err := ParseItems(cfg.Items)
	if err != nil {
		return nil, err
	}
	if len(items) == 0 && cfg.Inf != 240 {
		return nil, fmt.Errorf("--items is required with --inf %d", cfg.Inf)
	}
	return items, nil
}

// CheckGenericRead validates `client generic read`.
func CheckGenericRead(cfg *config.GenericReadConfig) error {
	_, err := checkGenericRead(cfg)
	return err
}

// GenericReadItems sends a generic read command with the information number and the
// generic identifiers of cfg (clause 7.4.8.5, 7.4.8.6).
func GenericReadItems(ctx context.Context, cfg *config.GenericReadConfig, stderr io.Writer) (*types.GenericResult, error) {
	items, err := checkGenericRead(cfg)
	if err != nil {
		return nil, errs.WrapValidation(err)
	}
	inf := iec103.InfoNumber(cfg.Inf & 0xFF)
	return runGeneric(ctx, &cfg.ConnConfig, traceOf(cfg.Debug, stderr), GenericRead, inf,
		func(rctx context.Context, c *client.Client) (*client.GenericResult, error) {
			return c.GenericRead(rctx, inf, items...)
		})
}

func checkEntry(cfg *config.GenericEntryConfig, def iec103.KOD) (iec103.GIN, iec103.KOD, error) {
	if _, err := CheckConn(&cfg.ConnConfig); err != nil {
		return iec103.GIN{}, 0, err
	}
	gin, err := ParseGIN(cfg.GIN)
	if err != nil {
		return gin, 0, err
	}
	kod, err := ParseKOD(cfg.KOD, def)
	return gin, kod, err
}

// CheckGenericEntry validates `client generic entry` and `client generic directory`.
func CheckGenericEntry(cfg *config.GenericEntryConfig) error {
	_, _, err := checkEntry(cfg, iec103.KODActualValue)
	return err
}

// GenericReadEntry reads attribute --kod of entry --gin (INF 244, clause 7.4.8.6.2).
func GenericReadEntry(ctx context.Context, cfg *config.GenericEntryConfig, stderr io.Writer) (*types.GenericResult, error) {
	gin, kod, err := checkEntry(cfg, iec103.KODActualValue)
	if err != nil {
		return nil, errs.WrapValidation(err)
	}
	return runGeneric(ctx, &cfg.ConnConfig, traceOf(cfg.Debug, stderr), GenericEntry, iec103.InfReadEntry,
		func(rctx context.Context, c *client.Client) (*client.GenericResult, error) {
			return c.ReadEntry(rctx, gin, kod)
		})
}

// GenericReadDirectory reads every attribute of entry --gin (INF 243, clause 7.4.8.6.1).
func GenericReadDirectory(ctx context.Context, cfg *config.GenericEntryConfig, stderr io.Writer) (*types.GenericResult, error) {
	gin, _, err := checkEntry(cfg, iec103.KODActualValue)
	if err != nil {
		return nil, errs.WrapValidation(err)
	}
	return runGeneric(ctx, &cfg.ConnConfig, traceOf(cfg.Debug, stderr), GenericDirectory, iec103.InfReadDirectory,
		func(rctx context.Context, c *client.Client) (*client.GenericResult, error) {
			return c.ReadDirectory(rctx, gin)
		})
}

func checkGroup(cfg *config.GenericGroupConfig) (iec103.KOD, error) {
	if _, err := CheckConn(&cfg.ConnConfig); err != nil {
		return 0, err
	}
	if cfg.Group > 255 {
		return 0, fmt.Errorf("--group %d: use 0-255", cfg.Group)
	}
	return ParseKOD(cfg.KOD, iec103.KODActualValue)
}

// CheckGenericGroup validates `client generic group`.
func CheckGenericGroup(cfg *config.GenericGroupConfig) error {
	_, err := checkGroup(cfg)
	return err
}

// GenericReadGroup reads attribute --kod of every entry of --group (INF 241, clause
// 7.4.8.5.2).
func GenericReadGroup(ctx context.Context, cfg *config.GenericGroupConfig, stderr io.Writer) (*types.GenericResult, error) {
	kod, err := checkGroup(cfg)
	if err != nil {
		return nil, errs.WrapValidation(err)
	}
	group := uint8(cfg.Group & 0xFF)
	return runGeneric(ctx, &cfg.ConnConfig, traceOf(cfg.Debug, stderr), GenericGroup, iec103.InfReadGroup,
		func(rctx context.Context, c *client.Client) (*client.GenericResult, error) {
			return c.ReadGroup(rctx, group, kod)
		})
}

// checkWrite validates `client generic write` and returns the data sets it writes: those
// of --data, or the one of --gin, --kod, --type, --size and --value.
func checkWrite(cfg *config.GenericWriteConfig) ([]iec103.DataSet, error) {
	if _, err := CheckConn(&cfg.ConnConfig); err != nil {
		return nil, err
	}
	switch strings.ToLower(strings.TrimSpace(cfg.Mode)) {
	case WriteExecute, WriteAbort:
	default:
		return nil, fmt.Errorf("invalid --mode %q: use execute or abort", cfg.Mode)
	}
	if len(cfg.Data) == 0 {
		set, err := checkWriteFlags(cfg)
		if err != nil {
			return nil, err
		}
		return []iec103.DataSet{set}, nil
	}
	if cfg.GIN != "" || cfg.Type != "" || cfg.Value != "" || cfg.Size != 0 {
		return nil, errs.InvalidInput(errs.CodeInvalidFlagCombination, "--data cannot be used with --gin, --type, --size or --value: give every data set as --data", nil)
	}
	if len(cfg.Data) > MaxDataSets {
		return nil, fmt.Errorf("%d data sets: one write takes at most %d", len(cfg.Data), MaxDataSets)
	}
	sets := make([]iec103.DataSet, 0, len(cfg.Data))
	for _, d := range cfg.Data {
		set, err := ParseDataSet(d)
		if err != nil {
			return nil, err
		}
		sets = append(sets, set)
	}
	return sets, nil
}

// checkWriteFlags returns the data set of --gin, --kod, --type, --size and --value.
func checkWriteFlags(cfg *config.GenericWriteConfig) (iec103.DataSet, error) {
	gin, err := ParseGIN(cfg.GIN)
	if err != nil {
		return iec103.DataSet{}, err
	}
	kod, err := ParseKOD(cfg.KOD, iec103.KODActualValue)
	if err != nil {
		return iec103.DataSet{}, err
	}
	t, err := ParseDataType(cfg.Type)
	if err != nil {
		return iec103.DataSet{}, err
	}
	v, err := ParseValue(t, cfg.Size, cfg.Value)
	if err != nil {
		return iec103.DataSet{}, err
	}
	return iec103.DataSet{GIN: gin, KOD: kod, Value: v}, nil
}

// CheckGenericWrite validates `client generic write`.
func CheckGenericWrite(cfg *config.GenericWriteConfig) error {
	_, err := checkWrite(cfg)
	return err
}

// GenericWriteSet writes the data sets (clause 7.4.8.6.3, figure 17), once: with execution
// (INF 250), or staged with write entry (INF 248) and then aborted (INF 251). The abort is
// also sent when the write entry failed, on a context of its own: the link layer may still
// deliver the write entry. When the abort was not acknowledged, the error says that the
// value may still be staged on the device.
func GenericWriteSet(ctx context.Context, cfg *config.GenericWriteConfig, stderr io.Writer) (*types.GenericResult, error) {
	sets, err := checkWrite(cfg)
	if err != nil {
		return nil, errs.WrapValidation(err)
	}
	mode := strings.ToLower(strings.TrimSpace(cfg.Mode))
	s, err := open(ctx, &cfg.ConnConfig, sessionOptions{trace: traceOf(cfg.Debug, stderr)})
	if err != nil {
		return nil, err
	}
	defer s.close()
	start := time.Now()
	inf := iec103.InfWriteExecute
	var r *client.GenericResult
	staging := false // --mode abort ended with an error: is the value still staged?
	var abortErr error
	err = s.request(ctx, s.timeout(), func(rctx context.Context) error {
		w, e := s.c.BeginWrite()
		if e != nil {
			return e
		}
		if mode == WriteAbort {
			inf = iec103.InfWriteAbort
			if e := w.Stage(rctx, sets...); e != nil {
				// The link layer still delivers a write entry whose confirmation rctx did
				// not wait for: the abort gets a context of its own, so that INF 251 is sent.
				actx, cancel := context.WithTimeout(context.WithoutCancel(ctx), s.timeout())
				defer cancel()
				staging = true
				_, abortErr = s.abortWrite(actx, w, false)
				return e
			}
			r, e = s.abortWrite(rctx, w, true)
			staging, abortErr = e != nil, e
			return e
		}
		r, e = w.Execute(rctx, sets...)
		return e
	})
	res := genericResult(s, GenericWrite, inf, r, start)
	res.Mode = mode
	if err != nil {
		if e, ok := errs.As(err); ok {
			if errors.Is(err, iec103.ErrNegative) && r != nil {
				var codes []string
				for _, d := range res.Sets {
					codes = append(codes, d.GIN+" "+d.Reply)
				}
				e.Message = fmt.Sprintf("the device refused the write (cause %d, %s): %s", res.Cause, res.CauseName, strings.Join(codes, ", "))
			}
			switch {
			case !staging:
			case abortErr != nil:
				e.Message += ": the write may still be staged on the device: the write abort (INF 251) was not acknowledged"
			default:
				e.Message += ": the write was aborted (INF 251 acknowledged)"
			}
		}
		return nil, err
	}
	return res, nil
}

// abortWrite ends w with write abort (INF 251). When ctx ends before the device answered,
// INF 251 is sent once more with the RII of w, on a context of its own bounded by
// --timeout, and the error of the first abort is returned: a write the device staged ends
// only with an abort or with the line. The second abort is the same ASDU again, with the
// COUNT bit of NGD (clause 7.2.6.30) the client gave the first: set when the write entry
// before it was confirmed (staged).
func (s *session) abortWrite(ctx context.Context, w *client.StagedWrite, staged bool) (*client.GenericResult, error) {
	r, err := w.Abort(ctx)
	if !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
		return r, err
	}
	actx, cancel := context.WithTimeout(context.WithoutCancel(ctx), s.timeout())
	defer cancel()
	_ = s.c.Send(actx, iec103.New(iec103.CauseGenericWrite, s.commonAddr(), iec103.FunGeneric, iec103.InfWriteAbort,
		iec103.GenericData{RII: w.RII(), NGD: iec103.Counted{Count: staged}}))
	return r, err
}
