// SPDX-License-Identifier: MIT

package station_test

import (
	"bytes"
	"testing"

	"github.com/otfabric/go-iec60870-5/asdu"
	"github.com/otfabric/iec60870ctl/internal/iec104ctl/stationtest"
)

// uploadLimits is a station that takes three files of up to 100 octets, 250 octets
// together.
const uploadLimits = `
station: {common_address: 1}
points:
  - {ioa: 100, type: M_SP_NA_1, value: true}
uploads: {accept: true, max_files: 3, max_file_size: 100, max_total_size: 250}
`

// announce offers a file of length octets at ioa and reports whether the station called it.
func (p *peer) announce(ioa asdu.IOA, name uint16, length uint32) bool {
	p.t.Helper()
	p.send(asdu.New(asdu.CauseFileTransfer, 1, asdu.FileReady{IOA: ioa, Name: name, Length: length}))
	a := p.expect("the answer to the file ready", func(a *asdu.ASDU) bool {
		return a.Type == asdu.F_SC_NA_1 || a.Type == asdu.F_FR_NA_1
	})
	return a.Type == asdu.F_SC_NA_1 && !a.Negative
}

// deliver sends the file data, announced at ioa and called, in one section and ends it.
func (p *peer) deliver(ioa asdu.IOA, name uint16, data []byte) {
	p.t.Helper()
	var sum uint8
	for _, b := range data {
		sum += b
	}
	file := func(o asdu.InformationObject) { p.send(asdu.New(asdu.CauseFileTransfer, 1, o)) }
	ack := func(what string) uint8 {
		a := p.expect(what, func(a *asdu.ASDU) bool { return a.Type == asdu.F_AF_NA_1 })
		return a.First().(asdu.FileAck).Qualifier
	}
	file(asdu.SectionReady{IOA: ioa, Name: name, Section: 1, Length: uint32(len(data))})
	p.expect("the call of the section", func(a *asdu.ASDU) bool { return a.Type == asdu.F_SC_NA_1 })
	file(asdu.FileSegment{IOA: ioa, Name: name, Section: 1, Data: data})
	file(asdu.FileLastSegment{IOA: ioa, Name: name, Section: 1, Qualifier: asdu.LastSectionNoDeactivation, Checksum: sum})
	if q := ack("the acknowledgement of the section"); q != asdu.AckSectionPositive {
		p.t.Fatalf("section acknowledged with %d", q)
	}
	file(asdu.FileLastSegment{IOA: ioa, Name: name, Qualifier: asdu.LastFileNoDeactivation, Checksum: sum})
	if q := ack("the acknowledgement of the file"); q != asdu.AckFilePositive {
		p.t.Fatalf("file acknowledged with %d", q)
	}
}

// The files being delivered count against the upload limits from the moment they are
// accepted, across sessions; a delivery gives its place back when it is stored (it then
// counts as delivered), when it is abandoned and when its session ends.
func TestUploadsInProgressCountAgainstTheLimits(t *testing.T) {
	// The parameters of stationtest send no test frames: a peer that reads nothing for a
	// while keeps its session.
	st := stationtest.Start(t, uploadLimits)
	a, b := dialPeer(t, st), dialPeer(t, st)
	a.startDT()
	b.startDT()

	if !a.announce(600, 1, 100) || !a.announce(601, 1, 100) {
		t.Fatal("two files within the limits were refused")
	}
	// 200 octets are being delivered: 100 more do not fit 250, from any session.
	if b.announce(700, 1, 100) {
		t.Error("a file beyond max_total_size, counting the files being delivered, was accepted")
	}
	if !b.announce(701, 1, 50) {
		t.Fatal("a file that fits was refused")
	}
	// Three addresses are taken by files being delivered.
	if b.announce(702, 1, 0) {
		t.Error("a file beyond max_files, counting the files being delivered, was accepted")
	}

	// Stored, the 50 octets count once, as a delivered file.
	b.deliver(701, 1, bytes.Repeat([]byte{1}, 50))
	if data, ok := st.Uploaded(701); !ok || len(data) != 50 {
		t.Fatalf("the delivered file: %d octets, %v", len(data), ok)
	}
	if b.announce(702, 1, 1) {
		t.Error("a file was accepted while the delivered and the delivering files fill the limits")
	}

	// An abandoned delivery gives its place back.
	a.send(asdu.New(asdu.CauseFileTransfer, 1, asdu.FileLastSegment{IOA: 601, Name: 1, Qualifier: asdu.LastFileDeactivation}))
	eventually(t, "the place of the abandoned delivery", func() bool { return b.announce(702, 1, 100) })

	// The deliveries of a session that ends give their places back: 600 (100 octets).
	_ = a.conn.Close()
	eventually(t, "the places of the session that ended", func() bool { return b.announce(703, 1, 100) })
	// 701 is delivered (50), 702 and 703 are being delivered (200): full again.
	if b.announce(704, 1, 1) {
		t.Error("a file was accepted beyond the limits after the session ended")
	}
}
