// SPDX-License-Identifier: MIT

package iec

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/otfabric/go-iec60870-5/iec103"
	"github.com/otfabric/iec60870ctl/internal/errs"
	"github.com/otfabric/iec60870ctl/internal/iec103ctl/config"
	"github.com/otfabric/iec60870ctl/internal/iec103ctl/types"
)

// DefaultTransferTimeout bounds a disturbance data transfer when --transfer-timeout is 0.
const DefaultTransferTimeout = 2 * time.Minute

// functionType returns --fun, or the function type of the equipment's identification.
func (s *session) functionType(ctx context.Context, fun uint16) (iec103.FunctionType, error) {
	if fun != 0 {
		return iec103.FunctionType(fun & 0xFF), nil
	}
	wctx, cancel := context.WithTimeout(ctx, s.timeout())
	defer cancel()
	a := s.waitIdentification(wctx, 0)
	if ctx.Err() != nil {
		return 0, WrapError(ctx.Err())
	}
	if a == nil {
		return 0, errs.New(errs.KindTimeout, errs.CodeIEC103NoIdentification,
			fmt.Sprintf("timeout: %s sent no identification to take the function type from: set --fun", s.device()), nil)
	}
	return a.Fun, nil
}

func checkFun(fun uint16) error {
	if fun > 255 {
		return fmt.Errorf("--fun %d: use 1-255 (0 = the function type of the identification)", fun)
	}
	return nil
}

// CheckDisturbanceList validates `client disturbance list`.
func CheckDisturbanceList(cfg *config.DisturbanceListConfig) error {
	if _, err := CheckConn(&cfg.ConnConfig); err != nil {
		return err
	}
	return checkFun(cfg.Fun)
}

// DisturbanceList requests the list of recorded disturbances (ASDU 24, TOO 24; clause
// 7.4.7) and returns the faults of the ASDU 23 the equipment answers with.
func DisturbanceList(ctx context.Context, cfg *config.DisturbanceListConfig, stderr io.Writer) (*types.DisturbanceListResult, error) {
	if err := CheckDisturbanceList(cfg); err != nil {
		return nil, errs.WrapValidation(err)
	}
	s, err := open(ctx, &cfg.ConnConfig, sessionOptions{trace: traceOf(cfg.Debug, stderr)})
	if err != nil {
		return nil, err
	}
	defer s.close()
	fun, err := s.functionType(ctx, cfg.Fun)
	if err != nil {
		return nil, err
	}
	start := time.Now()
	var faults []iec103.RecordedFault
	if err := s.request(ctx, s.timeout(), func(rctx context.Context) error {
		var e error
		faults, e = s.c.DisturbanceList(rctx, fun)
		return e
	}); err != nil {
		return nil, err
	}
	res := &types.DisturbanceListResult{Device: s.device(), Fun: uint8(fun), DurationMs: since(start), Faults: []types.Fault{}}
	for _, f := range faults {
		res.Faults = append(res.Faults, FaultOf(f))
	}
	return res, nil
}

// checkOutputFile refuses an existing file without --force, before anything is sent.
func checkOutputFile(path string, force bool) error {
	if path == "" || force {
		return nil
	}
	if _, err := os.Stat(filepath.Clean(path)); err == nil {
		return errs.InvalidInput(errs.CodeOutputFileCreateFailed, fmt.Sprintf("%s exists: pass --force to overwrite it", path), nil)
	}
	return nil
}

// CheckDisturbanceGet validates `client disturbance get`.
func CheckDisturbanceGet(cfg *config.DisturbanceGetConfig) error {
	if _, err := CheckConn(&cfg.ConnConfig); err != nil {
		return err
	}
	if err := checkFun(cfg.Fun); err != nil {
		return err
	}
	if cfg.FAN > 0xFFFF {
		return fmt.Errorf("--fan %d: use 0-65535", cfg.FAN)
	}
	if cfg.Output != "" && cfg.Output == cfg.CSV {
		return fmt.Errorf("--output and --csv name the same file")
	}
	if err := checkOutputFile(cfg.Output, cfg.Force); err != nil {
		return err
	}
	return checkOutputFile(cfg.CSV, cfg.Force)
}

// DisturbanceGet transfers one recorded disturbance with the procedure of clause 7.4.7 and
// writes it to --output (JSON) and --csv when asked. The final positive acknowledgement
// allows the equipment to delete the record, so the files are created before anything is
// sent: a file that cannot be created ends the command with invalid input, and the record
// stays on the equipment. When the command fails after that, the files are removed.
func DisturbanceGet(ctx context.Context, cfg *config.DisturbanceGetConfig, stderr io.Writer) (*types.DisturbanceResult, error) {
	if err := CheckDisturbanceGet(cfg); err != nil {
		return nil, errs.WrapValidation(err)
	}
	out, err := createOutputs(cfg)
	if err != nil {
		return nil, err
	}
	written := false
	defer func() {
		if !written {
			out.remove()
		}
	}()
	s, err := open(ctx, &cfg.ConnConfig, sessionOptions{trace: traceOf(cfg.Debug, stderr)})
	if err != nil {
		return nil, err
	}
	defer s.close()
	fun, err := s.functionType(ctx, cfg.Fun)
	if err != nil {
		return nil, err
	}
	start := time.Now()
	var rec *iec103.Disturbance
	if err := s.request(ctx, Timeout(cfg.TransferTimeout, DefaultTransferTimeout), func(rctx context.Context) error {
		var e error
		rec, e = s.c.Disturbance(rctx, fun, uint16(cfg.FAN&0xFFFF))
		return e
	}); err != nil {
		return nil, err
	}
	res := &types.DisturbanceResult{Device: s.device(), Disturbance: DisturbanceOf(rec), DurationMs: since(start)}
	if out.json != nil {
		data, err := json.MarshalIndent(res.Disturbance, "", "  ")
		if err != nil {
			return nil, errs.Output(errs.CodeJSONEncodeFailed, err)
		}
		if err := out.json.write(append(data, '\n')); err != nil {
			return nil, err
		}
		res.Output = cfg.Output
	}
	if out.csv != nil {
		if err := out.csv.write(ChannelsCSV(&res.Disturbance)); err != nil {
			return nil, err
		}
		res.CSV = cfg.CSV
	}
	written = true
	return res, nil
}

// outputFile is a file of `client disturbance get`, created before the transfer.
type outputFile struct {
	path string
	f    *os.File
}

// outputs are the files of --output and --csv; nil where the flag is not set.
type outputs struct{ json, csv *outputFile }

// createOutputs creates the files of --output and --csv; when one of them cannot be
// created, none is left behind.
func createOutputs(cfg *config.DisturbanceGetConfig) (outputs, error) {
	var out outputs
	for _, f := range []struct {
		flag, path string
		to         **outputFile
	}{{"--output", cfg.Output, &out.json}, {"--csv", cfg.CSV, &out.csv}} {
		if f.path == "" {
			continue
		}
		file, err := createOutput(f.flag, f.path, cfg.Force)
		if err != nil {
			out.remove()
			return outputs{}, err
		}
		*f.to = file
	}
	return out, nil
}

// createOutput creates path: a new file, or with force an existing one emptied. A file that
// cannot be created is invalid input: nothing was sent.
func createOutput(flag, path string, force bool) (*outputFile, error) {
	flags := os.O_WRONLY | os.O_CREATE | os.O_EXCL
	if force {
		flags = os.O_WRONLY | os.O_CREATE | os.O_TRUNC
	}
	f, err := os.OpenFile(filepath.Clean(path), flags, 0o600)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			return nil, errs.InvalidInput(errs.CodeOutputFileCreateFailed, fmt.Sprintf("%s exists: pass --force to overwrite it", path), err)
		}
		return nil, errs.InvalidInput(errs.CodeOutputFileCreateFailed, fmt.Sprintf("%s: %v: nothing was requested", flag, err), err)
	}
	return &outputFile{path: path, f: f}, nil
}

// write writes data to the file and closes it.
func (o *outputFile) write(data []byte) error {
	f := o.f
	o.f = nil
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		return errs.Output(errs.CodeOutputFileWriteFailed, err)
	}
	if err := f.Close(); err != nil {
		return errs.Output(errs.CodeOutputFileWriteFailed, err)
	}
	return nil
}

// remove closes and removes the files.
func (out outputs) remove() {
	for _, o := range []*outputFile{out.json, out.csv} {
		if o == nil {
			continue
		}
		if o.f != nil {
			_ = o.f.Close()
		}
		_ = os.Remove(filepath.Clean(o.path))
	}
}

// ChannelsCSV renders the channel values of d: a header, then one row per value number
// with the time from the first value in microseconds (number times INT) and the raw SDV of
// every channel. A channel with fewer values leaves its cell empty.
func ChannelsCSV(d *types.Disturbance) []byte {
	var buf bytes.Buffer
	w := csv.NewWriter(&buf)
	head := []string{"index", "time_us"}
	n := 0
	for _, c := range d.Channels {
		head = append(head, fmt.Sprintf("acc%d_%s", c.ACC, c.Name))
		n = max(n, len(c.Values))
	}
	_ = w.Write(head)
	for i := range n {
		row := []string{strconv.Itoa(i), strconv.FormatInt(int64(i)*int64(d.IntervalUs), 10)}
		for _, c := range d.Channels {
			cell := ""
			if i < len(c.Values) {
				cell = strconv.Itoa(int(c.Values[i]))
			}
			row = append(row, cell)
		}
		_ = w.Write(row)
	}
	w.Flush()
	return buf.Bytes()
}
