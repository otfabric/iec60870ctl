// SPDX-License-Identifier: MIT

package iec

import (
	"context"
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/otfabric/go-iec104/asdu"
	"github.com/otfabric/go-iec104/client"
	"github.com/otfabric/iec104ctl/internal/config"
	"github.com/otfabric/iec104ctl/internal/errs"
	"github.com/otfabric/iec104ctl/internal/types"
)

// Command modes of `client command`.
const (
	ModeExecute = "execute"
	ModeSelect  = "select"
	ModeSBO     = "sbo"
	ModeCancel  = "cancel"
)

// CommandModes lists the values of --mode with a description for shell completion.
func CommandModes() map[string]string {
	return map[string]string{
		ModeExecute: "direct execute",
		ModeSelect:  "select only",
		ModeSBO:     "select, then execute",
		ModeCancel:  "select, then deactivate the selection",
	}
}

// commandKinds maps the names --type accepts to the type without time tag.
var commandKinds = map[string]asdu.TypeID{
	"single":              asdu.C_SC_NA_1,
	"double":              asdu.C_DC_NA_1,
	"step":                asdu.C_RC_NA_1,
	"setpoint-normalized": asdu.C_SE_NA_1,
	"setpoint-scaled":     asdu.C_SE_NB_1,
	"setpoint-float":      asdu.C_SE_NC_1,
	"bitstring":           asdu.C_BO_NA_1,
}

// CommandTypes lists the values of --type with a description for shell completion.
func CommandTypes() map[string]string {
	out := map[string]string{}
	for name, t := range commandKinds {
		out[name] = t.String() + ": " + t.Description()
	}
	return out
}

// CommandQualifiers lists the named values of --qualifier.
func CommandQualifiers() []string { return []string{"none", "short", "long", "persistent"} }

// timeTagged maps a command type to its CP56Time2a variant and back.
var timeTagged = map[asdu.TypeID]asdu.TypeID{
	asdu.C_SC_NA_1: asdu.C_SC_TA_1,
	asdu.C_DC_NA_1: asdu.C_DC_TA_1,
	asdu.C_RC_NA_1: asdu.C_RC_TA_1,
	asdu.C_SE_NA_1: asdu.C_SE_TA_1,
	asdu.C_SE_NB_1: asdu.C_SE_TB_1,
	asdu.C_SE_NC_1: asdu.C_SE_TC_1,
	asdu.C_BO_NA_1: asdu.C_BO_TA_1,
}

// commandType resolves --type to the type without time tag and whether the name given was
// the time-tagged variant.
func commandType(s string) (base asdu.TypeID, tagged bool, err error) {
	if t, ok := commandKinds[strings.ToLower(strings.TrimSpace(s))]; ok {
		return t, false, nil
	}
	t, perr := ParseType(s)
	if perr == nil {
		if _, ok := timeTagged[t]; ok {
			return t, false, nil
		}
		for b, tt := range timeTagged {
			if tt == t {
				return b, true, nil
			}
		}
	}
	return 0, false, fmt.Errorf("invalid --type %q: use single, double, step, setpoint-normalized, setpoint-scaled, setpoint-float or bitstring, or the name of a command type such as C_SC_NA_1", s)
}

func parseOnOff(s string) (on, ok bool) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "on", "1", "true", "close", "closed":
		return true, true
	case "off", "0", "false", "open":
		return false, true
	}
	return false, false
}

// commandSpec is a command ready to be sent, apart from the select bit and the time tag.
type commandSpec struct {
	base      asdu.TypeID
	tagged    bool
	ioa       asdu.IOA
	value     any // as shown in the result
	qualifier any
	build     func(sel bool, t asdu.Timestamp) asdu.InformationObject
	// selectable is false for a bitstring command, which has no select bit.
	selectable bool
}

func (c commandSpec) typeName() string {
	if c.tagged {
		return timeTagged[c.base].String()
	}
	return c.base.String()
}

func (c commandSpec) object(sel bool) asdu.InformationObject {
	var t asdu.Timestamp
	if c.tagged {
		t = asdu.Now()
	}
	return c.build(sel, t)
}

// parseCommand validates --type, --value, --qualifier and --ioa and builds the command.
func parseCommand(cfg *config.CommandConfig) (commandSpec, error) {
	var spec commandSpec
	base, tagged, err := commandType(cfg.Type)
	if err != nil {
		return spec, err
	}
	if cfg.IOA == 0 || cfg.IOA > MaxIOA {
		return spec, fmt.Errorf("invalid --ioa %d: the information object address of the command is required (1-%d)", cfg.IOA, MaxIOA)
	}
	if strings.TrimSpace(cfg.Value) == "" {
		return spec, fmt.Errorf("no value given: set --value")
	}
	spec = commandSpec{base: base, tagged: tagged || cfg.TimeTag, ioa: asdu.IOA(cfg.IOA), selectable: true}
	ioa, value := spec.ioa, strings.TrimSpace(cfg.Value)

	switch base {
	case asdu.C_SC_NA_1, asdu.C_DC_NA_1, asdu.C_RC_NA_1:
		q, err := parseCommandQualifier(cfg.Qualifier)
		if err != nil {
			return spec, err
		}
		spec.qualifier = commandQualifier(q)
		if q > asdu.QualifierPersistent {
			spec.qualifier = int(q)
		}
		switch base {
		case asdu.C_SC_NA_1:
			on, ok := parseOnOff(value)
			if !ok {
				return spec, fmt.Errorf("invalid --value %q for a single command: use on or off", value)
			}
			spec.value = on
			spec.build = func(sel bool, t asdu.Timestamp) asdu.InformationObject {
				return asdu.SingleCommand{IOA: ioa, Value: on, Qualifier: q, Select: sel, Time: t}
			}
		case asdu.C_DC_NA_1:
			on, ok := parseOnOff(value)
			if !ok {
				return spec, fmt.Errorf("invalid --value %q for a double command: use on or off", value)
			}
			state := asdu.DoubleOff
			if on {
				state = asdu.DoubleOn
			}
			spec.value = state.String()
			spec.build = func(sel bool, t asdu.Timestamp) asdu.InformationObject {
				return asdu.DoubleCommand{IOA: ioa, Value: state, Qualifier: q, Select: sel, Time: t}
			}
		default:
			var dir asdu.StepDirection
			switch strings.ToLower(value) {
			case "higher", "up", "raise":
				dir = asdu.StepHigher
			case "lower", "down":
				dir = asdu.StepLower
			default:
				return spec, fmt.Errorf("invalid --value %q for a regulating step command: use higher or lower", value)
			}
			spec.value = stepDirection(dir)
			spec.build = func(sel bool, t asdu.Timestamp) asdu.InformationObject {
				return asdu.StepCommand{IOA: ioa, Value: dir, Qualifier: q, Select: sel, Time: t}
			}
		}

	case asdu.C_SE_NA_1, asdu.C_SE_NB_1, asdu.C_SE_NC_1:
		ql, err := parseSetpointQualifier(cfg.Qualifier)
		if err != nil {
			return spec, err
		}
		spec.qualifier = int(ql)
		f, err := strconv.ParseFloat(value, 64)
		if err != nil || math.IsNaN(f) || math.IsInf(f, 0) {
			return spec, fmt.Errorf("invalid --value %q for a set point command: not a number", value)
		}
		switch base {
		case asdu.C_SE_NA_1:
			if f < -1 || f >= 1 {
				return spec, fmt.Errorf("invalid --value %v for a normalized set point: the range is -1 up to, not including, 1", f)
			}
			n := asdu.NormalizedFromFloat(f)
			spec.value = n.Float64()
			spec.build = func(sel bool, t asdu.Timestamp) asdu.InformationObject {
				return asdu.SetpointNormalized{IOA: ioa, Value: n, Qualifier: ql, Select: sel, Time: t}
			}
		case asdu.C_SE_NB_1:
			if f != math.Trunc(f) || f < math.MinInt16 || f > math.MaxInt16 {
				return spec, fmt.Errorf("invalid --value %q for a scaled set point: use a whole number from -32768 to 32767", value)
			}
			n := int16(f)
			spec.value = int(n)
			spec.build = func(sel bool, t asdu.Timestamp) asdu.InformationObject {
				return asdu.SetpointScaled{IOA: ioa, Value: n, Qualifier: ql, Select: sel, Time: t}
			}
		default:
			if math.Abs(f) > math.MaxFloat32 {
				return spec, fmt.Errorf("invalid --value %q for a short floating point set point: out of range", value)
			}
			n := float32(f)
			spec.value = n
			spec.build = func(sel bool, t asdu.Timestamp) asdu.InformationObject {
				return asdu.SetpointFloat{IOA: ioa, Value: n, Qualifier: ql, Select: sel, Time: t}
			}
		}

	default: // bitstring
		if q := strings.TrimSpace(cfg.Qualifier); q != "" && q != "none" && q != "0" {
			return spec, fmt.Errorf("a bitstring command has no qualifier: remove --qualifier")
		}
		n, err := strconv.ParseUint(value, 0, 32)
		if err != nil {
			return spec, fmt.Errorf("invalid --value %q for a bitstring command: use a number from 0 to 4294967295, or 0x hex", value)
		}
		if n > math.MaxUint32 {
			return spec, fmt.Errorf("invalid --value %q for a bitstring command: use a number from 0 to 4294967295, or 0x hex", value)
		}
		v := uint32(n)
		spec.value, spec.selectable = v, false
		spec.build = func(_ bool, t asdu.Timestamp) asdu.InformationObject {
			return asdu.BitstringCommand{IOA: ioa, Value: v, Time: t}
		}
	}
	return spec, nil
}

func parseCommandQualifier(s string) (asdu.CommandQualifier, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "none":
		return asdu.QualifierNone, nil
	case "short":
		return asdu.QualifierShortPulse, nil
	case "long":
		return asdu.QualifierLongPulse, nil
	case "persistent":
		return asdu.QualifierPersistent, nil
	}
	n, err := strconv.ParseUint(strings.TrimSpace(s), 10, 8)
	if err != nil || n > 31 {
		return 0, fmt.Errorf("invalid --qualifier %q: use none, short, long or persistent, or a number 0-31", s)
	}
	return asdu.CommandQualifier(n), nil
}

func parseSetpointQualifier(s string) (uint8, error) {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" || s == "none" {
		return 0, nil
	}
	n, err := strconv.ParseUint(s, 10, 8)
	if err != nil || n > 127 {
		return 0, fmt.Errorf("invalid --qualifier %q: the qualifier of a set point command is a number 0-127", s)
	}
	return uint8(n), nil
}

// CheckCommand validates the configuration of `client command`.
func CheckCommand(cfg *config.CommandConfig) error {
	if err := Validate(&cfg.ConnConfig); err != nil {
		return err
	}
	if err := CheckCommonAddr(cfg.CommonAddr, false); err != nil {
		return err
	}
	if err := oneOf("mode", cfg.Mode, ModeExecute, ModeSelect, ModeSBO, ModeCancel); err != nil {
		return err
	}
	spec, err := parseCommand(cfg)
	if err != nil {
		return err
	}
	if !spec.selectable && cfg.Mode != ModeExecute {
		return fmt.Errorf("a bitstring command has no select: use --mode execute")
	}
	if cfg.Termination != 0 && (cfg.Mode == ModeSelect || cfg.Mode == ModeCancel) {
		return fmt.Errorf("--wait-termination needs an execute: use --mode execute or sbo")
	}
	return nil
}

// terminations waits for the activation termination of one command, for --wait-termination.
// It keeps nothing of what else the station sends: a station that floods terminations of
// other objects costs neither memory nor a search per termination.
type terminations struct {
	typ  asdu.TypeID
	ca   asdu.CommonAddr
	ioa  asdu.IOA
	once sync.Once
	done chan struct{}
}

func newTerminations(typ asdu.TypeID, ca asdu.CommonAddr, ioa asdu.IOA) *terminations {
	return &terminations{typ: typ, ca: ca, ioa: ioa, done: make(chan struct{})}
}

func (t *terminations) HandleASDU(a *asdu.ASDU) {
	if a.Cause != asdu.CauseActivationTerm || a.Type != t.typ || a.CommonAddr != t.ca ||
		a.First() == nil || a.First().Address() != t.ioa {
		return
	}
	t.once.Do(func() { close(t.done) })
}

// wait returns true when the termination has arrived within d.
func (t *terminations) wait(ctx context.Context, d time.Duration) bool {
	deadline := time.NewTimer(d)
	defer deadline.Stop()
	select {
	case <-t.done:
		return true
	case <-deadline.C:
	case <-ctx.Done():
	}
	// A termination that arrived together with the deadline still counts.
	select {
	case <-t.done:
		return true
	default:
		return false
	}
}

// Command sends a process command: direct execute, select, select before operate, or a
// select followed by its deactivation. Everything is validated before anything is sent, and nothing
// is repeated: a command whose confirmation does not arrive is reported, not sent again.
func Command(ctx context.Context, cfg *config.CommandConfig, trace io.Writer) (*types.CommandResult, error) {
	spec, err := parseCommand(cfg)
	if err != nil {
		return nil, err
	}
	ca := asdu.CommonAddr(cfg.CommonAddr)
	typ := spec.base
	if spec.tagged {
		typ = timeTagged[spec.base]
	}
	term := newTerminations(typ, ca, spec.ioa)
	s, err := Dial(ctx, cfg.Conn(), trace, client.WithHandler(term))
	if err != nil {
		return nil, err
	}
	defer func() { _ = s.Close() }()

	res := &types.CommandResult{
		Target:     s.Target.String(),
		CommonAddr: cfg.CommonAddr,
		IOA:        cfg.IOA,
		Type:       spec.typeName(),
		Value:      spec.value,
		Mode:       cfg.Mode,
		Qualifier:  spec.qualifier,
	}
	step := func(name string, send func() error) error {
		begin := time.Now()
		if err := send(); err != nil {
			// Say which step failed: after a select that was confirmed, it matters.
			wrapped := WrapError(err)
			if te, ok := errs.As(wrapped); ok {
				named := *te
				named.Message = name + ": " + te.Error()
				return &named
			}
			return wrapped
		}
		res.Steps = append(res.Steps, types.CommandStep{Step: name, Result: "confirmed", RTTMs: ms(time.Since(begin))})
		return nil
	}

	switch cfg.Mode {
	case ModeSelect:
		err = step("select", func() error { return s.Command(ctx, ca, spec.object(true)) })
	case ModeCancel:
		// A selection belongs to the connection that made it, so the cancel that can be
		// shown from the command line is that of a selection of its own.
		if err = step("select", func() error { return s.Command(ctx, ca, spec.object(true)) }); err == nil {
			err = step("cancel", func() error { return s.Deactivate(ctx, ca, spec.object(true)) })
		}
	case ModeSBO:
		if err = step("select", func() error { return s.Command(ctx, ca, spec.object(true)) }); err == nil {
			err = step("execute", func() error { return s.Command(ctx, ca, spec.object(false)) })
		}
	default:
		err = step("execute", func() error { return s.Command(ctx, ca, spec.object(false)) })
	}
	if err != nil {
		return nil, err
	}
	if cfg.Termination != 0 {
		begin := time.Now()
		result := "not-received"
		if term.wait(ctx, time.Duration(cfg.Termination)*time.Millisecond) {
			result = "received"
		}
		res.Steps = append(res.Steps, types.CommandStep{Step: "termination", Result: result, RTTMs: ms(time.Since(begin))})
	}
	return res, nil
}
