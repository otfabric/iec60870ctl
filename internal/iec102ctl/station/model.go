// SPDX-License-Identifier: MIT

// Package station is the simulated integrated total station (an electricity meter) behind
// `iec102ctl server start`: a model of records read from YAML or JSON, and the station that
// serves it with go-iec60870-5's iec102/server. The tests of the client commands run against
// it as well.
package station

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/otfabric/go-iec60870-5/iec102"
	"github.com/otfabric/iec60870ctl/internal/iec102ctl/meter"
	"gopkg.in/yaml.v3"
)

// Duration is a time.Duration that is written as a string ("1h", "15m") in a model.
type Duration time.Duration

// UnmarshalYAML implements yaml.Unmarshaler.
func (d *Duration) UnmarshalYAML(n *yaml.Node) error {
	var s string
	if err := n.Decode(&s); err != nil {
		return fmt.Errorf("line %d: a duration is written as a string such as 1h or 15m", n.Line)
	}
	return d.set(s)
}

func (d *Duration) set(s string) error {
	v, err := time.ParseDuration(strings.TrimSpace(s))
	if err != nil || v < 0 {
		return fmt.Errorf("invalid duration %q: use a form such as 1h or 15m", s)
	}
	*d = Duration(v)
	return nil
}

// MarshalYAML implements yaml.Marshaler.
func (d Duration) MarshalYAML() (any, error) { return time.Duration(d).String(), nil }

// MarshalJSON implements json.Marshaler.
func (d Duration) MarshalJSON() ([]byte, error) { return json.Marshal(time.Duration(d).String()) }

// Model describes a meter.
type Model struct {
	Station StationSpec  `yaml:"station" json:"station"`
	Totals  []TotalsSpec `yaml:"totals,omitempty" json:"totals,omitempty"`
	Events  []EventsSpec `yaml:"events,omitempty" json:"events,omitempty"`
	REE     *REESpec     `yaml:"ree,omitempty" json:"ree,omitempty"`
}

// StationSpec holds what concerns the meter as a whole.
type StationSpec struct {
	// Name is shown in the event log.
	Name string `yaml:"name,omitempty" json:"name,omitempty"`
	// LinkAddress is the link address of the meter (default 1).
	LinkAddress *uint16 `yaml:"link_address,omitempty" json:"link_address,omitempty"`
	// CommonAddress is the measuring point address, the common address of ASDU (default 1).
	CommonAddress *uint16 `yaml:"common_address,omitempty" json:"common_address,omitempty"`
	// Clock is "running" (the computer's clock, the default) or a time b
	// (2026-01-15T10:20:30.500) at which the clock stands still until a controlling station
	// sets it. A clock set by C_CS_TA_2 keeps its offset as long as the process runs.
	Clock string `yaml:"clock,omitempty" json:"clock,omitempty"`
	// Product answers C_RD_NA_2.
	Product ProductSpec `yaml:"product" json:"product"`
}

// ProductSpec is the manufacturer and product specification, P_MP_NA_2.
type ProductSpec struct {
	Standard     uint8  `yaml:"standard" json:"standard"`
	Manufacturer uint8  `yaml:"manufacturer" json:"manufacturer"`
	Product      uint32 `yaml:"product" json:"product"`
}

// TotalsSpec is the integrated totals of one record address and type.
type TotalsSpec struct {
	Record uint8 `yaml:"record" json:"record"`
	// Type is the type the periods are served as, M_IT_TA_2 .. M_IT_TM_2; or give Kind:
	// operational (absolute, M_IT_TG_2), reset-operational (incremental, M_IT_TK_2),
	// accounting (M_IT_TA_2) or reset-accounting (M_IT_TD_2).
	Type string `yaml:"type,omitempty" json:"type,omitempty"`
	Kind string `yaml:"kind,omitempty" json:"kind,omitempty"`
	// Signed adds the signature octet to every total.
	Signed bool `yaml:"signed,omitempty" json:"signed,omitempty"`
	// Periods are the integration periods, oldest first.
	Periods []PeriodSpec `yaml:"periods,omitempty" json:"periods,omitempty"`
	// Generate adds periods that follow each other at an interval.
	Generate *GenerateSpec `yaml:"generate,omitempty" json:"generate,omitempty"`
}

// PeriodSpec is one integration period.
type PeriodSpec struct {
	// End is the end of the period, time a (2026-01-01T01:00).
	End    string      `yaml:"end" json:"end"`
	Totals []TotalSpec `yaml:"totals" json:"totals"`
}

// TotalSpec is one integrated total.
type TotalSpec struct {
	Address   uint8  `yaml:"address" json:"address"`
	Value     uint32 `yaml:"value" json:"value"`
	Quality   uint8  `yaml:"quality,omitempty" json:"quality,omitempty"`
	Signature uint8  `yaml:"signature,omitempty" json:"signature,omitempty"`
}

// GenerateSpec generates Count periods: the first ends at Start, each next one Interval
// later. A total of an absolute type starts at its Start value and grows by Step each
// period; a total of a periodical reset type is Step in every period.
type GenerateSpec struct {
	Start    string         `yaml:"start" json:"start"`
	Interval Duration       `yaml:"interval" json:"interval"`
	Count    int            `yaml:"count" json:"count"`
	Totals   []GenTotalSpec `yaml:"totals" json:"totals"`
}

// GenTotalSpec is one generated integrated total.
type GenTotalSpec struct {
	Address uint8  `yaml:"address" json:"address"`
	Start   uint32 `yaml:"start,omitempty" json:"start,omitempty"`
	Step    uint32 `yaml:"step,omitempty" json:"step,omitempty"`
}

// EventsSpec is a list of single-point information with time tag.
type EventsSpec struct {
	// Record is the list address, normally 50-55.
	Record uint8       `yaml:"record" json:"record"`
	Points []PointSpec `yaml:"points" json:"points"`
}

// PointSpec is one single-point information.
type PointSpec struct {
	Address   uint8 `yaml:"address" json:"address"`
	On        bool  `yaml:"on" json:"on"`
	Qualifier uint8 `yaml:"qualifier,omitempty" json:"qualifier,omitempty"`
	// Time is the time tag, time b (2026-01-01T00:15:00.000).
	Time string `yaml:"time" json:"time"`
}

// REESpec turns on the Spanish REE profile: the session with the access key, clock setting
// and the REE reads below.
type REESpec struct {
	// AccessKey is the key C_AC_NA_2 must carry.
	AccessKey uint32 `yaml:"access_key" json:"access_key"`
	// ContractedPowers answer C_PC_NA_2.
	ContractedPowers []PowerSpec `yaml:"contracted_powers,omitempty" json:"contracted_powers,omitempty"`
	// SpecialDays (time a or a date) answer C_DF_NA_2.
	SpecialDays []string `yaml:"special_days,omitempty" json:"special_days,omitempty"`
	// Tariffs answer C_TA_VC_2 and C_TA_VM_2.
	Tariffs []TariffSpec `yaml:"tariffs,omitempty" json:"tariffs,omitempty"`
}

// PowerSpec is one contracted power.
type PowerSpec struct {
	Address uint8  `yaml:"address" json:"address"`
	PowerKW uint32 `yaml:"power_kw" json:"power_kw"`
}

// TariffSpec is the tariff information of one contract register.
type TariffSpec struct {
	// Register is 134-136 (contracts I-III) or 137-139 (the latent ones).
	Register uint8 `yaml:"register" json:"register"`
	// Current answers C_TA_VC_2, Stored C_TA_VM_2 (the records whose period_end lies in
	// the requested range).
	Current []TariffRecordSpec `yaml:"current,omitempty" json:"current,omitempty"`
	Stored  []TariffRecordSpec `yaml:"stored,omitempty" json:"stored,omitempty"`
}

// TariffRecordSpec is one record of M_TA_VC_2 or M_TA_VM_2; the times are times a.
type TariffRecordSpec struct {
	Address            uint8  `yaml:"address" json:"address"`
	ActiveAbs          uint32 `yaml:"active_abs" json:"active_abs"`
	ActiveInc          uint32 `yaml:"active_inc" json:"active_inc"`
	ActiveQuality      uint8  `yaml:"active_quality,omitempty" json:"active_quality,omitempty"`
	ReactiveIndAbs     uint32 `yaml:"reactive_ind_abs" json:"reactive_ind_abs"`
	ReactiveIndInc     uint32 `yaml:"reactive_ind_inc" json:"reactive_ind_inc"`
	ReactiveIndQuality uint8  `yaml:"reactive_ind_quality,omitempty" json:"reactive_ind_quality,omitempty"`
	ReactiveCapAbs     uint32 `yaml:"reactive_cap_abs" json:"reactive_cap_abs"`
	ReactiveCapInc     uint32 `yaml:"reactive_cap_inc" json:"reactive_cap_inc"`
	ReactiveCapQuality uint8  `yaml:"reactive_cap_quality,omitempty" json:"reactive_cap_quality,omitempty"`
	MaxPower           uint32 `yaml:"max_power" json:"max_power"`
	MaxPowerTime       string `yaml:"max_power_time" json:"max_power_time"`
	MaxPowerQuality    uint8  `yaml:"max_power_quality,omitempty" json:"max_power_quality,omitempty"`
	ExcessPower        uint32 `yaml:"excess_power" json:"excess_power"`
	ExcessPowerQuality uint8  `yaml:"excess_power_quality,omitempty" json:"excess_power_quality,omitempty"`
	PeriodStart        string `yaml:"period_start" json:"period_start"`
	PeriodEnd          string `yaml:"period_end" json:"period_end"`
}

// Limits of a model, so that a model file cannot make the station hold unbounded memory.
const (
	MaxModelSize = 16 << 20
	MaxPeriods   = 100000
	MaxEvents    = 100000
	MaxRecords   = 10000
)

// Load reads a model from a YAML or JSON file.
func Load(path string) (*Model, error) {
	f, err := os.Open(filepath.Clean(path))
	if err != nil {
		return nil, fmt.Errorf("model: %w", err)
	}
	defer func() { _ = f.Close() }()
	data, err := io.ReadAll(io.LimitReader(f, MaxModelSize+1))
	if err != nil {
		return nil, fmt.Errorf("model %s: %w", path, err)
	}
	if len(data) > MaxModelSize {
		return nil, fmt.Errorf("model %s: larger than %d octets", path, MaxModelSize)
	}
	m, err := Parse(data)
	if err != nil {
		return nil, fmt.Errorf("model %s: %w", path, err)
	}
	return m, nil
}

// Parse reads a model from YAML or JSON (JSON is YAML). Unknown fields are errors.
func Parse(data []byte) (*Model, error) {
	if len(data) > MaxModelSize {
		return nil, fmt.Errorf("larger than %d octets", MaxModelSize)
	}
	var m Model
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&m); err != nil {
		if errors.Is(err, io.EOF) {
			return nil, errors.New("empty model")
		}
		return nil, err
	}
	return &m, nil
}

// Encode writes the model as JSON.
func (m *Model) Encode(indent bool) ([]byte, error) {
	if indent {
		return json.MarshalIndent(m, "", "  ")
	}
	return json.Marshal(m)
}

// totalsTypes maps the names of a TotalsSpec to its type.
var totalsTypes = map[string]iec102.TypeID{
	"operational": iec102.M_IT_TG_2, "absolute": iec102.M_IT_TG_2,
	"reset-operational": iec102.M_IT_TK_2, "incremental": iec102.M_IT_TK_2,
	"accounting": iec102.M_IT_TA_2, "reset-accounting": iec102.M_IT_TD_2,
}

// typeOf returns the type of a totals list.
func (t TotalsSpec) typeOf() (iec102.TypeID, error) {
	switch {
	case t.Type != "" && t.Kind != "":
		return 0, errors.New("give type or kind, not both")
	case t.Kind != "":
		typ, ok := totalsTypes[strings.ToLower(strings.TrimSpace(t.Kind))]
		if !ok {
			return 0, fmt.Errorf("invalid kind %q: use operational, reset-operational, accounting or reset-accounting", t.Kind)
		}
		return typ, nil
	case t.Type != "":
		typ, err := meter.ParseTypeID(t.Type)
		if _, ok := typ.IntegratedTotalsType(); err != nil || !ok {
			return 0, fmt.Errorf("invalid type %q: use M_IT_TA_2 .. M_IT_TM_2", t.Type)
		}
		return typ, nil
	}
	return 0, errors.New("give the type (M_IT_TA_2 .. M_IT_TM_2) or the kind of the totals")
}

// reset reports a periodical reset type: its totals are the increments of a period.
func reset(t iec102.TypeID) bool {
	f := t.Family()
	return f == iec102.FamilyResetAccounting || f == iec102.FamilyResetOperational
}
