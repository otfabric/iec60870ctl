# iec104ctl — IEC 60870-5-104 Command-Line Tool: Interrogate, Monitor, Command, Transfer Files & Simulate an RTU

[![Go Version](https://img.shields.io/badge/Go-1.26+-00ADD8?style=flat&logo=go)](https://go.dev/)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)
[![CI Status](https://github.com/otfabric/iec104ctl/actions/workflows/ci.yml/badge.svg)](https://github.com/otfabric/iec104ctl/actions/workflows/ci.yml)
[![Code Coverage](https://codecov.io/gh/otfabric/iec104ctl/graph/badge.svg)](https://codecov.io/gh/otfabric/iec104ctl)
[![Latest Release](https://img.shields.io/github/v/release/otfabric/iec104ctl?label=release)](https://github.com/otfabric/iec104ctl/releases)

**iec104ctl is a command-line toolkit for IEC 60870-5-104 (IEC 104), the telecontrol protocol of power system SCADA, in both roles.** As a controlling station it interrogates an RTU, gateway or IED, watches the events it sends, issues commands and set points (direct or select-before-operate), synchronizes its clock and transfers files. As a controlled station it simulates an RTU from a model of points, commands and files, so that a SCADA front end or a protocol gateway has something to talk to. Both roles speak plain TCP, TLS (IEC 62351-3) and the application-layer security of IEC 62351-5.

It is built for commissioning and site acceptance tests, point-to-point checks, troubleshooting a link between a control centre and a substation, and hardware-free test setups. Output can be plain text, a table or JSON with stable exit codes, so it works in scripts and CI. It is written in Go on top of [go-iec104](https://github.com/otfabric/go-iec104).

**Quick links:** [Quick start](#quick-start) · [Features](#features) · [Install](#install) · [IEC 104 in two minutes](#iec-104-in-two-minutes) · [Connecting](#connecting-to-a-station) · [Commands](#common-commands) · [Simulated station](#simulate-a-station) · [FAQ](#faq)

> **Responsible use:** iec104ctl sends real telecontrol requests. Only use it on stations and networks you own or are authorized to test. `client command` operates switchgear and changes set points, `client clock` and `client reset` change the station itself, and `client file put` can deliver a configuration: know what an address controls before you send to it. A station often accepts few connections and sends its events on only one: connecting to a station that is in service can take its events away from the control centre.

## Install

```bash
curl -fsSL https://get.otfabric.io/iec104ctl | sh
```

```powershell
irm https://get.otfabric.io/iec104ctl.ps1 | iex
```

Linux (amd64, arm64, armv7), macOS (Intel, Apple silicon) and Windows (amd64, arm64). The installer verifies the release's SHA-256 checksum, installs per user without `sudo` (`~/.local/bin`, Windows `%LOCALAPPDATA%\OTFabric\bin`), and upgrades in place when run again. Pin a version with `OTF_VERSION=x.y.z`; for CI, containers and device images use `OTF_UNMANAGED_INSTALL=<dir>`; all options: [otfabric/install](https://github.com/otfabric/install#options). For offline machines, the archives and checksums are on [Releases](https://github.com/otfabric/iec104ctl/releases).

From source (Go 1.26+): `go install github.com/otfabric/iec104ctl@latest`

### Shell completion

Flags with a fixed set of values complete to those values, **`--type`** to the type names, file flags to files of their type, and addresses and numbers to no file names.

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

## Why iec104ctl?

- **See the whole process image** — `client interrogate` runs a general or group interrogation and lists every value with its quality and time tag; `client counters` reads the integrated totals.
- **Watch a station live** — `client monitor` stays connected and prints every spontaneous event and periodic value as it arrives, as text, a table or JSON Lines.
- **Operate, carefully** — `client command` sends single, double, step, set point and bitstring commands, direct or select-before-operate, with qualifier and optional time tag. Everything is validated first, and a command is sent exactly once.
- **Find what is there** — `discover` sweeps subnets for IEC 104 stations with a single test frame; `client scan` finds the stations (common addresses) behind one connection.
- **Test the link** — `client ping` measures the round trip of test frames or test commands; `--debug` traces every frame, decoded.
- **Files** — list the directory of a station, fetch disturbance records, deliver a file.
- **Simulate an RTU** — `server start` serves a station from a YAML model: values that change by themselves, commands with select-before-operate, events that are queued and delivered, redundant connections.
- **Secure** — IEC 62351-3 TLS with mutual authentication, and IEC 62351-5 application-layer security (`--secure`: certificates by authority or pinned, HMAC or AES-256-GCM, roles at the station), for client and server, separately or together.
- **Script-friendly** — `--format text|json|table`, a single JSON error object on failure, stable exit codes and `IEC104CTL_*` environment variables.
- **One-line install** — Linux, macOS and Windows (x86-64 and ARM), see [Install](#install); a single binary, no runtime to install.

---

## Quick start

Install iec104ctl (see [Install](#install)). No station at hand? Start the built-in demo station in one terminal:

```console
iec104ctl server start
```

and talk to it from another (replace `127.0.0.1` with your station):

```console
# What does this station hold? (general interrogation)
iec104ctl client interrogate --ip 127.0.0.1 --ca 1

# Watch events and measured values as they arrive, until Ctrl-C
iec104ctl client monitor --ip 127.0.0.1 --interrogate

# Open a breaker: single command, direct execute
iec104ctl client command --ip 127.0.0.1 --type single --ioa 5000 --value off

# Operate a disconnector with select-before-operate
iec104ctl client command --ip 127.0.0.1 --type double --ioa 5001 --value off --mode sbo

# Is the link alive, and how fast?
iec104ctl client ping --ip 127.0.0.1

# Which stations are on this subnet?
iec104ctl discover --subnets 192.168.1.0/24

# The same interrogation as JSON, for a script
iec104ctl client interrogate --ip 127.0.0.1 --format json
```

Addresses are plain numbers: the **common address** (`--ca`) names the station, the **information object address** (`--ioa`) the point. See [IEC 104 in two minutes](#iec-104-in-two-minutes).

---

## How it compares

IEC 104 test tools are mostly graphical: protocol test harnesses and simulators with a window per connection, many of them commercial and Windows-only. Protocol libraries are the other end of the range: they do everything, once you have written a program. iec104ctl sits in between.

| | iec104ctl | GUI test tools and simulators | Protocol libraries | Wireshark |
|---|:---:|:---:|:---:|:---:|
| Interrogate, monitor, command without writing code | ✅ | ✅ | — | — |
| Works over SSH, on a headless gateway or a jump host | ✅ | — | ✅ | partial (tshark) |
| Scriptable: JSON output, exit codes, environment variables | ✅ | rarely | ✅ (it is code) | partial |
| Simulate a controlled station from a text file | ✅ | ✅ | with code | — |
| Runs in CI, in a container, on a Raspberry Pi | ✅ | — | ✅ | ✅ |
| Decodes traffic between two other parties | — | some | — | ✅ |
| Single static binary, Linux / macOS / Windows | ✅ | — | — | — |

They combine well: use iec104ctl to drive and check a station from the command line, a GUI tool when you want to click through a point list, and Wireshark when you need to see what two other devices say to each other.

---

## Use cases

- **Commissioning and site acceptance tests** — Interrogate the station, compare with the signal list, operate each command with select-before-operate and watch the return information arrive.
- **Point-to-point tests** — Leave `client monitor` running while someone operates the primary equipment; every change is printed with its time tag.
- **Troubleshooting a link** — `client ping` and `--debug` show whether the station answers, how fast, and what exactly is on the wire.
- **Front-end and gateway development** — Point your SCADA front end or protocol gateway at `server start` and get a station that answers interrogations, executes commands and produces events.
- **Network inventory** — Sweep subnets for IEC 104 stations and list the common addresses behind each.
- **Automation and CI** — Use `--format json`, exit codes and environment variables in scripts and pipelines; run the simulated station as a test fixture.
- **Collecting records** — Fetch disturbance records and logs from a station's file directory.

---

## Table of Contents

- [Install](#install)
- [Why iec104ctl?](#why-iec104ctl)
- [Quick start](#quick-start)
- [How it compares](#how-it-compares)
- [Use cases](#use-cases)
- [Features](#features)
- [IEC 104 in two minutes](#iec-104-in-two-minutes)
- [Usage](#usage)
  - [General CLI usage](#general-cli-usage)
  - [Connecting to a station](#connecting-to-a-station)
  - [Common commands](#common-commands)
  - [Simulate a station](#simulate-a-station)
- [Environment variable configuration](#environment-variable-configuration)
- [FAQ](#faq)
- [Project structure](#project-structure)
- [Development](#development)
- [Version history](#version-history)
- [Contributing](#contributing)
- [License](#license)

---

## Features

| **Feature**      | **Description**                                                                                                           |
|------------------|---------------------------------------------------------------------------------------------------------------------------|
| Interrogate      | General (station) and group interrogation (`C_IC_NA_1`): every value with quality and time tag, optionally filtered by type and address. |
| Counters         | Counter interrogation (`C_CI_NA_1`), general or per counter group, with read, freeze, freeze-with-reset and reset.        |
| Read             | Read command (`C_RD_NA_1`) for a list or range of information object addresses.                                           |
| Monitor          | Stay connected and stream every value the station sends: spontaneous, periodic, background, return information. Reconnects, optional general interrogation, filters, `--count` and `--duration`. |
| Command          | Single, double, regulating step, set point (normalized, scaled, short floating point) and bitstring commands; direct execute, select, select-before-operate and cancel; qualifier of command; optional CP56Time2a time tag; optional wait for the activation termination. |
| Clock            | Clock synchronization (`C_CS_NA_1`) with the current or a given time, in the time zone of the station.                    |
| Reset            | Reset process command (`C_RP_NA_1`): general reset of the process, or reset of pending events.                            |
| Ping             | Link test frames (TESTFR act/con) or test commands (`C_TS_TA_1`) at an interval, with round-trip times.                   |
| Scan             | Find the common addresses behind one connection: one interrogation to the broadcast address, or a sweep of a range.       |
| Files            | File transfer of IEC 60870-5-101/104: list the directory, fetch a file, deliver a file, with checksums verified.          |
| Types            | List the type identifications iec104ctl knows, with direction and description.                                            |
| Discover         | Find IEC 104 stations on subnets with a non-intrusive probe (one test frame; data transfer is not started).               |
| Server           | A simulated controlled station from a YAML or JSON model: interrogations, read, counters, commands with select-before-operate, clock, reset, files and uploads, simulated values, queued events, redundant connections, TLS, IEC 62351-5 security with roles, allow-list. |
| Transports       | TCP, and TLS (IEC 62351-3) with server verification and client certificates, selected with **`--url`**. See [Connecting to a station](#connecting-to-a-station). |
| Security         | The application-layer security of IEC 62351-5 (A-Profile of IEC TS 60870-5-7) on every client command and on the simulated station: **`--secure`**, certificates trusted by authority or pinned, HMAC-SHA-256 or AES-256-GCM, role-based access control at the station. See [Connecting to a station](#connecting-to-a-station). |
| Protocol parameters | `k`, `w`, `t0`-`t3`, originator address, and the ASDU field sizes of IEC 60870-5-101 gateways (cause, common address, object address). |
| Type coverage    | Every monitoring type of IEC 60870-5-104 including the CP24Time2a and CP56Time2a variants, protection events, packed single points, parameters and file transfer, and the security types of IEC 62351-5. |
| `--format`       | Every command that prints a result accepts **`--format text\|json\|table`** (or **`IEC104CTL_FORMAT`**); streams write JSON Lines. |
| `--debug`        | Trace every frame sent and received, decoded, to stderr.                                                                  |
| Completion       | Shell completion for bash, zsh, fish and PowerShell; flags with a fixed set of values (**`--format`**, **`--type`**, **`--mode`**, **`--method`**, …) complete to those values. |

The following tree view gives the overview of available commands.

```
iec104ctl
├── client
│   ├── interrogate      General or group interrogation (C_IC_NA_1)
│   ├── counters         Counter interrogation (C_CI_NA_1)
│   ├── read             Read information objects (C_RD_NA_1)
│   ├── monitor          Print every value the station sends, until interrupted
│   ├── command          Process command: single, double, step, set point, bitstring (execute | select | sbo | cancel)
│   ├── clock            Clock synchronization (C_CS_NA_1)
│   ├── reset            Reset process command (C_RP_NA_1)
│   ├── ping             Test frames (TESTFR) or test commands (C_TS_TA_1) with round-trip time
│   ├── scan             Find the stations (common addresses) behind a connection
│   ├── file
│   │   ├── list         List the file directory
│   │   ├── get          Fetch a file
│   │   └── put          Deliver a file
│   └── types            List the ASDU type identifications
├── server
│   ├── start            Serve a simulated station from a model (or the built-in demo)
│   └── generate-config  Write an example station model
├── discover             Find IEC 60870-5-104 stations on subnets
├── completion           Generate shell completion script (bash|zsh|fish|powershell)
└── version              Print the version line (--json: one JSON line)
```

---

## IEC 104 in two minutes

IEC 60870-5-104 carries the telecontrol messages of IEC 60870-5-101 over TCP, by default on port **2404**. One side is the **controlling station** (the control centre, a SCADA front end: the client), the other the **controlled station** (an RTU, a substation gateway, an IED: the server).

| Term | Meaning | In iec104ctl |
|---|---|---|
| Common address (CA) | The address of a station. One connection can lead to several: a gateway answers for every station behind it. 65535 is the broadcast address | `--ca` |
| Information object address (IOA) | The address of one point within a station: a breaker position, a voltage, a command | `--ioa` |
| Type identification | What kind of object an ASDU carries. `M_…` types travel in monitor direction (station → centre), `C_…` in control direction, `F_…` are file transfer. `M_SP_NA_1` is a single point, `M_ME_NC_1` a floating point measured value, `C_SC_NA_1` a single command. A suffix such as `_TB_1` marks the variant with a time tag | `--type`, `client types` |
| Cause of transmission (COT) | Why an ASDU was sent: `spontaneous` (a change), `periodic`, `interrogated-station` (answer to a general interrogation), `activation` / `activation-con` / `activation-term` (a command and its answers) | shown with every value |
| Quality | Flags that qualify a value: `IV` invalid, `NT` not topical, `SB` substituted, `BL` blocked, `OV` overflow | shown with every value |
| General interrogation | The request that makes a station send its whole process image. Every station supports it | `client interrogate` |
| STARTDT / STOPDT | A connection carries data only after the controlling station has started data transfer. A station with several connections sends its events on one of them | done by every client command |
| Select before operate | A command in two steps: the station first confirms that the object is free and the command acceptable (select), then carries it out (execute) | `client command --mode sbo` |
| `k`, `w`, `t0`-`t3` | The window sizes and timers of the transport: how many frames may be unacknowledged, and when a silent link is given up | `--k`, `--w`, `--t0` … `--t3` |

There are no names on the wire: a station does not tell you that IOA 1000 is "Voltage L1". The signal list of the station is where addresses get their meaning.

---

## Usage

### General CLI Usage

```console
iec104ctl [command] [flags]

iec104ctl client interrogate
iec104ctl client counters
iec104ctl client read
iec104ctl client monitor
iec104ctl client command
iec104ctl client clock
iec104ctl client reset
iec104ctl client ping
iec104ctl client scan
iec104ctl client file list
iec104ctl client file get
iec104ctl client file put
iec104ctl client types

iec104ctl server start
iec104ctl server generate-config

iec104ctl discover
iec104ctl version [--json]
```

Every command has `--help`, with a description and examples.

**Global flag:** `--debug` — trace every frame sent and received to stderr, with the ASDU decoded and in hex, and print the chain of causes after an error.

**Output format (`--format`):** every command that prints a result accepts **`--format text`** (default), **`json`** or **`table`**, or set **`IEC104CTL_FORMAT`**. It controls **only** the result on **stdout**. Progress, connection state, summaries and the debug trace go to **stderr**, so stdout can be piped or parsed as it is. The JSON shapes are a stable contract: see [docs/json-output.md](docs/json-output.md).

**Streams:** `client monitor`, `client ping` and `server start` write one record as it happens: one line (**text**), one JSON object per line (**json**, JSON Lines) or one table row (**table**). They run until interrupted with Ctrl-C, which ends them normally.

**Fatal errors:** with `--format json` a command writes a **single JSON error object** to stdout on failure (fields `kind`, `code`, `message`); with **text** or **table** it writes a single **`error:`** line to stderr and leaves stdout empty. Exit codes are stable:

| Code | Meaning |
|------|---------|
| 0 | Success |
| 2 | Usage: unknown command or flag, missing required flag |
| 3 | Invalid input: nothing was sent |
| 4 | Transport: cannot connect, connection lost, TLS failure |
| 5 | Timeout: no answer |
| 6 | The station refused the request, or a protocol error |
| 7 | Partial success: a valid result that holds failures |
| 10 | Output or internal error |
| 130 | Interrupted before a result |

See [docs/exitcodes.md](docs/exitcodes.md) for the details and the `code` values.

### Connecting to a station

Every `iec104ctl client` command takes the station as a URL, or as an address and a port. The two styles are **mutually exclusive**, and one of them is required.

| Flags | Connects to |
|---|---|
| `--url tcp://host[:port]` | IEC 104 over TCP; port 2404 when omitted |
| `--url tls://host[:port]` | IEC 104 over TLS (IEC 62351-3); port 19998 when omitted |
| `--ip host [--port n]` | TCP on port 2404 or `n`; TLS when a `--tls…` flag is given |

`--ca` is the common address of the station (default 1). `--timeout` bounds connecting and each request in milliseconds (default 10000); a large station may need more for its general interrogation. `--originator` sets the originator address sent with every request.

**TLS**

| Flag | Meaning |
|---|---|
| `--tls` | Use TLS with `--ip` (a `tls://` URL implies it) |
| `--tls-ca` | PEM file with the CA certificates to verify the station with; the system roots by default |
| `--tls-cert`, `--tls-key` | PEM files with the client certificate and its key, for a station that authenticates its clients |
| `--tls-server-name` | The name to verify the station's certificate against, when it is not the host you connect to |
| `--tls-insecure` | Do not verify the station's certificate. For test setups only |

```console
iec104ctl client interrogate --url tls://rtu.example.net --tls-ca ca.pem --tls-cert scada.pem --tls-key scada.key
```

**Application-layer security (IEC 62351-5)**

`--secure` runs the A-Profile of IEC 62351-5, as IEC TS 60870-5-7 maps it onto IEC 104. After STARTDT the client and the station first agree on keys in a Station Association authenticated with their certificates, then on session keys; from then on every ASDU travels as Secure Data (`S_SD_NA_1`), authenticated with a truncated HMAC, or encrypted and authenticated with AES-256-GCM, and numbered against replay. The command starts only once the session is secure. It works over TCP and over TLS.

| Flag | Meaning |
|---|---|
| `--secure` | Run the A-Profile. Every other flag of this table is invalid without it, and `--secure` needs the next three |
| `--security-cert` | PEM file with the own station certificate: P-256, or X25519 signed by an authority |
| `--security-key` | PEM file with its private key (PKCS #8, or SEC 1 for P-256) |
| `--security-trust` | PEM file with one or more certificates of the stations to trust. A CA certificate (basic constraints CA:TRUE) is an **authority**: every station certificate it signed is accepted. Any other certificate is **pinned**: that station's self-signed certificate is accepted, by its fingerprint |
| `--association-id` | The AIM, this controlling station's half of the association ID, 1-65535 (default 1) |
| `--mal` | MAC algorithm, by code or name: `4` / `hmac-sha-256` (default), `3` / `hmac-sha-256/8`, `8` / `hmac-sha3-256`, `7` / `hmac-sha3-256/8` |
| `--dpa` | Data protection algorithm, by code or name: a MAC algorithm as for `--mal`, or `11` / `aes-256-gcm` to encrypt as well (default: `--mal`) |

Pinned self-signed certificates, one per station (only P-256 keys can sign their own certificate):

```console
openssl req -x509 -newkey ec -pkeyopt ec_paramgen_curve:P-256 -nodes -days 365 \
  -keyout station.key -out station.pem -subj "/CN=station 1" -addext basicConstraints=critical,CA:FALSE
openssl req -x509 -newkey ec -pkeyopt ec_paramgen_curve:P-256 -nodes -days 365 \
  -keyout scada.key -out scada.pem -subj "/CN=scada" -addext basicConstraints=critical,CA:FALSE

# Each side trusts the other's certificate
iec104ctl server start --secure --security-cert station.pem --security-key station.key --security-trust scada.pem
iec104ctl client interrogate --ip 127.0.0.1 --secure --security-cert scada.pem --security-key scada.key --security-trust station.pem
```

A plant authority that signs the station certificates, so that every side trusts the authority alone:

```console
openssl req -x509 -newkey ec -pkeyopt ec_paramgen_curve:P-256 -nodes -days 3650 \
  -keyout ca.key -out ca.pem -subj "/CN=Plant authority" \
  -addext basicConstraints=critical,CA:TRUE -addext keyUsage=critical,keyCertSign
printf 'basicConstraints=critical,CA:FALSE\n' > station.ext
for n in station scada; do
  openssl req -new -newkey ec -pkeyopt ec_paramgen_curve:P-256 -nodes -keyout $n.key -out $n.csr -subj "/CN=$n"
  openssl x509 -req -in $n.csr -CA ca.pem -CAkey ca.key -CAcreateserial -days 365 -out $n.pem -extfile station.ext
done

iec104ctl server start --secure --security-cert station.pem --security-key station.key --security-trust ca.pem --roles viewer
iec104ctl client interrogate --ip 127.0.0.1 --secure --security-cert scada.pem --security-key scada.key --security-trust ca.pem --dpa aes-256-gcm
```

A refused certificate, an algorithm the station does not accept, or no answer to the key management ends the command with exit code 4 and the error code `security_failed`, before anything else is sent. A request the station's roles do not permit is refused with cause 17 (`not-authorized`), exit code 6. A client without `--secure` gets no answer from a secure station (exit code 5). With `--debug` the security events (`STAS_PROC_SUCC`, `NODE_NOT_AUTR`, …) and the algorithms of the session are traced with the frames.

**Protocol parameters**

The parameters of IEC 60870-5-104 clause 9 default to the standard values. Both ends must agree on them; change them only to match a station that deviates.

| Flag | Parameter | Default |
|---|---|---|
| `--k` | Maximum number of unacknowledged I frames sent | 12 |
| `--w` | Acknowledge after at most this many received I frames | 8 |
| `--t0` | Connection establishment timeout, seconds | 30 |
| `--t1` | Timeout for the acknowledgement of a sent frame, seconds | 15 |
| `--t2` | Acknowledge after this many seconds without sending (must be below t1) | 10 |
| `--t3` | Send a test frame after this many seconds of inactivity | 20 |

A station behind an IEC 60870-5-101 gateway may use shorter address fields. `--cot-size` (1 or 2 octets), `--ca-size` (1 or 2) and `--ioa-size` (1 to 3) select them; the defaults are the IEC 104 values 2, 2 and 3. If every value looks scrambled, or the station closes the connection on the first request, these are the first thing to check.

**Time zone.** A time tag on the wire carries no zone. `--timezone` says which zone the station keeps its clock in: `utc` (default), `local`, or an IANA name such as `Europe/Brussels`. It applies to the time tags iec104ctl prints and to the time `client clock` sends. A time tag carries a two-digit year, read as 1970 to 2069, so `client clock --time` refuses a time outside those years in that zone (exit 3) instead of sending it.

### Common Commands

#### Interrogate a station

```console
# General interrogation of station 1
iec104ctl client interrogate --ip 192.168.1.10 --ca 1

# Interrogation group 3 only
iec104ctl client interrogate --ip 192.168.1.10 --group 3

# Show only single points and floating point values of addresses 100-1999
iec104ctl client interrogate --ip 192.168.1.10 --type M_SP_NA_1,M_ME_NC_1 --ioa 100-1999

# As a table, or as JSON
iec104ctl client interrogate --ip 192.168.1.10 --format table
iec104ctl client interrogate --ip 192.168.1.10 --format json
```

Example result:

```console
General interrogation of station 1 at tcp://192.168.1.10:2404: 10 objects in 1 ms
  M_SP_NA_1  ca=1   ioa=100      on                [good]   interrogated-station
  M_SP_NA_1  ca=1   ioa=101      off               [good]   interrogated-station
  M_DP_NA_1  ca=1   ioa=200      on                [good]   interrogated-station
  M_ST_NA_1  ca=1   ioa=300      3                 [good]   interrogated-station
  M_BO_NA_1  ca=1   ioa=400      15                [good]   interrogated-station
  M_ME_NC_1  ca=1   ioa=1000     230.4             [good]   interrogated-station
  M_ME_NC_1  ca=1   ioa=1001     50                [good]   interrogated-station
  M_ME_NC_1  ca=1   ioa=1010     230               [good]   interrogated-station
  M_ME_NB_1  ca=1   ioa=1002     1250              [good]   interrogated-station
  M_ME_NA_1  ca=1   ioa=1003     0.25 (raw=8192)   [good]   interrogated-station
```

Each line is one information object: type, common address, object address, value, quality, cause of transmission and, for types that carry one, the time tag after `@`. `--type` and `--ioa` filter what is **shown**; the station is always asked for everything.

Values are printed as the type holds them: `on` / `off` for single points, `off` / `on` / `intermediate` / `indeterminate` for double points, the number for everything else. A normalized value is shown as a fraction of -1 … 1 with the raw 16-bit number beside it; scaling to engineering units is up to the signal list.

Integrated totals are not part of an interrogation:

```console
# All counters, then counter group 1
iec104ctl client counters --ip 192.168.1.10
iec104ctl client counters --ip 192.168.1.10 --group 1

# Freeze the counters and report the frozen values
iec104ctl client counters --ip 192.168.1.10 --freeze freeze
```

`--freeze` is `read` (default), `freeze`, `freeze-reset` or `reset`. Anything but `read` changes the counters in the station.

#### Read single objects

```console
iec104ctl client read --ip 192.168.1.10 --ioa 1000
iec104ctl client read --ip 192.168.1.10 --ioa 100,1000-1003 --format table
```

One read command (`C_RD_NA_1`) is sent per address. An address the station refuses or does not answer is reported in its row and the others are still read; the exit code is then **7**. Not every station implements the read command: an interrogation always works.

#### Monitor events and measured values

```console
# Everything the station sends, starting with a general interrogation, until Ctrl-C
iec104ctl client monitor --ip 192.168.1.10 --interrogate

# Two objects of station 1, the next 10 changes
iec104ctl client monitor --ip 192.168.1.10 --ca 1 --ioa 100,200 --count 10

# One minute of floating point values as JSON Lines, into a file
iec104ctl client monitor --ip 192.168.1.10 --type M_ME_NC_1,M_ME_TF_1 --duration 60 --format json > values.jsonl
```

Example result (stdout, with the connection state and the summary on stderr):

```console
monitor: data transfer started
2026-10-10T09:15:17.676Z  M_DP_TB_1  ca=1   ioa=200      off               [good]   return-remote  @ 2026-10-10T09:15:17.666Z
2026-10-10T09:15:19.286Z  M_ME_TF_1  ca=1   ioa=1000     230.20906         [good]   spontaneous  @ 2026-10-10T09:15:19.285Z
2026-10-10T09:15:22.285Z  M_ME_TE_1  ca=1   ioa=1002     1275              [good]   spontaneous  @ 2026-10-10T09:15:22.285Z
--- tcp://192.168.1.10:2404: 3 values in 5.001s (M_DP_TB_1×1 M_ME_TE_1×1 M_ME_TF_1×1)
```

The first column is when the value **arrived**; the time after `@` is the **time tag** the station gave it. An event that was queued in the station shows the difference.

| Flag | Effect |
|---|---|
| `--interrogate` | Run a general interrogation after connecting, and after every reconnect, so the stream starts with all current values |
| `--ca` | Only this station; also the station `--interrogate` addresses (default: all stations, interrogation by broadcast) |
| `--type`, `--ioa` | Only these types and addresses. Note that an event has the time-tagged type (`M_SP_TB_1`), an interrogated value the plain one (`M_SP_NA_1`) |
| `--count`, `--duration` | Stop after that many values, or seconds |
| `--all` | Also show confirmations and terminations of commands, and file transfer |
| `--no-reconnect` | Exit with code 4 when the connection is lost, instead of reconnecting |

A station sends its events on **one** connection, usually the one that started data transfer last. While you monitor, the regular controlling station may not receive them. Stations with several redundancy groups keep a queue per group; ask before you monitor a station in service.

#### Send a command

```console
# Single command, direct execute
iec104ctl client command --ip 192.168.1.10 --type single --ioa 5000 --value on

# Double command with select-before-operate and a short pulse
iec104ctl client command --ip 192.168.1.10 --type double --ioa 5001 --value off --mode sbo --qualifier short

# Regulating step command: tap changer one step higher
iec104ctl client command --ip 192.168.1.10 --type step --ioa 5002 --value higher

# Set points
iec104ctl client command --ip 192.168.1.10 --type setpoint-float --ioa 5003 --value 231.5
iec104ctl client command --ip 192.168.1.10 --type setpoint-scaled --ioa 5004 --value 1200
iec104ctl client command --ip 192.168.1.10 --type setpoint-normalized --ioa 5005 --value 0.5

# With a time tag (C_SC_TA_1), waiting up to 5 s for the activation termination
iec104ctl client command --ip 192.168.1.10 --type single --ioa 5000 --value on --time-tag --wait-termination 5000
```

| `--type` | Type identification | `--value` |
|---|---|---|
| `single` | `C_SC_NA_1` | `on`, `off` |
| `double` | `C_DC_NA_1` | `on`, `off` |
| `step` | `C_RC_NA_1` | `higher`, `lower` |
| `setpoint-normalized` | `C_SE_NA_1` | -1 … < 1 |
| `setpoint-scaled` | `C_SE_NB_1` | -32768 … 32767 |
| `setpoint-float` | `C_SE_NC_1` | any number |
| `bitstring` | `C_BO_NA_1` | 0 … 4294967295, or `0x` hex |

`--type` also takes the type name (`C_SC_NA_1`, or `C_SC_TA_1` for the time-tagged variant).

| `--mode` | Procedure |
|---|---|
| `execute` | Direct execute (default) |
| `sbo` | Select before operate: select, and when the station has confirmed it, execute |
| `select` | Select only, to see whether the station accepts the selection |
| `cancel` | Select, then deactivate the selection |

A selection belongs to the connection that made it and ends with it, so `select` and `cancel` operate nothing: they show how the station handles the two steps.

`--qualifier` is the qualifier of command for switching commands (`none`, `short`, `long`, `persistent`) or the QL number 0-127 for set points.

Example result of a select-before-operate:

```console
Command C_DC_NA_1 to station 1 at tcp://192.168.1.10:2404, ioa=5001, value off (sbo)
  select       confirmed     0.3 ms
  execute      confirmed     0.2 ms
  termination  received      0.0 ms
```

> **This changes the process.** Everything is validated before anything is sent; an invalid invocation exits **3** and has sent nothing. A command is sent **exactly once**: it is never repeated, also not when its confirmation does not arrive (exit **5**). A command the station refuses — negative confirmation, or "unknown information object address" — exits **6**, and the message says which step was refused.

The effect of a command arrives as an event in monitor direction, usually with cause `return-remote`. Run `client monitor` in a second terminal to see it.

#### Clock synchronization and reset

```console
# Set the clock of station 1 to the current time
iec104ctl client clock --ip 192.168.1.10 --ca 1

# A station that keeps local time; and a given time for every station behind the link
iec104ctl client clock --ip 192.168.1.10 --timezone Europe/Brussels
iec104ctl client clock --ip 192.168.1.10 --ca 65535 --time 2026-01-31T12:00:00Z

# Clear the event buffer, or restart the process
iec104ctl client reset --ip 192.168.1.10 --qualifier events
iec104ctl client reset --ip 192.168.1.10 --qualifier process
```

Both change the station and are sent once. `client reset` has no default for `--qualifier`: a process reset restarts the station's application, and it may drop its connections.

#### Test the link

```console
# Four link tests, one per second
iec104ctl client ping --ip 192.168.1.10

# The application of station 1, every 500 ms until Ctrl-C
iec104ctl client ping --ip 192.168.1.10 --method command --ca 1 --interval 500 --count 0
```

```console
2026-10-10T09:15:23.307Z  seq=1  testfr  rtt=0.19 ms
2026-10-10T09:15:23.508Z  seq=2  testfr  rtt=0.52 ms
2026-10-10T09:15:23.710Z  seq=3  testfr  rtt=0.48 ms
--- tcp://192.168.1.10:2404 testfr: 3 tests, 0 failed, rtt min/avg/max = 0.19/0.40/0.52 ms
```

`--method testfr` (default) sends the link test frame, which the protocol machine of the station answers; it changes nothing. `--method command` sends the test command `C_TS_TA_1` through the station's application; not every station implements it. A failed test is reported and testing continues; the exit code is **7** when any test failed.

To see exactly what is exchanged, add `--debug` to any command:

```console
$ iec104ctl --debug client read --ip 192.168.1.10 --ioa 100
debug: 09:04:48.676 -- 192.168.1.10:2404 connected
debug: 09:04:48.676 -> 192.168.1.10:2404 U STARTDT act
debug: 09:04:48.677 <- 192.168.1.10:2404 U STARTDT con
debug: 09:04:48.677 -> 192.168.1.10:2404 I N(S)=0 N(R)=0 len=9  C_RD_NA_1 request ca=1 n=1  [66 01 05 00 01 00 64 00 00]
debug: 09:04:48.677 <- 192.168.1.10:2404 I N(S)=0 N(R)=1 len=10  M_SP_NA_1 request ca=1 n=1  [01 01 05 00 01 00 64 00 00 00]
debug: 09:04:48.677 -- 192.168.1.10:2404 disconnected
```

#### Find stations

On a network:

```console
iec104ctl discover --subnets 192.168.1.0/24
iec104ctl discover --subnets 10.0.1.0/24,10.0.2.10-50 --ports 2404,19998 --parallel 64 --format json
```

```console
Discovery: 1 IEC 60870-5-104 stations among 254 hosts in 1031 ms
  192.168.1.10    port 2404   iec104  0.5 ms
```

The probe opens a TCP connection and sends one link test frame. A station answers it in any state: data transfer is **not** started and no data is asked for. A port that accepts the connection without answering as a station is listed as `open`. More than 4096 hosts need `--force-large-scan`. A station in service may turn the probe away, or log it.

Behind one connection:

```console
# Which common addresses answer? (one interrogation to the broadcast address)
iec104ctl client scan --ip 192.168.1.10

# The same by trying addresses 1-50, for stations that ignore the broadcast address
iec104ctl client scan --ip 192.168.1.10 --method sweep --start 1 --end 50 --timeout 1000
```

```console
Scan of tcp://192.168.1.10:2404 (broadcast): 1 stations found, 1 common addresses probed, 1528 ms
  ca=1      found     10 objects  M_SP_NA_1×2 M_DP_NA_1×1 M_ST_NA_1×1 M_BO_NA_1×1 M_ME_NA_1×1 M_ME_NB_1×1 M_ME_NC_1×3
```

Both methods make every station found send its whole process image.

#### Transfer files

```console
# What does the station offer?
iec104ctl client file list --ip 192.168.1.10

# Fetch a file, or pipe it
iec104ctl client file get --ip 192.168.1.10 --ioa 30000 --name 1 --output record.bin
iec104ctl client file get --ip 192.168.1.10 --ioa 30001 --name 1 --output - | less

# Deliver a file, in sections of 4096 octets
iec104ctl client file put --ip 192.168.1.10 --ioa 40000 --name 1 --input settings.bin --section-size 4096
```

```console
Directory of station 1 at tcp://192.168.1.10:2404: 2 entries
  ioa=30000    name=1         4096 octets  2026-10-10T09:15:17.283Z  file
  ioa=30001    name=1           84 octets  2026-10-10T09:15:17.283Z  file
```

A file is identified by its information object address and its **name of file**, a number (`--name`, default 1). The checksum of every section and of the file is verified in both directions, and the result carries the SHA-256 of the content. A file holds at most 16 777 215 octets. `file put` changes the station and is sent once.

### Simulate a station

```console
# The built-in demo station on port 2404
iec104ctl server start

# Write its model, edit it, serve it
iec104ctl server generate-config --output station.yaml
iec104ctl server start --config station.yaml

# On another port, on localhost only, events as JSON Lines
iec104ctl server start --config station.yaml --listen 127.0.0.1 --port 12404 --format json

# With TLS, and only for two front ends
iec104ctl server start --config station.yaml --tls-cert server.pem --tls-key server.key --tls-client-ca ca.pem --allow 10.0.0.5,10.0.0.6

# With the application-layer security of IEC 62351-5; controlling stations may look, not operate
iec104ctl server start --config station.yaml --secure --security-cert station.pem --security-key station.key --security-trust ca.pem --roles viewer
```

The station is described by a model: its common address, its points with their initial values, its commands and its files.

```yaml
station:
  name: Demo bay
  common_address: 1
  cyclic_interval: 30s

points:
  - {ioa: 100, type: M_SP_NA_1, name: Circuit breaker closed, value: true, group: 1}
  - ioa: 1000
    type: M_ME_NC_1
    name: Voltage L1 (kV)
    value: 230.4
    simulate: {mode: sine, interval: 2s, min: 228, max: 232}
  - {ioa: 2000, type: M_IT_NA_1, name: Energy counter (kWh), value: 123456}

commands:
  - {ioa: 5000, type: C_SC_NA_1, name: Circuit breaker, target: 100}
  - {ioa: 5001, type: C_DC_NA_1, name: Disconnector, target: 200, select_required: true}

files:
  - {ioa: 30000, name: 1, size: 4096}

uploads:
  accept: true
```

What it does:

- **Answers** general, group and counter interrogations, read commands, clock synchronization, the test command and the reset process command.
- **Executes commands** on the points named as their `target`, direct or select-before-operate, and reports the change as an event with cause "return information caused by a remote command". `select_required` refuses an execute without a select.
- **Changes values by itself** where the model says `simulate` (`toggle`, `ramp`, `sine`, `random`, `count`), and reports the changes as spontaneous events with a time tag. `cyclic_interval` sends the measured values periodically.
- **Queues events** of state changes and commands while no controlling station is connected and delivers them to the next one, as an RTU does.
- **Serves any number of connections**, of which one receives the events (redundancy).
- **Serves files** and, with `uploads.accept`, takes them.

Every event is written to stdout as it happens:

```console
2026-10-10T09:15:17.284Z  listening              127.0.0.1:2404, station 1 (Demo bay), 11 points, 7 commands, 2 files
2026-10-10T09:15:17.624Z  connected              127.0.0.1:52488  data transfer stopped
2026-10-10T09:15:17.624Z  started                127.0.0.1:52488  data transfer started
2026-10-10T09:15:17.625Z  interrogation          127.0.0.1:52488  ca=1  station, 10 objects
2026-10-10T09:15:17.666Z  command                127.0.0.1:52491  ca=1 ioa=5001  C_DC_NA_1  off  selected
2026-10-10T09:15:17.666Z  command                127.0.0.1:52491  ca=1 ioa=5001  C_DC_NA_1  off  executed, point 200 set
2026-10-10T09:15:17.666Z  disconnected           127.0.0.1:52491  connection lost: closed by peer
```

With `--secure` the station also logs `secured` when a controlling station has associated, `security-refused` when an association is refused (an untrusted certificate, an algorithm it does not accept), and `refused` for a request its roles do not permit.

| Flag | Meaning |
|---|---|
| `--config` | The model file (YAML or JSON). Without it the built-in demo station is served |
| `--listen`, `--port` | Address and port to listen on. All interfaces and 2404 by default (19998 with TLS) |
| `--tls-cert`, `--tls-key` | Serve TLS with this certificate |
| `--tls-client-ca` | Require a client certificate of this CA (mutual TLS) |
| `--secure`, `--security-cert`, `--security-key`, `--security-trust` | The application-layer security of IEC 62351-5, as for the client: every controlling station must associate with a certificate `--security-trust` accepts, and every ASDU travels as Secure Data |
| `--association-id` | The AIS, the station's half of the association ID (default 1). Every controlling station (AIM) gets its own association |
| `--roles` | The roles every controlling station has: `viewer`, `operator`, `engineer`, `installer`, `secadm`, `secaud`, `rbacmnt`. A request no role permits is refused with cause 17. Without it there is no access control |
| `--allow` | Only accept clients from these addresses or CIDR prefixes |
| `--max-sessions` | Maximum number of simultaneous connections |
| `--duration` | Stop after this many seconds, for use in test pipelines |
| `--k`, `--w`, `--t0` … `--t3`, `--cot-size`, `--ca-size`, `--ioa-size`, `--timezone` | As for the client |

The full reference of the model is in [docs/server-config.md](docs/server-config.md). The server listens on all interfaces and accepts any client unless told otherwise: see [SECURITY.md](SECURITY.md).

---

## Environment Variable Configuration

Every flag can also be set through an environment variable, which is convenient for automation and for flags you would otherwise repeat on every command (`IEC104CTL_URL`, `IEC104CTL_CA`, `IEC104CTL_FORMAT`). A flag on the command line wins over the environment. A value that cannot be parsed (a port that is not a number) stops iec104ctl at startup. A flag that is **required** (`--ioa` of `client read`, `--type` and `--value` of `client command`, `--qualifier` of `client reset`) must still be given on the command line. The global flag `--debug` has no environment variable.

```console
export IEC104CTL_URL=tcp://192.168.1.10:2404
export IEC104CTL_CA=1
iec104ctl client interrogate
iec104ctl client read --ioa 1000
```

<details>
<summary>All <code>IEC104CTL_*</code> environment variables</summary>

| Variable | Type | Description | Flag | Default |
|----------|------|-------------|------|---------|
| **IEC104CTL_ALL** | bool | **`client monitor`**: also show confirmations and terminations of commands, and file transfer | `--all` | false |
| **IEC104CTL_ALLOW** | list | **`server start`**: only accept clients from these addresses or CIDR prefixes | `--allow` | any |
| **IEC104CTL_ASSOCIATION_ID** | uint16 | Association ID of IEC 62351-5, 1-65535: the AIM of a client, the AIS of **`server start`** (needs `--secure`) | `--association-id` | 1 |
| **IEC104CTL_CA** | uint16 | Common address of the station (**`client monitor`**: only show this station) | `--ca` | 1 (monitor: all) |
| **IEC104CTL_CA_SIZE** | uint8 | Octets of the common address, 1 or 2 (0 = 2) | `--ca-size` | 0 |
| **IEC104CTL_COMMAND_MODE** | string | **`client command`**: execute, select, sbo or cancel | `--mode` | execute |
| **IEC104CTL_COMMAND_TYPE** | string | **`client command`**: single, double, step, setpoint-normalized, setpoint-scaled, setpoint-float, bitstring, or a type name | `--type` | |
| **IEC104CTL_CONFIG** | string | **`server start`**: station model file (YAML or JSON) | `--config` | built-in demo |
| **IEC104CTL_CONFIG_FORMAT** | string | **`server generate-config`**: yaml or json | `--format` | yaml |
| **IEC104CTL_COT_SIZE** | uint8 | Octets of the cause of transmission, 1 or 2 (0 = 2) | `--cot-size` | 0 |
| **IEC104CTL_COUNT** | uint32 | **`client monitor`**: stop after this many values; **`client ping`**: number of tests (0 = until interrupted) | `--count` | 0 (ping: 4) |
| **IEC104CTL_DELAY** | uint32 | **`client scan`**: delay in ms between the requests of a sweep | `--delay` | 0 |
| **IEC104CTL_DIRECTION** | string | **`client types`**: monitor or control | `--direction` | |
| **IEC104CTL_DISCOVER_FORCE_LARGE** | bool | **`discover`**: allow more hosts than the built-in safety cap | `--force-large-scan` | false |
| **IEC104CTL_DPA** | string | Data protection algorithm of IEC 62351-5, by code or name (needs `--secure`) | `--dpa` | `--mal` |
| **IEC104CTL_DURATION** | uint32 | **`client monitor`**, **`server start`**: stop after this many seconds (0 = until interrupted) | `--duration` | 0 |
| **IEC104CTL_END** | uint16 | **`client scan`**: last common address of a sweep | `--end` | 255 |
| **IEC104CTL_FILE_NAME** | uint16 | **`client file get`**, **`put`**: name of file (NOF) | `--name` | 1 |
| **IEC104CTL_FORCE** | bool | **`server generate-config`**: overwrite an existing file | `--force` | false |
| **IEC104CTL_FREEZE** | string | **`client counters`**: read, freeze, freeze-reset or reset | `--freeze` | read |
| **IEC104CTL_GROUP** | uint8 | Interrogation group 1-16 (**`client interrogate`**) or counter group 1-4 (**`client counters`**); 0 = general | `--group` | 0 |
| **IEC104CTL_INPUT** | string | **`client file put`**: file whose content is sent | `--input` | |
| **IEC104CTL_INTERROGATE** | bool | **`client monitor`**: general interrogation after connecting and after every reconnect | `--interrogate` | false |
| **IEC104CTL_INTERVAL** | uint32 | **`client ping`**: interval between tests in ms | `--interval` | 1000 |
| **IEC104CTL_IOA** | uint32 / list | Information object address: of the command or file; a list with ranges for **`client read`**, **`interrogate`** and **`monitor`** | `--ioa` | |
| **IEC104CTL_IOA_SIZE** | uint8 | Octets of the information object address, 1 to 3 (0 = 3) | `--ioa-size` | 0 |
| **IEC104CTL_IP** | string | Station IP address or host name | `--ip` | |
| **IEC104CTL_K** | uint16 | k: maximum number of unacknowledged I frames sent (0 = 12) | `--k` | 0 |
| **IEC104CTL_LISTEN** | string | **`server start`**: address to listen on | `--listen` | all interfaces |
| **IEC104CTL_MAL** | string | MAC algorithm of IEC 62351-5, by code or name (needs `--secure`) | `--mal` | hmac-sha-256 |
| **IEC104CTL_MAX_SESSIONS** | uint16 | **`server start`**: maximum number of connections (0 = no limit) | `--max-sessions` | 0 |
| **IEC104CTL_NO_RECONNECT** | bool | **`client monitor`**: exit when the connection is lost | `--no-reconnect` | false |
| **IEC104CTL_ORIGINATOR** | uint8 | Originator address sent with every request | `--originator` | 0 |
| **IEC104CTL_OUTPUT** | string | **`client file get`**: file to write (`-` = stdout); **`server generate-config`**: file to write | `--output` | (generate-config: `-`) |
| **IEC104CTL_FORMAT** | string | (`IEC104CTL_OUTPUT_FORMAT` stays accepted) Output format: text, json or table | `--format` | text |
| **IEC104CTL_PARALLEL** | uint16 | **`discover`**: concurrent probes (1-256) | `--parallel` | 32 |
| **IEC104CTL_PING_METHOD** | string | **`client ping`**: testfr or command | `--method` | testfr |
| **IEC104CTL_PORT** | uint16 | Station TCP port; **`server start`**: port to listen on (0 = 2404, or 19998 with TLS) | `--port` | 2404 |
| **IEC104CTL_PORTS** | list | **`discover`**: TCP ports to probe | `--ports` | 2404 |
| **IEC104CTL_QUALIFIER** | string | **`client command`**: none, short, long, persistent, or a number | `--qualifier` | none |
| **IEC104CTL_RESET_QUALIFIER** | string | **`client reset`**: process or events (the flag is required) | `--qualifier` | |
| **IEC104CTL_ROLES** | list | **`server start`**: roles of every controlling station (needs `--secure`) | `--roles` | no access control |
| **IEC104CTL_SCAN_METHOD** | string | **`client scan`**: broadcast or sweep | `--method` | broadcast |
| **IEC104CTL_SECTION_SIZE** | uint32 | **`client file put`**: octets per section (0 = one section) | `--section-size` | 0 |
| **IEC104CTL_SECURE** | bool | Run the application-layer security of IEC 62351-5 | `--secure` | false |
| **IEC104CTL_SECURITY_CERT** | string | PEM file with the own station certificate of IEC 62351-5 | `--security-cert` | |
| **IEC104CTL_SECURITY_KEY** | string | PEM file with the private key of `--security-cert` | `--security-key` | |
| **IEC104CTL_SECURITY_TRUST** | string | PEM file with the trusted authorities and pinned station certificates | `--security-trust` | |
| **IEC104CTL_START** | uint16 | **`client scan`**: first common address of a sweep | `--start` | 1 |
| **IEC104CTL_SUBNETS** | list | **`discover`**: subnets, ranges or addresses to probe | `--subnets` | |
| **IEC104CTL_T0** | uint16 | t0: connection establishment timeout in seconds (0 = 30) | `--t0` | 0 |
| **IEC104CTL_T1** | uint16 | t1: acknowledgement timeout in seconds (0 = 15) | `--t1` | 0 |
| **IEC104CTL_T2** | uint16 | t2: acknowledge after this many seconds without sending (0 = 10) | `--t2` | 0 |
| **IEC104CTL_T3** | uint16 | t3: test frame after this many seconds of inactivity (0 = 20) | `--t3` | 0 |
| **IEC104CTL_TERMINATION** | uint32 | **`client command`**: wait this many ms for the activation termination (0 = do not wait) | `--wait-termination` | 0 |
| **IEC104CTL_TIME** | string | **`client clock`**: now, or an RFC 3339 time | `--time` | now |
| **IEC104CTL_TIMEOUT** | uint32 | Timeout in ms for connecting and for each request (0 = 10000); **`discover`**: per probe (0 = 1000) | `--timeout` | 0 |
| **IEC104CTL_TIMEZONE** | string | Time zone of the time tags on the wire: utc, local or an IANA name | `--timezone` | utc |
| **IEC104CTL_TIME_TAG** | bool | **`client command`**: send the CP56Time2a variant of the type | `--time-tag` | false |
| **IEC104CTL_TLS** | bool | Use TLS with `--ip` | `--tls` | false |
| **IEC104CTL_TLS_CA** | string | PEM file with the CA certificates to verify the station with | `--tls-ca` | system roots |
| **IEC104CTL_TLS_CERT** | string | PEM file with the own certificate (client: authentication; server: the server certificate) | `--tls-cert` | |
| **IEC104CTL_TLS_CLIENT_CA** | string | **`server start`**: CA whose certificates clients must present | `--tls-client-ca` | |
| **IEC104CTL_TLS_INSECURE** | bool | Do not verify the station certificate (testing only) | `--tls-insecure` | false |
| **IEC104CTL_TLS_KEY** | string | PEM file with the private key of `--tls-cert` | `--tls-key` | |
| **IEC104CTL_TLS_SERVER_NAME** | string | Name to verify the station certificate against | `--tls-server-name` | the host |
| **IEC104CTL_TYPES** | string | **`client interrogate`**, **`monitor`**: only show these types | `--type` | |
| **IEC104CTL_URL** | string | Station URL: `tcp://host[:port]` or `tls://host[:port]` | `--url` | |
| **IEC104CTL_VALUE** | string | **`client command`**: the value | `--value` | |
| **IEC104CTL_W** | uint16 | w: acknowledge after at most this many received I frames (0 = 8) | `--w` | 0 |

</details>

---

## FAQ

### What is iec104ctl?

iec104ctl is a command-line tool for IEC 60870-5-104, the telecontrol protocol used between control centres and substations. As a client it interrogates and monitors a station, sends commands and set points, synchronizes the clock and transfers files; as a server it simulates an RTU from a model file. It also finds stations on a network.

### How do I see all the values of an RTU from the command line?

Run a general interrogation: `iec104ctl client interrogate --ip 192.168.1.10 --ca 1`. Add `--format table` or `--format json` for other output. Counters need `iec104ctl client counters`. See [Interrogate a station](#interrogate-a-station).

### How do I watch IEC 104 events as they happen?

`iec104ctl client monitor --ip 192.168.1.10 --interrogate` prints the current values and then every change, with its time tag, until you press Ctrl-C. `--format json` gives one JSON object per line. See [Monitor events and measured values](#monitor-events-and-measured-values).

### How do I send a command with select-before-operate?

`iec104ctl client command --ip 192.168.1.10 --type double --ioa 5001 --value off --mode sbo` selects, waits for the confirmation and executes. See [Send a command](#send-a-command).

### How do I simulate an IEC 104 RTU or server without hardware?

`iec104ctl server start` serves a built-in demo station on port 2404. `iec104ctl server generate-config --output station.yaml` writes its model for you to edit, and `iec104ctl server start --config station.yaml` serves yours. See [Simulate a station](#simulate-a-station) and [docs/server-config.md](docs/server-config.md).

### The station closes the connection, or does not answer. What should I check?

- **Is it an IEC 104 station at all?** `iec104ctl discover --subnets <address>` tells in a second, without starting data transfer.
- **Does it accept your address?** Many stations only accept connections from the controlling stations they are configured for, and only a few at a time.
- **The common address.** "unknown common address" (exit code 6) means the station is there and `--ca` is wrong; `iec104ctl client scan` finds the right one.
- **The field sizes.** A station behind an IEC 101 gateway may use shorter addresses: try `--ioa-size 2`, `--ca-size 1`, `--cot-size 1`.
- **What is on the wire.** `--debug` shows every frame in both directions.

### Why are the time tags off by an hour or two?

A time tag carries no time zone. iec104ctl reads it as UTC unless you say which zone the station keeps: `--timezone Europe/Brussels`, or `--timezone local`.

### Which stations and stacks has it been tested with?

Every client command was exercised against the servers of three independent protocol stacks: [lib60870-C](https://github.com/mz-automation/lib60870), [j60870](https://www.openmuc.org/iec-60870-5-104/) (OpenMUC) and [wendy512/iec104](https://github.com/wendy512/iec104). The simulated station was driven by the clients of the same three: interrogation, counter interrogation, read, clock synchronization, test command, commands (direct, select-before-operate, cancel, refused) and, with lib60870-C and j60870, file download. The underlying library, [go-iec104](https://github.com/otfabric/go-iec104), runs an interoperability suite against these stacks on every change. If you use iec104ctl with a device it has trouble with, please [open an issue](https://github.com/otfabric/iec104ctl/issues) with the `--debug` trace.

### Does iec104ctl support TLS?

Yes: `--url tls://host` (port 19998 by default) with `--tls-ca`, and `--tls-cert` / `--tls-key` for a station that wants a client certificate. The simulated station serves TLS with `--tls-cert` and `--tls-key`. This is the transport security of IEC 62351-3.

### Does iec104ctl support IEC 62351-5?

Yes, the A-Profile of IEC TS 60870-5-7: `--secure` with `--security-cert`, `--security-key` and `--security-trust` on every client command and on `server start`, with or without TLS. See [Connecting to a station](#connecting-to-a-station). The implementation is the one of go-iec104, tested for interoperability with an independent implementation; the standards are not public, and conformance is not established independently.

### Does it speak IEC 60870-5-101?

Not the serial link layer. It does speak IEC 104 with the shorter address fields of 101, which gateways between the two often pass on: `--cot-size`, `--ca-size` and `--ioa-size`.

### Will monitoring a live station disturb it?

It can. A station sends its events on one connection, and yours may become that one; and a station may accept few connections. A general interrogation makes it send its whole process image. On a station in service, agree on a time first, or use a station that keeps a separate event queue for you.

### How do I use iec104ctl in scripts and CI?

Pass `--format json` (or set `IEC104CTL_FORMAT=json`). Progress goes to stderr, the result goes to stdout, and a failure returns one JSON error object with a stable exit code. `iec104ctl server start --config model.yaml --duration 60` gives a pipeline a station to test against. See [General CLI usage](#general-cli-usage), [docs/json-output.md](docs/json-output.md) and [docs/exitcodes.md](docs/exitcodes.md).

---

## Project structure

<details>
<summary>Repository layout</summary>

```console
iec104ctl/
├── cmd/ (root command, version, completion)
│   ├── client (interrogate, counters, read, monitor, command, clock, reset, ping, scan, file, types)
│   ├── discover (subnet discovery)
│   └── server (start, generate-config)
├── internal/
│   ├── config (CLI & env: configuration types, flag and environment binding, station URLs)
│   ├── cli (shared Cobra helpers: enum completion, required flags, debug)
│   ├── format (stdout **text/json/table** via `Write`, streaming output via `StreamWriter`, table rendering)
│   ├── runner (command wiring, formatted and streaming runs, fatal JSON/text rendering)
│   ├── errs (typed CLI errors, stable codes, exit-code mapping)
│   ├── iec (what the commands do: connections, TLS, IEC 62351-5 security, requests, monitor, scan, file transfer, discovery, error classification)
│   ├── station (the simulated station: model, validation, behaviour, simulation)
│   ├── stationtest (the simulated station for tests, with throw-away TLS certificates)
│   └── types (DTOs for JSON/text/table output)
├── docs/ (exit codes, JSON output contract, station model reference)
├── Makefile (Build & test commands)
└── go.mod (Dependencies)
```

</details>

## Development

A toplevel `Makefile` is provided for convenience. Run **`make`** (or **`make help`**) for the full list. Common targets:

| Target | Purpose |
|--------|---------|
| **`make test`** | Unit and end-to-end tests with race detector (`go test -race ./...`) |
| **`make coverage`** | Tests + **`coverage.out`** across the whole module; fails below **85%** total statement coverage (`COVERAGE_MIN`) |
| **`make compile`** | Full-module typecheck (`go build ./...`) |
| **`make lint`** | `compile` then **staticcheck** |
| **`make lint-ci`** | `compile`, **go vet**, **golangci-lint** (uses `.golangci.yml`) |
| **`make fmt`** / **`fmt-check`** | Format with the gofmt of the Go version in `go.mod` (the one CI uses); `fmt-check` fails if that gofmt or the locally installed one would change any file |
| **`make check`** | `fmt`, `fmt-check`, `lint`, `lint-ci`, `vet`, `test`, **`coverage`** |
| **`make build`** | Runs **`check`** then writes **`bin/iec104ctl`** |
| **`make build-all`** / **`release-all`** | Cross-compile and package archives |

### Building

`make build` writes the binary to **`bin/iec104ctl`** (not the repo root).

```console
make build
make build-all
```

### Running Tests

```console
make test
make coverage
```

Tests need no hardware and no network beyond the loopback interface: `internal/stationtest` serves the simulated station of `iec104ctl server` on a free port, and the tests of every command run the real command tree against it, over TCP and TLS.

## Version History

Release notes for each version are maintained in **[RELEASE.md](RELEASE.md)**.

---

## Contributing

Bug reports, station-compatibility notes and pull requests are welcome. Please [open an issue](https://github.com/otfabric/iec104ctl/issues) to discuss larger changes first, and run `make check` before submitting. See [CONTRIBUTING.md](CONTRIBUTING.md) for the development setup and guidelines.

Please report security vulnerabilities privately, as described in [SECURITY.md](SECURITY.md), not in a public issue.

If iec104ctl is useful to you, a star on GitHub helps other engineers find it.

---

## License

This project is licensed under the MIT License. See [LICENSE](LICENSE).
