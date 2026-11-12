// SPDX-License-Identifier: MIT

// Package station is the simulated controlled station behind `iec104ctl server start`: a
// model of points, commands and files read from YAML or JSON, and the station that serves
// it. The tests of the client commands run against it as well.
package station

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/otfabric/go-iec104/asdu"
	"gopkg.in/yaml.v3"
)

// Duration is a time.Duration that is written as a string ("5s", "500ms") in a model.
type Duration time.Duration

// UnmarshalYAML implements yaml.Unmarshaler.
func (d *Duration) UnmarshalYAML(n *yaml.Node) error {
	var s string
	if err := n.Decode(&s); err != nil {
		return fmt.Errorf("line %d: a duration is written as a string such as 5s or 500ms", n.Line)
	}
	v, err := time.ParseDuration(strings.TrimSpace(s))
	if err != nil || v < 0 {
		return fmt.Errorf("line %d: invalid duration %q: use a form such as 5s or 500ms", n.Line, s)
	}
	*d = Duration(v)
	return nil
}

// MarshalYAML implements yaml.Marshaler.
func (d Duration) MarshalYAML() (any, error) { return time.Duration(d).String(), nil }

// MarshalJSON implements json.Marshaler.
func (d Duration) MarshalJSON() ([]byte, error) { return json.Marshal(time.Duration(d).String()) }

// Model describes a station.
type Model struct {
	Station  StationSpec   `yaml:"station" json:"station"`
	Points   []PointSpec   `yaml:"points" json:"points"`
	Commands []CommandSpec `yaml:"commands,omitempty" json:"commands,omitempty"`
	Files    []FileSpec    `yaml:"files,omitempty" json:"files,omitempty"`
	Uploads  UploadSpec    `yaml:"uploads,omitempty" json:"uploads"`

	// dir is the directory of the model file: relative paths in the model start there.
	dir string
}

// StationSpec holds what concerns the station as a whole.
type StationSpec struct {
	// Name is shown in the event log.
	Name string `yaml:"name,omitempty" json:"name,omitempty"`
	// CommonAddress is the common address of ASDU of the station, 1-65534.
	CommonAddress uint16 `yaml:"common_address" json:"common_address"`
	// CyclicInterval, when not zero, makes the station send its measured values with cause
	// "periodic" at this interval.
	CyclicInterval Duration `yaml:"cyclic_interval,omitempty" json:"cyclic_interval,omitempty"`
}

// PointSpec is one information object in monitor direction.
type PointSpec struct {
	IOA uint32 `yaml:"ioa" json:"ioa"`
	// Type is the type identification without time tag: M_SP_NA_1, M_DP_NA_1, M_ST_NA_1,
	// M_BO_NA_1, M_ME_NA_1, M_ME_NB_1, M_ME_NC_1 or M_IT_NA_1.
	Type string `yaml:"type" json:"type"`
	// Name describes the point. It is not sent: IEC 60870-5-104 carries no names.
	Name string `yaml:"name,omitempty" json:"name,omitempty"`
	// Value is the initial value: true/false (single point), off/on/intermediate/
	// indeterminate (double point) or a number.
	Value any `yaml:"value" json:"value"`
	// Quality lists the quality flags that are set: OV, BL, SB, NT, IV.
	Quality []string `yaml:"quality,omitempty" json:"quality,omitempty"`
	// Group is the interrogation group 1-16 of the point, or the counter group 1-4 of an
	// integrated total; 0 for none. A general interrogation returns every point.
	Group uint8 `yaml:"group,omitempty" json:"group,omitempty"`
	// TimeTag makes the station report changes of the point with a time tag (the
	// CP56Time2a type, for example M_SP_TB_1). Default true.
	TimeTag *bool `yaml:"time_tag,omitempty" json:"time_tag,omitempty"`
	// Simulate makes the value change by itself.
	Simulate *SimSpec `yaml:"simulate,omitempty" json:"simulate,omitempty"`
}

// SimSpec describes how a value changes by itself.
type SimSpec struct {
	// Mode is toggle (states), ramp (up to max, then back to min), sine, random (uniform
	// between min and max) or count (adds step, for counters).
	Mode     string   `yaml:"mode" json:"mode"`
	Interval Duration `yaml:"interval" json:"interval"`
	Min      float64  `yaml:"min,omitempty" json:"min,omitempty"`
	Max      float64  `yaml:"max,omitempty" json:"max,omitempty"`
	// Step is the increment of ramp and count (default 1), and the period of sine in
	// intervals (default 60).
	Step float64 `yaml:"step,omitempty" json:"step,omitempty"`
}

// CommandSpec is one information object in control direction.
type CommandSpec struct {
	IOA uint32 `yaml:"ioa" json:"ioa"`
	// Type is the command type without time tag: C_SC_NA_1, C_DC_NA_1, C_RC_NA_1,
	// C_SE_NA_1, C_SE_NB_1, C_SE_NC_1 or C_BO_NA_1. The time-tagged variant is accepted too.
	Type string `yaml:"type" json:"type"`
	Name string `yaml:"name,omitempty" json:"name,omitempty"`
	// Target is the address of the point that takes the commanded value; 0 for a command
	// that is only confirmed.
	Target uint32 `yaml:"target,omitempty" json:"target,omitempty"`
	// SelectRequired refuses an execute that was not preceded by a select.
	SelectRequired bool `yaml:"select_required,omitempty" json:"select_required,omitempty"`
	// SelectTimeout is how long a selection lasts. Default 30s.
	SelectTimeout Duration `yaml:"select_timeout,omitempty" json:"select_timeout,omitempty"`
}

// FileSpec is one file the station offers.
type FileSpec struct {
	IOA  uint32 `yaml:"ioa" json:"ioa"`
	Name uint16 `yaml:"name" json:"name"`
	// One of Path (a file, relative to the model file), Content (text) or Size (that many
	// octets of a repeating pattern).
	Path    string `yaml:"path,omitempty" json:"path,omitempty"`
	Content string `yaml:"content,omitempty" json:"content,omitempty"`
	Size    int    `yaml:"size,omitempty" json:"size,omitempty"`
	// SectionSize is the number of octets per section. Default 2048.
	SectionSize int `yaml:"section_size,omitempty" json:"section_size,omitempty"`
}

// UploadSpec says whether the station takes files.
type UploadSpec struct {
	// Accept makes the station take the files controlling stations deliver.
	Accept bool `yaml:"accept,omitempty" json:"accept,omitempty"`
	// Dir, when set, is where delivered files are written, as ioa-<ioa>-name-<name>.bin
	// (relative to the model file). Without it they are kept in memory.
	Dir string `yaml:"dir,omitempty" json:"dir,omitempty"`
	// MaxFiles is the number of delivered files the station keeps. A delivery to another
	// address beyond it is refused; one to an address that already holds a delivered file
	// replaces it. 0 means DefaultUploadMaxFiles.
	MaxFiles int `yaml:"max_files,omitempty" json:"max_files,omitempty"`
	// MaxFileSize is the largest file the station takes, in octets. 0 means
	// DefaultUploadMaxFileSize; the protocol allows up to 16 777 215.
	MaxFileSize int `yaml:"max_file_size,omitempty" json:"max_file_size,omitempty"`
	// MaxTotalSize is the number of octets of all delivered files together. 0 means
	// DefaultUploadMaxTotalSize.
	MaxTotalSize int `yaml:"max_total_size,omitempty" json:"max_total_size,omitempty"`
}

// The limits of uploads when the model does not set them.
const (
	DefaultUploadMaxFiles     = 64
	DefaultUploadMaxFileSize  = 1 << 20
	DefaultUploadMaxTotalSize = 16 << 20
)

// limits returns the upload limits with the defaults filled in.
func (u UploadSpec) limits() (files, fileSize, total int) {
	files, fileSize, total = u.MaxFiles, u.MaxFileSize, u.MaxTotalSize
	if files == 0 {
		files = DefaultUploadMaxFiles
	}
	if fileSize == 0 {
		fileSize = DefaultUploadMaxFileSize
	}
	if total == 0 {
		total = DefaultUploadMaxTotalSize
	}
	// A smaller total than the default file size makes the total the file size too.
	return files, min(fileSize, total), total
}

// monitorTypes are the point types a model may use.
var monitorTypes = map[string]asdu.TypeID{
	"M_SP_NA_1": asdu.M_SP_NA_1,
	"M_DP_NA_1": asdu.M_DP_NA_1,
	"M_ST_NA_1": asdu.M_ST_NA_1,
	"M_BO_NA_1": asdu.M_BO_NA_1,
	"M_ME_NA_1": asdu.M_ME_NA_1,
	"M_ME_NB_1": asdu.M_ME_NB_1,
	"M_ME_NC_1": asdu.M_ME_NC_1,
	"M_IT_NA_1": asdu.M_IT_NA_1,
}

// commandTypes maps the command types a model may use, time-tagged variants included, to
// the type without time tag.
var commandTypes = map[string]asdu.TypeID{
	"C_SC_NA_1": asdu.C_SC_NA_1, "C_SC_TA_1": asdu.C_SC_NA_1,
	"C_DC_NA_1": asdu.C_DC_NA_1, "C_DC_TA_1": asdu.C_DC_NA_1,
	"C_RC_NA_1": asdu.C_RC_NA_1, "C_RC_TA_1": asdu.C_RC_NA_1,
	"C_SE_NA_1": asdu.C_SE_NA_1, "C_SE_TA_1": asdu.C_SE_NA_1,
	"C_SE_NB_1": asdu.C_SE_NB_1, "C_SE_TB_1": asdu.C_SE_NB_1,
	"C_SE_NC_1": asdu.C_SE_NC_1, "C_SE_TC_1": asdu.C_SE_NC_1,
	"C_BO_NA_1": asdu.C_BO_NA_1, "C_BO_TA_1": asdu.C_BO_NA_1,
}

// commandTargets lists, per command type, the point types that can take its value.
var commandTargets = map[asdu.TypeID][]asdu.TypeID{
	asdu.C_SC_NA_1: {asdu.M_SP_NA_1},
	asdu.C_DC_NA_1: {asdu.M_DP_NA_1},
	asdu.C_RC_NA_1: {asdu.M_ST_NA_1},
	asdu.C_SE_NA_1: {asdu.M_ME_NA_1, asdu.M_ME_NB_1, asdu.M_ME_NC_1},
	asdu.C_SE_NB_1: {asdu.M_ME_NA_1, asdu.M_ME_NB_1, asdu.M_ME_NC_1},
	asdu.C_SE_NC_1: {asdu.M_ME_NA_1, asdu.M_ME_NB_1, asdu.M_ME_NC_1},
	asdu.C_BO_NA_1: {asdu.M_BO_NA_1},
}

var qualityFlags = map[string]asdu.Quality{
	"OV": asdu.QualityOverflow,
	"BL": asdu.QualityBlocked,
	"SB": asdu.QualitySubstituted,
	"NT": asdu.QualityNotTopical,
	"IV": asdu.QualityInvalid,
}

const maxIOA = 1<<24 - 1

// Load reads a model from a YAML or JSON file and validates it.
func Load(path string) (*Model, error) {
	data, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return nil, err
	}
	m, err := Parse(data)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	m.dir = filepath.Dir(path)
	if err := m.Validate(); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return m, nil
}

// Parse decodes a model from YAML; JSON is a subset of YAML and is read the same way. A
// field the model does not know is an error, so that a typo does not go unnoticed.
func Parse(data []byte) (*Model, error) {
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	var m Model
	if err := dec.Decode(&m); err != nil {
		if errors.Is(err, io.EOF) {
			return nil, errors.New("the model is empty")
		}
		return nil, err
	}
	return &m, nil
}

func (m *Model) resolve(path string) string {
	if path == "" || filepath.IsAbs(path) || m.dir == "" {
		return path
	}
	return filepath.Join(m.dir, path)
}

// Validate checks the model, files on disk included, and reports everything that is wrong
// with it at once.
func (m *Model) Validate() error {
	var problems []string
	bad := func(format string, args ...any) { problems = append(problems, fmt.Sprintf(format, args...)) }

	if ca := m.Station.CommonAddress; ca == 0 || ca == uint16(asdu.Broadcast) {
		bad("station.common_address %d: use 1-65534", ca)
	}
	if d := time.Duration(m.Station.CyclicInterval); d != 0 && d < 100*time.Millisecond {
		bad("station.cyclic_interval %s: use at least 100ms", d)
	}
	if len(m.Points) == 0 {
		bad("points: a station needs at least one point")
	}

	used := map[uint32]string{}
	claim := func(ioa uint32, what string) bool {
		switch {
		case ioa == 0 || ioa > maxIOA:
			bad("%s: ioa %d: use 1-%d", what, ioa, maxIOA)
			return false
		case used[ioa] != "":
			bad("%s: ioa %d is already used by %s", what, ioa, used[ioa])
			return false
		}
		used[ioa] = what
		return true
	}

	pointTypes := map[uint32]asdu.TypeID{}
	for i, p := range m.Points {
		what := fmt.Sprintf("points[%d]", i)
		typ, ok := monitorTypes[strings.ToUpper(strings.TrimSpace(p.Type))]
		if !ok {
			bad("%s: type %q: use one of M_SP_NA_1, M_DP_NA_1, M_ST_NA_1, M_BO_NA_1, M_ME_NA_1, M_ME_NB_1, M_ME_NC_1, M_IT_NA_1", what, p.Type)
			claim(p.IOA, what)
			continue
		}
		if claim(p.IOA, what) {
			pointTypes[p.IOA] = typ
		}
		if _, err := parseValue(typ, p.Value); err != nil {
			bad("%s (ioa %d): value: %v", what, p.IOA, err)
		}
		for _, q := range p.Quality {
			if _, ok := qualityFlags[strings.ToUpper(strings.TrimSpace(q))]; !ok {
				bad("%s (ioa %d): quality %q: use OV, BL, SB, NT or IV", what, p.IOA, q)
			}
		}
		maxGroup, kind := uint8(16), "interrogation group"
		if typ == asdu.M_IT_NA_1 {
			maxGroup, kind = 4, "counter group"
		}
		if p.Group > maxGroup {
			bad("%s (ioa %d): group %d: the %s is 1-%d", what, p.IOA, p.Group, kind, maxGroup)
		}
		if s := p.Simulate; s != nil {
			if err := s.validate(typ); err != nil {
				bad("%s (ioa %d): simulate: %v", what, p.IOA, err)
			}
		}
	}

	for i, c := range m.Commands {
		what := fmt.Sprintf("commands[%d]", i)
		typ, ok := commandTypes[strings.ToUpper(strings.TrimSpace(c.Type))]
		if !ok {
			bad("%s: type %q: use one of C_SC_NA_1, C_DC_NA_1, C_RC_NA_1, C_SE_NA_1, C_SE_NB_1, C_SE_NC_1, C_BO_NA_1", what, c.Type)
			claim(c.IOA, what)
			continue
		}
		claim(c.IOA, what)
		if typ == asdu.C_BO_NA_1 && c.SelectRequired {
			bad("%s (ioa %d): select_required: a bitstring command has no select", what, c.IOA)
		}
		if c.Target == 0 {
			continue
		}
		target, ok := pointTypes[c.Target]
		if !ok {
			bad("%s (ioa %d): target %d is not a point of the model", what, c.IOA, c.Target)
			continue
		}
		fits := false
		for _, t := range commandTargets[typ] {
			fits = fits || t == target
		}
		if !fits {
			bad("%s (ioa %d): target %d is a %s, which a %s cannot set", what, c.IOA, c.Target, target, typ)
		}
	}

	for i, f := range m.Files {
		what := fmt.Sprintf("files[%d]", i)
		claim(f.IOA, what)
		sources := 0
		for _, set := range []bool{f.Path != "", f.Content != "", f.Size != 0} {
			if set {
				sources++
			}
		}
		if sources != 1 {
			bad("%s (ioa %d): give exactly one of path, content and size", what, f.IOA)
			continue
		}
		if f.SectionSize < 0 || f.Size < 0 {
			bad("%s (ioa %d): size and section_size cannot be negative", what, f.IOA)
			continue
		}
		data, err := m.fileContent(f)
		switch {
		case err != nil:
			bad("%s (ioa %d): %v", what, f.IOA, err)
		case len(data) == 0:
			bad("%s (ioa %d): the file is empty", what, f.IOA)
		case len(data) > maxIOA:
			bad("%s (ioa %d): %d octets: the maximum is %d", what, f.IOA, len(data), maxIOA)
		case (len(data)+sectionSize(f)-1)/sectionSize(f) > 254:
			bad("%s (ioa %d): more than 254 sections: use a larger section_size", what, f.IOA)
		}
	}
	if m.Uploads.Dir != "" && !m.Uploads.Accept {
		bad("uploads.dir is set but uploads.accept is not")
	}
	if u := m.Uploads; u.MaxFiles < 0 || u.MaxFileSize < 0 || u.MaxTotalSize < 0 {
		bad("uploads: max_files, max_file_size and max_total_size cannot be negative")
	}
	if m.Uploads.MaxFileSize > maxIOA {
		bad("uploads.max_file_size %d: the maximum is %d", m.Uploads.MaxFileSize, maxIOA)
	}
	if _, _, total := m.Uploads.limits(); m.Uploads.MaxFileSize > total {
		bad("uploads.max_file_size %d is larger than uploads.max_total_size %d", m.Uploads.MaxFileSize, total)
	}

	if len(problems) == 0 {
		return nil
	}
	return fmt.Errorf("invalid model:\n  - %s", strings.Join(problems, "\n  - "))
}

func sectionSize(f FileSpec) int {
	if f.SectionSize > 0 {
		return f.SectionSize
	}
	return 2048
}

// fileContent returns the octets of a file of the model.
func (m *Model) fileContent(f FileSpec) ([]byte, error) {
	switch {
	case f.Path != "":
		return os.ReadFile(filepath.Clean(m.resolve(f.Path)))
	case f.Content != "":
		return []byte(f.Content), nil
	}
	data := make([]byte, f.Size)
	for i := range data {
		data[i] = byte('A' + i%26)
	}
	return data, nil
}

func (s *SimSpec) validate(typ asdu.TypeID) error {
	if time.Duration(s.Interval) < 50*time.Millisecond {
		return fmt.Errorf("interval %s: use at least 50ms", time.Duration(s.Interval))
	}
	numeric := typ != asdu.M_SP_NA_1 && typ != asdu.M_DP_NA_1
	switch strings.ToLower(s.Mode) {
	case "toggle":
		if numeric {
			return fmt.Errorf("mode toggle is for single and double points; a %s takes ramp, sine, random or count", typ)
		}
	case "ramp", "sine", "random":
		if !numeric {
			return fmt.Errorf("mode %s is for numeric points; a %s takes toggle", s.Mode, typ)
		}
		if s.Max <= s.Min {
			return fmt.Errorf("mode %s needs max above min", s.Mode)
		}
	case "count":
		if !numeric {
			return fmt.Errorf("mode count is for numeric points; a %s takes toggle", typ)
		}
	default:
		return fmt.Errorf("mode %q: use toggle, ramp, sine, random or count", s.Mode)
	}
	if s.Step < 0 {
		return errors.New("step cannot be negative")
	}
	return nil
}

// value is the state of a point: a boolean, a double point state or a number.
type value struct {
	b bool
	d asdu.DoubleState
	f float64
}

func toFloat(v any) (float64, bool) {
	switch n := v.(type) {
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	case uint64:
		return float64(n), true
	case float64:
		return n, true
	case string:
		f, err := strconv.ParseFloat(strings.TrimSpace(n), 64)
		if err != nil {
			u, uerr := strconv.ParseUint(strings.TrimSpace(n), 0, 32)
			return float64(u), uerr == nil
		}
		return f, true
	}
	return 0, false
}

// limits returns the range of values a numeric point type holds.
func limits(typ asdu.TypeID) (lo, hi float64) {
	switch typ {
	case asdu.M_ST_NA_1:
		return -64, 63
	case asdu.M_BO_NA_1:
		return 0, math.MaxUint32
	case asdu.M_ME_NA_1:
		return -1, float64(math.MaxInt16) / 32768
	case asdu.M_ME_NB_1:
		return math.MinInt16, math.MaxInt16
	case asdu.M_IT_NA_1:
		return math.MinInt32, math.MaxInt32
	default:
		return -math.MaxFloat32, math.MaxFloat32
	}
}

// parseValue converts the value of a model to the state of a point of type typ.
func parseValue(typ asdu.TypeID, v any) (value, error) {
	switch typ {
	case asdu.M_SP_NA_1:
		switch b := v.(type) {
		case bool:
			return value{b: b}, nil
		case string:
			switch strings.ToLower(strings.TrimSpace(b)) {
			case "on", "true", "1":
				return value{b: true}, nil
			case "off", "false", "0":
				return value{}, nil
			}
		case int:
			if b == 0 || b == 1 {
				return value{b: b == 1}, nil
			}
		}
		return value{}, fmt.Errorf("%v is not a single point state: use true or false (on or off)", v)
	case asdu.M_DP_NA_1:
		switch s := v.(type) {
		case bool:
			if s {
				return value{d: asdu.DoubleOn}, nil
			}
			return value{d: asdu.DoubleOff}, nil
		case string:
			switch strings.ToLower(strings.TrimSpace(s)) {
			case "off":
				return value{d: asdu.DoubleOff}, nil
			case "on":
				return value{d: asdu.DoubleOn}, nil
			case "intermediate":
				return value{d: asdu.DoubleIntermediate}, nil
			case "indeterminate":
				return value{d: asdu.DoubleIndeterminate}, nil
			}
		case int:
			if s >= 0 && s <= 3 {
				return value{d: asdu.DoubleState(s)}, nil
			}
		}
		return value{}, fmt.Errorf("%v is not a double point state: use off, on, intermediate or indeterminate", v)
	default:
	}
	f, ok := toFloat(v)
	if !ok || math.IsNaN(f) || math.IsInf(f, 0) {
		return value{}, fmt.Errorf("%v is not a number", v)
	}
	lo, hi := limits(typ)
	if f < lo || f > hi {
		return value{}, fmt.Errorf("%v is out of range for %s (%v to %v)", v, typ, lo, hi)
	}
	if typ != asdu.M_ME_NA_1 && typ != asdu.M_ME_NC_1 && f != math.Trunc(f) {
		return value{}, fmt.Errorf("%v is not a whole number, which %s needs", v, typ)
	}
	return value{f: f}, nil
}

// Encode writes the model as YAML, or as JSON when asJSON is set.
func (m *Model) Encode(asJSON bool) ([]byte, error) {
	if asJSON {
		b, err := json.MarshalIndent(m, "", "  ")
		if err != nil {
			return nil, err
		}
		return append(b, '\n'), nil
	}
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(m); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
