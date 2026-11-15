// SPDX-License-Identifier: MIT

//go:build unix

package station

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// A path that names a named pipe is refused from what the file system reports: neither
// Validate nor New waits for a writer.
func TestModelFileFIFO(t *testing.T) {
	dir := t.TempDir()
	if err := syscall.Mkfifo(filepath.Join(dir, "pipe"), 0o600); err != nil {
		t.Fatal(err)
	}
	m := filesModel("  - {ioa: 10, name: 1, path: pipe}\n")
	m.dir = dir
	done := make(chan error, 2)
	go func() { done <- m.Validate() }()
	go func() { _, err := m.fileContent(m.Files[0]); done <- err }()
	for range 2 {
		select {
		case err := <-done:
			if err == nil || !strings.Contains(err.Error(), "is not a regular file") {
				t.Errorf("a named pipe: %v", err)
			}
		case <-time.After(10 * time.Second):
			t.Fatal("still waiting on the named pipe after 10 s")
		}
	}
}

// A model read from a named pipe is read up to one octet beyond MaxModelSize and then
// refused.
func TestModelFromAPipe(t *testing.T) {
	path := filepath.Join(t.TempDir(), "model")
	if err := syscall.Mkfifo(path, 0o600); err != nil {
		t.Fatal(err)
	}
	go func() {
		f, err := os.OpenFile(path, os.O_WRONLY, 0)
		if err != nil {
			return
		}
		defer func() { _ = f.Close() }()
		chunk := make([]byte, 1<<20)
		for range 2 * MaxModelSize / len(chunk) {
			if _, err := f.Write(chunk); err != nil {
				return // the reader has stopped
			}
		}
	}()
	if _, err := Load(path); err == nil || !strings.Contains(err.Error(), "larger than 16777216 octets") {
		t.Errorf("a model of more than %d octets from a pipe: %v", MaxModelSize, err)
	}
}
