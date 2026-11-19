# Releases

Release notes of iec101ctl, iec102ctl, iec103ctl and iec104ctl, newest first. The four
tools share one version; each section is headed exactly `## vX.Y.Z` and ends with a line
`---` ([docs/RELEASING.md](docs/RELEASING.md)). The releases of iec104ctl from its own
repository are in [docs/iec104ctl/HISTORY.md](docs/iec104ctl/HISTORY.md).

## v0.13.0

**Date:** 2026-11-20
**Previous release:** v0.12.2

Adds `client parameter` to iec101ctl and iec104ctl, clears the queued time-tagged events of the iec104ctl station on a reset of pending events, writes the errors found before a command runs as JSON when JSON was asked for, and accepts each tool's name for the per-address scan. Built on go-iec60870-5 v0.10.0 and go-serial v0.6.0.

### Breaking

- **iec103ctl: the times the tool writes are RFC 3339 in UTC with milliseconds** (`2026-11-20T09:15:23.307Z`), as in iec101ctl and iec104ctl: `received` of `client monitor`, `timestamp` of the `server start` events, and `value` of its `clock-sync` event. Before, `received` and `timestamp` were local time with nanoseconds and the `clock-sync` value was in the device's time zone with nanoseconds. A script that parsed them as RFC 3339 keeps working; one that compared them as text against local times must convert. The time tags the device sends, and `sent` and `device_time` of `client clock`, stay in `--timezone`; CP32Time2a tags (`HH:MM:SS.mmm`) carry no date and stay as they are.
- iec103ctl `client generic write`: `--gin`, `--type` and `--value` are no longer marked as required flags, because `--data` replaces them. Without `--data`, leaving one off still exits `2` (usage), but help and shell completion no longer show them as required.

### Changed

- **All four tools: an error found before the command runs is written as JSON** when the command line has `--format json` (or `--format=json`) or `<TOOL>_FORMAT=json` is set: an unknown flag, a missing required flag, a flag value that does not parse (exit `2`), and an environment variable that does not parse (exit `3`). Before, these were one `error:` line on stderr whatever the format, against what [docs/json-output.md](docs/json-output.md) promised. The exit codes are unchanged; a script that read such an error from stderr with `--format json` reads one `{"error": …}` object on stdout now.
- Built on go-iec60870-5 v0.10.0 (was v0.9.0) and go-serial v0.6.0 (was v0.5.0) (go-iec60870-5 RELEASE.md, v0.10.0; go-serial RELEASE.md, v0.6.0). docs/iec102ctl/LIMITATIONS.md and docs/iec103ctl/LIMITATIONS.md name go-iec60870-5 v0.10.0.

### Added

- iec101ctl and iec104ctl: **`client parameter`** (alias `param`) loads a parameter of measured value, `P_ME_NA_1`, `P_ME_NB_1` or `P_ME_NC_1` (`--qualifier threshold`, `smoothing`, `low-limit` or `high-limit`), or sends a parameter activation, `P_AC_NA_1` (`--qualifier loaded` at `--ioa 0`, `object` or `cyclic`), and waits for the activation confirmation (IEC 60870-5-104:2000 7.9; IEC 60870-5-101:2003 7.2.6.24, 7.2.6.25, 7.4.9, 7.4.10). It changes the station: everything is validated before anything is sent (exit `3`), it is sent once, and a refusal exits `6`. `--type`, `--ioa`, `--value` and `--qualifier` have no environment variable. JSON: `target`, `ca`, `ioa`, `type`, `value` (omitted for an activation), `qualifier`, `rtt_ms` ([docs/iec104ctl/json-output.md](docs/iec104ctl/json-output.md)).
- iec101ctl and iec104ctl `server start`: the simulated station confirms a parameter of measured value for a measured value of its model, and a parameter activation, with a new `parameter` event (`detail` such as `loaded: threshold` or `activated: object parameter`). A parameter for another object, an unknown address or kind, a non-finite value and a cause other than activation are refused. The station keeps no parameters and does not apply them (LIMITATIONS.md). Before, it answered these types with "unknown type identification".
- iec104ctl `server start`: a reset process command with the qualifier `events` (QRP 2, reset of pending information with time tag, IEC 60870-5-101:2003 7.2.6.27) removes the queued events that carry a time tag; the `reset` event has the `detail` `pending events: N with time tag cleared`. Before, the events stayed queued and reached the next controlling station. The iec101ctl station already cleared its queue.
- iec101ctl and iec102ctl: **`--link-retries -1`** sends an unanswered frame once and does not repeat it (go-iec60870-5 link.Config). `0` stays the default, 3. A value outside -1 to 255 exits `3` before the line is opened; before, a negative value did not parse (exit `2`).
- iec102ctl: `--ca` is accepted for `--common-address`, the name iec101ctl and iec104ctl use. Shell completion of `--type` for `client totals` (the reads of integrated totals `C_CI_NA_2` .. `C_CI_NU_2`) and `client request` (every type the tool knows by name).
- iec104ctl `client scan --method ca` and iec101ctl `client scan --method sweep`: each tool accepts the other's name for one general interrogation per common address. The result reports the tool's own name (`sweep`, `ca`).

- iec103ctl `client monitor`: filters `--type` (numbers or names), `--fun`, `--inf` and `--cause` (numbers or names), each a list or range (`--fun 160,176 --inf 64-71`), with the variables `IEC103CTL_MONITOR_TYPES`, `IEC103CTL_MONITOR_FUN`, `IEC103CTL_MONITOR_INF` and `IEC103CTL_MONITOR_CAUSE`. `--count` counts the messages shown.
- iec103ctl `client generic write --data GIN[:KOD]=TYPE[/SIZE]:VALUE`, repeatable: up to 64 data sets in one write, in as many ASDUs 10 as they need, write entry (INF 248) and the last with write with execution (INF 250), for the device to activate together (IEC 60870-5-103:1997 7.4.8.6.3, as go-iec60870-5 implements it). Command line only; with `--gin`, `--type`, `--size` or `--value` on the command line it exits `3`.
- iec103ctl `client types`: the ASDU types of IEC 60870-5-103 the tool knows (`count`, `types`: `id`, `name`, `description`, `direction` `monitor`, `control` or `both`), with `--direction`; `--type` of `client monitor` completes to them.
- iec103ctl: `--ca` is accepted for `--common-address` and `--link-retries` for `--retries`, the names of the other tools; `--retries -1` is none, as `0`, and a value outside -1 to 10 exits `3`.

### Fixed

- iec101ctl: shell completion of `--type` (`client interrogate`, `client monitor`) offers only the types IEC 60870-5-101 defines; before, it also offered IEC 104 types such as `C_SC_TA_1` and `C_TS_TA_1`.

### Documentation

- [docs/exitcodes.md](docs/exitcodes.md) lists the partial exit `7` of iec102ctl (an REE session whose logout was not confirmed) and of iec103ctl (`client generic read` answered with cause 43), and `client parameter` among the commands that change a station.
- The iec101ctl and iec104ctl JSON references say that `qualifier` of `client command` is a name for single, double and step commands and the number QL for set points.

---

## v0.12.2

**Date:** 2026-11-16
**Previous release:** v0.12.1

No command or flag is added or changed.

### Changed

- Built on go-iec60870-5 v0.9.0 (was v0.8.0) (go-iec60870-5 RELEASE.md, v0.9.0).

---

## v0.12.1

**Date:** 2026-11-16
**Previous release:** v0.12.0

No command or flag is added or changed.

### Changed

- Built on go-iec60870-5 v0.8.0 (was v0.7.0) and go-serial v0.5.0 (was v0.4.1) (go-iec60870-5 RELEASE.md, v0.8.0; go-serial RELEASE.md, v0.5.0). For the tool this means: the state changes of the IEC 101, 102 and 103 stations reach it one at a time and in order, the closed state last; on Windows a serial device must be given by its device name (`COM3`, `COM10`, `\\.\COM10`, a device interface name), another form is refused when the port is opened, and every port has a `COM` name. docs/iec102ctl/LIMITATIONS.md and docs/iec103ctl/LIMITATIONS.md name go-iec60870-5 v0.8.0.

---

## v0.12.0

**Date:** 2026-11-15
**Previous release:** v0.11.1

Takes the parameters that decide what a command changes at a station from the command line only, refuses TLS flags with a `tcp://` URL, gives NaN and infinite numbers a JSON form, bounds what the simulated stations take from a model, a line and an upload, and limits iec104ctl `server start` to 16 connections by default. Still built on go-iec60870-5 v0.7.0 and go-serial v0.4.1.

### Breaking

- **All four tools: the parameters that decide what a command changes are taken only from the command line.** When one of them is set only by its environment variable, the variable is not used: the command runs with the flag's built-in default and writes one line on stderr before anything is sent, for example `notice: IEC104CTL_FREEZE=reset is not used: --freeze is taken only from the command line`. Before, the value of the variable was used. To keep the old handling, give the flag on the command line. Read-only uses of the same variables are unchanged (`IEC101CTL_CA=255` still sends `client interrogate` to every station). The parameters, and what a script that set only the variable gets now:
  - iec101ctl: `IEC101CTL_FREEZE` other than `read` (`client counters`): a counter read; `IEC101CTL_SET_DELAY` (`client delay`): a measurement only, no delay sent; `IEC101CTL_QUALIFIER` (`client command`): `none`; `IEC101CTL_TIME` other than `now` (`client clock`): the current time; `IEC101CTL_CA` at the broadcast address (255, or 65535 with `--ca-size 2`) for `client clock`, `client reset`, and `client counters` with a freeze: `--ca 1`.
  - iec102ctl: `IEC102CTL_TIME` (`client ree set-time`): the current time; `IEC102CTL_CAUSE`, `IEC102CTL_RECORD`, `IEC102CTL_DATA` and `IEC102CTL_OBJECTS` (`client request`): the defaults (`--cause activation`, `--record 0`, no `--data`, `--objects -1`). The read commands still take `IEC102CTL_RECORD`.
  - iec103ctl: `IEC103CTL_BROADCAST=true` (`client clock`): a synchronization of the one device at `--link-address`, confirmed by it (`"broadcast": false` with `device_time`); `IEC103CTL_TIME` other than `now`: the time of sending, and a value that is not a time is no longer refused with exit `3`.
  - iec104ctl: `IEC104CTL_FREEZE` other than `read` (`client counters`): a counter read; `IEC104CTL_QUALIFIER` (`client command`): `none`; `IEC104CTL_TIME` other than `now` (`client clock`): the current time; `IEC104CTL_CA=65535` for `client clock`, `client reset`, and `client counters` with a freeze: `--ca 1`.
- **iec101ctl and iec104ctl `client command`, iec103ctl `client generic write`: a procedure from the environment refuses the command.** With `IEC101CTL_COMMAND_MODE`, `IEC104CTL_COMMAND_MODE` or `IEC103CTL_WRITE_MODE` set and no `--mode`, the command exits `3` (`invalid_input`, `invalid_flag_combination`) before it connects, with `IEC104CTL_COMMAND_MODE=select is not used: --mode is taken only from the command line; give --mode select to run it so`, for every value of the variable, `execute` included. It does not fall back to the default, which would execute (or, for iec103ctl, write with execution) where the variable asked to select or to abort. Before, the procedure of the variable was run. Give `--mode` on the command line.
- **iec104ctl: a TLS flag with a `tcp://` URL is refused.** `--tls`, `--tls-ca`, `--tls-cert`, `--tls-key`, `--tls-server-name` or `--tls-insecure`, or one of the variables `IEC104CTL_TLS`, `IEC104CTL_TLS_CA`, `IEC104CTL_TLS_CERT`, `IEC104CTL_TLS_KEY`, `IEC104CTL_TLS_SERVER_NAME`, `IEC104CTL_TLS_INSECURE`, together with `--url tcp://…` now ends every client command with exit `2` (JSON error kind `usage`, code `invalid_flag_combination`) before it connects; the message names the flags and the `tls://` form of the URL. Before, the command connected over plain TCP without the TLS flags. To connect with TLS, give a `tls://` URL, or `--ip` with the TLS flags. To keep plain TCP, unset the TLS flags and their variables, also a shell that exports `IEC104CTL_TLS_CERT` and `IEC104CTL_TLS_KEY` for `server start`.
- **iec101ctl and iec104ctl: `<TOOL>_IOA` is only the address list** of `client interrogate`, `client read` and `client monitor`. The address of `client command` is `IEC101CTL_COMMAND_IOA` and `IEC104CTL_COMMAND_IOA`, the address of iec104ctl `client file list`, `get` and `put` is `IEC104CTL_FILE_IOA`. A script that chose the directory of iec104ctl `client file list` with `IEC104CTL_IOA` sets `IEC104CTL_FILE_IOA` now; with only `IEC104CTL_IOA` it lists the default directory (0). `--ioa` stays a required flag of `client command`, `file get` and `file put`, which no variable replaces. Before, a list or range in `<TOOL>_IOA` (`100-199`) stopped every command of the tool at start-up with exit `3`, `version` included; it now filters, and every other command runs.
- **iec102ctl `server start --serial` exits `4`** (transport, `connection_lost`) when its serial port fails while it is served, for example when the adapter is removed; it exited `0`. Ctrl-C and the end of `--duration` still exit `0`. A script or supervisor that restarted the server on exit `0` should treat exit `4` as the port failure it is.
- **iec104ctl `server start` serves at most 16 connections at once by default, and the built-in demo station takes no files.** A further connection is closed at once, as with `--max-sessions` before; before, the default was no limit. To keep the old handling, give `--max-sessions 0` (no limit) or the number you need, also as `IEC104CTL_MAX_SESSIONS`. The demo station, which `server start` serves without `--config` and `server generate-config` writes, has no `uploads` section: `client file put` to it is refused (exit `6`). To take files, serve a model with `uploads: {accept: true}`, for example the written demo with that section added.

### Added

- All four tools: a NaN or infinite floating point number a station or device sent is `null` in JSON, with a new field `non_finite` next to it: `"NaN"`, `"+Inf"` or `"-Inf"`. `non_finite` is omitted for every other value; text and table output show the same names ([docs/json-output.md](docs/json-output.md)). Where it is:
  - iec101ctl and iec104ctl: the points of `client interrogate`, `counters`, `read`, `scan` and `monitor` (`value`), and the `command` events of `server start` (a refused set point);
  - iec103ctl: in place of `scl` (ASDU 4), as one string per element of `values` for generic data sets and description elements (structure fields too), and as an object for `rpv`, `rsv` and `rfa` of a disturbance channel.
- iec102ctl `server start`: `warning` and `error` events in the event log, for an answer that cannot be queued and for the warnings and errors of the library's server.

### Changed

- All four tools: a second Ctrl-C (or SIGTERM) while a command is still ending, a logout or the end of a stream, ends the process at once with exit `130` ([docs/exitcodes.md](docs/exitcodes.md)). Before, it was not noticed.
- iec101ctl, iec102ctl and iec103ctl `server start` over TCP: at most four connections wait for the line while one is served, each up to half a second; a further one is closed at once (`busy`). An accept that fails while the server listens is logged as a `busy` event whose `detail` names the error, and the server accepts again after a pause of 5 ms growing to 1 s; before, `server start` ended with exit `4`. The help of `server start` no longer says that a second connection is closed at once.
- iec101ctl `discover --link-mode balanced` and `client ping --method link-status|link-test`: user data the station sends meanwhile (its events) is answered with a negative acknowledgement (DFC set), and the station keeps it for the controlling station that brings the link up next. Before, it was acknowledged and dropped.
- iec101ctl and iec104ctl `server start`: a set point that is NaN or infinite is answered with a negative confirmation, as a select and as an execute; the target keeps its value, and the `command` event names the number in `non_finite`. A model whose `simulate` has a NaN or infinite `min`, `max` or `step` is refused.
- iec102ctl: `--access-key` is deprecated: it still works and prints a warning on stderr. Give the key with `--access-key-file` or `IEC102CTL_ACCESS_KEY`; the help and the documents use `--access-key-file`.
- iec102ctl `server start`: a model holds at most 1000000 integrated totals in all, listed and generated, and a generated period at most 256 totals, each address once; at most 48 contracted powers and 49 special days (was 127 each), as many as one answer carries. A model beyond a limit is refused when it is loaded, before its totals are generated.
- iec103ctl `client disturbance get` creates `--output` and `--csv` before it connects: a file that cannot be created, in a directory that does not exist for example, is exit `3` (`output_file_create_failed`) before anything is sent; it was exit `10` after the transfer, whose final acknowledgement lets the device delete the record. `--force` empties an existing file at the start; the files are removed when the command fails.
- iec103ctl `server start`: `simulate` of an indication is 0 (none) or 100 to 86400000 ms; any other value is refused when the model is loaded.
- iec103ctl `server start`: a generic write staged with write entry belongs to the line that staged it and ends with the line and with a reset CU or reset FCB. One write stages at most 64 data sets and a line at most four writes; beyond that the write is refused with cause 41 and reply code 7.
- iec104ctl `server start`: a model file holds at most 16 MiB. A file of the model holds at most 16777215 octets, checked before anything is read or made: from `size` and `content`, and for `path` from the size the file system reports, which must be a regular file (not a directory, device or named pipe). The station reads it when it starts.
- iec104ctl `server start`: the upload limits (`uploads.max_files`, `uploads.max_total_size`) count the files being delivered, with their announced length, across all connections: a delivery beyond them is refused when it is announced. A delivery gives its place back when it is stored, when it ends without being stored, and when its connection ends.
- iec104ctl `client command` help and usage.md: ending a selection with its connection is the simulated station's rule; a real station may keep a selection until its own select time-out.
- Release workflow: `bump: none` tests, builds and stamps the commit of the newest tag, and takes its notes from that commit, whatever branch it is dispatched from.

### Fixed

- iec101ctl, iec103ctl and iec104ctl: a NaN or infinite number from a station or device no longer ends `client interrogate`, `client read`, iec103ctl generic reads and `disturbance get` with exit `10` (`json_encode_failed`), or `client monitor --format json` at the first such value (`output_write_failed`).
- iec101ctl `server start`: a counter interrogation with freeze 3 (counter reset) is carried out, as server-config.md says; it was refused with a negative confirmation.
- iec101ctl and iec104ctl on 32-bit platforms (linux/arm/v7): an `--ioa` list is counted in 64 bits, so a list of more than 100000 addresses is refused there as well (iec104ctl: "more than 100000"), and a `client monitor --count` of 2^31 or more no longer ends at the first value.
- iec102ctl `server start`: long answers (a year of hourly periods, a whole list of events, every tariff record of a register) are served in full: the queue of a line holds the longest answer of the model. Before, an answer longer than the library's queue was cut short and the client waited for its time-out. A request whose answer cannot be queued ends without its termination and is logged, instead of being confirmed as complete.
- iec102ctl `--debug`: the frame trace hides the access key also of a login frame that cannot be decoded.
- iec103ctl `client generic write --mode abort` sends the write abort (INF 251) also when the write entry fails, times out or is interrupted, on a `--timeout` of its own, and sends an abort that is not answered in time once more. The error says whether the abort was acknowledged or that the value may still be staged on the device. Before, the abort was not sent in these cases.
- SECURITY.md and the guides: `--allow` is iec104ctl's; every iec103ctl client command starts the link with reset CU, which empties the relay's transmit buffers unless `--reset-fcb` is given; `disturbance get` lets the device delete the record; the iec102ctl simulator answers the clock setting and the REE reads without a session; the iec101ctl naming rule of the environment variables and its exceptions.
- Tests: the station backlog test of iec101ctl runs on a fake clock, and the iec104ctl test that nothing is sent for wrong input counts connections instead of waiting; new tests cover the reconnection of the iec104ctl monitor, and the goroutine checks of iec102ctl have no tolerance. CI runs the tests on linux/386.

---

## v0.11.1

**Date:** 2026-11-15
**Previous release:** v0.11.0

No command or flag is added or changed.

### Changed

- Built on go-serial v0.4.1 (was v0.4.0): on Windows a serial write that times out returns an error together with the bytes written, where it returned a short count without one; on Linux, macOS and the BSDs a port path that is not a character device is refused when it is opened; reads wait correctly on OpenBSD (go-serial RELEASE.md, v0.4.1). This reaches the serial lines of iec101ctl, iec102ctl and iec103ctl.

---

## v0.11.0

**Date:** 2026-11-14
**Previous release:** v0.10.1

Reports a request that hits the result limit of go-iec60870-5 as a protocol error. Still built on go-iec60870-5 v0.7.0.

### Changed

- All four tools: a request that hits the library's result limit now exits `6` (protocol) instead of `10` (internal), with JSON error kind `protocol` and the new code `result_limit` (was `internal` / `internal_unexpected`). This covers iec101ctl and iec104ctl `client interrogate` and `client counters`, iec104ctl `client file list`, the iec104ctl requests that end because more than 16 answers to earlier requests are queued ahead of them, and the collecting requests of iec102ctl and iec103ctl. Scripts that treated exit `10` from such a request as the result limit should check for `6` and `result_limit` ([docs/exitcodes.md](docs/exitcodes.md)).

---

## v0.10.1

**Date:** 2026-11-14
**Previous release:** v0.10.0

Moves the four tools to go-iec60870-5 v0.7.0 (was v0.6.0). The commands' options, output and exit codes are unchanged; the library's new bounds apply with their defaults.

### Changed

- Built on go-iec60870-5 v0.7.0 (go-iec60870-5 RELEASE.md, v0.7.0). For the tools this means:
  - iec104ctl `client interrogate`, `client counters` and `client file list`: one request collects at most 65536 ASDUs (directory entries for `file list`), as iec101ctl does since v0.10.0. A request that would collect more ends with the library's result-limit error and what arrived so far; it is reported as in iec101ctl, iec102ctl and iec103ctl. A request that waits for one answer (a command, a read, a clock synchronization) ends with the same error when more than 16 answers to earlier requests are queued ahead of it.
  - iec104ctl `client file get`: a file the station announces with more than 255 sections ends with a protocol error.
  - iec104ctl `server start`: a state handler or accept filter that does not return normally is logged with its stack and closes only its own connection; the station goes on serving the others.
  - iec104ctl with IEC 62351-5 (`--secure`): a remote certificate whose key usage allows neither key agreement nor digital signature is refused, and an authority certificate outside its validity period authorizes nothing. At the simulated station (`server start`), a completed Station Association ends the session keys of the earlier association; the controlling station then changes the session keys. Session keys that reach their use count or lifetime are no longer accepted from the message that finds them so, not only at the next timer tick.
  - iec103ctl `server start`: the values of a disturbance channel are queued all or none. When the class 1 queue has no room for them the channel ends with TOO 37, which the controlling system acknowledges negatively and may request again; a record with a channel of more than 25575 values (more than the default class 1 queue of 1024 ASDUs holds) is refused with TOO 34 at its selection and no transfer starts. NOE announces the longest channel of the record.
  - iec103ctl `client disturbance get`: keeps to the selected fault number (FAN): an end of transmission of another FAN is skipped, a channel offered for another FAN ends the transfer with what was received so far, and a channel whose values carry another FAN is acknowledged negatively.
  - iec102ctl `client totals` and `client events` keep only the data of the requested record address.
  - iec101ctl, iec102ctl and iec103ctl: a link exchange whose frame waits for room in a full send queue ends at its deadline or on Ctrl-C.

---

## v0.10.0

**Date:** 2026-11-14
**Previous release:** v0.9.3

Moves the four tools to go-iec60870-5 v0.6.0 (was v0.5.2). The commands' options and output are unchanged; the library's new bounds apply with their defaults.

### Changed

- Built on go-iec60870-5 v0.6.0, with the fixes of v0.5.3 (go-iec60870-5 RELEASE.md, v0.5.3 and v0.6.0). For the tools this means:
  - iec101ctl, iec102ctl and iec103ctl: one request collects at most 65536 ASDUs (iec102ctl: 131072). A request that would collect more ends with the library's result-limit error and what arrived so far.
  - iec104ctl `station`: a session has at most 16 file transfers open at once, in addition to the station's own upload limits; one more is refused with "unknown information object address" until a transfer of that session ends.
  - A library callback that does not return normally is logged with its stack and stops only the station that called it.
  - iec101ctl: a request after the client closed reports that the client is closed, where it reported that the link was not initialized.
- Built on go-serial v0.4.0 (was v0.3.0), for the serial lines of iec101ctl, iec102ctl and iec103ctl (go-serial RELEASE.md, v0.4.0).

### Fixed

- iec103ctl `station` over TCP: a line ends when its station ends, as in iec101ctl and iec102ctl. With go-iec60870-5 v0.6.0 the station ends itself when a write fails, without a read failing first. The line then stayed open, and every later controlling system was refused as busy. `TestHostileMaster` failed 24 of 45 runs that way and now passes.

---

## v0.9.3

**Date:** 2026-11-13
**Previous release:** v0.9.2

Fixes `iec101ctl client monitor` ending on a line that closed during the link start-up,
and moves the interop suites to the images of iec60870-5-interop v0.5.0.

### Fixed

- **iec101ctl `client monitor`:** a line that closed while its link was being
  initialized ended the monitor with "connection error: the line closed before the link
  came up" instead of being opened again. This happened to a line opened again after a
  loss, and to the first line when the station closed it right after the link came up,
  before the dial returned. Now only a first line whose link never came up ends the
  monitor; every other lost line is opened again unless `--no-reconnect`. iec102ctl has
  no monitor, iec103ctl already opened such a line again, and iec104ctl's reconnection is
  the library's.
- **iec101ctl, every client command:** a TCP line that closed during the link start-up
  failed the command at once with "the line closed before the link came up", where
  iec102ctl and iec103ctl open such a line again: a terminal server (or an interop
  image) that still serves the previous connection closes the next one. It is now opened
  again, with a growing pause, until `--timeout`; nothing but the link start-up was sent
  on it. A serial line is not.

### Changed

- **Interop:** the suites of all four tools run against the images of
  iec60870-5-interop v0.5.0 (contract 2.3 unchanged), pinned by the digests of that
  release, with its case and known-bug tables. The iec103ctl and iec102ctl suites read
  the role a known-bug entry may now carry (`<adapter>@client`, `<adapter>@server`).

---

## v0.9.2

**Date:** 2026-11-12
**Previous release:** v0.9.1

Built on go-iec60870-5 v0.5.2 (was v0.5.1) and go-serial v0.3.0 (was v0.2.1). No
command, flag or output changes.

### Changed

- **go-serial v0.3.0** (iec101ctl, iec102ctl, iec103ctl): serial parameters are checked
  before they are narrowed, so an out-of-range data-bits or stop-bits value is an error
  instead of being truncated to a valid one, and an RS-485 RTS delay above 2^32-1 ms is
  refused instead of wrapping around. go-serial now requires Go 1.26, as these tools
  already did.
- **go-iec60870-5 v0.5.2**: its interop suites moved to the images of
  iec60870-5-interop v0.5.0; the library's code is unchanged.

---

## v0.9.1

**Date:** 2026-11-12
**Previous release:** v0.9.0

Built on go-iec60870-5 v0.5.1 (was v0.4.1). Fixes a silent loss of data on noisy
serial lines in iec101ctl, iec102ctl and iec103ctl.

### Fixed

- **iec101ctl, iec102ctl, iec103ctl:** on a line that corrupts octets, a command could
  succeed with part of its answer missing (seen: `client interrogate` of iec103ctl
  returning 7 of 8 messages). After rejecting a corrupted frame, the FT 1.2 decoder of
  go-iec60870-5 took an E5 octet inside that frame for a valid single-character answer,
  so the station's next message was skipped. go-iec60870-5 v0.5.1 never resynchronizes
  on the octets of a rejected frame. The chaos tests now check that a command which
  succeeds returns everything a clean line returns.
- **`--debug`:** no frame is traced after a command ended (go-iec60870-5 v0.5.0: a link
  station's `Close` waits for its writer).
- **iec103ctl:** a line that closes during the link start-up is reported by the library
  (go-iec60870-5 v0.5.0: `WaitUp` returns the transport error); iec103ctl's own watch
  of the line is gone. Errors and exit codes are unchanged (`connection_lost`, exit 4),
  and a TCP line closed during the start-up is still opened again until `--timeout`.

No command, flag, output format or exit code changed.

---

## v0.9.0

**Date:** 2026-11-12
**Previous release:** none from this repository (iec104ctl v0.8.0 in otfabric/iec104ctl)

The first release of iec60870ctl: four command-line tools for the IEC 60870-5 companion
standards on [go-iec60870-5](https://github.com/otfabric/go-iec60870-5) v0.4.1, with one
set of conventions, output formats and exit codes ([docs/exitcodes.md](docs/exitcodes.md)).
Install any of them with `curl -fsSL https://get.otfabric.io/<tool> | sh`.

### iec101ctl (new)

IEC 60870-5-101 over FT 1.2, on a serial port (`serial:///dev/ttyUSB0`, 9600 8E1 by
default) or a TCP terminal server, unbalanced and balanced, with configurable link
address, cause, common address and object address sizes.

- `client interrogate`, `counters`, `read`, `command` (direct, select, select-before-operate,
  cancel), `clock`, `reset`, `delay` (C_CD_NA_1), `monitor`, `ping` (test command, request
  status of link, link test), `scan` (broadcast, common addresses, link addresses), `types`.
- `server start` (a simulated station from a YAML/JSON model, one connection at a time) and
  `server generate-config`; `discover` (request status of link on TCP hosts and ports).
- Interop with lib60870-C, cB-Sigma/iec60870 and digitalpetri/iec60870 (iec60870-5-interop
  v0.4.0), both roles, both link modes, default and four non-default field-size sets: 399
  cases pass; the digitalpetri balanced station is a known upstream bug.

### iec102ctl (new)

IEC 60870-5-102, the transmission of integrated totals (electricity meters), with the
Spanish REE profile.

- `client info`, `time`, `totals` (every read command C_CI_NA_2 to C_CI_NU_2: windows,
  periods, oldest), `events`, `request` (one raw ASDU), `types`, and `client ree login`,
  `set-time`, `tariffs`, `contracted-powers`, `special-days`; `--access-key` (or
  `--access-key-file`, the environment) wraps a command in an REE session. The key is never
  printed.
- `server start` (a simulated meter) and `server generate-config`.
- Interop: gisce/iec870ree against the simulated meter, 19 checks pass. No open reference
  meter exists: the client is tested against go-iec60870-5's own server only.

### iec103ctl (new)

IEC 60870-5-103, the informative interface of protection equipment.

- `client interrogate`, `command`, `clock` (also broadcast), `identify`, `monitor`,
  `scan`; `client generic headings|read|entry|group|directory|interrogate|write`
  (write with execute or abort, sent once); `client disturbance list|get` (channels as
  JSON, `--csv`).
- `server start` (a simulated device; serves the interop fixtures as they are) and
  `server generate-config`.
- Interop with vitusdong/iec103-dotnet, ARIEC103, Open103 and go-iecp5 cs103: 73 checks
  pass; skipped are known upstream bugs and features an image does not declare.

### iec104ctl (moved here from otfabric/iec104ctl)

Commands, flags, output and exit codes are those of iec104ctl v0.8.0; the move and every
difference are recorded in [docs/MIGRATION_AUDIT.md](docs/MIGRATION_AUDIT.md).

- Built on go-iec60870-5 v0.4.1 instead of go-iec104 v0.3.0. Install it from here:
  `go install github.com/otfabric/iec60870ctl/cmd/iec104ctl@latest`; get.otfabric.io
  now installs it from this repository.
- **Changed**, from go-iec60870-5's checks against IEC 60870-5-104 ed. 1 and
  IEC TS 60870-5-7:2025:
  - `--w` may exceed `--k` (both 1..32767); it used to be invalid input (exit 3).
  - `client counters --freeze freeze|freeze-reset|reset` returns at the station's
    confirmation and lists no values (`count` 0); read the frozen values with a following
    `client counters`.
  - The simulated station refuses the 8-octet MACs (`--mal 3`/`7`) on TCP/IP: a client
    asking for one gets `security_failed`, exit 4.
  - The simulated station answers a deactivation of clock synchronization, reset process
    or test command with a negative mirror, cause 45.
  - The test sequence counter of `client ping --method command` starts at 0.
  - `version` names go-iec60870-5.
- **Fixed:** `client monitor --count N` against a station that floods right after
  STARTDT could reach N while the connection was still being set up and then report its
  own stop as "interrupted" (exit 130) instead of ending normally; the race was in
  iec104ctl v0.8.0 too.
- Interop with lib60870-C, OpenMUC j60870, wendy512/iec104, cB-Sigma/iec60870 and
  pascaldekloe/part5 (iec60870-5-interop v0.4.0), both roles: 42 checks pass; the read
  against j60870 1.8.0 is a known upstream bug.

### All tools

- Assets: `<tool>-0.9.0-<os>-<arch>.tar.gz` (`.zip` for Windows) for Linux amd64, arm64,
  armv7, macOS amd64, arm64 and Windows amd64, arm64, with `<tool>-0.9.0-checksums.txt`.
- Tests: unit coverage 95.7 % (every package at least 90 %), 45 fuzz targets, chaos and
  hostile-peer tests, e2e against go-iec60870-5's servers and clients, interop against the
  images of iec60870-5-interop v0.4.0 pinned by digest. SAST (gosec, opengrep): no open
  findings.
- `--link-timeout` of the serial tools must exceed the slowest round trip of the line:
  FT 1.2 cannot tell a late answer from the next one ([LIMITATIONS.md](LIMITATIONS.md)).

---
