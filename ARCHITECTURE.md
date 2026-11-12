# Architecture

One Go module, `github.com/otfabric/iec60870ctl`, four binaries. The tools share what
makes them behave alike (exit codes, output formats, flag and environment binding, the
serial transport) and keep everything protocol-specific apart.

```
cmd/<tool>/main.go           main: link-time version metadata, os.Exit(cmd.Execute())
internal/<tool>/
    cmd/                     cobra command tree: root, client …, server …, version, completion
    config/                  flag and environment structs (struct tags env/desc/flag)
    types/                   the JSON output types (the contract in docs/<tool>/json-output.md)
    <engine>/                what the commands do, on go-iec60870-5's client of the standard
    station/, stationtest/   the simulated station, its model, and a test helper serving it
internal/errs                error kinds, stable codes, exit codes (docs/exitcodes.md)
internal/format              text, table and JSON rendering; JSON Lines for streams
internal/runner              runs a command: invocation state, fatal-error rendering
internal/cli                 completion and required-flag helpers
internal/buildinfo           the version line and the libraries a binary is built on
internal/envflag             binds tagged struct fields to flags and environment variables
internal/line                FT 1.2 transport of iec101ctl, iec102ctl, iec103ctl
e2e/<tool>/                  the built binary against go-iec60870-5's servers and clients
interop/<tool>/              the built binary against the iec60870-5-interop images
```

## How a command runs

1. `main` hands the link-time metadata to the tool's `cmd` package and calls `Execute`,
   which builds a fresh command tree. The flags of every command are bound to a config
   struct; the environment (`<TOOL>_…`) fills it first, flags override it. A malformed
   environment variable is invalid input (exit 3), not a panic.
2. The command validates its configuration before any I/O: a target, addresses, values,
   sizes. Invalid input exits 3 and has sent nothing.
3. The engine opens the transport (TCP, TLS, or for the serial standards a serial port or a
   TCP terminal server through `internal/line`), starts the link, runs the procedure on the
   library's client and returns a result of `types`.
4. `runner` renders the result in the format asked for, or a fatal error as one line on
   stderr (text) or one JSON object on stdout (JSON), and maps the error's kind to the
   exit code.

Errors of the library are classified in each engine's `WrapError` into the shared kinds
and codes of `internal/errs`, so the four tools report the same failure the same way.

## The simulated stations

`server start` serves a station built from a YAML or JSON model (`server
generate-config` writes the demo model as a starting point). The station is built on the
library's server of the standard; it logs every request it handles as an event (text,
table or JSON Lines on stdout). IEC 104 serves many connections; the serial standards
serve one connection at a time, as a line has one controlling station
(`internal/line.Serve`).

## Dependencies

go-iec60870-5 (the protocol), go-serial (serial ports), cobra and pflag (command line),
yaml.v3 (station models). All `github.com/otfabric/*` dependencies are pinned to released
tags.
