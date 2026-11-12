// SPDX-License-Identifier: MIT

package linecfg

import (
	"bytes"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/otfabric/go-iec60870-5/ft12"
	"github.com/otfabric/go-iec60870-5/iec103"
	"github.com/otfabric/iec60870ctl/internal/iec103ctl/config"
)

func TestTracerWritesFramesAndDiscards(t *testing.T) {
	var b bytes.Buffer
	onFrame, onDiscard := Tracer(&b)
	a, _ := iec103.New(9, 1, 255, 0, iec103.GIInit{SCN: 1}).Encode()
	onFrame(true, ft12.Frame{Kind: ft12.KindVariable, Data: a})
	onFrame(false, ft12.Frame{Kind: ft12.KindVariable, Data: []byte{0xFF}})
	onFrame(false, ft12.Frame{Kind: ft12.KindFixed})
	onDiscard([]byte{1, 2}, errors.New("checksum"))
	out := b.String()
	for _, want := range []string{"tx", "rx", "discarded 01 02: checksum", "general interrogation", " FF\n"} {
		if !strings.Contains(out, want) {
			t.Errorf("%q not in %q", want, out)
		}
	}
}

func TestLocation(t *testing.T) {
	for in, want := range map[string]*time.Location{"": time.UTC, "UTC": time.UTC, "local": time.Local} {
		if l, err := Location(in); err != nil || l != want {
			t.Errorf("%q: %v %v", in, l, err)
		}
	}
	if l, err := Location("Europe/Brussels"); err != nil || l.String() != "Europe/Brussels" {
		t.Fatal(l, err)
	}
	if _, err := Location("Mars/Base"); err == nil {
		t.Fatal("Mars accepted")
	}
}

func TestSerialSettings(t *testing.T) {
	s := LineSettings(config.SerialConfig{})
	if s.Baud != 9600 || s.DataBits != 8 || s.StopBits != 1 || s.Parity != "" {
		t.Fatalf("%+v", s)
	}
	if s := Settings(config.SerialConfig{Baud: 19200, Parity: "odd"}); s.Baud != 19200 || s.Parity != "odd" {
		t.Fatalf("%+v", s)
	}
	if err := CheckSerial(config.SerialConfig{Parity: "even"}); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []config.SerialConfig{{Parity: "mark"}, {DataBits: 3}, {StopBits: 7}} {
		if err := CheckSerial(bad); err == nil {
			t.Errorf("%+v accepted", bad)
		}
	}
}
