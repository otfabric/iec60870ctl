// SPDX-License-Identifier: MIT

package station

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/otfabric/go-iec104/asdu"
	"github.com/otfabric/go-iec104/server"
)

// The length a directory reports fits the 24 bits of a length of file; what does not fit is
// reported as the largest length, never wrapped.
func TestFileLengthBounds(t *testing.T) {
	for n, want := range map[int]uint32{
		0:                      0,
		1:                      1,
		asdu.MaxFileLength - 1: asdu.MaxFileLength - 1,
		asdu.MaxFileLength:     asdu.MaxFileLength,
		asdu.MaxFileLength + 1: asdu.MaxFileLength,
		1 << 32:                asdu.MaxFileLength,
		1<<32 + 5:              asdu.MaxFileLength, // would wrap to 5 as a uint32
		-1:                     asdu.MaxFileLength,
	} {
		if got := fileLength(n); got != want {
			t.Errorf("fileLength(%d) = %d, want %d", n, got, want)
		}
	}
}

// The upload directory and what is delivered into it are the operator's: the directory is
// not open to others, a file not to group or others, whatever the umask.
func TestUploadsArePrivate(t *testing.T) {
	dir := t.TempDir()
	model := "station: {common_address: 3}\npoints: [{ioa: 1, type: M_SP_NA_1, value: true}]\n" +
		"uploads: {accept: true, dir: uploads/nested}\n"
	path := filepath.Join(dir, "station.yaml")
	if err := os.WriteFile(path, []byte(model), 0o600); err != nil {
		t.Fatal(err)
	}
	m, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	st, err := New(m, Options{})
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range []string{"uploads", "uploads/nested"} {
		info, err := os.Stat(filepath.Join(dir, d))
		if err != nil {
			t.Fatal(err)
		}
		if perm := info.Mode().Perm(); perm&0o007 != 0 {
			t.Errorf("%s has mode %v: others can access it", d, perm)
		}
	}
	if err := st.StoreFile(nil, 3, 77, 4, server.NewFile([]byte("delivered"), 4)); err != nil {
		t.Fatal(err)
	}
	stored := filepath.Join(dir, "uploads", "nested", "ioa-77-name-4.bin")
	info, err := os.Stat(stored)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm&0o077 != 0 {
		t.Errorf("a delivered file has mode %v: group or others can access it", perm)
	}
	if data, ok := st.Uploaded(77); !ok || string(data) != "delivered" {
		t.Errorf("Uploaded: %q, %v", data, ok)
	}
	// The directory listing reports the length of the delivered file.
	for _, e := range st.directory(nil, 3, 0) {
		if e.IOA == 77 && e.Length != 9 {
			t.Errorf("directory entry %+v", e)
		}
	}
}

// uploadStation is a station that takes uploads with the given limits, in dir when set.
func uploadStation(t *testing.T, limits, dir string) (*Station, string) {
	t.Helper()
	base := t.TempDir()
	u := "accept: true" + limits
	if dir != "" {
		u += ", dir: " + dir
	}
	path := filepath.Join(base, "station.yaml")
	model := "station: {common_address: 3}\npoints: [{ioa: 1, type: M_SP_NA_1, value: true}]\nuploads: {" + u + "}\n"
	if err := os.WriteFile(path, []byte(model), 0o600); err != nil {
		t.Fatal(err)
	}
	m, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	st, err := New(m, Options{})
	if err != nil {
		t.Fatal(err)
	}
	return st, base
}

func TestUploadDefaultLimits(t *testing.T) {
	st, _ := uploadStation(t, "", "")
	if !st.AcceptFile(nil, 3, 10, 1, DefaultUploadMaxFileSize) {
		t.Error("a file of the default maximum size was refused")
	}
	if st.AcceptFile(nil, 3, 10, 1, DefaultUploadMaxFileSize+1) {
		t.Error("a file above the default maximum size was accepted")
	}
	size := DefaultUploadMaxTotalSize / DefaultUploadMaxFiles
	for i := range DefaultUploadMaxFiles {
		if err := st.StoreFile(nil, 3, asdu.IOA(100+i), 1, server.NewFile(make([]byte, size), 2048)); err != nil {
			t.Fatalf("file %d: %v", i, err)
		}
	}
	if st.AcceptFile(nil, 3, 99, 1, 1) {
		t.Error("a file beyond the default number of files was accepted")
	}
	// Replacing a delivered file is not one more.
	if !st.AcceptFile(nil, 3, 100, 1, size) {
		t.Error("replacing a delivered file was refused")
	}
}

func TestUploadLimits(t *testing.T) {
	st, _ := uploadStation(t, ", max_files: 2, max_file_size: 10, max_total_size: 15", "")
	store := func(ioa asdu.IOA, n int) error {
		return st.StoreFile(nil, 3, ioa, 1, server.NewFile(make([]byte, n), 4))
	}
	if st.AcceptFile(nil, 3, 10, 1, 11) {
		t.Error("a file above max_file_size was accepted")
	}
	if err := store(10, 10); err != nil {
		t.Fatal(err)
	}
	if st.AcceptFile(nil, 3, 11, 1, 6) {
		t.Error("a file beyond max_total_size was accepted")
	}
	if err := store(11, 6); err == nil {
		t.Error("a file beyond max_total_size was stored")
	}
	if _, ok := st.Uploaded(11); ok {
		t.Error("a refused file is served")
	}
	if err := store(11, 5); err != nil {
		t.Fatal(err)
	}
	if st.AcceptFile(nil, 3, 12, 1, 0) {
		t.Error("a file beyond max_files was accepted")
	}
	// A replacement counts its new size, not both.
	if err := store(10, 10); err != nil {
		t.Errorf("replacing a file with one of the same size: %v", err)
	}
	if err := store(10, 1); err != nil {
		t.Fatal(err)
	}
	if err := store(11, 10); err != nil {
		t.Errorf("after shrinking a file the room is free again: %v", err)
	}
	// A refusal at the address of a point is still that.
	if st.AcceptFile(nil, 3, 1, 1, 1) {
		t.Error("a file at a point's address was accepted")
	}
}

// Uploads accepted together are each checked again when they are stored: only the ones
// that still fit are kept.
func TestUploadLimitsConcurrent(t *testing.T) {
	st, _ := uploadStation(t, ", max_files: 100, max_file_size: 10, max_total_size: 50", "")
	var wg sync.WaitGroup
	var stored atomic.Int32
	for i := range 40 {
		wg.Go(func() {
			if st.StoreFile(nil, 3, asdu.IOA(1000+i), 1, server.NewFile(make([]byte, 10), 4)) == nil {
				stored.Add(1)
			}
		})
	}
	wg.Wait()
	if n := stored.Load(); n != 5 {
		t.Errorf("%d files of 10 octets stored under max_total_size 50, want 5", n)
	}
}

// On disk a delivered file that replaces another under a new name removes the old one,
// so the directory stays within the limits.
func TestUploadReplacementOnDisk(t *testing.T) {
	st, base := uploadStation(t, ", max_files: 1, max_total_size: 64", "up")
	if err := st.StoreFile(nil, 3, 50, 1, server.NewFile([]byte("first"), 4)); err != nil {
		t.Fatal(err)
	}
	if err := st.StoreFile(nil, 3, 50, 2, server.NewFile([]byte("second"), 4)); err != nil {
		t.Fatal(err)
	}
	if err := st.StoreFile(nil, 3, 51, 1, server.NewFile([]byte("third"), 4)); err == nil {
		t.Error("a second address was stored under max_files 1")
	}
	entries, err := os.ReadDir(filepath.Join(base, "up"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "ioa-50-name-2.bin" {
		t.Errorf("upload directory: %v", entries)
	}
}

// A file that cannot be written leaves the limits as they were.
func TestUploadWriteFailureFreesItsPlace(t *testing.T) {
	st, base := uploadStation(t, ", max_files: 1", "up")
	dir := filepath.Join(base, "up")
	if err := os.Remove(dir); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dir, nil, 0o600); err != nil { // a file where the directory was
		t.Fatal(err)
	}
	if err := st.StoreFile(nil, 3, 60, 1, server.NewFile([]byte("x"), 4)); err == nil {
		t.Fatal("stored into a file instead of a directory")
	}
	if err := os.Remove(dir); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(dir, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := st.StoreFile(nil, 3, 61, 1, server.NewFile([]byte("y"), 4)); err != nil {
		t.Errorf("the failed file still holds its place: %v", err)
	}
}

func TestUploadLimitsValidation(t *testing.T) {
	for _, c := range []struct{ limits, want string }{
		{", max_files: -1", "cannot be negative"},
		{", max_file_size: -1", "cannot be negative"},
		{", max_total_size: -5", "cannot be negative"},
		{", max_file_size: 16777216, max_total_size: 33554432", "the maximum is 16777215"},
		{", max_file_size: 100, max_total_size: 50", "larger than uploads.max_total_size"},
		{", max_file_size: 33554432", "larger than"},
	} {
		dir := t.TempDir()
		path := filepath.Join(dir, "station.yaml")
		model := "station: {common_address: 3}\npoints: [{ioa: 1, type: M_SP_NA_1}]\nuploads: {accept: true" + c.limits + "}\n"
		if err := os.WriteFile(path, []byte(model), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := Load(path); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: %v, want %q", c.limits, err, c.want)
		}
	}
	// The largest file the protocol carries, with room for it.
	st, _ := uploadStation(t, ", max_file_size: 16777215, max_total_size: 33554430", "")
	if !st.AcceptFile(nil, 3, 10, 1, asdu.MaxFileLength) {
		t.Error("raised limits do not take the largest file")
	}
}
