# iec103ctl

The IEC 60870-5-103 tool of [iec60870ctl](../../README.md); install it as described there.

**iec103ctl polls and simulates protection equipment (protection relays) that speak IEC 60870-5-103 (IEC 103), the companion standard for the informative interface of protection equipment.** As a controlling system it runs a general interrogation, sends general commands, synchronizes the clock, reads the identification, reads and writes settings with the generic services, lists the recorded faults and transfers their disturbance data, monitors what the relay sends and scans a line for link addresses. As protection equipment it simulates a relay from a model, so that a station controller or gateway has something to poll.

The line is FT 1.2 (IEC 60870-5-1) over a serial port, or over one TCP connection to a terminal server (serial device server) that passes the byte stream through. Output is text, a table or JSON with the exit codes of [docs/exitcodes.md](../exitcodes.md). It is written in Go on top of [go-iec60870-5](https://github.com/otfabric/go-iec60870-5) (packages `iec103`, `iec103/client`, `iec103/server`, `link`, `ft12`, `cp`).

> **Responsible use:** iec103ctl sends real requests to protection relays. Only use it on equipment and lines you own or are authorized to test. `client command` switches protection functions, `client generic write` changes settings that are in force at once, `client clock` sets the relay's clock, and `client scan` resets the link of every device it finds (which clears their transmit buffers). A relay has one controlling system per line: while iec103ctl holds it, the station controller cannot poll.

Read [LIMITATIONS.md](LIMITATIONS.md) before relying on a result.

## Quick start

No relay at hand? Start the built-in demo device in one terminal (port 2401, all interfaces):

```console
iec103ctl server start
```

and poll it from another:

```console
iec103ctl client identify --url tcp://127.0.0.1:2401
iec103ctl client interrogate --url tcp://127.0.0.1:2401
iec103ctl client command --url tcp://127.0.0.1:2401 --fun 160 --inf 16 --value off --wait-report 1000
iec103ctl client generic group --url tcp://127.0.0.1:2401 --group 1 --format table
iec103ctl client disturbance list --url tcp://127.0.0.1:2401
iec103ctl client disturbance get --url tcp://127.0.0.1:2401 --fan 1 --csv fault1.csv
```

## IEC 103 in two minutes

The controlling system (the *primary*) polls one item of protection equipment (the *secondary*) over an unbalanced FT 1.2 link with a one-octet link address (clause 6.2.3): it starts the link (request status of link, reset of the communication unit), then polls class 1 data while the device sets ACD (events, answers) and class 2 data otherwise (measurands). After the reset the device identifies itself (ASDU 5).

An information object is addressed by a **function type** (FUN: 128 distance, 160 overcurrent, 176 transformer differential, 192 line differential protection, 254 generic, 255 global) and an **information number** (INF), whose meaning tables 8 to 18 define per function type. The common address of ASDU is normally the link address.

## Line and link flags (every client command)

| Flag | Default | Meaning |
|---|---|---|
| `--url` | | `tcp://host[:port]` (a terminal server, port 2401) or `serial:///dev/ttyUSB0` (`serial://COM3` on Windows) |
| `--ip`, `--port` | 2401 | short form of a TCP line |
| `--baud`, `--data-bits`, `--parity`, `--stop-bits` | 9600, 8, even, 1 | the serial character format (FT 1.2); ignored on a TCP line, but still validated |
| `--link-address` | 1 | the device's link address, 0-254 |
| `--common-address` | 0 = the link address | common address of ASDU, 1-255 |
| `--link-timeout`, `--retries` | 1000 ms, 3 | how long a frame waits for its answer, and how often it is repeated |
| `--reset-fcb` | off | initialize the link with reset FCB (function 7) instead of reset CU |
| `--poll-interval` | 100 ms | pause between class 2 polls when the device has nothing to send |
| `--timeout` | 10000 ms | connecting, the link start-up, and each request |
| `--timezone` | utc | zone of the time tags on the wire: `utc`, `local` or an IANA name |
| `--format` | text | `text`, `json` or `table` ([json-output.md](json-output.md)) |
| `--debug` | | every frame sent and received, decoded, on stderr |

Every environment variable is the flag in upper case with `IEC103CTL_` in front (`IEC103CTL_URL`, `IEC103CTL_FORMAT`, ...); `--help` of each command names them.

The link start-up is never followed by the automatic time synchronization and general interrogation of clause 7.4.1: every command does only what it is asked to. A TCP line that a terminal server closes during the link start-up (it still serves the previous connection) is opened again until `--timeout`.

## Commands

### client interrogate

General interrogation (ASDU 7, clause 7.4.3): the state of every indication that takes part, until the termination with the same scan number.

```console
iec103ctl client interrogate --url tcp://10.0.0.20:2401
iec103ctl client gi --url serial:///dev/ttyUSB0 --baud 19200 --link-address 5 --format json
```

### client command

General command (ASDU 20, clause 7.4.4) with `--fun`, `--inf` and `--value on|off`, acknowledged by ASDU 1 with cause 20. A negative acknowledgement (cause 21) is exit code 6. Commands outside table 17 (INF 16-19 and 23-26 with a compatible function type; INF 19 and 23-26 ON only) are refused before anything is sent (exit code 3). `--wait-report` waits for the change of state (cause 12). Sent exactly once.

```console
iec103ctl client command --url tcp://10.0.0.20 --fun 160 --inf 16 --value off
iec103ctl client command --url tcp://10.0.0.20 --fun 160 --inf 19 --value on --wait-report 1000
```

### client clock

Time synchronization (ASDU 6, clause 7.4.2) with `--time` (`now` or RFC 3339), confirmed by the ASDU 6 the device answers with; `--broadcast` sends it to link address 255 with send/no reply (not confirmed).

```console
iec103ctl client clock --url tcp://10.0.0.20
iec103ctl client clock --url serial:///dev/ttyUSB0 --broadcast --time 2026-01-31T12:00:00Z
```

### client identify

The identification (ASDU 5, clause 7.4.1) the reset of the link makes the device send (cause 4, or 3 with `--reset-fcb`), and those of start/restart (5) and power on (6) that arrive within `--wait` ms.

```console
iec103ctl client identify --url tcp://10.0.0.20
```

### client generic headings, group, directory, entry, read, interrogate, write

The generic services of clause 7.4.8. A GIN is `GROUP.ENTRY`; `--kod` is a number or a name (`actual`, `default`, `range`, `precision`, `factor`, `reference`, `enumeration`, `dimension`, `description`, `password`, `read-only`, `write-only`, `fun-inf`, `event`, `text-array`, `value-array`, `related-entries`, `none`). A read answered with invalid data (cause 43) is shown with the generic reply codes and ends with exit code 7.

```console
iec103ctl client generic headings --url tcp://10.0.0.20
iec103ctl client generic group --url tcp://10.0.0.20 --group 1 --kod description
iec103ctl client generic directory --url tcp://10.0.0.20 --gin 1.2
iec103ctl client generic entry --url tcp://10.0.0.20 --gin 1.2
iec103ctl client generic read --url tcp://10.0.0.20 --inf 244 --items 1.2,1.3,1.4:description
iec103ctl client generic interrogate --url tcp://10.0.0.20 --format table
iec103ctl client generic write --url tcp://10.0.0.20 --gin 1.2 --type float32 --value 1.25
iec103ctl client generic write --url tcp://10.0.0.20 --gin 1.3 --type uint --size 2 --value 450 --mode abort
```

`generic write` writes one data set: `--type` `ascii` (1), `uint` (3), `int` (4), `float32` (7) or `float64` (8), `--size` the octets of an integer (1-8; for `ascii` the length of the text), `--value`. `--mode execute` (default) is the write with execution (INF 250): cause 40 is success, cause 41 a refusal with a generic reply code per data set (exit code 6). `--mode abort` stages the value with write entry (INF 248) and drops it with write abort (INF 251): nothing is activated. Sent exactly once.

### client disturbance list, get

The transmission of disturbance data (clause 7.4.7). `list` requests the list of recorded disturbances (ASDU 24 TOO 24, answered by ASDU 23). `get` transfers one record (`--fan`): the description (ASDU 26), the tags (ASDU 29) and every channel (ASDUs 27 and 30), acknowledging each part. `--fun` is the function type of the orders, by default the one of the device's identification. `--output` writes the record as JSON, `--csv` the values as CSV (one row per value number: `index`, `time_us` from the first value, one column per channel with the raw SDV); an existing file is refused before anything is sent unless `--force`. `--transfer-timeout` bounds the transfer (2 minutes). The alias of `disturbance` is `fault`.

```console
iec103ctl client disturbance list --url tcp://10.0.0.20
iec103ctl client disturbance get --url tcp://10.0.0.20 --fan 7 --output fault7.json --csv fault7.csv
```

### client monitor

Polls the device and prints every ASDU it sends as it arrives (JSON Lines with `--format json`), until Ctrl-C, `--duration` seconds or `--count` messages. `--interrogate` runs a general interrogation after every link start-up. A lost line is opened again unless `--no-reconnect` (then exit code 4). Link state and the summary go to stderr.

```console
iec103ctl client monitor --url tcp://10.0.0.20 --interrogate
iec103ctl client monitor --url serial:///dev/ttyUSB0 --duration 60 --format json > events.jsonl
```

### client scan

Tries every link address from `--start` to `--end` (0-254) on one line, with `--timeout` per address (1000 ms), and lists those that answer the link start-up with their identification.

```console
iec103ctl client scan --url serial:///dev/ttyUSB0 --start 1 --end 32
```

### server start, server generate-config

`server start` serves the device of `--config` (YAML or JSON, [server-config.md](server-config.md)), or the built-in demo, over TCP (`--listen`, `--port` 2401, one controlling system at a time) or a serial port (`--serial /dev/ttyUSB1` with `--baud` and the other serial flags). `--link-address` and `--common-address` override the model's. Every event goes to stdout (JSON Lines with `--format json`); `--duration` stops it after so many seconds. `server generate-config` writes the commented demo model (`--format yaml|json`, `--output`, `--force`).

```console
iec103ctl server generate-config --output relay.yaml
iec103ctl server start --config relay.yaml --listen 127.0.0.1 --link-address 5
iec103ctl server start --config relay.yaml --serial /dev/ttyUSB1 --baud 19200
```

### version, completion

```console
iec103ctl version --json
source <(iec103ctl completion bash)
```

`discover` does not exist for IEC 103: a line has one controlling system and a terminal server's port says nothing about the devices behind it; `client scan` finds the devices of a line.

## Exit codes

As for every tool of the repository ([exitcodes.md](../exitcodes.md)): 0 success, 2 usage, 3 invalid input (validated before anything is sent), 4 transport (no line, line lost, link down), 5 timeout (no answer to the link start-up or a request; no identification), 6 refused by the device (negative acknowledgement, refused write, aborted transfer), 7 a read answered with invalid data (cause 43), 10 internal or output error, 130 interrupted. The codes iec103ctl adds: `link_down`, `transfer_aborted`, `no_identification`.
