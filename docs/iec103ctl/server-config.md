# iec103ctl device model

`iec103ctl server start --config <file>` serves the protection device a model describes; without `--config` it serves the built-in demo, which `iec103ctl server generate-config` writes with comments. The model is YAML or JSON (by the extension `.json`); unknown fields are errors.

The format is a superset of the IEC 103 fixture of [otfabric/iec60870-5-interop](https://github.com/otfabric/iec60870-5-interop) (docs/FIXTURES.md, contract 2.3): a fixture such as `fixtures/iec103/baseline/fixture.json` is a model as it is, and the device behaves as the fixture's station behaviour describes. The interop suite (`interop/iec103ctl`) serves the references' own fixtures this way.

## Top level

| Field | Meaning |
|-------|---------|
| `schemaVersion`, `name`, `description` | free text, not interpreted |
| `station` | the device: addresses and identification |
| `indications` | double-point indications (ASDU 1) |
| `measurands` | measurands sent cyclically as class 2 data |
| `commands` | the general commands the device takes |
| `groups` | headings of the generic groups |
| `generic` | generic entries (settings) |
| `disturbances` | recorded disturbances, at most eight |

## station

| Field | Meaning |
|-------|---------|
| `linkAddress` | link address 0-254; default 1. `--link-address` overrides it |
| `commonAddress` | common address of ASDU 0-254, normally the link address (clause 7.2.4). `--common-address` overrides it; a new `--link-address` alone also replaces a common address that equals the model's link address |
| `identification` | `fun` the main function type 1-255 (160 overcurrent, 128 distance, ...), `col` the compatibility level (2 without, 3 with generic services), `ascii` at most eight printable characters (padded with blanks), `software` four octets in hex |
| `startCause` | cause of the identification after start: `power-on` (default) or `start-restart` |

The device identifies itself after a reset of the communication unit (cause 4, INF 3), a reset of the frame count bit (cause 3, INF 2) and once after start, as go-iec60870-5's protection equipment station does (clause 7.4.1).

## indications

| Field | Meaning |
|-------|---------|
| `fun`, `inf` | 0-254 and 0-255, unique together (also with the measurands) |
| `dpi` | initial state: 1 OFF, 2 ON (0 and 3 are allowed as states) |
| `name` | free text |
| `gi` | `false` keeps it out of the general interrogation (default true) |
| `simulate` | when set, the indication toggles every so many milliseconds (at least 100) and the change is sent spontaneously (cause 1) |

The general interrogation answers every indication that takes part, in the model's order, with ASDU 1, cause 9 and the scan number, then the termination; with disturbances the list of recorded disturbances follows (ASDU 23, cause 31).

## measurands

`fun`, `inf`, `type` 3 (measurands I, at most four values) or 9 (measurands II, at most nine), `values`: `value` (-4096..4095, the 13-bit MVAL), `ov`, `er`. They are queued one after the other as class 2 data with cause 2, cyclically, whenever the class 2 queue is empty.

## commands

`fun`, `inf` of an indication of the model. A general command to it with DCO OFF or ON that table 17 allows sets the indication and is acknowledged with cause 20; when the state changed, the change follows with cause 12. Anything else is refused with cause 21: another FUN/INF, DCO 0 or 3, OFF where table 17 allows only ON (INF 19, 23-26 with a compatible function type), a command before the previous one was acknowledged.

## groups and generic

`groups`: `group` 0-255 and `description` (the heading, read with INF 240 as GIN `GROUP.0`, KOD 10). Groups that have entries but no heading are headed `GROUP n`.

`generic`: `gin` `{group, entry}` (1-255 each, unique), `description` (KOD 10, 1-64 printable characters), `type` 1 ASCII (`size` the number of characters), 3 unsigned or 4 signed integer (`size` 1, 2 or 4), 7 R32.23 (`size` 4), 8 R64.53 (`size` 8), `value` that fits, `writable` (default false).

| Request | Answer |
|---------|--------|
| INF 240 read headings | one data set per group |
| INF 241 read a group | the attribute `kod` of every entry of the group |
| INF 243 read a directory | ASDU 11: KOD 1 (value) and KOD 10 (description) |
| INF 244 read an entry | KOD 1 the value, KOD 10 the description |
| INF 245 general interrogation of generic data | KOD 1 of every entry, then the end (cause 10) |
| INF 248 write entry | staged, no answer |
| INF 249 write with confirmation | staged and echoed with cause 44 |
| INF 250 write with execution | every staged and given data set of KOD 1 to a writable entry with its type and size is applied, cause 40; otherwise none is, cause 41 with a reply code per failed set: 1 (GIN not defined), 8 (read only), 5 (another type, size or KOD) |
| INF 251 write abort | the staged sets are dropped, cause 40 |

A read of an entry or group the device lacks, or of another KOD, is answered with cause 43 and generic reply code 1 (GIN not defined) or 2 (data do not exist). Written values live as long as the server.

## disturbances

`fan` (unique), `sof` `{tp, tm, test, otev}`, `time` of the fault (`2026-01-02T03:04:05.678Z`), `nof`, `interval` (µs between values), `start` the time of day of the first value (`HH:MM:SS.mmm`, taken on the date of `time`), `tags` `{pos, fun, inf, dpi}` in ascending position (the initial states at position 0 first), `channels` `{acc, rpv, rsv, rfa, values}` in ascending ACC, every channel with the same number of values, each -32768..32767.

The list and the transfer run go-iec60870-5's procedure of clause 7.4.7 (figure 10): ASDU 26, 28, the tags (ASDU 29), each channel offered by ASDU 27 with its values in ASDUs 30, the ends (ASDU 31) and the acknowledgements. The records stay after a transfer.

## The process state and the lines

Over TCP one controlling system is served at a time; every connection is a line that has just come up (link state, class 1 and class 2 queues and the clock offset start afresh), while the indications and the generic values live as long as the server. A serial port is one line for the whole run.
