# AGENTS.md — iec60870ctl

## What this is

Four command-line tools for the IEC 60870-5 companion standards, built on [go-iec60870-5](https://github.com/otfabric/go-iec60870-5): `iec101ctl`, `iec102ctl`, `iec103ctl` and `iec104ctl` (moved here from otfabric/iec104ctl; see docs/MIGRATION_AUDIT.md). Each has controlling-station commands and a simulated station. Pure Go, one module, one binary per tool (`cmd/<tool>`). Profile: **cli**.

## Layout

| Path | What |
|---|---|
| `cmd/<tool>/main.go` | main package of each tool |
| `internal/<tool>/` | the tool: `cmd` (cobra tree), `config`, `types` (JSON output), the engine and the simulated station |
| `internal/{errs,format,runner,cli,buildinfo,envflag}` | shared by the four tools: exit codes, output formats, wiring |
| `internal/line` | FT 1.2 transport of the serial tools: serial port or TCP terminal server |
| `e2e/<tool>/` | `make e2e`: the built tool against go-iec60870-5's servers and clients |
| `interop/<tool>/` | `make interop`: the built tool against the iec60870-5-interop v0.4.0 images, pinned by digest |

## Organisation standards

This repository follows **otfabric/engineering policy 1.5**, profile **cli** (`.otfabric/policy.yaml`, mode `enforce`). Where this file and the organisation standards disagree, this file wins here — say so in your report.

## Where the truth lives

| Question | Source |
|---|---|
| Exit codes (script-facing API, shared) | docs/exitcodes.md |
| JSON output contract | docs/<tool>/json-output.md |
| Commands and usage | docs/<tool>/usage.md, README.md |
| How to contribute | CONTRIBUTING.md |
| Protocol behaviour | go-iec60870-5 |
| Release history | RELEASE.md (`## vX.Y.Z` … `---`; one version for the four tools), docs/RELEASING.md |

## Hard rules

1. Exit codes and JSON output are a public contract: a change is called out in RELEASE.md and documented in docs/exitcodes.md or docs/<tool>/json-output.md.
2. Never lower `COVERAGE_MIN` or the CI `coverage-min`, add lint suppressions without a reason, or skip or delete tests to make CI pass.
3. Never invent protocol semantics or claim interoperability or conformance beyond recorded evidence; protocol questions go to go-iec60870-5.
4. `github.com/otfabric/*` dependencies are pinned to released tags; no committed `replace` directives.
5. A command that changes the process validates everything before sending and is sent exactly once.
6. Never modify otfabric/iec104ctl, go-iec104 or iec104-interop from here.

## Commands

| When you change… | Run |
|---|---|
| anything | `make check` |
| the command line, exit codes or `main` | `make e2e` (builds the tools and runs them) |
| protocol behaviour of a tool | `make interop` (Docker) |
| a parser of user input | a `Fuzz` target, `make fuzz` |

## Definition of Done

`make check` green; tests for new commands, error paths included; exit codes and JSON documented; RELEASE.md section for user-visible changes; coverage at least 90 % per package.

## Git and releases

Branches and PRs. Do not push to `main`, tag or run the Release workflow unless the maintainer asks in the session.
