# `--format json` field conventions

These conventions are a **stable contract** for scripting. Field names are snake_case. A breaking change to a name or shape is called out in the release notes.

## General

| Concept | JSON fields |
|--------|-------------|
| Station | `target`: the URL of the station (`tcp://host:2404`, `tls://host:19998`) |
| Common address | `ca` (number) |
| Information object address | `ioa` (number) |
| Type identification | `type` (name, `"M_ME_NC_1"`) and `type_id` (number, `13`) |
| Cause of transmission | `cause` (name, `"spontaneous"`) and `cot` (number, `3`) |
| Times | RFC 3339 with milliseconds. `timestamp` is the time tag of an object, in the zone `--timezone` names; `received_at` is when a value arrived, in UTC |
| Durations | `duration_ms`, `rtt_ms` (numbers, milliseconds with fractions) |
| Repeated rows | `points`, `rows`, `steps`, `stations`, `entries`, `devices`: **arrays of objects**, empty arrays rather than null |
| A row that failed | `error` object (`kind`, `code`, `message`); omitted when the row is fine |
| Fatal error | one object `{"error": {"kind", "code", "message"}}` on stdout, nothing else; see [exitcodes.md](exitcodes.md) |

## Point

One information object as a station reported it. Used by `client interrogate`, `counters`, `read` and `monitor`.

| Field | Meaning |
|-------|---------|
| `ca`, `ioa`, `type`, `type_id`, `cause`, `cot` | as above |
| `value` | boolean for single points and single commands; string for double points (`"off"`, `"on"`, `"intermediate"`, `"indeterminate"`) and step commands (`"higher"`, `"lower"`); number for everything else; `null` for a type without a value |
| `quality` | `"good"`, or the set flags joined with `\|` (`"NT\|IV"`; flags `OV`, `BL`, `SB`, `NT`, `IV`, and `EI` for protection events). Omitted for types without a quality descriptor |
| `timestamp`, `timestamp_invalid` | the time tag, for types that carry one (`…_TA_1`, `…_TB_1` and so on); `timestamp_invalid` when the IV bit of the time tag is set |
| `negative`, `test` | the P/N and T bits of the ASDU; omitted when not set |
| `originator` | originator address; omitted when 0 |
| `attributes` | what only some types carry: `raw` (the 16-bit value of a normalized value), `transient` (step position), `sequence`, `carry`, `adjusted` (integrated total), `elapsed_ms`, `duration_ms`, `operating_ms` (protection events), `changed` (packed single points), `qualifier`, `select` (commands), `time_substituted`, `summer_time` (time tag) |
| `received_at` | streams (`client monitor`) only |

## Results

| Command | Shape |
|---------|-------|
| `client interrogate`, `client counters` | `target`, `ca`, `request` (`"interrogation"` or `"counter-interrogation"`), `group`, `freeze` (counters), `duration_ms`, `received` (objects the station sent), `count` (objects listed after `--type` / `--ioa`), `points` |
| `client read` | `target`, `ca`, `requested`, `failed`, `rows`: `ioa` and either `point` or `error` |
| `client monitor` (stream) | one **Point** per line (JSON Lines), written as it arrives. Connection state and the summary go to stderr as text |
| `client command` | `target`, `ca`, `ioa`, `type` (the type sent, `"C_DC_NA_1"` or `"C_DC_TA_1"`), `value`, `mode`, `qualifier`, `steps`: `step` (`"select"`, `"execute"`, `"cancel"`, `"termination"`), `result` (`"confirmed"`, or `"received"` / `"not-received"` for the termination), `rtt_ms` |
| `client clock`, `client reset` | `target`, `ca`, `request` (`"clock-synchronization"` or `"reset-process"`), `time` or `qualifier`, `rtt_ms` |
| `client ping` (stream) | one object per test (JSON Lines): `seq` (1-based), `timestamp`, `method` (`"testfr"` or `"command"`), `rtt_ms`, and `error` on a failed test. The summary goes to stderr as text |
| `client scan` | `target`, `method`, `probed`, `found`, `duration_ms`, `stations`: `ca`, `status` (`"found"`, `"refused"`), `objects`, `types` (`type`, `count`), `error` |
| `client file list` | `target`, `ca`, `ioa`, `count`, `entries`: `ioa`, `name`, `length`, `timestamp`, `directory`, `active` |
| `client file get`, `client file put` | `target`, `ca`, `direction` (`"download"` or `"upload"`), `ioa`, `name`, `length`, `sections` (upload), `sha256`, `path`, `duration_ms` |
| `client types` | `count`, `types`: `id`, `name`, `description`, `direction` (`"monitor"` or `"control"`), `time_tag`, `decoded` |
| `discover` | `subnets`, `ports`, `hosts` (addresses probed), `found`, `duration_ms`, `devices`: `ip`, `port`, `status` (`"iec104"` or `"open"`), `rtt_ms` |
| `version --json` | one line, keys in this order: `name`, `version` (no leading `v`; `"dev"` for a build without one), `tag` (`""` for a build that is not a release), `commit` (`"unknown"` when not recorded; `-dirty` for a modified tree), `built` (RFC 3339 UTC, or `"unknown"`), `libraries` (the OT Fabric libraries the binary is built on, sorted: `name`, `version`; `[]` when none), `go`, `platform` (`os/arch`). Every key is always present. `IEC104CTL_FORMAT=json` gives the same |
| `server start` (stream) | one event per line (JSON Lines): `timestamp`, `event`, and where they apply `remote`, `ca`, `ioa`, `type`, `value`, `detail`. Events: `listening`, `connected`, `started`, `stopped`, `disconnected`, `refused`, `secured`, `security-refused`, `interrogation`, `counter-interrogation`, `read`, `command`, `clock`, `reset`, `test`, `directory`, `file-sent`, `file-received`, `stopping`. With `--secure`: `secured` when a controlling station has associated (`detail`: its AIM, certificate subject, the AIS and MAC algorithm), `security-refused` when an association is refused (`detail`: the AIM, the security event and why), and `refused` for a request the roles do not permit; these carry no `remote` |

## Streams and stderr

- A streaming command writes **compact** JSON, one object per line; every other command writes **one** indented JSON document.
- Progress, connection state, summaries and the `--debug` frame trace go to **stderr**, as text, in every format. stdout holds nothing but the result.
- `client file get --output -` is the exception that proves the rule: the file content takes stdout, and the result goes to stderr.
