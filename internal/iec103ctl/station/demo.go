// SPDX-License-Identifier: MIT

package station

// DemoYAML is the model of the built-in demo device, with comments: what
// `server generate-config` writes and `server start` serves without --config.
const DemoYAML = `# iec103ctl device model: a simulated item of protection equipment (IEC 60870-5-103).
# The format is a superset of the IEC 103 fixture of otfabric/iec60870-5-interop
# (docs/FIXTURES.md), so such a fixture can be served as it is.
name: demo
description: Overcurrent protection of one feeder.

station:
  # Link address 0-254 (255 is the broadcast address) and common address of ASDU,
  # normally the same (clause 7.2.4). --link-address and --common-address override them.
  linkAddress: 1
  commonAddress: 1
  # The identification (ASDU 5) sent after a reset of the link and once after start:
  # the main function type (160 overcurrent, 128 distance, 176 transformer differential,
  # 192 line differential), the compatibility level (2 without, 3 with generic services),
  # eight ASCII characters and four octets of software identification in hex.
  identification: { fun: 160, col: 3, ascii: "IEC103CT", software: "00010000" }
  # Cause of the identification after start: power-on (default) or start-restart.
  startCause: power-on

# Double-point indications (ASDU 1): dpi 1 off, 2 on. gi: false keeps one out of the
# general interrogation. simulate: N toggles it every N ms and reports the change
# spontaneously (cause 1).
indications:
  - { fun: 160, inf: 16, dpi: 2, name: auto-recloser active }
  - { fun: 160, inf: 17, dpi: 1, name: teleprotection active }
  - { fun: 160, inf: 18, dpi: 2, name: protection active }
  - { fun: 160, inf: 19, dpi: 1, name: LED reset }
  - { fun: 160, inf: 27, dpi: 1, name: auxiliary input 1, simulate: 30000 }
  - { fun: 160, inf: 36, dpi: 1, name: trip circuit supervision }
  - { fun: 160, inf: 84, dpi: 1, name: general start, gi: false }
  - { fun: 160, inf: 68, dpi: 1, name: general trip, gi: false }

# Measurands sent cyclically as class 2 data (cause 2): type 3 (measurands I, at most
# four values) or 9 (measurands II, at most nine), each the 13-bit value -4096..4095 with
# the overflow (ov) and error (er) bits.
measurands:
  - fun: 160
    inf: 145
    type: 3
    values: [ { value: 1707 }, { value: 2840 } ]
  - fun: 160
    inf: 148
    type: 9
    values:
      - { value: 1700 }
      - { value: 1710 }
      - { value: 1695 }
      - { value: 2840 }
      - { value: 2845 }
      - { value: 2838 }
      - { value: 1200 }
      - { value: 300 }
      - { value: 4095 }

# General commands the device takes (ASDU 20), each acting on the indication with the
# same FUN and INF: acknowledged with cause 20, the change reported with cause 12. Any
# other command is refused with cause 21. INF 19 and 23-26 take ON only (table 17).
commands:
  - { fun: 160, inf: 16 }
  - { fun: 160, inf: 17 }
  - { fun: 160, inf: 19 }

# Generic services (clause 7.4.8): the headings of the groups, and the entries with their
# description (KOD 10) and value (KOD 1). type 1 ASCII (size = characters), 3 unsigned,
# 4 integer (size 1, 2 or 4), 7 R32.23 (size 4), 8 R64.53 (size 8). writable entries take
# a generic write with execution.
groups:
  - { group: 1, description: SETTINGS }
  - { group: 2, description: COUNTERS }
generic:
  - { gin: { group: 1, entry: 1 }, description: RATED I, type: 3, size: 2, value: 1000 }
  - { gin: { group: 1, entry: 2 }, description: PICKUP I>, type: 7, size: 4, value: 1.5, writable: true }
  - { gin: { group: 1, entry: 3 }, description: DELAY t>, type: 3, size: 2, value: 300, writable: true }
  - { gin: { group: 1, entry: 4 }, description: FEEDER, type: 1, size: 8, value: "FEEDER 1", writable: true }
  - { gin: { group: 2, entry: 1 }, description: TRIPS, type: 4, size: 4, value: 12 }

# Recorded disturbances (clause 7.4.7), at most eight: the list (ASDU 23) and the
# transfer of each with its tags and channels. time is the fault, start the time of day of
# the first value, interval the time between values in microseconds; every channel has
# the same number of values (SDV, -32768..32767).
disturbances:
  - fan: 1
    sof: { tp: true }
    time: "2026-01-15T10:20:30.040Z"
    nof: 1
    interval: 1000
    start: "10:20:30.000"
    tags:
      - { pos: 0, fun: 160, inf: 84, dpi: 1 }
      - { pos: 0, fun: 160, inf: 68, dpi: 1 }
      - { pos: 8, fun: 160, inf: 84, dpi: 2 }
      - { pos: 14, fun: 160, inf: 68, dpi: 2 }
    channels:
      - acc: 1
        rpv: 600
        rsv: 1
        rfa: 1
        values: [0, 3090, 5878, 8090, 9511, 10000, 9511, 8090, 5878, 3090, 0, -3090, -5878, -8090, -9511, -10000,
                 -9511, -8090, -5878, -3090, 0, 6180, 11756, 16180, 19021, 20000, 19021, 16180, 11756, 6180]
      - acc: 4
        rpv: 600
        rsv: 1
        rfa: 1
        values: [0, 0, 0, 0, 0, 0, 0, 0, 120, 410, 900, 1500, 2100, 2600, 2900, 3000,
                 2900, 2600, 2100, 1500, 900, 410, 120, 0, 0, 0, 0, 0, 0, 0]
`

// Demo returns the model of the built-in demo device.
func Demo() *Model {
	m, err := Parse([]byte(DemoYAML), false)
	if err != nil {
		panic("station: the demo model is invalid: " + err.Error())
	}
	return m
}
