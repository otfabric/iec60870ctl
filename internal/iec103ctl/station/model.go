// SPDX-License-Identifier: MIT

// Package station is the simulated protection equipment of `iec103ctl server start`: a
// device described by a model (YAML or JSON) served with go-iec60870-5's protection
// equipment station (iec103/server) on a TCP line or a serial port. The model is a
// superset of the IEC 103 fixture of otfabric/iec60870-5-interop (docs/FIXTURES.md), so a
// fixture is a model as it is.
package station

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/otfabric/go-iec60870-5/iec103"
	"gopkg.in/yaml.v3"
)

// MaxModelSize bounds a model file.
const MaxModelSize = 4 << 20

// Model is a simulated protection device.
type Model struct {
	SchemaVersion string        `json:"schemaVersion,omitempty" yaml:"schemaVersion,omitempty"`
	Name          string        `json:"name,omitempty" yaml:"name,omitempty"`
	Description   string        `json:"description,omitempty" yaml:"description,omitempty"`
	Station       StationDef    `json:"station" yaml:"station"`
	Indications   []Indication  `json:"indications,omitempty" yaml:"indications,omitempty"`
	Measurands    []MeasurandIO `json:"measurands,omitempty" yaml:"measurands,omitempty"`
	Commands      []CommandDef  `json:"commands,omitempty" yaml:"commands,omitempty"`
	Groups        []GroupDef    `json:"groups,omitempty" yaml:"groups,omitempty"`
	Generic       []GenericDef  `json:"generic,omitempty" yaml:"generic,omitempty"`
	Disturbances  []Record      `json:"disturbances,omitempty" yaml:"disturbances,omitempty"`
}

// StationDef is the device itself.
type StationDef struct {
	// LinkAddress is the link address, 0..254; absent means 1.
	LinkAddress *int `json:"linkAddress,omitempty" yaml:"linkAddress,omitempty"`
	// CommonAddress is the common address of ASDU, 0..254.
	CommonAddress  int            `json:"commonAddress" yaml:"commonAddress"`
	Identification Identification `json:"identification" yaml:"identification"`
	// StartCause is the cause of the identification after start: power-on (default) or
	// start-restart.
	StartCause string `json:"startCause,omitempty" yaml:"startCause,omitempty"`
}

// Identification is the body of ASDU 5 and the function type of the device.
type Identification struct {
	Fun      int    `json:"fun" yaml:"fun"`
	Col      int    `json:"col" yaml:"col"`
	ASCII    string `json:"ascii" yaml:"ascii"`
	Software string `json:"software" yaml:"software"`
}

// Indication is a double-point indication (ASDU 1).
type Indication struct {
	Fun  int    `json:"fun" yaml:"fun"`
	Inf  int    `json:"inf" yaml:"inf"`
	DPI  int    `json:"dpi" yaml:"dpi"`
	Name string `json:"name,omitempty" yaml:"name,omitempty"`
	// GI false keeps it out of the general interrogation.
	GI *bool `json:"gi,omitempty" yaml:"gi,omitempty"`
	// Simulate, when set, toggles the indication every so many milliseconds and reports
	// the change spontaneously (cause 1).
	Simulate int `json:"simulate,omitempty" yaml:"simulate,omitempty"`
}

// MeasurandIO is a set of measurands sent cyclically as class 2 data (ASDU 3 or 9).
type MeasurandIO struct {
	Fun    int     `json:"fun" yaml:"fun"`
	Inf    int     `json:"inf" yaml:"inf"`
	Type   int     `json:"type" yaml:"type"`
	Values []MeaIO `json:"values" yaml:"values"`
}

// MeaIO is one measurand: the 13-bit value and its quality bits.
type MeaIO struct {
	Value int  `json:"value" yaml:"value"`
	OV    bool `json:"ov,omitempty" yaml:"ov,omitempty"`
	ER    bool `json:"er,omitempty" yaml:"er,omitempty"`
}

// CommandDef is a general command the device takes, acting on the indication with the
// same FUN and INF.
type CommandDef struct {
	Fun int `json:"fun" yaml:"fun"`
	Inf int `json:"inf" yaml:"inf"`
}

// GroupDef names a generic group (the heading, entry 0).
type GroupDef struct {
	Group       int    `json:"group" yaml:"group"`
	Description string `json:"description" yaml:"description"`
}

// GINDef is a generic identification number.
type GINDef struct {
	Group int `json:"group" yaml:"group"`
	Entry int `json:"entry" yaml:"entry"`
}

// GenericDef is a generic entry: its description, data type, size, value and whether it
// may be written.
type GenericDef struct {
	GIN         GINDef `json:"gin" yaml:"gin"`
	Description string `json:"description" yaml:"description"`
	Type        int    `json:"type" yaml:"type"`
	Size        int    `json:"size" yaml:"size"`
	Value       any    `json:"value" yaml:"value"`
	Writable    bool   `json:"writable,omitempty" yaml:"writable,omitempty"`
}

// SOFDef is the status of fault.
type SOFDef struct {
	TP   bool `json:"tp,omitempty" yaml:"tp,omitempty"`
	TM   bool `json:"tm,omitempty" yaml:"tm,omitempty"`
	Test bool `json:"test,omitempty" yaml:"test,omitempty"`
	OTEV bool `json:"otev,omitempty" yaml:"otev,omitempty"`
}

// TagDef is a binary signal of a record from value Pos on.
type TagDef struct {
	Pos int `json:"pos" yaml:"pos"`
	Fun int `json:"fun" yaml:"fun"`
	Inf int `json:"inf" yaml:"inf"`
	DPI int `json:"dpi" yaml:"dpi"`
}

// ChannelDef is an analogue channel of a record.
type ChannelDef struct {
	ACC    int     `json:"acc" yaml:"acc"`
	RPV    float64 `json:"rpv" yaml:"rpv"`
	RSV    float64 `json:"rsv" yaml:"rsv"`
	RFA    float64 `json:"rfa" yaml:"rfa"`
	Values []int   `json:"values" yaml:"values"`
}

// Record is a recorded disturbance.
type Record struct {
	FAN      int          `json:"fan" yaml:"fan"`
	SOF      SOFDef       `json:"sof" yaml:"sof"`
	Time     string       `json:"time" yaml:"time"`
	NOF      int          `json:"nof" yaml:"nof"`
	Interval int          `json:"interval" yaml:"interval"`
	Start    string       `json:"start" yaml:"start"`
	Tags     []TagDef     `json:"tags,omitempty" yaml:"tags,omitempty"`
	Channels []ChannelDef `json:"channels,omitempty" yaml:"channels,omitempty"`
}

// Load reads and validates the model file path (YAML, or JSON by its extension).
func Load(path string) (*Model, error) {
	f, err := os.Open(filepath.Clean(path))
	if err != nil {
		return nil, fmt.Errorf("reading the model: %w", err)
	}
	defer func() { _ = f.Close() }()
	var buf bytes.Buffer
	if _, err := buf.ReadFrom(ioLimit(f)); err != nil {
		return nil, fmt.Errorf("reading the model: %w", err)
	}
	if buf.Len() > MaxModelSize {
		return nil, fmt.Errorf("the model %s is larger than %d octets", path, MaxModelSize)
	}
	return Parse(buf.Bytes(), strings.EqualFold(filepath.Ext(path), ".json"))
}

// Parse decodes and validates a model: JSON when asJSON, else YAML (of which JSON is a
// subset). Unknown fields are errors.
func Parse(data []byte, asJSON bool) (*Model, error) {
	var m Model
	if asJSON {
		dec := json.NewDecoder(bytes.NewReader(data))
		dec.DisallowUnknownFields()
		dec.UseNumber()
		if err := dec.Decode(&m); err != nil {
			return nil, fmt.Errorf("model: %w", err)
		}
	} else {
		dec := yaml.NewDecoder(bytes.NewReader(data))
		dec.KnownFields(true)
		if err := dec.Decode(&m); err != nil {
			return nil, fmt.Errorf("model: %w", err)
		}
	}
	if err := m.Validate(); err != nil {
		return nil, err
	}
	return &m, nil
}

// Encode returns the model as JSON (indented) or YAML.
func (m *Model) Encode(asJSON bool) ([]byte, error) {
	if asJSON {
		b, err := json.MarshalIndent(m, "", "  ")
		if err != nil {
			return nil, err
		}
		return append(b, '\n'), nil
	}
	return yaml.Marshal(m)
}

// LinkAddr returns the link address of the model.
func (m *Model) LinkAddr() int {
	if m.Station.LinkAddress == nil {
		return 1
	}
	return *m.Station.LinkAddress
}

type funInf struct{ fun, inf int }

func inRange(v, lo, hi int) bool { return v >= lo && v <= hi }

// Validate checks the model against the rules of the fixture format (iec60870-5-interop
// docs/FIXTURES.md, IEC 103) and what the standard allows.
func (m *Model) Validate() error {
	var errs []error
	bad := func(format string, args ...any) { errs = append(errs, fmt.Errorf(format, args...)) }
	st := m.Station
	if !inRange(m.LinkAddr(), 0, 254) {
		bad("station.linkAddress %d: use 0-254", m.LinkAddr())
	}
	if !inRange(st.CommonAddress, 0, 254) {
		bad("station.commonAddress %d: use 0-254", st.CommonAddress)
	}
	if !inRange(st.Identification.Fun, 1, 255) {
		bad("station.identification.fun %d: use 1-255", st.Identification.Fun)
	}
	if !inRange(st.Identification.Col, 0, 255) {
		bad("station.identification.col %d: use 0-255", st.Identification.Col)
	}
	if len(st.Identification.ASCII) > 8 || !printable(st.Identification.ASCII) {
		bad("station.identification.ascii %q: at most eight printable ASCII characters", st.Identification.ASCII)
	}
	if sw, err := hex.DecodeString(st.Identification.Software); err != nil || len(sw) != 4 {
		bad("station.identification.software %q: four octets in hex, e.g. 01020304", st.Identification.Software)
	}
	switch st.StartCause {
	case "", "power-on", "start-restart":
	default:
		bad("station.startCause %q: use power-on or start-restart", st.StartCause)
	}
	seen := map[funInf]bool{}
	inds := map[funInf]bool{}
	for i, ind := range m.Indications {
		if !inRange(ind.Fun, 0, 254) || !inRange(ind.Inf, 0, 255) {
			bad("indications[%d]: fun 0-254 and inf 0-255", i)
		}
		if !inRange(ind.DPI, 0, 3) {
			bad("indications[%d]: dpi %d: use 0-3 (1 off, 2 on)", i, ind.DPI)
		}
		if ind.Simulate < 0 || (ind.Simulate > 0 && ind.Simulate < 100) {
			bad("indications[%d]: simulate %d: at least 100 ms", i, ind.Simulate)
		}
		k := funInf{ind.Fun, ind.Inf}
		if seen[k] {
			bad("indications[%d]: fun %d inf %d twice", i, ind.Fun, ind.Inf)
		}
		seen[k], inds[k] = true, true
	}
	for i, me := range m.Measurands {
		k := funInf{me.Fun, me.Inf}
		if !inRange(me.Fun, 0, 254) || !inRange(me.Inf, 0, 255) {
			bad("measurands[%d]: fun 0-254 and inf 0-255", i)
		}
		if seen[k] {
			bad("measurands[%d]: fun %d inf %d is already used", i, me.Fun, me.Inf)
		}
		seen[k] = true
		limit := 0
		switch me.Type {
		case 3:
			limit = iec103.MaxMeasurands1
		case 9:
			limit = iec103.MaxMeasurands2
		default:
			bad("measurands[%d]: type %d: use 3 (measurands I) or 9 (measurands II)", i, me.Type)
		}
		if len(me.Values) == 0 || (limit > 0 && len(me.Values) > limit) {
			bad("measurands[%d]: 1-%d values for type %d", i, limit, me.Type)
		}
		for j, v := range me.Values {
			if !inRange(v.Value, -4096, 4095) {
				bad("measurands[%d].values[%d]: %d: use -4096..4095", i, j, v.Value)
			}
		}
	}
	cmds := map[funInf]bool{}
	for i, c := range m.Commands {
		k := funInf{c.Fun, c.Inf}
		if !inds[k] {
			bad("commands[%d]: fun %d inf %d is no indication of the model", i, c.Fun, c.Inf)
		}
		if cmds[k] {
			bad("commands[%d]: fun %d inf %d twice", i, c.Fun, c.Inf)
		}
		cmds[k] = true
	}
	groups := map[int]bool{}
	for i, g := range m.Groups {
		if !inRange(g.Group, 0, 255) || groups[g.Group] {
			bad("groups[%d]: group %d: 0-255, once", i, g.Group)
		}
		if !printable(g.Description) || len(g.Description) == 0 || len(g.Description) > 64 {
			bad("groups[%d]: description: 1-64 printable ASCII characters", i)
		}
		groups[g.Group] = true
	}
	gins := map[GINDef]bool{}
	for i, g := range m.Generic {
		if !inRange(g.GIN.Group, 1, 255) || !inRange(g.GIN.Entry, 1, 255) {
			bad("generic[%d]: gin group and entry are 1-255", i)
		}
		if gins[g.GIN] {
			bad("generic[%d]: gin %d.%d twice", i, g.GIN.Group, g.GIN.Entry)
		}
		gins[g.GIN] = true
		if !printable(g.Description) || len(g.Description) == 0 || len(g.Description) > 64 {
			bad("generic[%d]: description: 1-64 printable ASCII characters", i)
		}
		if _, err := g.Actual(); err != nil {
			bad("generic[%d]: %v", i, err)
		}
	}
	if len(m.Disturbances) > iec103.MaxDisturbances {
		bad("disturbances: at most %d", iec103.MaxDisturbances)
	}
	fans := map[int]bool{}
	for i, d := range m.Disturbances {
		if fans[d.FAN] {
			bad("disturbances[%d]: fan %d twice", i, d.FAN)
		}
		fans[d.FAN] = true
		if err := d.validate(); err != nil {
			bad("disturbances[%d]: %v", i, err)
		}
	}
	return errors.Join(errs...)
}

func printable(s string) bool {
	for _, r := range s {
		if r < 0x20 || r > 0x7E {
			return false
		}
	}
	return true
}

// number converts a value of the model (YAML or JSON) to a float and reports whether it
// is an integer.
func number(v any) (float64, bool, error) {
	switch n := v.(type) {
	case int:
		return float64(n), true, nil
	case int64:
		return float64(n), true, nil
	case uint64:
		return float64(n), true, nil
	case float64:
		return n, n == math.Trunc(n), nil
	case json.Number:
		if i, err := n.Int64(); err == nil {
			return float64(i), true, nil
		}
		f, err := n.Float64()
		if err != nil {
			return 0, false, err
		}
		return f, f == math.Trunc(f), nil
	}
	return 0, false, fmt.Errorf("value %v is not a number", v)
}

// Actual returns the value of the entry as generic data (KOD 1).
func (g GenericDef) Actual() (iec103.Value, error) {
	switch g.Type {
	case 1:
		s, ok := g.Value.(string)
		if !ok || !printable(s) || len(s) == 0 {
			return iec103.Value{}, fmt.Errorf("type 1 needs a printable text value")
		}
		if g.Size != len(s) {
			return iec103.Value{}, fmt.Errorf("size %d is not the %d characters of the value", g.Size, len(s))
		}
		return iec103.ASCII(s), nil
	case 3, 4:
		if g.Size != 1 && g.Size != 2 && g.Size != 4 {
			return iec103.Value{}, fmt.Errorf("size %d: an integer has 1, 2 or 4 octets", g.Size)
		}
		f, isInt, err := number(g.Value)
		if err != nil || !isInt {
			return iec103.Value{}, fmt.Errorf("type %d needs an integer value", g.Type)
		}
		bits := 8 * g.Size
		if g.Type == 3 {
			if f < 0 || f >= math.Ldexp(1, bits) {
				return iec103.Value{}, fmt.Errorf("value %v does not fit %d octets unsigned", g.Value, g.Size)
			}
			return iec103.Uints(iec103.DataUint, g.Size, uint64(f)), nil
		}
		if f < -math.Ldexp(1, bits-1) || f >= math.Ldexp(1, bits-1) {
			return iec103.Value{}, fmt.Errorf("value %v does not fit %d octets", g.Value, g.Size)
		}
		return iec103.Ints(g.Size, int64(f)), nil
	case 7:
		if g.Size != 4 {
			return iec103.Value{}, fmt.Errorf("size of type 7 is 4")
		}
		f, _, err := number(g.Value)
		if err != nil || math.IsInf(f, 0) || math.IsNaN(f) || math.Abs(f) > math.MaxFloat32 {
			return iec103.Value{}, fmt.Errorf("type 7 needs a single precision number")
		}
		return iec103.Float32s(float32(f)), nil
	case 8:
		if g.Size != 8 {
			return iec103.Value{}, fmt.Errorf("size of type 8 is 8")
		}
		f, _, err := number(g.Value)
		if err != nil || math.IsInf(f, 0) || math.IsNaN(f) {
			return iec103.Value{}, fmt.Errorf("type 8 needs a number")
		}
		return iec103.Float64s(f), nil
	}
	return iec103.Value{}, fmt.Errorf("type %d: use 1 (ASCII), 3 (unsigned), 4 (integer), 7 (R32.23) or 8 (R64.53)", g.Type)
}

// The time formats of a record: the fault (CP56Time2a) and the first value (time of day).
const (
	faultTime = "2006-01-02T15:04:05.000Z07:00"
	startTime = "15:04:05.000"
)

func (d Record) validate() error {
	if !inRange(d.FAN, 0, 65535) || !inRange(d.NOF, 0, 65535) || !inRange(d.Interval, 0, 65535) {
		return fmt.Errorf("fan, nof and interval are 0-65535")
	}
	if _, err := time.Parse(faultTime, d.Time); err != nil {
		return fmt.Errorf("time %q: use 2026-01-02T03:04:05.678Z", d.Time)
	}
	if _, err := time.Parse(startTime, d.Start); err != nil {
		return fmt.Errorf("start %q: use HH:MM:SS.mmm", d.Start)
	}
	last := 0
	for i, t := range d.Tags {
		if !inRange(t.Pos, last, 65535) {
			return fmt.Errorf("tags[%d]: pos %d: 0-65535, ascending", i, t.Pos)
		}
		last = t.Pos
		if !inRange(t.Fun, 0, 255) || !inRange(t.Inf, 0, 255) || !inRange(t.DPI, 0, 3) {
			return fmt.Errorf("tags[%d]: fun and inf 0-255, dpi 0-3", i)
		}
	}
	if len(d.Channels) > 255 {
		return fmt.Errorf("at most 255 channels")
	}
	acc := -1
	for i, c := range d.Channels {
		if !inRange(c.ACC, acc+1, 255) {
			return fmt.Errorf("channels[%d]: acc %d: 1-255, ascending", i, c.ACC)
		}
		acc = c.ACC
		if len(c.Values) != len(d.Channels[0].Values) {
			return fmt.Errorf("channels[%d]: every channel has the same number of values", i)
		}
		if len(c.Values) > 65535 {
			return fmt.Errorf("channels[%d]: at most 65535 values", i)
		}
		for _, v := range c.Values {
			if !inRange(v, math.MinInt16, math.MaxInt16) {
				return fmt.Errorf("channels[%d]: value %d: use -32768..32767", i, v)
			}
		}
		for _, f := range []float64{c.RPV, c.RSV, c.RFA} {
			if math.IsNaN(f) || math.Abs(f) > math.MaxFloat32 {
				return fmt.Errorf("channels[%d]: rpv, rsv and rfa are single precision numbers", i)
			}
		}
	}
	return nil
}

// parseAddress parses an address flag of `server start`: "" keeps def.
func parseAddress(s, what string, def int) (int, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return def, nil
	}
	n, err := strconv.Atoi(s)
	if err != nil || !inRange(n, 0, 254) {
		return 0, fmt.Errorf("invalid %s %q: use 0-254", what, s)
	}
	return n, nil
}

// clamp16 returns v limited to the range of an int16 (Validate keeps the values of a
// model in range; the clamp makes the conversion safe whatever it is given).
func clamp16(v int) int16 {
	return int16(min(max(v, math.MinInt16), math.MaxInt16))
}
