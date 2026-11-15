// SPDX-License-Identifier: MIT

package types

import (
	"encoding/json"
	"strings"
	"testing"
)

// A point and a server event without a value but with non_finite show the name in text
// and table output, and encode as "value": null (point) or without a value (event).
func TestNonFiniteText(t *testing.T) {
	p := Point{CommonAddr: 1, IOA: 7, Type: "M_ME_NC_1", Cause: "spontaneous", NonFinite: "-Inf"}
	if p.ValueText() != "-Inf" || !strings.Contains(p.Line(), "-Inf") || p.Row()[3] != "-Inf" {
		t.Errorf("point: %q, %q, %q", p.ValueText(), p.Line(), p.Row())
	}
	if (Point{}).ValueText() != "-" {
		t.Error("a point without a value")
	}
	data, err := json.Marshal(p)
	if err != nil || !strings.Contains(string(data), `"value":null,"non_finite":"-Inf"`) {
		t.Errorf("point JSON %s, %v", data, err)
	}
	if data, _ := json.Marshal(Point{Value: 1.5}); strings.Contains(string(data), "non_finite") {
		t.Errorf("a finite point: %s", data)
	}
	e := &ServerEvent{Timestamp: "t", Event: "command", CommonAddr: 5, IOA: 1000, Type: "C_SE_NC_1", NonFinite: "NaN", Detail: "refused: x"}
	text, _ := e.MarshalTextOutput()
	if !strings.Contains(text, "  NaN  refused: x") {
		t.Errorf("event text %q", text)
	}
	if row := e.TableRows()[0]; row[6] != "NaN" {
		t.Errorf("event row %q", row)
	}
	data, err = json.Marshal(e)
	if err != nil || strings.Contains(string(data), `"value"`) || !strings.Contains(string(data), `"non_finite":"NaN"`) {
		t.Errorf("event JSON %s, %v", data, err)
	}
}
