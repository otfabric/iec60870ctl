// SPDX-License-Identifier: MIT

package station_test

// The simulated station (`iec104ctl server`) against a hostile controlling station:
// malformed frames, requests without objects, uploads that are announced huge or arrive out
// of order, floods, peers that say nothing, and more connections than allowed. Every case
// checks that the station ends what it must within a bound, keeps serving others, and leaves
// no goroutine behind once closed.

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/otfabric/go-iec104/apci"
	"github.com/otfabric/go-iec104/asdu"
	"github.com/otfabric/go-iec104/client"
	"github.com/otfabric/iec104ctl/internal/station"
	"github.com/otfabric/iec104ctl/internal/stationtest"
)

// hostileParams are short timers, so that a peer that stops talking is noticed quickly.
func hostileParams() apci.Params {
	return apci.Params{K: 12, W: 8, T0: time.Second, T1: 500 * time.Millisecond, T2: 100 * time.Millisecond, T3: 300 * time.Millisecond}
}

// closeBound is how long the station may take to end a connection that broke the protocol
// or fell silent: t3, t1 and slack for the race detector.
const closeBound = 4 * time.Second

// startHostile starts a station with short timers. The returned function closes it and
// fails t when goroutines started since are still running.
func startHostile(t *testing.T, edit ...func(*station.Options)) (*stationtest.Station, func()) {
	t.Helper()
	baseline := runtime.NumGoroutine()
	st := stationtest.Start(t, stationtest.Model, append([]func(*station.Options){func(o *station.Options) { o.Link = hostileParams() }}, edit...)...)
	return st, func() {
		t.Helper()
		_ = st.Close()
		deadline := time.Now().Add(5 * time.Second)
		for runtime.NumGoroutine() > baseline && time.Now().Before(deadline) {
			time.Sleep(10 * time.Millisecond)
		}
		if n := runtime.NumGoroutine(); n > baseline {
			buf := make([]byte, 1<<16)
			buf = buf[:runtime.Stack(buf, true)]
			t.Errorf("%d goroutines before, %d after the station was closed:\n%s", baseline, n, buf)
		}
	}
}

// peer is a controlling station that speaks raw APDUs.
type peer struct {
	t      *testing.T
	conn   net.Conn
	vs, vr uint16
}

func dialPeer(t *testing.T, st *stationtest.Station) *peer {
	t.Helper()
	conn, err := net.DialTimeout("tcp", st.Addr(), 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return &peer{t: t, conn: conn}
}

func (p *peer) write(b []byte) error {
	_ = p.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
	_, err := p.conn.Write(b)
	return err
}

func (p *peer) frame(f apci.Frame) {
	p.t.Helper()
	b, err := f.MarshalBinary()
	if err != nil {
		p.t.Fatal(err)
	}
	if err := p.write(b); err != nil {
		p.t.Fatalf("write %s: %v", f, err)
	}
}

func (p *peer) raw(asduOctets []byte) {
	p.t.Helper()
	p.frame(apci.NewI(p.vs, p.vr, asduOctets))
	p.vs = apci.SeqNext(p.vs)
}

func (p *peer) send(a *asdu.ASDU) {
	p.t.Helper()
	b, err := a.Encode(asdu.IEC104)
	if err != nil {
		p.t.Fatal(err)
	}
	p.raw(b)
}

// next returns the next ASDU of the station, acknowledging it and confirming test frames.
func (p *peer) next() (*asdu.ASDU, error) {
	deadline := time.Now().Add(5 * time.Second)
	for {
		_ = p.conn.SetReadDeadline(deadline)
		f, err := apci.ReadFrame(p.conn)
		if err != nil {
			return nil, err
		}
		switch {
		case f.Format == apci.FormatI:
			p.vr = apci.SeqNext(p.vr)
			p.frame(apci.NewS(p.vr))
			return asdu.Decode(f.ASDU, asdu.IEC104)
		case f.Format == apci.FormatU && f.Function == apci.TestFRAct:
			p.frame(apci.NewU(apci.TestFRCon))
		}
	}
}

func (p *peer) expect(what string, match func(*asdu.ASDU) bool) *asdu.ASDU {
	p.t.Helper()
	for {
		a, err := p.next()
		if err != nil {
			p.t.Fatalf("waiting for %s: %v", what, err)
		}
		if match(a) {
			return a
		}
	}
}

func (p *peer) startDT() {
	p.t.Helper()
	p.frame(apci.NewU(apci.StartDTAct))
	_ = p.conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	for {
		f, err := apci.ReadFrame(p.conn)
		if err != nil {
			p.t.Fatalf("STARTDT: %v", err)
		}
		if f.Format == apci.FormatU && f.Function == apci.StartDTCon {
			return
		}
	}
}

// interrogate proves the connection is still served.
func (p *peer) interrogate() {
	p.t.Helper()
	p.send(asdu.New(asdu.CauseActivation, 1, asdu.Interrogation{Qualifier: asdu.QOIStation}))
	p.expect("the end of the interrogation", func(a *asdu.ASDU) bool {
		if a.Type == asdu.C_IC_NA_1 && a.Negative {
			p.t.Fatalf("interrogation refused: %s", a)
		}
		return a.Type == asdu.C_IC_NA_1 && a.Cause == asdu.CauseActivationTerm
	})
}

// expectClosed waits until the station has closed the connection.
func (p *peer) expectClosed(bound time.Duration) {
	p.t.Helper()
	_ = p.conn.SetReadDeadline(time.Now().Add(bound))
	_, err := io.Copy(io.Discard, p.conn)
	var ne net.Error
	if errors.As(err, &ne) && ne.Timeout() {
		p.t.Fatalf("connection still open after %s", bound)
	}
}

// served proves the station serves a well-behaved controlling station.
func served(t *testing.T, st *stationtest.Station) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, err := client.Dial(ctx, st.Addr(), client.WithParams(hostileParams()), client.WithRequestTimeout(3*time.Second))
	if err != nil {
		t.Fatalf("a well-behaved client cannot connect: %v", err)
	}
	defer func() { _ = c.Close() }()
	if data, err := c.Interrogate(ctx, 1, asdu.QOIStation); err != nil || len(data) == 0 {
		t.Fatalf("a well-behaved client is not served: %d ASDUs, %v", len(data), err)
	}
}

// Every request type the station handles, sent without an information object (a VSQ of
// zero): refused, and the connection goes on being served.
func TestHostileRequestsWithoutObjects(t *testing.T) {
	st, closed := startHostile(t)
	p := dialPeer(t, st)
	p.startDT()
	for _, c := range []struct {
		typ   asdu.TypeID
		cause asdu.Cause
	}{
		{asdu.C_IC_NA_1, asdu.CauseActivation}, {asdu.C_CI_NA_1, asdu.CauseActivation}, {asdu.C_RD_NA_1, asdu.CauseRequest},
		{asdu.C_CS_NA_1, asdu.CauseActivation}, {asdu.C_TS_TA_1, asdu.CauseActivation}, {asdu.C_RP_NA_1, asdu.CauseActivation},
		{asdu.C_SC_NA_1, asdu.CauseActivation}, {asdu.C_DC_NA_1, asdu.CauseActivation}, {asdu.C_RC_NA_1, asdu.CauseActivation},
		{asdu.C_SE_NA_1, asdu.CauseActivation}, {asdu.C_SE_NB_1, asdu.CauseActivation}, {asdu.C_SE_NC_1, asdu.CauseActivation},
		{asdu.C_BO_NA_1, asdu.CauseActivation}, {asdu.C_SC_TA_1, asdu.CauseActivation}, {asdu.C_SE_TC_1, asdu.CauseDeactivation},
		{asdu.F_SC_NA_1, asdu.CauseFileTransfer}, {asdu.F_FR_NA_1, asdu.CauseFileTransfer}, {asdu.F_SR_NA_1, asdu.CauseFileTransfer},
		{asdu.F_LS_NA_1, asdu.CauseFileTransfer}, {asdu.F_AF_NA_1, asdu.CauseFileTransfer},
	} {
		p.raw([]byte{byte(c.typ), 0x00, byte(c.cause), 0, 1, 0})
		a := p.expect(fmt.Sprintf("the refusal of an empty %s", c.typ), func(a *asdu.ASDU) bool { return a.Type == c.typ })
		if !a.Negative {
			t.Errorf("an empty %s was answered %s", c.typ, a)
		}
	}
	// A segment carries its data in a single object: one without is undecodable and dropped.
	p.raw([]byte{byte(asdu.F_SG_NA_1), 0x00, byte(asdu.CauseFileTransfer), 0, 1, 0})
	p.interrogate()
	served(t, st)
	closed()
}

// Frames that break the APCI end that connection, within the bound, and nothing else.
func TestHostileMalformedFrames(t *testing.T) {
	st, closed := startHostile(t)
	for _, c := range []struct {
		name  string
		start bool
		send  []byte
	}{
		{"wrong start octet", false, []byte{0x69, 0x04, 0x07, 0x00, 0x00, 0x00}},
		{"length below the control field", false, []byte{0x68, 0x02, 0x07, 0x00}},
		{"U frame with two functions", false, []byte{0x68, 0x04, 0x43 | 0x10, 0x00, 0x00, 0x00}},
		{"I frame before STARTDT", false, []byte{0x68, 0x0E, 0x00, 0x00, 0x00, 0x00, 100, 0x01, 0x06, 0x00, 0x01, 0x00, 0x00, 0x00, 0x00, 20}},
		{"I frame out of sequence", true, []byte{0x68, 0x0E, 0x08, 0x00, 0x00, 0x00, 100, 0x01, 0x06, 0x00, 0x01, 0x00, 0x00, 0x00, 0x00, 20}},
		{"S frame acknowledging what was never sent", true, []byte{0x68, 0x04, 0x01, 0x00, 0x20, 0x00}},
		{"length that the octets never reach", true, []byte{0x68, 0xFD, 0x00, 0x00}},
	} {
		t.Run(c.name, func(t *testing.T) {
			p := dialPeer(t, st)
			if c.start {
				p.startDT()
			}
			if err := p.write(c.send); err != nil {
				t.Fatal(err)
			}
			// A frame that never completes is a silent peer: t1 of the test frame ends it.
			p.expectClosed(closeBound)
		})
	}
	served(t, st)
	closed()
}

// A peer that connects and says nothing, not even STARTDT, and does not answer test
// frames: the station ends the connection after t3 and t1.
func TestHostileSilentPeer(t *testing.T) {
	st, closed := startHostile(t)
	p := dialPeer(t, st)
	p.expectClosed(closeBound)
	served(t, st)
	closed()
}

// Uploads a hostile peer may try: the largest file announced (nothing is reserved for it),
// sections out of order, segments without a section, more data than announced, an address
// that holds a point. Each is refused or acknowledged negatively, nothing is stored, and the
// connection goes on being served.
func TestHostileUploads(t *testing.T) {
	st, closed := startHostile(t)
	p := dialPeer(t, st)
	p.startDT()
	const ioa, name = 40000, 3
	file := func(o asdu.InformationObject) { p.send(asdu.New(asdu.CauseFileTransfer, 1, o)) }
	reply := func(what string, typ asdu.TypeID) *asdu.ASDU {
		return p.expect(what, func(a *asdu.ASDU) bool { return a.Type == typ })
	}
	refused := func(what string, typ asdu.TypeID) {
		t.Helper()
		if a := reply(what, typ); !a.Negative || a.Cause != asdu.CauseUnknownIOA {
			t.Errorf("%s: answered %s", what, a)
		}
	}

	// The largest file announced: above the default uploads.max_file_size, refused.
	file(asdu.FileReady{IOA: ioa, Name: name, Length: asdu.MaxFileLength})
	refused("the largest file announced", asdu.F_FR_NA_1)
	// A file within the limits: called.
	file(asdu.FileReady{IOA: ioa, Name: name, Length: 4096})
	if call, ok := reply("the call of the file", asdu.F_SC_NA_1).First().(asdu.FileCall); !ok || call.Qualifier != asdu.FileRequest {
		t.Fatalf("the announced file was answered %+v", call)
	}
	// Section 2 before section 1.
	file(asdu.SectionReady{IOA: ioa, Name: name, Section: 2, Length: 10})
	refused("section 2 first", asdu.F_SR_NA_1)
	// A segment without a section.
	file(asdu.FileSegment{IOA: ioa, Name: name, Section: 1, Data: []byte("x")})
	refused("a segment without a section", asdu.F_SG_NA_1)
	// The end of the file with nothing delivered.
	file(asdu.FileLastSegment{IOA: ioa, Name: name, Section: 1, Qualifier: asdu.LastFileNoDeactivation})
	if a := reply("the end of an empty file", asdu.F_AF_NA_1); a.First().(asdu.FileAck).Qualifier != asdu.AckFileNegative {
		t.Errorf("the end of an empty file was acknowledged %s", a)
	}

	// A section that carries more than it announced.
	file(asdu.FileReady{IOA: ioa + 1, Name: name, Length: 4})
	reply("the call of the second file", asdu.F_SC_NA_1)
	file(asdu.SectionReady{IOA: ioa + 1, Name: name, Section: 1, Length: 4})
	reply("the call of its section", asdu.F_SC_NA_1)
	file(asdu.FileSegment{IOA: ioa + 1, Name: name, Section: 1, Data: []byte("more than four")})
	file(asdu.FileLastSegment{IOA: ioa + 1, Name: name, Section: 1, Qualifier: asdu.LastSectionNoDeactivation})
	if a := reply("the acknowledgement of the section", asdu.F_AF_NA_1); a.First().(asdu.FileAck).Qualifier != asdu.AckSectionNegative {
		t.Errorf("an overlong section was acknowledged %s", a)
	}

	// An address that holds a point.
	file(asdu.FileReady{IOA: 100, Name: name, Length: 4})
	refused("a file at a point's address", asdu.F_FR_NA_1)

	for _, at := range []uint32{ioa, ioa + 1, 100} {
		if _, ok := st.Uploaded(at); ok {
			t.Errorf("something was stored at %d", at)
		}
	}
	p.interrogate()
	served(t, st)
	closed()
}

// A peer that sends requests and never acknowledges the answers: the station stops at k
// unacknowledged frames and ends the connection after t1.
func TestHostileRequestFloodWithoutAcknowledgement(t *testing.T) {
	st, closed := startHostile(t)
	p := dialPeer(t, st)
	p.startDT()
	req := asdu.New(asdu.CauseActivation, 1, asdu.Interrogation{Qualifier: asdu.QOIStation})
	b, err := req.Encode(asdu.IEC104)
	if err != nil {
		t.Fatal(err)
	}
	for range 200 {
		f, _ := apci.NewI(p.vs, 0, b).MarshalBinary()
		p.vs = apci.SeqNext(p.vs)
		if p.write(f) != nil {
			break // the station has already closed
		}
	}
	p.expectClosed(closeBound)
	served(t, st)
	closed()
}

// A flood of test frames is answered and leaves the connection usable.
func TestHostileTestFrameFlood(t *testing.T) {
	st, closed := startHostile(t)
	p := dialPeer(t, st)
	p.startDT()
	act, _ := apci.NewU(apci.TestFRAct).MarshalBinary()
	const n = 2000
	var wg sync.WaitGroup
	wg.Go(func() {
		for range n {
			if p.write(act) != nil {
				return
			}
		}
	})
	got := 0
	_ = p.conn.SetReadDeadline(time.Now().Add(10 * time.Second))
	for got < n {
		f, err := apci.ReadFrame(p.conn)
		if err != nil {
			t.Fatalf("after %d confirmations: %v", got, err)
		}
		if f.Format == apci.FormatU && f.Function == apci.TestFRCon {
			got++
		}
	}
	wg.Wait()
	p.interrogate()
	closed()
}

// More connections than --max-sessions: the extra ones are closed at once, the allowed ones
// served, and when they go the station accepts again.
func TestHostileManyConnections(t *testing.T) {
	const allowed = 3
	st, closed := startHostile(t, func(o *station.Options) { o.MaxSessions = allowed })
	var kept []*peer
	for range allowed {
		p := dialPeer(t, st)
		p.startDT()
		kept = append(kept, p)
	}
	for range 20 {
		dialPeer(t, st).expectClosed(closeBound)
	}
	if n := st.Sessions(); n != allowed {
		t.Errorf("%d sessions, want %d", n, allowed)
	}
	for _, p := range kept {
		p.interrogate()
		_ = p.conn.Close()
	}
	eventually(t, "the sessions to end", func() bool { return st.Sessions() == 0 })
	served(t, st)
	closed()
}
