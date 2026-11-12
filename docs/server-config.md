# Station model (`server start --config`)

`iec104ctl server start` serves the controlled station a model file describes. The file is YAML, or JSON (which is YAML). `iec104ctl server generate-config` writes a commented example; this page is the reference.

A field the model does not know is an error, and everything that is wrong with a model is reported at once, before the server listens.

```yaml
station:
  name: Feeder bay 7           # shown in the event log; not sent
  common_address: 1            # 1-65534
  cyclic_interval: 30s         # optional: send the measured values with cause "periodic"

points:                        # information objects in monitor direction
  - ioa: 100
    type: M_SP_NA_1
    name: Circuit breaker closed
    value: true
    group: 1
  - ioa: 1000
    type: M_ME_NC_1
    value: 230.4
    quality: [NT]
    simulate: {mode: sine, interval: 2s, min: 228, max: 232}

commands:                      # information objects in control direction
  - {ioa: 5000, type: C_SC_NA_1, target: 100}
  - {ioa: 5001, type: C_DC_NA_1, target: 200, select_required: true, select_timeout: 30s}

files:
  - {ioa: 30000, name: 1, path: ./record.cfg}

uploads:
  accept: true
  dir: ./uploads
```

Information object addresses are unique across points, commands and files: 1 to 16 777 215.

## `station`

| Field | Meaning |
|-------|---------|
| `common_address` | Common address of ASDU, 1-65534. Required. Requests for another address are answered with "unknown common address"; requests to the broadcast address 65535 are answered with the station's own address |
| `name` | A label for the event log |
| `cyclic_interval` | When set (at least `100ms`), every measured value (`M_ME_NA_1`, `M_ME_NB_1`, `M_ME_NC_1`) is sent at this interval with cause "periodic", without time tag |

Durations are strings such as `500ms`, `5s` or `2m`.

## `points`

| Field | Meaning |
|-------|---------|
| `ioa` | Information object address. Required |
| `type` | The type identification without time tag. Required |
| `value` | The initial value. Required |
| `name` | A description. IEC 60870-5-104 carries no names: it is for you |
| `quality` | Quality flags that are set: `OV`, `BL`, `SB`, `NT`, `IV`. For an integrated total only `IV` has a meaning |
| `group` | Interrogation group 1-16, or counter group 1-4 for an integrated total. Every point answers a general interrogation whatever its group |
| `time_tag` | `false` reports changes of the point without time tag. By default changes are reported with the CP56Time2a type (`M_SP_TB_1` for an `M_SP_NA_1`, and so on) and the station's time |
| `simulate` | Makes the value change by itself; see below |

| `type` | Value | Range |
|--------|-------|-------|
| `M_SP_NA_1` single point | `true` / `false` (or `on` / `off`) | |
| `M_DP_NA_1` double point | `off`, `on`, `intermediate`, `indeterminate` (quote `"on"` and `"off"` in YAML) | |
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

A change is reported with cause "spontaneous". Changes of **state** (single and double points) are queued: the station keeps them, with their time tag, until a controlling station has acknowledged them, also while none is connected, and delivers them to the next one. Changes of **measured values, step positions, bitstrings and counters** are sent to whoever has data transfer started and are not kept: a controlling station that connects gets the current values from its general interrogation, not a backlog of old measurements.

## `commands`

| Field | Meaning |
|-------|---------|
| `ioa` | Information object address of the command. Required |
| `type` | `C_SC_NA_1`, `C_DC_NA_1`, `C_RC_NA_1`, `C_SE_NA_1`, `C_SE_NB_1`, `C_SE_NC_1` or `C_BO_NA_1`. The time-tagged variant of the type (`C_SC_TA_1` …) is accepted on the wire as well. Required |
| `target` | The point that takes the commanded value. Without it the command is confirmed and nothing changes |
| `select_required` | Refuse an execute that was not preceded by a select from the same connection |
| `select_timeout` | How long a selection lasts. Default `30s` |
| `name` | A description |

| Command | May target | Effect on the target |
|---------|------------|----------------------|
| `C_SC_NA_1` single command | `M_SP_NA_1` | takes the commanded state |
| `C_DC_NA_1` double command | `M_DP_NA_1` | takes the commanded state (off or on; anything else is refused) |
| `C_RC_NA_1` regulating step command | `M_ST_NA_1` | one step higher or lower, within -64 … 63 |
| `C_SE_NA_1` / `C_SE_NB_1` / `C_SE_NC_1` set points | `M_ME_NA_1`, `M_ME_NB_1`, `M_ME_NC_1` | takes the value, rounded and limited to what the target type holds |
| `C_BO_NA_1` bitstring command | `M_BO_NA_1` | takes the value |

How the station handles a command:

- **Select** (S/E = 1): confirmed, and remembered for the connection that sent it until `select_timeout`. A second controlling station cannot select or execute the object meanwhile.
- **Execute**: confirmed, carried out and terminated (activation termination). When the object is selected, the execute must come from the same connection and be for the same value and qualifier; otherwise it is refused and the selection is dropped.
- **Deactivation** of a selection: confirmed; without a selection it is refused.
- A change of the target is reported as an event with cause "return information caused by a remote command" (11), and queued like a change of state.
- A selection ends with its connection.
- An unknown address is answered with "unknown information object address"; a command of another type than the object takes, or a value that is not permitted, with a negative confirmation.

## `files`

| Field | Meaning |
|-------|---------|
| `ioa`, `name` | Address and name of file (NOF, a number) the file is fetched by. Required |
| `path` | A file on disk, relative to the model file |
| `content` | The content, as text |
| `size` | That many octets of a repeating pattern |
| `section_size` | Octets per section. Default 2048. A file has at most 254 sections and 16 777 215 octets |

Give exactly one of `path`, `content` and `size`. The files are listed in the directory (`client file list`) with the time the server started.

## `uploads`

| Field | Meaning |
|-------|---------|
| `accept` | Take the files controlling stations deliver (`client file put`), at any address that is not a point or a command. Without it a delivery is refused |
| `dir` | Write delivered files to this directory (relative to the model file, created if missing, mode 0750) as `ioa-<ioa>-name-<name>.bin` (mode 0600). Without it they are kept in memory |
| `max_files` | The number of delivered files kept. Default 64 |
| `max_file_size` | The largest file taken, in octets. Default 1048576 (1 MiB), at most 16777215 |
| `max_total_size` | The octets of all delivered files together. Default 16777216 (16 MiB) |

A delivered file appears in the directory and can be fetched back. A file delivered to an address that already holds a delivered file replaces it; with `dir`, the file it replaces is removed from the directory. A delivery beyond a limit is refused when it is announced (the event says which limit), so the limits bound both memory and disk. Files of the model's `files` do not count.

## What the station answers

| Request | Answer |
|---------|--------|
| General interrogation (`C_IC_NA_1`, QOI 20) | confirmation, every point except integrated totals, termination |
| Group interrogation (QOI 21-36) | the points of that `group` |
| Counter interrogation (`C_CI_NA_1`) | the integrated totals, general or of counter group 1-4. Freeze increments the sequence number; freeze with reset and reset set the counters to zero |
| Read (`C_RD_NA_1`) | the point with cause "request", or "unknown information object address" |
| Clock synchronization (`C_CS_NA_1`) | confirmed; the time tags of the station follow the new time |
| Test command (`C_TS_TA_1`) | confirmed |
| Reset process (`C_RP_NA_1`) | confirmed. QRP 1 puts every point back to the value of the model and sends an end of initialization (`M_EI_NA_1`) |
| File transfer (`F_SC_NA_1` …) | serves `files`, lists the directory, takes uploads |
| Anything else | "unknown type identification" |

The station serves any number of connections. Queued events go to one of them, the one that started data transfer last, and what a connection did not acknowledge goes to the next: the redundancy of IEC 60870-5-104.
