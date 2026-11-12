// SPDX-License-Identifier: MIT

package station

import (
	"fmt"
	"slices"

	"github.com/otfabric/go-iec60870-5/iec103"
	"github.com/otfabric/go-iec60870-5/iec103/server"
	"github.com/otfabric/iec60870ctl/internal/iec103ctl/types"
)

// entry returns the generic entry g of the model.
func (s *Station) entry(g iec103.GIN) (*GenericDef, bool) {
	for i := range s.m.Generic {
		if gin(s.m.Generic[i].GIN) == g {
			return &s.m.Generic[i], true
		}
	}
	return nil, false
}

// attribute returns attribute kod of entry e: KOD 1 the actual value, KOD 10 the
// description; anything else does not exist. The caller holds s.mu.
func (s *Station) attribute(e *GenericDef, kod iec103.KOD) (iec103.Value, bool) {
	switch kod {
	case iec103.KODActualValue:
		return s.generic[gin(e.GIN)], true
	case iec103.KODDescription:
		return iec103.ASCII(e.Description), true
	default:
		// The device has two attributes per entry; every other KOD does not exist.
		return iec103.Value{}, false
	}
}

// groups returns the groups of the model in ascending order with their headings: the
// named groups and those of the entries.
func (s *Station) groups() []GroupDef {
	names := map[int]string{}
	for _, g := range s.m.Groups {
		names[g.Group] = g.Description
	}
	for _, e := range s.m.Generic {
		if _, ok := names[e.GIN.Group]; !ok {
			names[e.GIN.Group] = fmt.Sprintf("GROUP %d", e.GIN.Group)
		}
	}
	var out []GroupDef
	for g, d := range names {
		out = append(out, GroupDef{Group: g, Description: d})
	}
	slices.SortFunc(out, func(a, b GroupDef) int { return a.Group - b.Group })
	return out
}

func failed(g iec103.GIN, kod iec103.KOD, grc iec103.GRC) iec103.DataSet {
	return iec103.DataSet{GIN: g, KOD: kod, Value: iec103.Reply(grc)}
}

// genericRead answers a generic read (ASDU 21, cause 42, clause 7.4.8.5 and 7.4.8.6) and
// the general interrogation of generic data (cause 9, INF 245, clause 7.4.8.7). An
// identifier the device does not have is answered with cause 43 and a generic reply code:
// 1 (GIN not defined) or 2 (data do not exist).
func (s *Station) genericRead(srv *server.Server, a *iec103.ASDU, b iec103.GenericCommand) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if a.Inf == iec103.InfGenericGI && a.Cause == iec103.CauseGI {
		var sets []iec103.DataSet
		for _, e := range s.m.Generic {
			sets = append(sets, iec103.DataSet{GIN: gin(e.GIN), KOD: iec103.KODActualValue, Value: s.generic[gin(e.GIN)]})
		}
		_ = srv.AnswerGenericGI(a, sets...)
		s.event(types.ServerEvent{Event: "generic-interrogation", Value: len(sets), Detail: fmt.Sprintf("rii=%d", b.RII)})
		return
	}
	if a.Cause != iec103.CauseGenericRead {
		return
	}
	cause := iec103.CauseGenericReadValid
	var sets []iec103.DataSet
	// The generic reads of table 18; another information number is not answered.
	switch a.Inf {
	case iec103.InfReadHeadings:
		for _, g := range s.groups() {
			sets = append(sets, iec103.DataSet{GIN: iec103.GIN{Group: uint8(g.Group & 0xFF)}, KOD: iec103.KODDescription, Value: iec103.ASCII(g.Description)})
		}
	case iec103.InfReadGroup:
		for _, it := range b.Items {
			found := false
			for i := range s.m.Generic {
				e := &s.m.Generic[i]
				if uint8(e.GIN.Group&0xFF) != it.GIN.Group {
					continue
				}
				found = true
				if v, ok := s.attribute(e, it.KOD); ok {
					sets = append(sets, iec103.DataSet{GIN: gin(e.GIN), KOD: it.KOD, Value: v})
				} else {
					cause = iec103.CauseGenericReadInvalid
					sets = append(sets, failed(gin(e.GIN), it.KOD, iec103.GRCNotExist))
				}
			}
			if !found {
				cause = iec103.CauseGenericReadInvalid
				sets = append(sets, failed(iec103.GIN{Group: it.GIN.Group}, it.KOD, iec103.GRCInvalidGIN))
			}
		}
	case iec103.InfReadDirectory:
		s.directory(srv, a, b)
		return
	case iec103.InfReadEntry:
		for _, it := range b.Items {
			e, ok := s.entry(it.GIN)
			if !ok {
				cause = iec103.CauseGenericReadInvalid
				sets = append(sets, failed(it.GIN, it.KOD, iec103.GRCInvalidGIN))
				continue
			}
			if v, ok := s.attribute(e, it.KOD); ok {
				sets = append(sets, iec103.DataSet{GIN: it.GIN, KOD: it.KOD, Value: v})
			} else {
				cause = iec103.CauseGenericReadInvalid
				sets = append(sets, failed(it.GIN, it.KOD, iec103.GRCNotExist))
			}
		}
	default:
		return
	}
	if cause == iec103.CauseGenericReadInvalid {
		// Clause 7.4.8.6.2: the answer is marked with cause 43 and carries the GIN with a
		// generic reply code for each failed element.
		sets = slices.DeleteFunc(sets, func(d iec103.DataSet) bool { return d.Value.Type != iec103.DataGRC })
	}
	_ = srv.GenericAnswer(a, cause, sets...)
	s.event(types.ServerEvent{Event: "generic-read", Value: len(sets), Detail: fmt.Sprintf("inf=%d rii=%d cause=%d", a.Inf, b.RII, cause)})
}

// directory answers a read of the directory of one entry (INF 243, clause 7.4.8.6.1) with
// ASDU 11: its value and its description. The caller holds s.mu.
func (s *Station) directory(srv *server.Server, a *iec103.ASDU, b iec103.GenericCommand) {
	var g iec103.GIN
	if len(b.Items) > 0 {
		g = b.Items[0].GIN
	}
	e, ok := s.entry(g)
	if !ok {
		_ = srv.GenericIdentAnswer(a, iec103.CauseGenericReadInvalid, g, iec103.DescElement{KOD: iec103.KODNone, Value: iec103.Reply(iec103.GRCInvalidGIN)})
		s.event(types.ServerEvent{Event: "generic-read", Detail: fmt.Sprintf("inf=%d rii=%d cause=%d", a.Inf, b.RII, iec103.CauseGenericReadInvalid)})
		return
	}
	_ = srv.GenericIdentAnswer(a, iec103.CauseGenericReadValid, g,
		iec103.DescElement{KOD: iec103.KODActualValue, Value: s.generic[g]},
		iec103.DescElement{KOD: iec103.KODDescription, Value: iec103.ASCII(e.Description)})
	s.event(types.ServerEvent{Event: "generic-read", Value: 2, Detail: fmt.Sprintf("inf=%d rii=%d cause=%d", a.Inf, b.RII, iec103.CauseGenericReadValid)})
}

// check returns the generic reply code of writing d: 0 when it may be written.
func (s *Station) check(d iec103.DataSet) iec103.GRC {
	e, ok := s.entry(d.GIN)
	switch {
	case !ok:
		return iec103.GRCInvalidGIN
	case !e.Writable:
		return iec103.GRCReadOnly
	case d.KOD != iec103.KODActualValue || int(d.Value.Type) != e.Type || int(d.Value.Size) != e.Size || d.Value.Number != 1:
		return iec103.GRCOutOfRange
	}
	return iec103.GRCAck
}

// genericWrite carries out a generic write (ASDU 10, cause 40, clause 7.4.8.6.3, figure
// 17): write entry (248) stages the data sets, write with confirmation (249) stages and
// echoes them with cause 44, write with execution (250) applies every staged set and its
// own when all may be written (cause 40) or none (cause 41 with a reply code per failed
// set), write abort (251) drops what was staged.
func (s *Station) genericWrite(srv *server.Server, a *iec103.ASDU, b iec103.GenericData) {
	if a.Cause != iec103.CauseGenericWrite {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	// The writes of table 18; another information number is not answered.
	switch a.Inf {
	case iec103.InfWriteEntry:
		s.staged[b.RII] = append(s.staged[b.RII], b.Sets...)
	case iec103.InfWriteConfirm:
		s.staged[b.RII] = append(s.staged[b.RII], b.Sets...)
		_ = srv.GenericAnswer(a, iec103.CauseGenericWriteConf, b.Sets...)
	case iec103.InfWriteAbort:
		delete(s.staged, b.RII)
		_ = srv.GenericAnswer(a, iec103.CauseGenericWriteAck)
		s.event(types.ServerEvent{Event: "generic-write", Detail: fmt.Sprintf("rii=%d outcome=aborted", b.RII)})
	case iec103.InfWriteExecute:
		all := append(s.staged[b.RII], b.Sets...)
		delete(s.staged, b.RII)
		var nack []iec103.DataSet
		for _, d := range all {
			if grc := s.check(d); grc != iec103.GRCAck {
				nack = append(nack, failed(d.GIN, d.KOD, grc))
			}
		}
		if nack != nil {
			_ = srv.GenericAnswer(a, iec103.CauseGenericWriteNack, nack...)
			s.event(types.ServerEvent{Event: "generic-write", Value: len(all), Detail: fmt.Sprintf("rii=%d outcome=rejected", b.RII)})
			return
		}
		for _, d := range all {
			s.generic[d.GIN] = d.Value
		}
		_ = srv.GenericAnswer(a, iec103.CauseGenericWriteAck)
		s.event(types.ServerEvent{Event: "generic-write", Value: len(all), Detail: fmt.Sprintf("rii=%d outcome=executed", b.RII)})
	default:
	}
}
