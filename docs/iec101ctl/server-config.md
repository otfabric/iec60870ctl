# iec101ctl: station model (`server start --config`)

`iec101ctl server start` serves the controlled station a model file describes. The file is YAML, or JSON (which is YAML). `iec101ctl server generate-config` writes a commented example; this page is the reference.

A field the model does not know is an error, and everything that is wrong with a model is reported at once, before the server serves. The link (mode, link addresses, field sizes) is not part of the model: it is given with the flags of `server start`, and every address of the model must fit the field sizes the station is started with (`--ioa-size 2`, the default, holds 1-65535; `--ca-size 1`, the default, holds common addresses 1-254).

```yaml
station:
  name: Feeder bay 7           # shown in the event log; not sent
  common_address: 1            # 1-254 (1-65534 with --ca-size 2)
  cyclic_interval: 30s         # optional: send the measured values with cause "periodic"
  counter_freeze: true         # optional: false refuses counter freeze commands

points:                        # information objects in monitor direction
  - ioa: 100
    type: M_SP_NA_1
    name: Circuit breaker closed
    value: true
    group: 1
  - {ioa: 300, type: M_IT_NA_1, value: 123456, sequence: 1}
  - ioa: 1000
    type: M_ME_NC_1
    value: 230.4
    quality: [NT]
    simulate: {mode: sine, interval: 2s, min: 228, max: 232}

commands:                      # information objects in control direction
  - {ioa: 5000, type: C_SC_NA_1, target: 100}
  - {ioa: 5001, type: C_DC_NA_1, target: 200, select_required: true, select_timeout: 30s, report_cause: 3}
```

Information object addresses are unique across points and commands: 1 to 16 777 215, and within the size of `--ioa-size`.

## `station`

| Field | Meaning |
|-------|---------|
| `common_address` | Common address of ASDU, 1-65534, and below the broadcast address of `--ca-size` (255 or 65535). Required. Requests for another address are answered with "unknown common address"; requests to the broadcast address are answered with the station's own address |
| `name` | A label for the event log |
| `cyclic_interval` | When set (at least `100ms`), every measured value (`M_ME_NA_1`, `M_ME_NB_1`, `M_ME_NC_1`) is sent at this interval with cause "periodic", without time tag, as class 2 data in unbalanced transmission |
| `counter_freeze` | `false` refuses a counter interrogation that freezes or resets the counters with a negative confirmation. Default `true` |

Durations are strings such as `500ms`, `5s` or `2m`.

## `points`

| Field | Meaning |
|-------|---------|
| `ioa` | Information object address. Required |
| `type` | The type identification without time tag. Required |
| `value` | The initial value. Required |
| `name` | A description. IEC 60870-5-101 carries no names: it is for you |
| `quality` | Quality flags that are set: `OV`, `BL`, `SB`, `NT`, `IV`. For an integrated total only `IV` has a meaning |
| `group` | Interrogation group 1-16, or counter group 1-4 for an integrated total. Every point answers a general interrogation whatever its group |
| `sequence` | The initial sequence number 0-31 of an integrated total |
| `time_tag` | `false` reports changes of the point without time tag. By default changes are reported with the CP56Time2a type (`M_SP_TB_1` for an `M_SP_NA_1`, and so on) and the station's time |
| `simulate` | Makes the value change by itself; see below |

| `type` | Value | Range |
|--------|-------|-------|
| `M_SP_NA_1` single point | `true` / `false` (or `on` / `off`) | |
| `M_DP_NA_1` double point | `off`, `on`, `intermediate`, `indeterminate` (quote `"on"` and `"off"` in YAML), or 0-3 | |
| `M_ST_NA_1` step position | whole number | -64 … 63 |
| `M_BO_NA_1` bitstring of 32 bit | whole number | 0 … 4 294 967 295 |
| `M_ME_NA_1` measured value, normalized | number | -1 … < 1 |
| `M_ME_NB_1` measured value, scaled | whole number | -32 768 … 32 767 |
| `M_ME_NC_1` measured value, short floating point | number | |
| `M_IT_NA_1` integrated total (counter) | whole number | -2 147 483 648 … 2 147 483 647 |

### `simulate`

| Field | Meaning |
|-------|---------|
| `mode` | `toggle`, `ramp`, `sine`, `random` or `count` |
| `interval` | How often the value changes, at least `50ms` |
| `min`, `max` | Bounds of `ramp`, `sine` and `random` |
| `step` | Increment of `ramp` and `count` (default 1); period of `sine`, in intervals (default 60) |

| Mode | For | Behaviour |
|------|-----|-----------|
| `toggle` | single and double points | flips between off and on |
| `ramp` | numeric points | adds `step` each interval; above `max` it starts again at `min` |
| `sine` | numeric points | a sine between `min` and `max` |
| `random` | numeric points | a uniformly distributed value between `min` and `max` |
| `count` | numeric points, meant for counters | adds `step`; a counter that passes its maximum starts again at 0 |

A change is reported with cause "spontaneous", as class 1 data in unbalanced transmission. Changes of **state** (single and double points) are kept while no line is served (up to 1024) and delivered on the next line. Changes of **measured values, step positions, bitstrings and counters** are sent only while a line is served: a controlling station that comes back gets the current values from its general interrogation.

## `commands`

| Field | Meaning |
|-------|---------|
| `ioa` | Information object address of the command. Required |
| `type` | `C_SC_NA_1`, `C_DC_NA_1`, `C_RC_NA_1`, `C_SE_NA_1`, `C_SE_NB_1`, `C_SE_NC_1` or `C_BO_NA_1`. IEC 60870-5-101 has no time-tagged command types; one that arrives is refused with "unknown type identification". Required |
| `target` | The point that takes the commanded value. Without it the command is confirmed and nothing changes |
| `select_required` | Refuse an execute that was not preceded by a select on the same line |
| `select_timeout` | How long a selection lasts. Default `30s` |
| `report_cause` | Cause of transmission of the report of the target's new value: 11 (return information caused by a remote command, the default) or 3 (spontaneous) |
| `name` | A description |

| Command | May target | Effect on the target |
|---------|------------|----------------------|
| `C_SC_NA_1` single command | `M_SP_NA_1` | takes the commanded state |
| `C_DC_NA_1` double command | `M_DP_NA_1` | takes the commanded state (off or on; anything else is refused) |
| `C_RC_NA_1` regulating step command | `M_ST_NA_1` | one step higher or lower; a step beyond -64 … 63 is refused |
| `C_SE_NA_1` / `C_SE_NB_1` / `C_SE_NC_1` set points | `M_ME_NA_1`, `M_ME_NB_1`, `M_ME_NC_1` | takes the value, rounded and limited to what the target type holds |
| `C_BO_NA_1` bitstring command | `M_BO_NA_1` | takes the value |

How the station handles a command:

- A command to an address that is not a command of the model, or of another type than the object takes, is answered with "unknown information object address" (cause 47); a cause other than activation or deactivation with "unknown cause of transmission" (cause 45).
- **Deactivation**: confirmed (cause 9), and a selection of the object is cleared.
- A value that is not permitted (a double or step command state 0 or 3) is answered with a negative confirmation.
- **Select** (S/E = 1): confirmed, and remembered for the line until `select_timeout`.
- **Execute**: when the object is selected, the execute must come from the same line and be for the same value and qualifier; otherwise it is refused and the selection is dropped. With `select_required` an execute without a selection is refused. Otherwise: activation confirmation, the **report** of the target with its CP56Time2a type and `report_cause`, and the activation termination, in this order, on the line of the command.
- A selection ends with its line.

## What the station answers

| Request | Answer |
|---------|--------|
| General interrogation (`C_IC_NA_1`, QOI 20) | confirmation, every point except integrated totals, termination. Points are sent without time tag in ascending address order; consecutive points of one type share an ASDU |
| Group interrogation (QOI 21-36) | the points of that `group`; a group without points is refused (negative confirmation) |
| Counter interrogation (`C_CI_NA_1`) | a counter request (freeze 0): the integrated totals, general or of counter group 1-4, then the termination. A counter freeze command (freeze 1-3): confirmed only, as IEC 60870-5-101 7.4.8 describes; freeze increments the sequence number, freeze with reset and reset set the counters to zero. Refused with `counter_freeze: false` |
| Read (`C_RD_NA_1`) | the point with cause "request", or "unknown information object address" |
| Clock synchronization (`C_CS_NA_1`) | confirmed; the station's clock is set to the time plus the transmission delay it was given, and its time tags follow it |
| Delay acquisition (`C_CD_NA_1`) | cause activation: confirmed with the time of the request plus the station's reaction time; cause spontaneous ("set delay"): the transmission delay is kept for clock synchronization, nothing is answered |
| Test command (`C_TS_NA_1`) | confirmed. `C_TS_TA_1`, IEC 60870-5-104's, is "unknown type identification" |
| Reset process (`C_RP_NA_1`) | confirmed. QRP 1 puts every point back to the value of the model and sends an end of initialization (`M_EI_NA_1`, cause of initialization 2); QRP 2 drops the kept events |
| Anything else | "unknown type identification" |

In unbalanced transmission confirmations, terminations, the data of interrogations and reads and spontaneous events are class 1 data; periodic values are class 2 data. After the first reset of the link on a line the station sends an end of initialization (cause of initialization 0, local power on) unless `--no-end-of-init`.
