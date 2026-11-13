# Releases

Release notes of iec101ctl, iec102ctl, iec103ctl and iec104ctl, newest first. The four
tools share one version; each section is headed exactly `## vX.Y.Z` and ends with a line
`---` ([docs/RELEASING.md](docs/RELEASING.md)). The releases of iec104ctl from its own
repository are in [docs/iec104ctl/HISTORY.md](docs/iec104ctl/HISTORY.md).

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
