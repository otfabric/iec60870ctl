# iec101ctl: `--format json` field conventions

These conventions are a **stable contract** for scripting. Field names are snake_case. A breaking change to a name or shape is called out in the release notes. The fatal error object and the exit codes are shared by the four tools: see [../exitcodes.md](../exitcodes.md).

## General

| Concept | JSON fields |
|--------|-------------|
| Line | `target`: the URL of the line (`tcp://host:2401`, `serial:///dev/ttyUSB0`) |
| Common address | `ca` (number) |
| Link address | `link_address` (number) |
| Information object address | `ioa` (number) |
| Type identification | `type` (name, `"M_ME_NC_1"`) and `type_id` (number, `13`) |
| Cause of transmission | `cause` (name, `"spontaneous"`) and `cot` (number, `3`) |
| Times | RFC 3339 with milliseconds. `timestamp` is the time tag of an object, in the zone `--timezone` names; `received_at` is when a value arrived, in UTC |
| Durations | `duration_ms`, `rtt_ms`, `measured_ms` (numbers, milliseconds with fractions) |
| Repeated rows | `points`, `rows`, `steps`, `stations`, `types`, `devices`: **arrays of objects**, empty arrays rather than null |
| A row that failed | `error` object (`kind`, `code`, `message`); omitted when the row is fine |
| Fatal error | one object `{"error": {"kind", "code", "message"}}` on stdout, nothing else |

## Point

One information object as a station reported it. Used by `client interrogate`, `counters`, `read` and `monitor`.

| Field | Meaning |
|-------|---------|
| `ca`, `ioa`, `type`, `type_id`, `cause`, `cot` | as above |
| `value` | boolean for single points and single commands; string for double points (`"off"`, `"on"`, `"intermediate"`, `"indeterminate"`) and step commands (`"higher"`, `"lower"`); number for everything else; `null` for a type without a value |
| `quality` | `"good"`, or the set flags joined with `\|` (`"NT\|IV"`; flags `OV`, `BL`, `SB`, `NT`, `IV`, and `EI` for protection events). Omitted for types without a quality descriptor |
| `timestamp`, `timestamp_invalid` | the time tag, for types that carry one (`…_TA_1`, `…_TB_1` and so on); `timestamp_invalid` when the IV bit of the time tag is set |
| `negative`, `test` | the P/N and T bits of the ASDU; omitted when not set |
| `originator` | originator address (with `--cot-size 2`); omitted when 0 |
| `attributes` | what only some types carry: `raw` (the 16-bit value of a normalized value), `transient` (step position), `sequence`, `carry`, `adjusted` (integrated total), `elapsed_ms`, `duration_ms`, `operating_ms` (protection events), `changed` (packed single points), `local_change` (end of initialization), `qualifier`, `select` (commands), `freeze` (counter interrogation), `name` (file directory), `raw_octets` (a type without object model), `time_substituted`, `summer_time` (time tag) |
| `received_at` | streams (`client monitor`) only |

## Results

| Command | Shape |
|---------|-------|
| `client interrogate`, `client counters` | `target`, `ca`, `request` (`"interrogation"` or `"counter-interrogation"`), `group`, `freeze` (counters), `duration_ms`, `received` (objects the station sent), `count` (objects listed after `--type` / `--ioa`), `points`. A counter freeze command has no points |
| `client read` | `target`, `ca`, `requested`, `failed`, `rows`: `ioa` and either `point` or `error` |
| `client monitor` (stream) | one **Point** per line (JSON Lines), written as it arrives. Link state and the summary go to stderr as text |
| `client command` | `target`, `ca`, `ioa`, `type` (the type sent, `"C_DC_NA_1"`), `value`, `mode`, `qualifier`, `steps`: `step` (`"select"`, `"execute"`, `"cancel"`, `"termination"`), `result` (`"confirmed"`, or `"received"` / `"not-received"` for the termination), `rtt_ms` |
| `client clock`, `client reset` | `target`, `ca`, `request` (`"clock-synchronization"` or `"reset-process"`), `time` or `qualifier`, `rtt_ms` |
| `client delay` | `target`, `ca`, `measured_ms` (the transmission delay measured; omitted when nothing was measured), `set_ms` (the delay sent to the station; omitted when nothing was sent), `rtt_ms` |
| `client ping` (stream) | one object per test (JSON Lines): `seq` (1-based), `timestamp`, `method` (`"command"`, `"link-status"` or `"link-test"`), `rtt_ms`, and `error` on a failed test. The summary goes to stderr as text |
| `client scan` (`--method broadcast`, `ca`) | `target`, `method`, `probed`, `found`, `duration_ms`, `stations`: `ca`, `status` (`"found"`, `"refused"`), `objects`, `types` (`type`, `count`), `error` |
| `client scan --method link` | `target`, `method` (`"link"`), `probed`, `found`, `duration_ms`, `stations`: `link_address`, `status` (`"answered"`, or `"refused"` for a link layer that answered with an error), `acd`, `dfc` (the access demand and data flow control bits of the status of link), `rtt_ms`, `error` |
| `client types` | `count`, `types`: `id`, `name`, `description`, `direction` (`"monitor"` or `"control"`), `time_tag`, `decoded` |
| `discover` | `subnets`, `ports`, `hosts` (addresses probed), `found`, `duration_ms`, `devices`: `ip`, `port`, `status` (`"iec101"` or `"open"`), `link_address` (the address asked), `rtt_ms` |
| `version --json` | one line, keys in this order: `name`, `version`, `tag`, `commit`, `built`, `libraries` (`name`, `version`), `go`, `platform`; as for every tool of the repository. `IEC101CTL_FORMAT=json` gives the same |
| `server start` (stream) | one event per line (JSON Lines): `timestamp`, `event`, and where they apply `remote` (the TCP peer, or the serial URL), `ca`, `ioa`, `type`, `value`, `detail`. Events: `listening`, `connected`, `busy` (a TCP connection turned away while a line is served), `link-up`, `link-down`, `disconnected`, `refused` (an unknown common address or type), `interrogation`, `counter-interrogation`, `read`, `command`, `clock`, `delay`, `reset`, `test`, `stopping` |

## Streams and stderr

- A streaming command writes **compact** JSON, one object per line; every other command writes **one** indented JSON document.
- Progress, link state, summaries and the `--debug` frame trace go to **stderr**, as text, in every format. stdout holds nothing but the result.

## Error codes of IEC 101

Besides the codes every tool shares, iec101ctl reports `link_init_failed` (the link did not come up: exit 5), `link_down` (a request while the link was down: exit 4), `link_refused` (the link layer answered NACK, data flow control, "not functioning" or "not implemented": exit 6) and `profile_violation` (an ASDU refused by the profile checks of `--strict`: exit 3).
