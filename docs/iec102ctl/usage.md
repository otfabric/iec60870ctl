# iec102ctl

The IEC 60870-5-102 tool of [iec60870ctl](../../README.md); install it as described there.

**iec102ctl reads and simulates integrated total stations: electricity meters and data concentrators that speak IEC 60870-5-102 (IEC 102), the transmission of integrated totals in electric power systems.** As a controlling station it reads a meter's product specification, its clock, the integrated totals (meter readings) of a record for a period window, its events, and the requests of the Spanish REE profile (a session with an access key, clock setting, tariff information, contracted powers, special days). As an integrated total station it simulates a meter from a model of records, so that a metering front end or data collector has something to read.

The line is FT 1.2 over a serial port, or over one TCP connection to a terminal server (serial device server) that passes the byte stream through. Output is text, a table or JSON with the exit codes of [docs/exitcodes.md](../exitcodes.md). It is written in Go on top of [go-iec60870-5](https://github.com/otfabric/go-iec60870-5) (packages `iec102`, `iec102/client`, `iec102/server`, `iec102/ree`, `link`, `ft12`).

> **Responsible use:** iec102ctl sends real requests to meters. Only use it on meters and lines you own or are authorized to test. `client ree set-time` changes the meter's clock, and with it the integration periods it records; `client request` sends whatever ASDU you give it. A meter often has one line: while iec102ctl holds it, the regular data collector cannot read.

Read [LIMITATIONS.md](LIMITATIONS.md) before relying on a result: we do not hold the text of IEC 60870-5-102 or of the REE profile, and the client has no independent peer in the interop harness.

## Quick start

No meter at hand? Start the built-in demo meter in one terminal (port 2401, all interfaces):

```console
iec102ctl server start
```

and read it from another:

```console
# Who is it, and what time does it have?
iec102ctl client info --url tcp://127.0.0.1:2401
iec102ctl client time --url tcp://127.0.0.1:2401

# The hourly load profile of 1 January, absolute values, in an REE session
iec102ctl client totals --url tcp://127.0.0.1:2401 --record 11 --from 2026-01-01T01:00 --to 2026-01-02T00:00 --access-key 7

# The same as increments, as a table
iec102ctl client totals --url tcp://127.0.0.1:2401 --kind incremental --from 2026-01-01 --to 2026-01-02 --format table

# Events of list 52
iec102ctl client events --url tcp://127.0.0.1:2401 --record 52

# REE: tariff information of contract I, contracted powers, set the clock
iec102ctl client ree tariffs --url tcp://127.0.0.1:2401 --access-key 7
iec102ctl client ree contracted-powers --url tcp://127.0.0.1:2401 --access-key 7
iec102ctl client ree set-time --url tcp://127.0.0.1:2401 --access-key 7
```

## IEC 102 in two minutes

A controlling station (the *primary*) reads one integrated total station (the *secondary*) over an unbalanced FT 1.2 link: it starts the link (request status of link, reset of remote link), sends a request with *user data, confirm*, and polls *class 2 data* until the answer is complete. IEC 102 has no spontaneous data: the meter answers only what it is asked, so there is no `client monitor`.

A request names a **record address** (11: integration period 1, the hourly load profile of REE meters; 12: period 2; 21: daily values; 1: the accounting period; 50-55: lists of single-point events) and, for integrated totals, a **range of integrated total addresses** (REE: 1 active import, 2 active export, 3-6 reactive quadrants 1-4) and a **time window** of time a values (minutes, the meter's local time, with a summer time bit SU). The meter confirms the request (cause 7), sends one ASDU per integration period (cause 5) and terminates the request (cause 10), or refuses it with a cause of refusal (13-18).

Every link and ASDU parameter must match the meter: the link address and its size (`--link-address`, `--link-address-size`, default 1 with two octets), the common address of ASDU, which the REE profile calls the measuring point address, and its size (`--common-address`, `--ca-size`, default 1 with two octets). These are the defaults of the IEC 102 link section of the otfabric/iec60870-5-interop contract.

## Connecting to a meter

| Flag | Meaning |
|------|---------|
| `--url` | `tcp://host[:port]` (terminal server, port 2401 by default) or `serial:///dev/ttyUSB0` (`serial://COM3`) |
| `--ip`, `--port` | the terminal server, instead of `--url` |
| `--baud`, `--data-bits`, `--parity`, `--stop-bits` | the serial line (FT 1.2: 9600 baud, 8 data bits, even parity, 1 stop bit) |
| `--link-address`, `--link-address-size` | the meter's link address, and the octets of the address field (1, 2, or 0 for none) |
| `--common-address`, `--ca-size` | the measuring point address and its octets (1 or 2) |
| `--timezone` | the zone of the meter's clock: times without a zone are read in it, and it sets the SU bit (`utc` by default; Spanish meters keep `Europe/Madrid`) |
| `--link-timeout`, `--link-retries` | how long to wait for the answer to a frame, and how often to repeat it with the same frame count bit |
| `--timeout` | milliseconds for connecting, starting the link and each request (30000) |
| `--profile` | `standard`, or `ree` to decode the types of the REE profile |
| `--access-key`, `--access-key-file` | the REE access key: the operation runs in a session (C_AC_NA_2 before, C_FS_NA_2 after); implies `--profile ree` |
| `--format` | `text`, `json` or `table` |
| `--debug` | trace every frame sent and received to stderr (the access key is not shown), and the causes of an error |

A line that is closed during the link start-up is busy (a terminal server, an interop image and `iec102ctl server start` serve one controlling station at a time); it is opened again after 100 ms and after 400 ms more, since nothing but the link start-up was sent. Any other failure is not retried.

Every flag has an environment variable, `IEC102CTL_<FLAG>` (for example `IEC102CTL_ACCESS_KEY`, `IEC102CTL_URL`, `IEC102CTL_FORMAT`). The access key is never printed; prefer `IEC102CTL_ACCESS_KEY` or `--access-key-file` to the flag, which other users of the machine can see in the process list.

## Commands

### client info

Reads the manufacturer and product specification: C_RD_NA_2, answered by P_MP_NA_2.

```console
iec102ctl client info --url tcp://10.0.0.20:2401
iec102ctl client info --url serial:///dev/ttyUSB0 --baud 9600 --link-address 3 --format json
```

### client time

Reads the meter's clock: C_TI_NA_2, answered by M_TI_TA_2 (time b). The result has the time as the meter sent it and, read in `--timezone`, how far it is from this computer's clock.

```console
iec102ctl client time --url tcp://10.0.0.20 --timezone Europe/Madrid
```

### client totals

Reads integrated totals of `--record`. `--kind` chooses the totals (`operational` = `absolute`, the default; `reset-operational` = `incremental`; `accounting`; `reset-accounting`); the selection chooses the read command:

| Selection | Read command (operational / reset-operational / accounting / reset-accounting) |
|-----------|------|
| `--from T --to T` (time range, addresses `--first`..`--last`, 1..8 by default) | C_CI_NT_2 / NU_2 / NR_2 / NS_2 |
| `--period T` | C_CI_NL_2 / NP_2 / NC_2 / NG_2 |
| `--period T` with `--first`/`--last` | C_CI_NM_2 / NQ_2 / ND_2 / NH_2 |
| `--oldest` | C_CI_NI_2 / NN_2 / NA_2 / NE_2 |
| `--oldest` with `--first`/`--last` | C_CI_NK_2 / NO_2 / NB_2 / NF_2 |

`--type` names the read command directly (C_CI_NA_2 .. C_CI_NU_2 or 104-123). Times are `2026-01-31T13:00`, `2026-01-31` (midnight) or RFC 3339 (converted to `--timezone`).

```console
iec102ctl client totals --url tcp://10.0.0.20 --record 11 --from 2026-01-31T01:00 --to 2026-02-01T00:00
iec102ctl client totals --url tcp://10.0.0.20 --kind incremental --first 1 --last 1 --from 2026-01-31 --to 2026-02-01 --access-key 7 --format table
iec102ctl client totals --url tcp://10.0.0.20 --record 1 --kind accounting --oldest
iec102ctl client totals --url tcp://10.0.0.20 --type C_CI_NM_2 --period 2026-01-31T13:00 --first 1 --last 2
```

A refusal ends with exit code 6 and the cause as the error code: `no_period` (18), `no_object` (17), `unknown_record_address` (16, also for another measuring point), `unknown_record_number` (15), `unknown_type` (14), `no_record` (13), `negative_confirmation` (the P/N bit).

### client events

Reads single-point information with time tag of a list (`--record`, 51 by default): all of it with C_SP_NA_2, or with `--from` and `--to` the events of a time range with C_SP_NB_2.

```console
iec102ctl client events --url tcp://10.0.0.20 --record 52
iec102ctl client events --url tcp://10.0.0.20 --record 52 --from 2026-01-01 --to 2026-02-01 --format json
```

### client request

Sends one ASDU built from `--type`, `--cause`, `--record`, `--data` (hex) and `--objects` (the VSQ number), and prints every ASDU the meter answers until `--until` (`reply`, `confirmation`, `termination`, or `auto`: termination for cause activation, else reply). For a type iec102ctl has no command for; the answers' objects are shown as hex. It is sent once and never repeated by iec102ctl. **It may change the meter**: the type decides what the request does.

```console
iec102ctl client request --url tcp://10.0.0.20 --type C_RM_NA_2 --access-key 7
iec102ctl client request --url tcp://10.0.0.20 --type 103 --cause request --format json
```

### client types

Lists the type identifications iec102ctl knows by name, standard and REE, with their direction and whether their objects are decoded. Nothing is sent.

```console
iec102ctl client types --profile ree --direction control
```

### client ree login

Opens an REE session with the access key and closes it again; nothing else is sent. A refused key ends with exit code 6 and `login_refused`.

```console
iec102ctl client ree login --url tcp://10.0.0.20 --access-key 7
IEC102CTL_ACCESS_KEY=7 iec102ctl client ree login --url serial:///dev/ttyUSB0
```

### client ree set-time

Sets the meter's clock (C_CS_TA_2 of the REE profile, confirmed with cause 7) to `--time`: `now` (the default, taken when the link is up), a time b (`2026-02-03T04:05:06.789`) or RFC 3339. **This changes the meter.** It is validated before anything is sent and sent once.

```console
iec102ctl client ree set-time --url tcp://10.0.0.20 --access-key 7 --timezone Europe/Madrid
iec102ctl client ree set-time --url tcp://10.0.0.20 --access-key 7 --time 2026-02-03T04:05:06.789
```

### client ree tariffs

Reads the tariff information of a contract register (`--register`, 134 by default; 134-136 contracts I-III, 137-139 the latent ones): current values (C_TA_VC_2), or with `--from` and `--to` the stored values of the billing periods that end in the range (C_TA_VM_2).

```console
iec102ctl client ree tariffs --url tcp://10.0.0.20 --access-key 7
iec102ctl client ree tariffs --url tcp://10.0.0.20 --access-key 7 --from 2025-11-01 --to 2026-01-01 --format json
```

### client ree contracted-powers

Reads the contracted powers (C_PC_NA_2, answered by M_PC_NA_2): the power in kW of each tariff period. There is no command to write them: no source we hold gives that request's layout.

```console
iec102ctl client ree contracted-powers --url tcp://10.0.0.20 --access-key 7
```

### client ree special-days

Reads the special days of the tariff calendar (C_DF_NA_2, answered by M_DF_NA_2).

```console
iec102ctl client ree special-days --url tcp://10.0.0.20 --access-key 7 --format table
```

### server start

Serves a simulated meter from the model `--config` (YAML or JSON; [server-config.md](server-config.md)), or the built-in demo meter, on TCP (`--listen`, `--port`, 2401 by default) or on a serial port (`--serial` with `--baud`, `--data-bits`, `--parity`, `--stop-bits`). `--link-address` and `--common-address` replace the model's; `--link-address-size`, `--ca-size` and `--timezone` set the link and the meter's clock. One line has one controlling station: a second TCP connection is closed at once (`busy`). Every event goes to stdout as text, a table row or one JSON object per line; `--debug` traces every frame to stderr. It runs until Ctrl-C or for `--duration` seconds.

```console
iec102ctl server start
iec102ctl server start --config meter.yaml --listen 127.0.0.1 --port 12401 --format json
iec102ctl server start --config meter.yaml --serial /dev/ttyUSB0 --baud 9600 --link-address 5 --timezone Europe/Madrid
iec102ctl server start --config meter.yaml --duration 60
```

### server generate-config

Writes the model of the demo meter, with comments (YAML) or as JSON (`--format json`), to stdout or `--output` (an existing file only with `--force`).

```console
iec102ctl server generate-config --output meter.yaml
iec102ctl server generate-config --format json --output meter.json
```

### version, completion

`iec102ctl version` prints the version, tag, commit, build date, the OT Fabric libraries and the platform on one line; `--json` (or `IEC102CTL_FORMAT=json`) as one JSON object. `iec102ctl completion bash|zsh|fish|powershell` writes a completion script.

## Exit codes

Those of [docs/exitcodes.md](../exitcodes.md), shared by the four tools: 0 success, 2 usage, 3 invalid input (validated before anything is sent), 4 transport (no line, the line was lost), 5 timeout (`link_timeout`: no meter answered the link at that address; `request_timeout`: the request was not complete in `--timeout`), 6 refused by the meter (the codes above and `login_refused`), 7 partial (the operation succeeded but the REE session did not end with a confirmed logout), 10 internal, 130 interrupted.
