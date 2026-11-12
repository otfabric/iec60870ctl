# Testing

Every tier runs locally with one make target and in CI. Nothing needs hardware: each tool
brings its own simulated station, go-iec60870-5 brings its servers and clients, and the
independent implementations come as published container images.

| Tier | Make target | CI | What it shows |
|------|-------------|----|---------------|
| Unit and command tests | `make test` (race detector) | CI, go 1.26 and 1.27 | every command through `Run`: stdout, stderr and exit code, error paths included, against the tool's simulated station on a loopback port |
| Coverage | `make coverage-pkg-check` | CI (`tools` job) and Codecov | at least 90 % of the statements, in total and in every package, measured across the module (`-coverpkg=./...`) |
| Fuzz | `make fuzz FUZZTIME=10s` | CI (go-ci, 10 s per target) | every parser of user input: URLs, address lists and ranges, values, types, times, station models, security names; targets are discovered, never listed |
| Hostile peers and chaos | `make chaos CHAOS_SEEDS=20` | CI (`tools` job) | `TestHostile…`: a peer that floods, stalls, sends garbage or malformed frames, never ends a procedure, closes mid-exchange. `TestChaos…`: commands through a link that drops, corrupts, splits, duplicates, delays and cuts traffic, by seed; every run ends within its time-out with a documented exit code and leaves no goroutines |
| e2e | `make e2e` | CI (`tools` job) | the built binaries: `version`, usage errors, and each tool's client against go-iec60870-5's own server of the standard, and its `server start` against go-iec60870-5's client |
| Interop | `make interop` (Docker) | Interop workflow, one job per tool | the built binaries against the independent implementations of [otfabric/iec60870-5-interop](https://github.com/otfabric/iec60870-5-interop) v0.4.0, pinned by digest, in both roles where the image has the role |

`INTEROP_PKG=./interop/iec104ctl/...` runs one tool's interop suite. `IEC10x_REQUIRE_INTEROP=1`
(set in CI) turns a missing image or Docker into a failure instead of a skip; the image of
each reference can be overridden with `IEC10x_INTEROP_<NAME>_IMAGE`.

## Interop references (iec60870-5-interop v0.4.0)

| Tool | Reference servers (our client) | Reference clients (our server) |
|------|-------------------------------|--------------------------------|
| iec104ctl | lib60870-C, OpenMUC j60870, wendy512/iec104, cB-Sigma/iec60870, pascaldekloe/part5 | the same five |
| iec101ctl | lib60870-C, cB-Sigma/iec60870, digitalpetri/iec60870 | the same three |
| iec102ctl | none exists (no open controlled station) | gisce/iec870ree (REE profile master) |
| iec103ctl | vitusdong/iec103-dotnet | vitusdong, ARIEC103, Open103, go-iecp5 cs103 |

Known upstream bugs are listed by the harness (`known-bugs*.tsv`) and skipped or reported
as known, never counted as passes.

Last full local run (2026-11-12, Docker on macOS arm64):

| Tool | Pass | Skipped | Fail | Skips |
|------|------|---------|------|-------|
| iec101ctl | 399 (22 client scenarios, both link modes, default and four non-default field-size sets; 369 reference-client cases) | 5 | 0 | digitalpetri balanced station: known upstream bug, confirmed to reproduce before skipping |
| iec102ctl | 18 (gisce against the simulated meter) | 0 | 0 | |
| iec103ctl | 59 | 36 | 0 | known upstream bugs of vitusdong, ARIEC103 and Open103; features an image does not declare |
| iec104ctl | 35 client subtests against 5 servers, 5 reference clients against the server | 1 | 0 | j60870 1.8.0 cannot decode C_RD_NA_1: known upstream bug |

## Writing tests

- Command tests run the command tree through `Run(ctx, args, stdout, stderr)` and assert
  on all three outputs; use the tool's `stationtest` for a station.
- A test that needs time uses an `eventually` helper or a channel, not a fixed sleep.
- A new parser of user input gets a `Fuzz` target with seeds; a crasher found by `make
  fuzz` is kept in `testdata/fuzz/`.
- A hostile or chaos test is deterministic for its seed and bounded in time.
