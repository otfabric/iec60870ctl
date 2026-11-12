// SPDX-License-Identifier: MIT

package iec

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/otfabric/go-iec104/asdu"
	"github.com/otfabric/iec104ctl/internal/config"
	"github.com/otfabric/iec104ctl/internal/errs"
	"github.com/otfabric/iec104ctl/internal/types"
)

// MaxFileLength is the largest file the file transfer of IEC 60870-5-101/104 carries: the
// length of file is a 24-bit number.
const MaxFileLength = 1<<24 - 1

func checksum(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// CheckFileList validates the configuration of `client file list`.
func CheckFileList(cfg *config.FileListConfig) error {
	if err := Validate(&cfg.ConnConfig); err != nil {
		return err
	}
	if cfg.IOA > MaxIOA {
		return fmt.Errorf("invalid --ioa %d: the maximum is %d", cfg.IOA, MaxIOA)
	}
	return CheckCommonAddr(cfg.CommonAddr, false)
}

// FileList calls the file directory of a station.
func FileList(ctx context.Context, cfg *config.FileListConfig, trace io.Writer) (*types.FileListResult, error) {
	s, err := Dial(ctx, cfg.Conn(), trace)
	if err != nil {
		return nil, err
	}
	defer func() { _ = s.Close() }()
	dir, err := s.ListFiles(ctx, asdu.CommonAddr(cfg.CommonAddr), asdu.IOA(cfg.IOA))
	if err != nil {
		return nil, WrapError(err)
	}
	res := &types.FileListResult{Target: s.Target.String(), CommonAddr: cfg.CommonAddr, IOA: cfg.IOA, Count: len(dir), Entries: []types.FileEntry{}}
	for _, e := range dir {
		entry := types.FileEntry{
			IOA:       uint32(e.IOA),
			Name:      e.Name,
			Length:    e.Length,
			Directory: e.Status&asdu.FileIsDirectory != 0,
			Active:    e.Status&asdu.FileActive != 0,
		}
		if !e.Time.IsZero() {
			entry.Timestamp = e.Time.Format(TimeLayout)
		}
		res.Entries = append(res.Entries, entry)
	}
	return res, nil
}

// CheckFileGet validates the configuration of `client file get`.
func CheckFileGet(cfg *config.FileGetConfig) error {
	if err := Validate(&cfg.ConnConfig); err != nil {
		return err
	}
	if err := CheckCommonAddr(cfg.CommonAddr, false); err != nil {
		return err
	}
	if cfg.IOA == 0 || cfg.IOA > MaxIOA {
		return fmt.Errorf("invalid --ioa %d: the information object address of the file is required (1-%d)", cfg.IOA, MaxIOA)
	}
	if cfg.Output == "" {
		return fmt.Errorf("no output given: set --output to a file, or to - for stdout")
	}
	return nil
}

// FileGet fetches a file from a station and writes it to the output of cfg. With "-" the
// content goes to content and the result is not meant for the same stream.
func FileGet(ctx context.Context, cfg *config.FileGetConfig, trace, content io.Writer) (*types.FileTransferResult, error) {
	s, err := Dial(ctx, cfg.Conn(), trace)
	if err != nil {
		return nil, err
	}
	defer func() { _ = s.Close() }()
	begin := time.Now()
	data, err := s.GetFile(ctx, asdu.CommonAddr(cfg.CommonAddr), asdu.IOA(cfg.IOA), cfg.Name)
	if err != nil {
		return nil, WrapError(err)
	}
	res := &types.FileTransferResult{
		Target:     s.Target.String(),
		CommonAddr: cfg.CommonAddr,
		Direction:  "download",
		IOA:        cfg.IOA,
		Name:       cfg.Name,
		Length:     len(data),
		SHA256:     checksum(data),
		DurationMs: ms(time.Since(begin)),
	}
	if cfg.Output == "-" {
		if _, err := content.Write(data); err != nil {
			return nil, errs.Output(errs.CodeOutputWriteFailed, err)
		}
		return res, nil
	}
	if err := os.WriteFile(filepath.Clean(cfg.Output), data, 0o600); err != nil {
		return nil, errs.Output(errs.CodeOutputFileWriteFailed, err)
	}
	res.Path = cfg.Output
	return res, nil
}

// CheckFilePut validates the configuration of `client file put`, the input file included.
func CheckFilePut(cfg *config.FilePutConfig) error {
	if err := Validate(&cfg.ConnConfig); err != nil {
		return err
	}
	if err := CheckCommonAddr(cfg.CommonAddr, false); err != nil {
		return err
	}
	if cfg.IOA == 0 || cfg.IOA > MaxIOA {
		return fmt.Errorf("invalid --ioa %d: the information object address of the file is required (1-%d)", cfg.IOA, MaxIOA)
	}
	if cfg.Input == "" {
		return fmt.Errorf("no input given: set --input to the file to send")
	}
	info, err := os.Stat(cfg.Input)
	switch {
	case err != nil:
		return fmt.Errorf("input file: %w", err)
	case info.IsDir():
		return fmt.Errorf("input file %s is a directory", cfg.Input)
	case info.Size() == 0:
		return fmt.Errorf("input file %s is empty", cfg.Input)
	case info.Size() > MaxFileLength:
		return fmt.Errorf("input file %s has %d octets: the maximum is %d", cfg.Input, info.Size(), MaxFileLength)
	}
	if n := info.Size(); cfg.SectionSize != 0 && (n+int64(cfg.SectionSize)-1)/int64(cfg.SectionSize) > 255 {
		return fmt.Errorf("--section-size %d makes more than 255 sections of %d octets: use a larger section size", cfg.SectionSize, n)
	}
	return nil
}

// sections splits data into sections of size octets; size 0 makes one section.
func sections(data []byte, size int) [][]byte {
	if size <= 0 || size >= len(data) {
		return [][]byte{data}
	}
	var out [][]byte
	for len(data) > 0 {
		n := min(size, len(data))
		out = append(out, data[:n])
		data = data[n:]
	}
	return out
}

// FilePut delivers a file to a station. It is sent once: a transfer that fails is reported,
// not repeated.
func FilePut(ctx context.Context, cfg *config.FilePutConfig, trace io.Writer) (*types.FileTransferResult, error) {
	data, err := os.ReadFile(cfg.Input)
	if err != nil {
		return nil, errs.New(errs.KindInvalidInput, errs.CodeInputFileOpenFailed, fmt.Sprintf("input file: %v", err), err)
	}
	s, err := Dial(ctx, cfg.Conn(), trace)
	if err != nil {
		return nil, err
	}
	defer func() { _ = s.Close() }()
	parts := sections(data, int(cfg.SectionSize))
	begin := time.Now()
	if err := s.PutFile(ctx, asdu.CommonAddr(cfg.CommonAddr), asdu.IOA(cfg.IOA), cfg.Name, parts...); err != nil {
		return nil, WrapError(err)
	}
	return &types.FileTransferResult{
		Target:     s.Target.String(),
		CommonAddr: cfg.CommonAddr,
		Direction:  "upload",
		IOA:        cfg.IOA,
		Name:       cfg.Name,
		Length:     len(data),
		Sections:   len(parts),
		SHA256:     checksum(data),
		Path:       cfg.Input,
		DurationMs: ms(time.Since(begin)),
	}, nil
}
