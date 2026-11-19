# iec103ctl `--format json` fields

These fields are a **stable contract** for scripting. Field names are snake_case. A breaking change to a name or shape is called out in the release notes.

## General

| Concept | JSON fields |
|--------|-------------|
| Device | `target` (the line: `tcp://host:2401` or `serial:///dev/ttyUSB0`), `link_address`, `ca` (the common address of ASDU the requests carry) |
| Function type and information number | `fun`, `inf` (numbers); `name` the meaning tables 8 to 18 give them, omitted when they give none |
| Cause of transmission | `cause` (number, `9`) and `cause_name` (`"general interrogation"`) |
| Times | the CP56Time2a of ASDU 6 and 23 as RFC 3339 with milliseconds in the zone `--timezone` names (also `sent` and `device_time` of `client clock`); the CP32Time2a of ASDU 1, 2, 4 and 26, which has no date, as `HH:MM:SS.mmm` in that zone; `received` of `client monitor` and `timestamp` and the `clock-sync` `value` of `server start` as RFC 3339 in UTC with milliseconds (`2026-11-20T09:15:23.307Z`), as iec101ctl and iec104ctl write them |
| Durations | `duration_ms`, `offset_ms` (numbers, milliseconds with fractions) |
| Repeated rows | `messages`, `identifications`, `found`, `sets`, `elements`, `faults`, `tags`, `channels`: **arrays**, empty rather than null |
| Fatal error | one object `{"error": {"kind", "code", "message"}}` on stdout, nothing else; see [exitcodes.md](../exitcodes.md) |

## Message

One ASDU of the monitor direction. Used by `client interrogate` (`messages`), `client command` (`report`) and `client monitor` (one per line).

| Field | Meaning |
|-------|---------|
| `received` | streams (`client monitor`) only: when the message arrived, RFC 3339 in UTC with milliseconds |
| `type`, `type_id` | the type identification: name (`"time-tagged-message"`, `"measurands-ii"`, `"identification"`, ...; `"reserved"` or `"private"` outside tables 3 and 4) and number |
| `cause`, `cause_name`, `ca`, `fun`, `inf`, `name` | as above |
| `dpi`, `state` | double-point information of ASDU 1 and 2: the number (1 OFF, 2 ON) and its name |
| `time`, `time_invalid` | the time tag; `time_invalid` when its IV bit is set |
| `sin` | supplementary information: the scan number in a general interrogation answer, the RII in a command acknowledgement |
| `scn` | scan number of ASDU 7 and 8 |
| `ret`, `fan` | relative time in ms and fault number (ASDU 2 and 4) |
| `scl` | short-circuit location in primary ohms (ASDU 4); absent when it is NaN or infinite, which JSON has no number for |
| `non_finite` | `"NaN"`, `"+Inf"` or `"-Inf"` in place of `scl` when the short-circuit location is NaN or infinite; text and table output show the same name (`scl=NaN`). `client monitor` keeps streaming |
| `values` | measurands of ASDU 3 and 9: `raw` (MVAL, -4096..4095), `value` (MVAL / 4096, the fraction of full scale), `ov`, `er` |
| `col`, `manufacturer`, `software` | identification (ASDU 5): compatibility level, the eight characters (trailing blanks removed), four octets in hex |
| `data` | the information elements after FUN and INF as hex, for the types not decoded into fields (generic data, disturbance data, private types) |

## Results

| Command | Shape |
|---------|-------|
| `client interrogate` | `target`, `link_address`, `ca`, `duration_ms`, `count`, `messages` (the answers with cause 9 of this scan number) |
| `client command` | `target`, `link_address`, `ca`, `fun`, `inf`, `name`, `command` (`"on"` or `"off"`), `dco` (2 or 1), `acknowledged` (true: a negative acknowledgement is an error), `duration_ms`, `report` (the **Message** with cause 12, when `--wait-report` received one) |
| `client clock` | `target`, `link_address`, `ca`, `broadcast`, `sent` (the time sent), `device_time` (the time of the device's answer; absent for a broadcast), `offset_ms` (`device_time` minus `sent`), `duration_ms` |
| `client identify` | `target`, `link_address`, `ca`, `identifications`: one per cause received, in the order 3, 4, 5, 6: `cause`, `cause_name`, `ca`, `fun`, `fun_name`, `inf`, `col`, `manufacturer`, `software` |
| `client generic …` | `target`, `link_address`, `ca`, `service` (`"headings"`, `"read"`, `"entry"`, `"group"`, `"directory"`, `"interrogate"`, `"write"`), `inf` (of the request; for a write the INF of its last ASDU, 250 or 251), `mode` (write: `"execute"` or `"abort"`), `cause`, `cause_name` (42 valid data, 43 invalid data, 40 write acknowledged, 10 end of the generic interrogation), `gin` and `elements` (directory, ASDU 11), `sets`, `duration_ms` |
| `client disturbance list` | `target`, `link_address`, `ca`, `fun`, `duration_ms`, `faults`: `fan`, `sof` (`raw` the octet, `tp` with trip, `tm` being transmitted, `test` recorded in test mode, `otev` started by another event), `time` (CP56Time2a) |
| `client disturbance get` | `target`, `link_address`, `ca`, `fun`, `fan`, `tov` (1: instantaneous values), `nof`, `noc` (channels), `noe` (values per channel), `interval_us`, `start` (time of the first value, `HH:MM:SS.mmm`), `tags`: `pos` (value number from which it holds), `fun`, `inf`, `dpi`, `state`; `channels`: `acc`, `name` (`IL1` ... `VEN`, `acc-N` otherwise), `rpv`, `rsv`, `rfa` (as sent; `null` when NaN or infinite, and `non_finite` then names each such value: `{"rpv": "NaN", "rfa": "+Inf"}`; text and table output show the name), `values` (every SDV, signed integers, by value number); `output` and `csv` (the files written), `duration_ms`. `--output` writes the same object without `target`, `link_address`, `ca`, `output`, `csv` and `duration_ms` |
| `client monitor` (stream) | one **Message** per line (JSON Lines), only those that pass `--type`, `--fun`, `--inf` and `--cause`. Link state and the summary go to stderr as text |
| `client types` | `count`, `types`: `id`, `name` (as `type` of a **Message**), `description` (the name in tables 3 and 4 of clause 7.2.1), `direction` (`"monitor"`, `"control"`, or `"both"` for types 6 and 10) |
| `client scan` | `target`, `start`, `end`, `tried`, `duration_ms`, `found`: `link_address`, `identified`, and from the identification `ca`, `fun`, `fun_name`, `col`, `manufacturer`, `software` |
| `version --json` | as every tool of the repository: `name`, `version`, `tag`, `commit`, `built`, `libraries`, `go`, `platform` |
| `server start` (stream) | one event per line (JSON Lines): `timestamp` (RFC 3339 in UTC with milliseconds), `event`, and where they apply `remote`, `fun`, `inf`, `value`, `detail`. Events: `listening` (`remote` the address or serial device, `detail` link, common address and function type), `connected`, `disconnected`, `busy` (a second line refused, after it waited up to half a second for the first to end, or at once while four wait; also an accept that failed, `detail` naming the error, after which the server accepts again), `link` (`detail` up or down), `interrogation` (`value` the number of answers, `detail` the scan number), `clock-sync` (`value` the time the device was set to, RFC 3339 in UTC with milliseconds, `detail` broadcast), `command` (`value` ON/OFF, `detail` RII and outcome `executed` or `rejected`), `spontaneous` (a simulated change), `generic-read` (`detail` INF, RII, cause), `generic-interrogation`, `generic-write` (`detail` RII and outcome `executed`, `rejected` or `aborted`), `disturbance-transmitted` (`value` the FAN, `detail` whether the final acknowledgement was positive), `stopping` |

## Data set

One data set of generic data (`sets`) or description element (`elements`, without `gin`).

| Field | Meaning |
|-------|---------|
| `gin` | `GROUP.ENTRY` (entry 0 names the group) |
| `kod`, `kod_name` | kind of description: number and name (`actual`, `description`, `range`, ...; `kod-N` otherwise) |
| `data_type`, `type_name` | DATATYPE of the GDD: number and name (`ascii`, `uint`, `int`, `float32`, `float64`, `reply`, `structure`, ...; `type-N` otherwise) |
| `size`, `number` | DATASIZE and NUMBER of the GDD |
| `values` | the elements: the text of an ASCII value; the numbers of unsigned, integer, floating point, reply code, index, text number, DPI and SPI values; for a structure one object per field (`type`, `values`, `data`, and `non_finite` as below). A floating point element that is NaN or infinite is `null`. Absent for the other data types and for data that does not match its GDD |
| `non_finite` | present when an element of `values` is NaN or infinite: one string per element, `"NaN"`, `"+Inf"` or `"-Inf"` where the element is `null`, `""` for every other element. Text and table output show the name in place of the number |
| `reply` | the name of a generic reply code (type 22): `ack`, `invalid-gin`, `not-exist`, `not-available`, `verify-error`, `out-of-range`, `entry-too-large`, `too-many-commands`, `read-only`, `password-protected`, `local-setting`, `error-with-description`, `grc-N` |
| `data` | the GID as hex, always |

## Streams and stderr

- A streaming command writes **compact** JSON, one object per line; every other command writes **one** indented JSON document.
- Progress, link state, summaries and the `--debug` frame trace go to **stderr**, as text, in every format. stdout holds nothing but the result.
