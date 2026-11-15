// SPDX-License-Identifier: MIT

package cmd

import (
	"net"
	"strconv"
	"sync"
	"testing"

	"github.com/otfabric/go-iec60870-5/asdu"
	"github.com/otfabric/go-iec60870-5/ft12"
	"github.com/otfabric/go-iec60870-5/iec101"
	"github.com/otfabric/go-iec60870-5/link"
)

// keepingStation is a balanced controlled station that has one event to send and keeps it
// until a controlling station confirms it positively, as a station keeps its events across
// connections. On every connection it sends the event once: before it answers the first
// request status of link (so that a probe sees the event before its answer), or after the
// reset of remote link of a controlling station that brings the link up.
type keepingStation struct {
	ln    net.Listener
	event []byte
	wg    sync.WaitGroup

	mu       sync.Mutex
	kept     bool // the event is still to be delivered
	positive int  // positive acknowledgements of the event
	negative int  // negative acknowledgements of the event
	conns    []net.Conn
}

// The link address of the station; the controlling station uses the same.
const keepingAddr = 3

var keepingFT12 = ft12.Params{AddrSize: 1}

func startKeepingStation(t *testing.T) *keepingStation {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ev, err := asdu.New(asdu.CauseSpontaneous, 1, asdu.SinglePoint{IOA: 100, Value: true, Time: asdu.Now()}).Encode(iec101.DefaultParams().ASDU)
	if err != nil {
		t.Fatal(err)
	}
	k := &keepingStation{ln: ln, event: ev, kept: true}
	k.wg.Go(func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			k.mu.Lock()
			k.conns = append(k.conns, c)
			k.mu.Unlock()
			k.wg.Go(func() { k.serve(c) })
		}
	})
	t.Cleanup(func() {
		_ = ln.Close()
		k.mu.Lock()
		for _, c := range k.conns {
			_ = c.Close()
		}
		k.mu.Unlock()
		k.wg.Wait()
	})
	return k
}

func (k *keepingStation) counts() (kept bool, positive, negative int) {
	k.mu.Lock()
	defer k.mu.Unlock()
	return k.kept, k.positive, k.negative
}

// flags are the client flags that reach the station.
func (k *keepingStation) flags() []string {
	return []string{"--ip", "127.0.0.1", "--port", strconv.Itoa(k.ln.Addr().(*net.TCPAddr).Port),
		"--link-mode", "balanced", "--link-address", strconv.Itoa(keepingAddr), "--link-address-size", "1",
		"--cot-size", "1", "--ca-size", "1", "--ioa-size", "2", "--link-timeout", "1000"}
}

func (k *keepingStation) serve(c net.Conn) {
	defer func() { _ = c.Close() }()
	const none = 0xFF
	var (
		last          byte = none // the function of the primary frame that waits for its answer
		fcb           bool
		tried         bool // the event was offered on this connection
		pendingStatus bool // a request status of link waits for the event's answer
	)
	write := func(f ft12.Frame) bool {
		f.Addr = keepingAddr
		b, err := f.Encode(keepingFT12)
		if err != nil {
			return false
		}
		_, err = c.Write(b)
		return err == nil
	}
	secondary := func(fn byte) ft12.Frame {
		return ft12.Frame{Kind: ft12.KindFixed, Control: byte(link.SecondaryControl(fn, false, false, false))}
	}
	primary := func(fn byte, data []byte) ft12.Frame {
		f := ft12.Frame{Kind: ft12.KindFixed, Control: byte(link.PrimaryControl(fn, false, false, false))}
		if data != nil {
			f = ft12.Frame{Kind: ft12.KindVariable, Control: byte(link.PrimaryControl(fn, fcb, true, false)), Data: data}
			fcb = !fcb
		}
		return f
	}
	// offer starts the delivery of the event on this connection: reset of the peer's link
	// first, then the event.
	offer := func() bool {
		k.mu.Lock()
		kept := k.kept
		k.mu.Unlock()
		if !kept || tried {
			return false
		}
		tried = true
		last = link.FuncResetLink
		return write(primary(link.FuncResetLink, nil))
	}
	dec := ft12.NewDecoder(c, keepingFT12)
	for {
		f, err := dec.Next()
		if err != nil {
			return
		}
		ctl := link.Control(f.Control)
		if f.Kind == ft12.KindSingle || !ctl.PRM() {
			// The answer to this station's primary frame.
			ack := f.Kind == ft12.KindSingle || ctl.Func() == link.FuncAck
			switch last {
			case link.FuncResetLink:
				last = link.FuncUserDataConf
				if !write(primary(link.FuncUserDataConf, k.event)) {
					return
				}
				continue
			case link.FuncUserDataConf:
				k.mu.Lock()
				if ack {
					k.positive++
					k.kept = false
				} else {
					k.negative++
				}
				k.mu.Unlock()
			}
			last = none
			if pendingStatus {
				pendingStatus = false
				if !write(secondary(link.FuncStatus)) {
					return
				}
			}
			continue
		}
		var ok bool
		switch ctl.Func() {
		case link.FuncRequestStatus:
			if offer() {
				pendingStatus = true
				continue
			}
			ok = write(secondary(link.FuncStatus))
		case link.FuncResetLink:
			ok = write(secondary(link.FuncAck))
			if ok && last == none {
				offer()
			}
		case link.FuncResetUser, link.FuncTestLink, link.FuncUserDataConf:
			ok = write(secondary(link.FuncAck))
		default:
			ok = write(secondary(link.FuncNotImplemented))
		}
		if !ok {
			return
		}
	}
}

// A balanced station's own user data is not taken by `client ping --method link-status` or
// by `discover --link-mode balanced`: it is answered with a negative acknowledgement, and
// the station keeps its event for the controlling station, which receives it afterwards.
func TestLinkProbesLeaveTheStationsData(t *testing.T) {
	k := startKeepingStation(t)
	cli := func(args ...string) result {
		t.Helper()
		return run(t, append(args, k.flags()...)...)
	}

	cli("client", "ping", "--method", "link-status", "--count", "2", "--interval", "50").want(t, 0)
	if kept, pos, neg := k.counts(); !kept || pos != 0 || neg != 1 {
		t.Fatalf("after client ping: kept %v, %d positive and %d negative acknowledgements; want the event kept, 0 and 1", kept, pos, neg)
	}

	port := strconv.Itoa(k.ln.Addr().(*net.TCPAddr).Port)
	r := run(t, "discover", "--subnets", "127.0.0.1", "--ports", port, "--link-mode", "balanced",
		"--link-address", strconv.Itoa(keepingAddr), "--timeout", "2000", "--format", "json").want(t, 0)
	r.has(t, `"status": "iec101"`)
	if kept, pos, neg := k.counts(); !kept || pos != 0 || neg != 2 {
		t.Fatalf("after discover: kept %v, %d positive and %d negative acknowledgements; want the event kept, 0 and 2", kept, pos, neg)
	}

	// The controlling station that comes next receives the event. (--duration rather than
	// --count: the station sends the event as soon as the link is up.)
	r = cli("client", "monitor", "--duration", "1", "--format", "json").want(t, 0)
	if lines := r.lines(t); len(lines) != 1 || lines[0]["type"] != "M_SP_TB_1" || lines[0]["ioa"] != float64(100) {
		t.Fatalf("monitor: %s", r.stdout)
	}
	if kept, pos, _ := k.counts(); kept || pos != 1 {
		t.Errorf("after the monitor: kept %v, %d positive acknowledgements; want delivered once", kept, pos)
	}
}
