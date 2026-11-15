// SPDX-License-Identifier: MIT

package station

import (
	"math"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

// allocated returns the octets f allocates.
func allocated(f func()) uint64 {
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	f()
	runtime.ReadMemStats(&after)
	return after.TotalAlloc - before.TotalAlloc
}

// allocationBudget is far below the smallest size checked against it (maxIOA).
const allocationBudget = 1 << 20

func filesModel(files string) *Model {
	m, err := Parse([]byte("station: {common_address: 3}\npoints: [{ioa: 1, type: M_SP_NA_1, value: true}]\nfiles:\n" + files))
	if err != nil {
		panic(err)
	}
	return m
}

// The size of a file of the model is checked from the model before anything is made: the
// largest file the protocol carries is accepted, and Validate allocates nothing like it; a
// larger size is refused, also one that an int barely holds.
func TestModelFileSizes(t *testing.T) {
	for _, c := range []struct {
		size int
		ok   bool
	}{
		{1, true},
		{maxIOA - 1, true},
		{maxIOA, true},
		{maxIOA + 1, false},
		{math.MaxInt32, false},
		{math.MaxInt, false},
	} {
		m := filesModel("  - {ioa: 10, name: 1, size: " + strconv.Itoa(c.size) + ", section_size: 100000}\n")
		var err error
		if n := allocated(func() { err = m.Validate() }); n > allocationBudget {
			t.Errorf("size %d: Validate allocated %d octets", c.size, n)
		}
		if (err == nil) != c.ok {
			t.Errorf("size %d: %v, want ok %v", c.size, err, c.ok)
		}
		if !c.ok && (err == nil || !strings.Contains(err.Error(), "the maximum is 16777215")) {
			t.Errorf("size %d: %v, want the maximum named", c.size, err)
		}
	}
	// New makes the file it serves, once.
	st, err := New(filesModel("  - {ioa: 10, name: 1, size: 5000}\n"), Options{})
	if err != nil {
		t.Fatal(err)
	}
	if data, ok := st.Uploaded(10); !ok || len(data) != 5000 || data[26] != 'A' {
		t.Errorf("the file of the model: %d octets, %v", len(data), ok)
	}
	// fileContent does not make a size beyond the limit, whoever calls it.
	for _, size := range []int{-1, maxIOA + 1, math.MaxInt} {
		if _, err := m0().fileContent(FileSpec{Size: size}); err == nil {
			t.Errorf("fileContent made a file of size %d", size)
		}
	}
}

func m0() *Model { return &Model{} }

// A file the model names by path is checked by what the file system reports: a regular
// file of at most maxIOA octets. Validate reads none of it; New reads it once and no more
// than one octet beyond the limit.
func TestModelFilePaths(t *testing.T) {
	dir := t.TempDir()
	write := func(name string, size int64) {
		t.Helper()
		f, err := os.Create(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		if err := f.Truncate(size); err != nil {
			t.Fatal(err)
		}
		if err := f.Close(); err != nil {
			t.Fatal(err)
		}
	}
	write("largest.bin", maxIOA)
	write("larger.bin", maxIOA+1)
	write("empty.bin", 0)
	if err := os.Mkdir(filepath.Join(dir, "sub"), 0o750); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		path, want string
	}{
		{"largest.bin", ""},
		{"larger.bin", "the maximum is 16777215"},
		{"empty.bin", "the file is empty"},
		{"sub", "is not a regular file"},
		{"missing.bin", "missing.bin"},
	} {
		m := filesModel("  - {ioa: 10, name: 1, path: " + c.path + ", section_size: 100000}\n")
		m.dir = dir
		var err error
		if n := allocated(func() { err = m.Validate() }); n > allocationBudget {
			t.Errorf("%s: Validate allocated %d octets", c.path, n)
		}
		switch {
		case c.want == "" && err != nil:
			t.Errorf("%s: %v", c.path, err)
		case c.want != "" && (err == nil || !strings.Contains(err.Error(), c.want)):
			t.Errorf("%s: %v, want %q", c.path, err, c.want)
		}
	}
	// fileContent, too, refuses a file beyond the limit from its size, before it reads it.
	m := &Model{dir: dir}
	var err error
	if n := allocated(func() { _, err = m.fileContent(FileSpec{Path: "larger.bin"}) }); err == nil || n > 3*maxIOA {
		t.Errorf("fileContent of a file beyond the limit: %v, %d octets allocated", err, n)
	}
	if _, err := m.fileContent(FileSpec{Path: "sub"}); err == nil {
		t.Error("fileContent read a directory")
	}
	data, err := m.fileContent(FileSpec{Path: "largest.bin"})
	if err != nil || len(data) != maxIOA {
		t.Errorf("fileContent of the largest file: %d octets, %v", len(data), err)
	}
}

// A model file is read up to MaxModelSize octets; a larger one is refused for its size.
func TestModelFileLimit(t *testing.T) {
	dir := t.TempDir()
	for _, c := range []struct {
		size  int64
		large bool
	}{
		{MaxModelSize - 1, false},
		{MaxModelSize, false},
		{MaxModelSize + 1, true},
		{4 * MaxModelSize, true},
	} {
		path := filepath.Join(dir, "station.yaml")
		f, err := os.Create(path)
		if err != nil {
			t.Fatal(err)
		}
		// A comment of that size: a model that parses, but has no station.
		if _, err := f.WriteString("#"); err != nil {
			t.Fatal(err)
		}
		if err := f.Truncate(c.size); err != nil {
			t.Fatal(err)
		}
		if err := f.Close(); err != nil {
			t.Fatal(err)
		}
		var lerr error
		n := allocated(func() { _, lerr = Load(path) })
		if lerr == nil {
			t.Fatalf("a model of %d octets loaded", c.size)
		}
		if got := strings.Contains(lerr.Error(), "larger than 16777216 octets"); got != c.large {
			t.Errorf("a model of %d octets: %v", c.size, lerr)
		}
		if c.large && n > 3*MaxModelSize {
			t.Errorf("a model of %d octets: Load allocated %d octets", c.size, n)
		}
	}
}
