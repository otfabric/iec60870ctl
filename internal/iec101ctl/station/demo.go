// SPDX-License-Identifier: MIT

package station

import "time"

// DemoYAML is the model `iec101ctl server start` serves when it is given none, and what
// `iec101ctl server generate-config` writes: a small substation bay with a point of every
// type and a command for each. It is kept as text so that the comments reach the generated
// file.
const DemoYAML = `# iec101ctl station model.
# Serve it with:  iec101ctl server start --config station.yaml
# Reference:      docs/iec101ctl/server-config.md
#
# Every address must fit the field sizes the station is started with: with the default
# --ioa-size 2 an information object address is 1-65535, with the default --ca-size 1 the
# common address is 1-254.

station:
  name: Demo bay
  common_address: 1
  # Send the measured values with cause "periodic" at this interval, as class 2 data in
  # unbalanced transmission (remove to turn off).
  cyclic_interval: 30s

# Information objects in monitor direction. A general interrogation returns all of them
# except the counters; "group" also puts a point in an interrogation group (1-16) or, for a
# counter, a counter group (1-4). "simulate" makes a value change by itself, which the
# station reports as a spontaneous event.
points:
  - {ioa: 100, type: M_SP_NA_1, name: Circuit breaker closed, value: true, group: 1}
  - ioa: 101
    type: M_SP_NA_1
    name: Door contact
    value: false
    group: 1
    simulate: {mode: toggle, interval: 15s}
  - {ioa: 200, type: M_DP_NA_1, name: Disconnector, value: "on", group: 1}
  - {ioa: 300, type: M_ST_NA_1, name: Tap changer position, value: 3, group: 2}
  - {ioa: 400, type: M_BO_NA_1, name: Status word, value: 15, group: 2}
  - ioa: 1000
    type: M_ME_NC_1
    name: Voltage L1 (kV)
    value: 230.4
    group: 3
    simulate: {mode: sine, interval: 2s, min: 228, max: 232}
  - ioa: 1001
    type: M_ME_NC_1
    name: Frequency (Hz)
    value: 50
    group: 3
    simulate: {mode: random, interval: 5s, min: 49.95, max: 50.05}
  - ioa: 1002
    type: M_ME_NB_1
    name: Active power (kW)
    value: 1250
    group: 3
    simulate: {mode: ramp, interval: 5s, min: 1000, max: 1500, step: 25}
  - {ioa: 1003, type: M_ME_NA_1, name: Load factor, value: 0.25, group: 3}
  - {ioa: 1010, type: M_ME_NC_1, name: Voltage set point (kV), value: 230, group: 3}
  - ioa: 2000
    type: M_IT_NA_1
    name: Energy counter (kWh)
    value: 123456
    group: 1
    simulate: {mode: count, interval: 10s, step: 1}

# Information objects in control direction. "target" is the point that takes the commanded
# value; the station reports its change between the confirmation and the termination of
# the command, with cause "report_cause" (11, return information caused by a remote
# command, by default; or 3, spontaneous). "select_required" refuses an execute without a
# select before it.
commands:
  - {ioa: 5000, type: C_SC_NA_1, name: Circuit breaker, target: 100}
  - {ioa: 5001, type: C_DC_NA_1, name: Disconnector, target: 200, select_required: true, select_timeout: 30s}
  - {ioa: 5002, type: C_RC_NA_1, name: Tap changer, target: 300}
  - {ioa: 5003, type: C_SE_NC_1, name: Voltage set point, target: 1010}
  - {ioa: 5004, type: C_SE_NB_1, name: Active power limit, target: 1002, report_cause: 3}
  - {ioa: 5005, type: C_SE_NA_1, name: Load factor, target: 1003}
  - {ioa: 5006, type: C_BO_NA_1, name: Status word, target: 400}
`

// Demo returns the built-in demo model.
func Demo() *Model {
	m, err := Parse([]byte(DemoYAML))
	if err == nil {
		err = m.Validate()
	}
	if err != nil {
		panic("iec101ctl: the built-in demo model is invalid: " + err.Error())
	}
	return m
}

// defaultSelectTimeout is how long a selection lasts when the model does not say.
const defaultSelectTimeout = 30 * time.Second
