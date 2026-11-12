// SPDX-License-Identifier: MIT

package station

import "testing"

// FuzzParseModel: the model loader never panics, and a model it accepts builds a device.
func FuzzParseModel(f *testing.F) {
	f.Add([]byte(minimal), true)
	f.Add([]byte(DemoYAML), false)
	f.Add([]byte(`{"station": {"commonAddress": 300}}`), true)
	f.Add([]byte("station: {linkAddress: -1}\ngeneric: [{gin: {group: 1, entry: 1}, type: 4, size: 2, value: 99999}]\n"), false)
	f.Add([]byte("disturbances: [{fan: 1, time: x}]"), false)
	f.Fuzz(func(t *testing.T, data []byte, asJSON bool) {
		m, err := Parse(data, asJSON)
		if err != nil {
			return
		}
		if _, err := New(m, Options{LinkAddr: -1, CommonAddr: -1}); err != nil {
			t.Fatalf("valid model refused by New: %v", err)
		}
		if _, err := m.Encode(asJSON); err != nil {
			t.Fatal(err)
		}
	})
}
