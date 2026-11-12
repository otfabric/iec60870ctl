# Migration audit: iec104ctl from otfabric/iec104ctl to iec60870ctl

iec104ctl moved from its own repository, [otfabric/iec104ctl](https://github.com/otfabric/iec104ctl)
at `96c596a` (v0.8.0 plus one dependency commit, on go-iec104 v0.3.0), into this repository,
onto [go-iec60870-5](https://github.com/otfabric/go-iec60870-5) v0.4.1. The old repository
is untouched. The first commit here is the old tree unchanged (`git archive`), so `git diff
<first commit> HEAD -- internal/iec104ctl` shows every change to the code.

The rule was: keep commands, flags, output formats, exit codes and behaviour, unless the
new library forces a change, and record every change. This document is that record.

## How it was checked

| Check | Result |
|-------|--------|
| Command tree: the docsync manifest (every command, alias, flag, shorthand, required and hidden flag) of the old and the new binary | identical: 23 commands, 480 flag entries |
| Exit codes (`internal/errs/exit.go`) and error codes (`internal/errs/codes.go`) | identical files |
| JSON output types (`internal/types` → `internal/iec104ctl/types`) | identical, apart from `ErrorInfo` and `ErrorEnvelope`, now aliases of the shared `errs.Info` and `errs.Envelope` with the same fields and JSON names |
| Flag descriptions, defaults and environment variables | unchanged (struct tags carried over as they were), apart from the help text named below |
| The old unit, command, hostile-peer and fuzz tests | all carried over; five assertions changed, each for a change listed below |
| Differential run: the old and the new binary, the same 30 invocations (every client command, the error paths, `server generate-config`, `types`), once against the old binary's simulated station and once against the new one's, outputs compared after removing timings | identical except the two client-side changes 1 and 2 below |
| New: e2e against go-iec60870-5's own `iec104/server` and `iec104/client` (`make e2e`), chaos through a lossy, corrupting link (`make chaos`), interop against the five IEC 104 images of iec60870-5-interop v0.4.0 (`make interop`) | green; see [TESTING.md](../TESTING.md) |

## What changed

### Behaviour, forced by go-iec60870-5

go-iec60870-5 was checked clause by clause against IEC 60870-5-104 edition 1 and
IEC TS 60870-5-7:2025 after it was copied from go-iec104; these are the fixes that reach
the command line. Changes 1-4 have tests here; 5-7 are tested in go-iec60870-5.

| # | Change | Before (go-iec104 v0.3.0) | Now (go-iec60870-5 v0.4.1) | Source |
|---|--------|---------------------------|----------------------------|--------|
| 1 | `--w` may exceed `--k` | `--k 4 --w 9` was invalid input, exit 3 | accepted: the standard bounds both to 1..32767 and only recommends `w <= 2k/3`. `--t2` not below `--t1`, and `--k`/`--w` above 32767, stay exit 3 | ed. 1 5.5 and clause 9 |
| 2 | `client counters --freeze freeze\|freeze-reset\|reset` | listed the integrated totals the station sent after its confirmation | a counter freeze command returns at the confirmation and lists no values (`count` 0); read the frozen values with a following `client counters` | IEC 60870-5-101:2003 7.4.8 (mode C), go-iec60870-5 v0.4.0 |
| 3 | The simulated station refuses the 8-octet MACs (`--mal 3`/`7`, HMAC-SHA-256/8, HMAC-SHA3-256/8) | accepted every MAC algorithm | over TCP/IP it accepts the 16-octet MACs only (and AES-256-GCM for data); a client asking for an 8-octet MAC gets no association: `security_failed`, exit 4. iec104ctl's client still offers them, for stations that accept them | IEC TS 60870-5-7:2025 9.3 |
| 4 | The simulated station's answer to a deactivation of clock synchronization, reset process and test command | a negative confirmation from the station's own check | a negative mirror with cause 45 (unknown cause), from the library's `Mux`, before the station sees it | ed. 1 7.1, 7.6, 7.9, 7.10, 8.8 |
| 5 | Test command C_TS_TA_1 (`client ping --method command`) | the test sequence counter of the first test was 1 | 0 | ed. 1 8.8 |
| 6 | An I frame without an ASDU | accepted | refused as a protocol error (closes the connection) | ed. 1 clause 5 |
| 7 | Request matching of the client | its own copy | the shared matching rules of the library's `telecontrol` package (same answers in every test and in the differential run) | go-iec60870-5 v0.4.0 |

The help text of `--mal` and the guide say that the 8-octet MACs are for serial links;
[usage.md](iec104ctl/usage.md) and [server-config.md](iec104ctl/server-config.md) describe
change 2.

### Output

| Change | Before | Now |
|--------|--------|-----|
| `version` names the library | `... go-iec104 0.3.0 ...`; JSON `"libraries":[{"name":"go-iec104",...}]` | `... go-iec60870-5 0.4.1 ...`; `"libraries":[{"name":"go-iec60870-5",...}]` (field names unchanged) |

Nothing else on stdout or stderr changed in the differential run.

### Repository, build and release

| Change | Before | Now |
|--------|--------|-----|
| Repository and module | `github.com/otfabric/iec104ctl`, main package at the root | `github.com/otfabric/iec60870ctl`, main package `cmd/iec104ctl` |
| `go install` | `go install github.com/otfabric/iec104ctl@latest` | `go install github.com/otfabric/iec60870ctl/cmd/iec104ctl@latest` |
| Library | go-iec104 v0.3.0 | go-iec60870-5 v0.4.1 (package map in go-iec60870-5 `docs/IEC104_PARITY.md`) |
| Release | `go-binary-release.yml`, tags `vX.Y.Z` | the repository's own Release workflow: `iec104ctl/vX.Y.Z`, or `vX.Y.Z` for a release of all four tools (not decided; [RELEASING.md](RELEASING.md)). Asset names unchanged: `iec104ctl-<version>-<os>-<arch>.tar.gz`/`.zip`, `iec104ctl-<version>-checksums.txt` |
| Version numbers | v0.8.0 was the last | the first release from here is not decided; it should not go below 0.9.0 if the per-tool scheme keeps the line of iec104ctl |
| Release notes | `RELEASE.md` | the old history in [iec104ctl/HISTORY.md](iec104ctl/HISTORY.md); new notes in [RELEASE.md](../RELEASE.md) |
| Coverage gate | 85 % total, 85 % per package | 90 % total and per package |
| Engineering policy | 1.4 | 1.5 |

### Documents

| Before | Now |
|--------|-----|
| `README.md` | [docs/iec104ctl/usage.md](iec104ctl/usage.md) (install section in the repository [README](../README.md); "tested with" now names the interop run of this repository) |
| `docs/json-output.md`, `docs/server-config.md` | `docs/iec104ctl/` |
| `docs/exitcodes.md` | [docs/exitcodes.md](exitcodes.md), shared by the four tools (iec104ctl's codes unchanged) |
| `RELEASE.md` | [docs/iec104ctl/HISTORY.md](iec104ctl/HISTORY.md) |

### Code, not visible to users

- Shared with the three new tools: `internal/errs`, `format`, `runner`, `cli`, `buildinfo`,
  and `envflag` (the environment and flag binding, from `internal/config`). The older
  spelling `IEC104CTL_OUTPUT_FORMAT` is registered by iec104ctl's `config` package, as
  before.
- iec104ctl's own packages moved under `internal/iec104ctl/` (`cmd`, `config`, `iec`,
  `station`, `stationtest`, `types`); import paths only.
- The panic message of a wiring error in `cli.MustMarkFlagRequired` names the command path
  instead of "iec104ctl:" (never reached by a user).
- Tests: the chaos test (`TestChaosClientThroughFaultyLink`), the e2e suite against the
  library's server and client, and the interop suite are new. The changed assertions: the
  two invalid-input cases `--k 4 --w 9` (client) and `--k 1 --w 5` (server) became
  `--k 40000`, `--t1 5 --t2 10`; the freeze-reset case asserts "0 objects"; the AES-256-GCM
  case reads with `--mal 8` and asserts that `--mal 3` is refused; the Legacy-spelling test
  moved with the format variable to iec104ctl's `config` package.

## What was kept

Commands, aliases, flags and their defaults, environment variables (including
`IEC104CTL_OUTPUT_FORMAT`), the text, table and JSON output of every command, the JSON
error envelope, exit codes 0, 2-7, 10 and 130 with every error code, the station model
format and the demo station, `server generate-config`, TLS and IEC 62351-5 flags, file
transfer, `discover`, the hostile-peer and fuzz tests, and shell completion.

## Found on the way

- go-iec60870-5 (as go-iec104 before it) applies `WithRequestTimeout` only to a context
  without a deadline. The process's context has none, so `--timeout` bounds every request;
  a caller that passes a context with a deadline gets that deadline instead. Documented
  library behaviour, not a defect; the chaos test runs commands as the process does.
