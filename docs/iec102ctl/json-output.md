# iec102ctl `--format json` field conventions

These conventions are a **stable contract** for scripting. Field names are snake_case. A breaking change to a name or shape is called out in the release notes.

## General

| Concept | JSON fields |
|--------|-------------|
| Meter | `target`: the line (`tcp://host:2401`, `serial:///dev/ttyUSB0`); `link_address`; `ca`: the common address of ASDU (the measuring point address) |
| REE session | `session`: `login` and `logout`, each true when its confirmation (cause 7) arrived and was positive; present only when an access key was given |
| Duration | `duration_ms`: from opening the line to closing it (milliseconds with fractions); `rtt_ms`: of one request |
| Time | an object: `time` (the wall clock as the meter sent it, its local time without a zone: time a `YYYY-MM-DDTHH:MM`, time b `YYYY-MM-DDTHH:MM:SS.mmm`), `su` (the summer time bit) and `time_invalid` (the IV bit); `su` and `time_invalid` are omitted when not set |
| Type identification | `type` (the mnemonic, `"M_IT_TK_2"`; REE names in the private range; `"TypeID(n)"` for a type without a name) and `type_id` (the number) |
| Repeated rows | `periods`, `totals`, `events`, `answers`, `records`, `powers`, `days`, `types`: **arrays**, empty rather than null |
| Fatal error | one object `{"error": {"kind", "code", "message"}}` on stdout, nothing else; see [exitcodes.md](../exitcodes.md) |

## Results

Every client result except `client types` starts with the meter fields `target`, `link_address`, `ca`, `session`, `duration_ms`.

| Command | Fields |
|---------|--------|
| `client info` | `standard` (the first octet of P_MP_NA_2), `manufacturer` (an octet), `product` (four octets) |
| `client time` | `time`, `su`, `time_invalid` (the clock, time b), `timestamp` (RFC 3339 in `--timezone`; omitted when the fields are no valid time), `drift_ms` (the meter's clock minus this computer's when the answer arrived; omitted with `timestamp`) |
| `client totals` | `record`, `request` and `request_id` (the read command sent, `"C_CI_NT_2"`, 122), `kind` (`"operational"`, `"reset-operational"`, `"accounting"`, `"reset-accounting"`), `from` and `to` (time a of the range; `from` alone for a past period; omitted for the oldest), `first` and `last` (the address range, when the command carries one), `count` (periods), `periods` |
| `client events` | `record`, `request` (`"C_SP_NA_2"` or `"C_SP_NB_2"`), `from`, `to`, `count`, `events` |
| `client request` | `request` (the ASDU sent), `until` (`"reply"`, `"confirmation"`, `"termination"`), `count`, `answers` (every ASDU of the measuring point received, mirrors included) |
| `client ree login` | the meter fields only |
| `client ree set-time` | `time`, `su`, `time_invalid` (the time b sent), `rtt_ms` |
| `client ree tariffs` | `register`, `request` (`"C_TA_VC_2"` or `"C_TA_VM_2"`), `from`, `to` (stored values), `count`, `records` |
| `client ree contracted-powers` | `record`, `time` (the time a of M_PC_NA_2; omitted without an answer), `count`, `powers` |
| `client ree special-days` | `record`, `count`, `days` (time objects) |
| `client types` | `count`, `types` |
| `server start` (stream) | one event per line (JSON Lines) |
| `version --json` | as for every tool of the repository: `name`, `version`, `tag`, `commit`, `built`, `libraries`, `go`, `platform` |

### Period (`periods[]`)

| Field | Meaning |
|-------|---------|
| `type`, `type_id` | the ASDU it came in, M_IT_TA_2 .. M_IT_TM_2 (2..13) |
| `end` | the common time tag: the end of the integration period (time object, time a) |
| `totals` | one object per integrated total: `address` (the information object address), `value` (the counter reading, unsigned), `quality` (the quality descriptor octet as received), `flags` (its set bits joined with `\|`: `IV`, `CA`, `CY`, and `SEQ=n` for the sequence number of the standard, or with `--profile ree` / an access key `VH`, `MP`, `IN`, `AL`, `RES` of the REE profile; `"good"` when none), `signature` (the signature octet when the meter sends one; not checked) |

A period that the meter split over several ASDUs is joined into one.

### Event (`events[]`)

`address` (SPA), `on` (SPI), `qualifier` (SPQ, 0-127), `time` (time object, time b).

### ASDU (`request`, `answers[]`)

`type`, `type_id`, `cause` (the name, `"activation confirmation"`), `cot` (the cause, 6 bits), `negative` (P/N), `test` (T), `ca`, `record`, `objects` (the number of the VSQ), `sq` (the SQ bit), `data` (the octets after the record address, lower-case hex; the time tag included). `negative`, `test` and `sq` are omitted when not set.

### Tariff record (`records[]`)

`address` (the register address of the contract), `active_abs`, `active_inc`, `active_quality`, `reactive_ind_abs`, `reactive_ind_inc`, `reactive_ind_quality`, `reactive_cap_abs`, `reactive_cap_inc`, `reactive_cap_quality` (absolute and incremental energies with their quality octets), `max_power`, `max_power_time` (time object), `max_power_quality`, `excess_power`, `excess_power_quality`, `period_start`, `period_end` (time objects: the billing period). The two reserved totals of the record are not reported.

### Contracted power (`powers[]`)

`address` (the tariff period), `power_kw`.

### Type (`types[]`)

`id`, `name`, `direction` (`"monitor"` or `"control"`), `profile` (`"standard"` or `"ree"`), `decoded` (whether iec102ctl shows the objects as fields; the others as hex).

### Server event (`server start`)

`timestamp` (RFC 3339 UTC, milliseconds), `event`, and where they apply `remote` (the peer, the listen address or the serial device), `ca`, `record`, `type`, `cot` and `detail`. Events:

| `event` | When |
|---------|------|
| `listening` | once, when the meter accepts lines (`remote`: the address; `detail`: the model's name and addresses) |
| `connected`, `disconnected` | a TCP connection |
| `busy` | a connection that arrived while a line was served, closed at once |
| `link-up` | the controlling station reset the link |
| `request` | an ASDU in control direction (`type`, `ca`, `record`, `cot`; `detail`: the range or period read) |
| `login` | C_AC_NA_2 (`detail`: `"access key accepted"` or `"access key refused"`) |
| `logout` | C_FS_NA_2 |
| `set-time` | C_CS_TA_2 (`detail`: the time b, with ` SU` when the bit is set) |
| `refused` | a request mirrored with a cause of refusal or the P/N bit (`cot`, `detail`: the cause) |
| `stopping` | once, when the server stops |

## Streams and stderr

- `server start` writes **compact** JSON, one object per line; every other command writes **one** indented JSON document.
- The `--debug` frame trace and the causes of an error go to **stderr**, as text, in every format. stdout holds nothing but the result.
