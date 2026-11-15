// SPDX-License-Identifier: MIT

package station

import "testing"

// FuzzParseModel: the model loader never panics, and a model it accepts builds a device
// whose simulated indications all have a positive period.
func FuzzParseModel(f *testing.F) {
	f.Add([]byte(minimal), true)
	for _, simulate := range []string{"86400000", "86400001", "9300000000000"} {
		f.Add([]byte(minimal[:len(minimal)-1]+`, "indications": [{"fun": 1, "inf": 1, "dpi": 1, "simulate": `+simulate+`}]}`), true)
	}
	f.Add([]byte(DemoYAML), false)
	f.Add([]byte(`{"station": {"commonAddress": 300}}`), true)
	f.Add([]byte("station: {linkAddress: -1}\ngeneric: [{gin: {group: 1, entry: 1}, type: 4, size: 2, value: 99999}]\n"), false)
	f.Add([]byte("disturbances: [{fan: 1, time: x}]"), false)
	f.Fuzz(func(t *testing.T, data []byte, asJSON bool) {
		m, err := Parse(data, asJSON)
		if err != nil {
			return
		}
		st, err := New(m, Options{LinkAddr: -1, CommonAddr: -1})
		if err != nil {
			t.Fatalf("valid model refused by New: %v", err)
		}
		for i, ind := range m.Indications {
			if ind.Simulate != 0 && st.periods[i] <= 0 {
				t.Fatalf("indications[%d]: simulate %d gives the period %v", i, ind.Simulate, st.periods[i])
			}
		}
		if _, err := m.Encode(asJSON); err != nil {
			t.Fatal(err)
		}
	})
}
