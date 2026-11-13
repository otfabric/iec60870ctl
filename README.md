# iec60870ctl — Command-Line Tools for IEC 60870-5-101, -102, -103 and -104

[![Go Version](https://img.shields.io/badge/Go-1.26+-00ADD8?style=flat&logo=go)](https://go.dev/)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)
[![CI Status](https://github.com/otfabric/iec60870ctl/actions/workflows/ci.yml/badge.svg)](https://github.com/otfabric/iec60870ctl/actions/workflows/ci.yml)
[![Interop](https://github.com/otfabric/iec60870ctl/actions/workflows/interop.yml/badge.svg)](https://github.com/otfabric/iec60870ctl/actions/workflows/interop.yml)
[![Code Coverage](https://codecov.io/gh/otfabric/iec60870ctl/graph/badge.svg)](https://codecov.io/gh/otfabric/iec60870ctl)
[![Latest Release](https://img.shields.io/github/v/release/otfabric/iec60870ctl?label=release)](https://github.com/otfabric/iec60870ctl/releases)

**Four command-line tools for the IEC 60870-5 telecontrol family, one per companion standard, in both roles: as the controlling station (client) and as a simulated controlled station (server).** They are built on [go-iec60870-5](https://github.com/otfabric/go-iec60870-5) and share one set of conventions: the same command structure, `--format text|json|table`, results on stdout and diagnostics on stderr, and the same [exit codes](docs/exitcodes.md).

| Tool | Standard | Transport | What it does | Guide |
|------|----------|-----------|--------------|-------|
| **iec101ctl** | IEC 60870-5-101 (telecontrol, serial) | FT 1.2 on a serial port, or over TCP to a terminal server | Interrogate, monitor, command, synchronize, unbalanced and balanced; simulate a station | [docs/iec101ctl/usage.md](docs/iec101ctl/usage.md) |
| **iec102ctl** | IEC 60870-5-102 (integrated totals) | FT 1.2 on a serial port, or over TCP | Read the integrated totals, events and clock of a meter, the REE profile; simulate a meter | [docs/iec102ctl/usage.md](docs/iec102ctl/usage.md) |
| **iec103ctl** | IEC 60870-5-103 (protection equipment) | FT 1.2 on a serial port, or over TCP | General interrogation, commands, identification, generic services, disturbance data; simulate a device | [docs/iec103ctl/usage.md](docs/iec103ctl/usage.md) |
| **iec104ctl** | IEC 60870-5-104 (telecontrol, TCP/IP) | TCP, TLS (IEC 62351-3), IEC 62351-5 | Interrogate, monitor, command, transfer files, discover; simulate an RTU | [docs/iec104ctl/usage.md](docs/iec104ctl/usage.md) |

iec104ctl was a repository of its own ([otfabric/iec104ctl](https://github.com/otfabric/iec104ctl), on go-iec104); it moved here onto go-iec60870-5 with its commands, flags, output and exit codes. What changed is listed in [docs/MIGRATION_AUDIT.md](docs/MIGRATION_AUDIT.md).

> **Responsible use:** these tools send real telecontrol requests. Only use them on stations, lines and networks you own or are authorized to test. Commands operate switchgear and change set points, clock synchronization and resets change the station itself, and on a serial line a second controlling station disturbs the first. Know what an address controls before you send to it.

## Install

```bash
curl -fsSL https://get.otfabric.io/iec101ctl | sh
curl -fsSL https://get.otfabric.io/iec102ctl | sh
curl -fsSL https://get.otfabric.io/iec103ctl | sh
curl -fsSL https://get.otfabric.io/iec104ctl | sh
```

```powershell
irm https://get.otfabric.io/iec101ctl.ps1 | iex
irm https://get.otfabric.io/iec102ctl.ps1 | iex
irm https://get.otfabric.io/iec103ctl.ps1 | iex
irm https://get.otfabric.io/iec104ctl.ps1 | iex
```

Linux (amd64, arm64, armv7), macOS (Intel, Apple silicon) and Windows (amd64, arm64). The installer verifies the release's SHA-256 checksum, installs per user without `sudo` (`~/.local/bin`, Windows `%LOCALAPPDATA%\OTFabric\bin`), and upgrades in place when run again. Pin a version with `OTF_VERSION=x.y.z`; for CI, containers and device images use `OTF_UNMANAGED_INSTALL=<dir>`; all options: [otfabric/install](https://github.com/otfabric/install#options). For offline machines, the archives and checksums are on [Releases](https://github.com/otfabric/iec60870ctl/releases).

From source (Go 1.26+):

```bash
go install github.com/otfabric/iec60870ctl/cmd/iec101ctl@latest
go install github.com/otfabric/iec60870ctl/cmd/iec102ctl@latest
go install github.com/otfabric/iec60870ctl/cmd/iec103ctl@latest
go install github.com/otfabric/iec60870ctl/cmd/iec104ctl@latest
```

### Shell completion

Flags with a fixed set of values complete to those values, type flags to the type names, file flags to files of their type, and addresses and numbers to no file names. Replace `iec104ctl` with the tool you installed:

```bash
# bash (needs the bash-completion package)
mkdir -p ~/.local/share/bash-completion/completions
iec104ctl completion bash > ~/.local/share/bash-completion/completions/iec104ctl

# zsh: a directory on your fpath, then restart the shell
mkdir -p ~/.zsh/completions && iec104ctl completion zsh > ~/.zsh/completions/_iec104ctl
# and once in ~/.zshrc:  fpath=(~/.zsh/completions $fpath); autoload -U compinit && compinit

# fish
mkdir -p ~/.config/fish/completions && iec104ctl completion fish > ~/.config/fish/completions/iec104ctl.fish
```

```powershell
# PowerShell: add this line to your profile ($PROFILE)
iec104ctl completion powershell | Out-String | Invoke-Expression
```

---

## Quick start

Each tool brings its own simulated station, so everything below runs without hardware. Start a station in one terminal, query it from another.

```bash
# IEC 104: the built-in demo RTU on port 2404, then a general interrogation
iec104ctl server start
iec104ctl client interrogate --ip 127.0.0.1

# IEC 101 over TCP (terminal-server style, port 2401), unbalanced
iec101ctl server start --port 2401
iec101ctl client interrogate --url tcp://127.0.0.1:2401

# IEC 101 on a serial port: 9600 baud, 8 data bits, even parity, 1 stop bit (FT 1.2)
iec101ctl client interrogate --url serial:///dev/ttyUSB0 --link-address 3 --ca 3

# IEC 102: the totals of a simulated meter
iec102ctl server start --port 2401
iec102ctl client totals --url tcp://127.0.0.1:2401 --oldest

# IEC 103: general interrogation of a simulated protection device
iec103ctl server start --port 2401
iec103ctl client interrogate --url tcp://127.0.0.1:2401
```

Add `--format json` for scripts, `--format table` for a grid, and the global `--debug` to trace every frame. Each tool's guide has the full command reference, the station model and the environment variables.

## Common conventions

- **Commands.** `client <operation>` talks to a station as the controlling station, `server start` simulates a station (`server generate-config` writes its model to start from), `version` and `completion` are the same everywhere.
- **Targets.** `--url tcp://host[:port]` or `--ip`/`--port`; the serial tools also take `serial:///dev/ttyUSB0` (`serial://COM3` on Windows) with `--baud`, `--data-bits`, `--parity` and `--stop-bits` (default 9600 8E1, as FT 1.2 requires). Over TCP the serial tools expect a terminal server that passes the line through unchanged.
- **Output.** `--format text` (default), `json` or `table`, or the environment variable `<TOOL>_FORMAT` (`IEC101CTL_FORMAT`, ...). Only the result goes to stdout; the JSON shapes are a stable contract, described per tool in `docs/<tool>/json-output.md`.
- **Exit codes.** Shared by the four tools: [docs/exitcodes.md](docs/exitcodes.md).
- **Safety.** Everything is validated before anything is sent; a command that changes the process is sent exactly once and never retried.

## Evidence

Every tool runs end to end against go-iec60870-5's own servers and clients (`make e2e`) and against the independent implementations published by [otfabric/iec60870-5-interop](https://github.com/otfabric/iec60870-5-interop) v0.5.0, pinned by digest (`make interop`, the Interop workflow). What ran, per tool, is in the "tested with" section of each guide and in [TESTING.md](TESTING.md). Interoperability with those implementations is not conformance to the standards; no tool claims more than its tests show.

## Documentation

| Document | Contents |
|----------|----------|
| [docs/exitcodes.md](docs/exitcodes.md) | Exit codes of the four tools |
| `docs/<tool>/usage.md` | Commands, flags, examples, FAQ |
| `docs/<tool>/json-output.md` | JSON output contract |
| `docs/<tool>/server-config.md` | The model of the simulated station |
| [ARCHITECTURE.md](ARCHITECTURE.md) | Layout of the repository and the shared packages |
| [TESTING.md](TESTING.md) | Unit, fuzz, chaos, e2e and interop tests |
| [LIMITATIONS.md](LIMITATIONS.md) | What the tools do not do |
| [docs/MIGRATION_AUDIT.md](docs/MIGRATION_AUDIT.md) | iec104ctl: what was kept and what changed in the move |
| [docs/RELEASING.md](docs/RELEASING.md) | Releases: one version for the four tools |
| [SECURITY.md](SECURITY.md) | Reporting vulnerabilities, safe use |
| [CONTRIBUTING.md](CONTRIBUTING.md) | Development setup |
| [RELEASE.md](RELEASE.md) | Release notes |

## Development

```bash
make check     # format, lint, vet, govulncheck, tests, coverage (90 % total and per package), docs
make e2e       # build the four tools and run them against go-iec60870-5's servers and clients
make interop   # run them against the iec60870-5-interop v0.5.0 images (Docker)
make fuzz      # every fuzz target for FUZZTIME (10s)
make chaos     # chaos and hostile-peer tests, CHAOS_SEEDS seeds
make build-nocheck   # bin/iec101ctl … bin/iec104ctl
```

## Contributing

Bug reports, station-compatibility notes and pull requests are welcome: please [open an issue](https://github.com/otfabric/iec60870ctl/issues) first for larger changes, and run `make check`. See [CONTRIBUTING.md](CONTRIBUTING.md). Report security vulnerabilities privately as described in [SECURITY.md](SECURITY.md).

## License

MIT. See [LICENSE](LICENSE).
