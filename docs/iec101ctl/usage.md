# iec101ctl

The IEC 60870-5-101 tool of [iec60870ctl](../../README.md); install it as described there.

**iec101ctl is a command-line toolkit for IEC 60870-5-101 (IEC 101), the serial telecontrol protocol of power system SCADA, in both roles.** As a controlling station it brings up the link to an RTU, interrogates it, watches what it sends, issues commands and set points (direct or select-before-operate), synchronizes its clock and measures the transmission delay. As a controlled station it simulates an RTU from a model of points and commands. It is written in Go on top of [go-iec60870-5](https://github.com/otfabric/go-iec60870-5).

The line is a serial port, or a TCP connection to a terminal server (serial device server) that carries the serial byte stream unchanged. Over TCP it is still IEC 60870-5-101 (FT 1.2 frames with the link procedures of IEC 60870-5-2), not IEC 60870-5-104.

> **Responsible use:** iec101ctl sends real telecontrol requests. Only use it on stations and lines you own or are authorized to test. `client command` operates switchgear and changes set points; `client clock`, `client reset` and `client delay --set` change the station itself. A line has one controlling station: what iec101ctl receives, the station's regular controlling station does not.

What iec101ctl does not do, and why: [LIMITATIONS.md](LIMITATIONS.md). JSON output: [json-output.md](json-output.md). Station model: [server-config.md](server-config.md). Exit codes: [../exitcodes.md](../exitcodes.md).

## Quick start

No station at hand? Start the built-in demo station in one terminal:

```console
iec101ctl server start
```

and talk to it from another:

```console
# What does this station hold? (general interrogation)
iec101ctl client interrogate --url tcp://127.0.0.1:2401

# Watch events and measured values as they arrive, until Ctrl-C
iec101ctl client monitor --url tcp://127.0.0.1:2401 --interrogate

# Open a breaker: single command, direct execute
iec101ctl client command --url tcp://127.0.0.1:2401 --type single --ioa 5000 --value off
```

## The line and the link

Every client command opens the line, brings up the link (request status of link, then reset of remote link) and closes the line again when it is done.

| Flag | Meaning |
|------|---------|
| `--url tcp://host[:port]` | A terminal server; port 2401 (the port of the iec60870-5-interop images) by default |
| `--url serial:///dev/ttyUSB0` (`serial://COM3`) | A serial port, with `--baud` (9600), `--data-bits` (8), `--parity` (`even`, FT 1.2's) and `--stop-bits` (1) |
| `--ip`, `--port` | A terminal server, instead of `--url` |
| `--link-mode` | `unbalanced` (default: iec101ctl is the primary station and polls the station for class 1 and class 2 data) or `balanced` (both stations send as primaries, on a full-duplex point-to-point line) |
| `--link-address` | Link address of the controlled station (default 1). The broadcast address (255, 65535) is not a station's |
| `--remote-link-address` | Balanced only: the link address of the controlling station (default: `--link-address`) |
| `--link-address-size` | Octets of the link address field: 1 (default) or 2; 0 (no address) in balanced mode only |
| `--cot-size`, `--ca-size`, `--ioa-size` | Octets of the cause of transmission (1 or 2), the common address (1 or 2) and the information object address (1, 2 or 3). Defaults 1, 1 and 2 |
| `--link-timeout`, `--link-retries` | How long the link layer waits for the answer to a frame before it repeats it (1000 ms) and how often (3). On a slow line raise the time-out: IEC 60870-5-101 6.2.2 gives about 2.25 s at 1200 bit/s |
| `--poll-interval` | Unbalanced: the pause between class 2 polls when the station has nothing to send (100 ms) |
| `--strict` | Apply the IEC 60870-5-101 profile checks of go-iec60870-5 to every ASDU sent and received |
| `--timeout` | Bringing up the link and each request (10 s). A TCP line that closes during the link start-up (a terminal server still serving the previous connection) is opened again until the timeout |
| `--ca` | Common address of the station (default 1); the broadcast address is 255, or 65535 with `--ca-size 2` |

Nothing on the wire says which field sizes a station uses: they must match the station. A link that does not come up ends with exit code 5 and the error code `link_init_failed`.

`--debug` traces every frame, decoded, to stderr.

## Client commands

### `client interrogate`

General (`--group` 0) or group (`--group 1-16`) interrogation, C_IC_NA_1: every value the station reports until the activation termination. `--type` and `--ioa` narrow what is shown. Alias `gi`.

```console
iec101ctl client interrogate --url serial:///dev/ttyUSB0 --link-address 3 --ca 3 --format table
iec101ctl client interrogate --url tcp://10.0.0.5:2401 --link-mode balanced --ioa-size 3 --group 2
```

### `client counters`

Counter interrogation, C_CI_NA_1: the integrated totals. `--freeze freeze|freeze-reset|reset` is a counter freeze command, which the station only confirms (IEC 60870-5-101 7.4.8).

```console
iec101ctl client counters --url tcp://10.0.0.5:2401 --format json
```

### `client read`

Read command, C_RD_NA_1, per address of `--ioa` (a list and ranges). A refused address is reported in its row; the exit code is then 7.

```console
iec101ctl client read --url tcp://10.0.0.5:2401 --ioa 100,1000-1003
```

### `client command`

A process command to `--ioa`: `--type single|double|step|setpoint-normalized|setpoint-scaled|setpoint-float|bitstring` (or the type name, such as `C_SC_NA_1`), `--value`, `--mode execute|select|sbo|cancel`, `--qualifier`, and `--wait-termination` to wait for the activation termination. IEC 60870-5-101 has no time-tagged command types. **This changes the process**: everything is validated before anything is sent, and a command is sent once (the link layer repeats an unanswered frame with the same frame count bit, which the station recognizes as a repetition). Alias `cmd`.

```console
iec101ctl client command --url tcp://10.0.0.5:2401 --type double --ioa 5001 --value off --mode sbo --wait-termination 5000
```

### `client clock`

Clock synchronization, C_CS_NA_1, with `--time` (now) in `--timezone` (UTC). A station corrects the time by the transmission delay it was given.

### `client reset`

Reset process, C_RP_NA_1: `--qualifier process` (general reset) or `events` (reset of pending information with time tag).

### `client delay`

The delay acquisition procedure of IEC 60870-5-101 7.4.12, C_CD_NA_1: without `--set` it measures the transmission delay; `--set auto` measures it and sends it to the station; `--set <ms>` sends that many milliseconds without measuring.

```console
iec101ctl client delay --url serial:///dev/ttyUSB0 --baud 1200 --set auto
iec101ctl client clock --url serial:///dev/ttyUSB0 --baud 1200
```

### `client monitor`

Stay on the line and print every value as it arrives (one line, JSON object or table row each), until Ctrl-C, `--duration` or `--count`. `--interrogate` runs a general interrogation each time the link comes up. A link that stops answering is initialized again; a lost line is opened again unless `--no-reconnect`, also one that closes while its link is being initialized; only a first line whose link never comes up ends the monitor. `--all` also shows confirmations and terminations.

```console
iec101ctl client monitor --url tcp://10.0.0.5:2401 --interrogate --format json > events.jsonl
```

### `client ping`

Test a station at `--interval`, `--count` times: `--method command` (test command C_TS_NA_1, default), `link-status` (request status of link, answered by the link layer, also before the link is reset) or `link-test` (the test function for link, balanced only). Exit code 7 when a test failed.

```console
iec101ctl client ping --url serial:///dev/ttyUSB0 --method link-status --link-address 3
```

### `client scan`

Find the stations on a line: `--method broadcast` (one general interrogation to the broadcast common address, default), `ca` (one interrogation per common address from `--start` to `--end`) or `link` (request status of link per link address, unbalanced: a multidrop line). `--delay` pauses that many milliseconds between the requests of a sweep.

```console
iec101ctl client scan --url serial:///dev/ttyUSB0 --method link --start 1 --end 30 --link-timeout 300
```

### `client types`

The type identifications IEC 60870-5-101 defines, with number, name, direction and description; `--direction monitor|control`.

## Simulated station

### `server start`

Serves the station of a model (`--config`, YAML or JSON; the built-in demo without it) as the controlled station: on TCP (`--listen`, `--port`, 2401 by default), one controlling station at a time as a terminal server carries one line, or on a serial port (`--serial /dev/ttyUSB0` with the line flags). The link flags are those of the client; `--remote-link-address` is the controlling station's address in balanced mode. `--single-char-ack` acknowledges with E5h, `--no-end-of-init` suppresses the end of initialization after the first link reset. Every event goes to stdout (`--format`); `--duration` stops it.

```console
iec101ctl server start --config station.yaml --listen 127.0.0.1
iec101ctl server start --config station.yaml --serial /dev/ttyUSB0 --baud 9600 --link-address 3
iec101ctl server start --link-mode balanced --link-address-size 2 --ca-size 2 --ioa-size 3 --format json
```

### `server generate-config`

Writes the demo model, commented YAML (`--format yaml`) or JSON, to `--output` (stdout by default); `--force` overwrites.

## `discover`

Probes `--subnets` on `--ports` (2401) for an FT 1.2 station behind a terminal server: one TCP connection and one request status of link to `--link-address` with `--link-address-size` and `--link-mode`. A port that accepts the connection but does not answer is listed as `open`. `--parallel` (32) probes run at a time, each bounded by `--timeout` (1000 ms); more than 4096 hosts need `--force-large-scan`.

```console
iec101ctl discover --subnets 192.168.1.0/24 --ports 2401,4001
```

## `version` and `completion`

`version` prints the version, tag, commit, build date, the OT Fabric libraries, Go version and platform (`--json` for one JSON object). `completion bash|zsh|fish|powershell` writes a shell completion script; flags with a fixed set of values complete to them.

## Environment

Every flag has an environment variable, `IEC101CTL_<FLAG>` (shown in `--help`), for example `IEC101CTL_URL`, `IEC101CTL_LINK_MODE`, `IEC101CTL_IOA_SIZE` and `IEC101CTL_FORMAT`. A flag given on the command line wins.
