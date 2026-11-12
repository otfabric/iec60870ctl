# Limitations

What the tools do not do, and why. Each tool's page has the details:
[iec101ctl](docs/iec101ctl/LIMITATIONS.md), [iec102ctl](docs/iec102ctl/LIMITATIONS.md),
[iec103ctl](docs/iec103ctl/LIMITATIONS.md); iec104ctl's are in its guide
([usage.md](docs/iec104ctl/usage.md#faq)). The protocol behaviour, and its limits, are
go-iec60870-5's ([its LIMITATIONS.md](https://github.com/otfabric/go-iec60870-5/blob/main/LIMITATIONS.md)).

## Evidence, not conformance

- Interoperability is shown against the independent implementations of
  iec60870-5-interop v0.4.0 over TCP ([TESTING.md](TESTING.md)), not against field devices,
  and for the serial tools not on a real serial line: the serial-port path is tested with
  in-memory lines.
- **IEC 102 has no reference controlled station.** iec102ctl's client is evidenced only
  against go-iec60870-5's own server; its simulated meter against the REE master gisce/iec870ree.
- **IEC 103** references have known defects on most pairings (listed by the harness); the
  generic services and the disturbance data of the client side have no conforming
  reference.
- The texts of IEC 60870-5-101 and -102 are not held by the project in full; where the
  library interprets them, the tools inherit the interpretation. No tool claims conformance
  or certification.

## Shared

- The serial tools have no security: IEC 62351-5 is not wired into go-iec60870-5 for 101,
  102 or 103, and the standards carry none. See [SECURITY.md](SECURITY.md).
- **The link time-out must exceed the slowest answer of the line.** FT 1.2 marks a request
  with one bit (FCB): an answer that arrives after the link time-out is taken for the answer
  to the next poll, and data the station sends meanwhile can be lost without an error
  (IEC 60870-5-2). Over a terminal server, a radio or a modem, raise `--link-timeout`
  above the worst round trip; the chaos tests run with a link time-out above their worst
  injected delay for this reason.
- A serial line has one controlling station: `server start` of the serial tools serves one
  connection at a time.
- iec104ctl only: TLS, IEC 62351-5, file transfer and redundancy-related options; the serial
  tools have none of them.
- go-iec60870-5 applies the request time-out (`--timeout`) to every request of a command;
  it is not a bound on a whole command that sends many requests (a `scan`, a `monitor`).
