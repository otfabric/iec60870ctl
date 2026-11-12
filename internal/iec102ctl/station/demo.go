// SPDX-License-Identifier: MIT

package station

import "time"

// DemoYAML is the model `iec102ctl server start` serves when it is given none, and what
// `iec102ctl server generate-config` writes: an electricity meter of the Spanish REE profile
// with two days of hourly load profile, a day of quarter hours, events and tariff
// information. It is kept as text so that the comments reach the generated file.
const DemoYAML = `# iec102ctl meter model (IEC 60870-5-102 integrated total station).
# Serve it with:  iec102ctl server start --config meter.yaml
# Reference:      docs/iec102ctl/server-config.md
#
# Times are the meter's local time without a zone, in the zone of --timezone (UTC by
# default): time a is 2026-01-01T01:00, time b 2026-01-15T10:20:30.500.

station:
  name: Demo meter
  link_address: 1
  # The measuring point address (common address of ASDU).
  common_address: 1
  # "running" follows this computer's clock; a time b makes the clock stand still there
  # until a controlling station sets it (C_CS_TA_2).
  clock: running
  # Answer to C_RD_NA_2 (P_MP_NA_2).
  product: {standard: 42, manufacturer: 33, product: 20261115}

# Integrated totals per record address. "kind" is operational (absolute values, M_IT_TG_2),
# reset-operational (increments per period, M_IT_TK_2), accounting (M_IT_TA_2) or
# reset-accounting (M_IT_TD_2); or give "type" (M_IT_TA_2 .. M_IT_TM_2). Periods go oldest
# first: list them under "periods", or let "generate" make them.
totals:
  # REE addresses: 1 active import, 2 active export, 3..6 reactive quadrants 1..4 (Wh, varh).
  - record: 11
    kind: operational
    periods:
      - end: 2026-01-01T01:00
        totals:
          - {address: 1, value: 1000}
          - {address: 2, value: 0}
          - {address: 3, value: 200, quality: 16}
    generate:
      start: 2026-01-01T02:00
      interval: 1h
      count: 47
      totals:
        - {address: 1, start: 1450, step: 450}
        - {address: 2, start: 0, step: 0}
        - {address: 3, start: 240, step: 40}
  - record: 11
    kind: reset-operational
    generate:
      start: 2026-01-01T01:00
      interval: 1h
      count: 48
      totals:
        - {address: 1, step: 450}
        - {address: 2, step: 0}
        - {address: 3, step: 40}
  - record: 12
    kind: reset-operational
    generate:
      start: 2026-01-01T00:15
      interval: 15m
      count: 96
      totals:
        - {address: 1, step: 112}
        - {address: 2, step: 0}

# Single-point information with time tag (C_SP_NA_2, C_SP_NB_2), per list address 50-55.
events:
  - record: 52
    points:
      - {address: 1, on: false, time: "2026-01-01T03:12:44.120"}
      - {address: 1, on: true, time: "2026-01-01T03:14:02.500"}
      - {address: 5, on: true, qualifier: 2, time: "2026-01-02T11:00:00.000"}

# The Spanish REE profile: a session with the access key (C_AC_NA_2, C_FS_NA_2), clock
# setting (C_CS_TA_2) and the reads below. Remove the section for a plain IEC 102 meter.
ree:
  access_key: 7
  contracted_powers:
    - {address: 1, power_kw: 15}
    - {address: 2, power_kw: 15}
    - {address: 3, power_kw: 20}
  special_days: [2026-01-01, 2026-01-06]
  tariffs:
    - register: 134
      current:
        - {address: 1, active_abs: 21600, active_inc: 21600, reactive_ind_abs: 1920, reactive_ind_inc: 1920,
           reactive_cap_abs: 0, reactive_cap_inc: 0, max_power: 3100, max_power_time: 2026-01-01T19:00,
           excess_power: 0, period_start: 2026-01-01T00:00, period_end: 2026-01-03T00:00}
      stored:
        - {address: 1, active_abs: 310000, active_inc: 310000, reactive_ind_abs: 27000, reactive_ind_inc: 27000,
           reactive_cap_abs: 0, reactive_cap_inc: 0, max_power: 4200, max_power_time: 2025-12-18T20:00,
           excess_power: 0, period_start: 2025-12-01T00:00, period_end: 2026-01-01T00:00}
`

// Demo returns the built-in demo model.
func Demo() *Model {
	m, err := Parse([]byte(DemoYAML))
	if err == nil {
		err = m.Validate(time.UTC)
	}
	if err != nil {
		panic("iec102ctl: the built-in demo model is invalid: " + err.Error())
	}
	return m
}
