// SPDX-License-Identifier: MIT

package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/otfabric/iec60870ctl/internal/iec102ctl/station"
	"github.com/otfabric/iec60870ctl/internal/iec102ctl/stationtest"
	"github.com/otfabric/iec60870ctl/internal/iec102ctl/types"
)

const answersHead = "station: {product: {standard: 1, manufacturer: 1, product: 1}}\n"

func startModel(t *testing.T, model string) *stationtest.Station {
	t.Helper()
	m, err := station.Parse([]byte(model))
	if err != nil {
		t.Fatal(err)
	}
	return stationtest.StartModel(t, m, station.Options{})
}

// reeReads returns a model with powers contracted powers and days special days.
func reeReads(powers, days int) string {
	var p, d []string
	for i := range powers {
		p = append(p, fmt.Sprintf("{address: %d, power_kw: %d}", i+1, 10+i))
	}
	for i := range days {
		d = append(d, fmt.Sprintf("2026-%02d-%02d", 1+i/28, 1+i%28))
	}
	return answersHead + "ree:\n  access_key: 7\n  contracted_powers: [" + strings.Join(p, ", ") + "]\n  special_days: [" + strings.Join(d, ", ") + "]\n"
}

// As many contracted powers and special days as one answer carries are served in full; a
// model with more is refused by `server start` before it serves.
func TestREEReadsServedInFullOrRefused(t *testing.T) {
	t.Setenv("IEC102CTL_ACCESS_KEY", "7")
	st := startModel(t, reeReads(48, 49))
	var powers types.PowersResult
	cli(t, st, "ree", "contracted-powers", "--format", "json").want(t, 0).decode(t, &powers)
	var days types.SpecialDaysResult
	cli(t, st, "ree", "special-days", "--format", "json").want(t, 0).decode(t, &days)
	if powers.Count != 48 || len(powers.Powers) != 48 || days.Count != 49 || len(days.Days) != 49 {
		t.Errorf("%d powers, %d days, want 48 and 49", powers.Count, days.Count)
	}

	dir := t.TempDir()
	for name, model := range map[string]string{"49 powers": reeReads(49, 1), "50 days": reeReads(1, 50)} {
		p := filepath.Join(dir, strings.ReplaceAll(name, " ", "-")+".yaml")
		if err := os.WriteFile(p, []byte(model), 0o600); err != nil {
			t.Fatal(err)
		}
		r := run(t, "server", "start", "--config", p, "--listen", "127.0.0.1", "--port", freePort(t, "127.0.0.1"), "--duration", "1", "--format", "json")
		if r.code != 3 || !strings.Contains(r.stdout, "one answer carries at most") {
			t.Errorf("%s: exit %d\n%s", name, r.code, r.stdout)
		}
	}
}

// A read whose answer is longer than the library's default class 2 queue (4096 ASDUs) is
// served in full: every period of a list, every stored tariff record of a register.
func TestLongAnswersServedInFull(t *testing.T) {
	const n = 5000
	var b strings.Builder
	b.WriteString(answersHead)
	b.WriteString("totals:\n  - record: 11\n    kind: absolute\n")
	fmt.Fprintf(&b, "    generate: {start: 2025-01-01T01:00, interval: 1h, count: %d, totals: [{address: 1, step: 1}]}\n", n)
	b.WriteString("ree:\n  access_key: 7\n  tariffs:\n    - register: 134\n      stored:\n")
	for i := range n {
		fmt.Fprintf(&b, "        - {address: 1, active_abs: %d, active_inc: 1, reactive_ind_abs: 0, reactive_ind_inc: 0, reactive_cap_abs: 0, reactive_cap_inc: 0, max_power: 0, max_power_time: 2025-01-01T00:00, excess_power: 0, period_start: 2025-01-01T00:00, period_end: 2025-%02d-%02dT%02d:00}\n",
			i, 1+i/(24*28), 1+(i/24)%28, i%24)
	}
	st := startModel(t, b.String())

	var totals types.TotalsResult
	cli(t, st, "totals", "--from", "2025-01-01T01:00", "--to", "2025-12-31T00:00", "--first", "1", "--last", "1", "--timeout", "60000", "--format", "json").
		want(t, 0).decode(t, &totals)
	if totals.Count != n {
		t.Errorf("%d periods, want %d", totals.Count, n)
	}
	var tariffs types.TariffsResult
	cli(t, st, "ree", "tariffs", "--access-key-file", keyFile(t), "--from", "2025-01-01", "--to", "2025-12-31", "--timeout", "60000", "--format", "json").
		want(t, 0).decode(t, &tariffs)
	if tariffs.Count != n {
		t.Errorf("%d tariff records, want %d", tariffs.Count, n)
	}
	for _, e := range st.Events() {
		if e.Event == "warning" || e.Event == "error" {
			t.Errorf("station event %s: %s", e.Event, e.Detail)
		}
	}
}

// keyFile returns a file holding the access key 7.
func keyFile(t *testing.T) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "key")
	if err := os.WriteFile(p, []byte("7\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}
