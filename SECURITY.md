# Security Policy

## Reporting a Vulnerability

If you discover a security vulnerability in iec101ctl, iec102ctl, iec103ctl or iec104ctl, please report it responsibly.

**Do not open a public GitHub issue for security vulnerabilities.**

Instead, please email **security@otfabric.io**, or open a private advisory on GitHub (the [Security](https://github.com/otfabric/iec60870ctl/security) tab → **Advisories** → **Report a vulnerability**), with:

- A description of the vulnerability
- Steps to reproduce the issue
- The affected version(s) (`<tool> version` output)
- Any potential impact you've identified

We will acknowledge your report within 48 hours and aim to provide a fix or mitigation within 7 days for critical issues.

## Supported Versions

| Version | Supported |
|---------|-----------|
| Latest release | Yes |
| Older releases | Best effort |

## Security Considerations

### IEC 60870-5-104 Has No Built-In Security

Plain IEC 60870-5-104 carries no authentication and no encryption: anyone who can reach a station can interrogate and command it, and anyone on the path can read or alter the traffic. `iec104ctl` does not change that.

- **Use it only on stations and networks you own or are authorized to test**
- **Keep telecontrol traffic on isolated, trusted networks**; never expose port 2404 to the Internet
- Use TLS where the station supports it (below), or a VPN or secured gateway in front of the station

### TLS

`iec104ctl` supports the transport security of IEC 62351-3: `tls://` URLs with `--tls-ca`, and `--tls-cert` / `--tls-key` for mutual authentication; the server takes `--tls-cert`, `--tls-key` and `--tls-client-ca`. TLS 1.2 is the minimum.

- `--tls-insecure` turns off verification of the station's certificate. It is for test setups; do not use it against a station that matters
- Private keys are read from the files you name and are never written or printed. Keep them readable by you alone
- TLS authenticates the connection, not the individual ASDU: see the application-layer security below

### Application-Layer Security (IEC 62351-5)

`--secure` runs the A-Profile of IEC 62351-5 as IEC TS 60870-5-7 maps it onto IEC 104, on the client and on the simulated station: a Station Association authenticated with certificates, then every ASDU as Secure Data with a MAC (or AES-256-GCM with `--dpa aes-256-gcm`) and a sequence number against replay. It can be combined with TLS.

- The implementation is [go-iec60870-5](https://github.com/otfabric/go-iec60870-5)'s package `sec`. The normative documents are not public; it was derived from an open-source implementation and is tested for interoperability with it, but its conformance to the standards is not established independently. See the security notes of go-iec60870-5
- `--security-trust` decides who may associate. A CA certificate in it accepts **every** station certificate that CA signed; any other certificate is pinned by its fingerprint. Trust a CA only if it signs nothing but the stations you mean
- Keys are read from the files you name and are never written or printed. The association is kept in memory only: every run of `iec104ctl` associates anew
- `--roles` on the simulated station applies to every controlling station alike; without it everything is permitted

### The Serial Tools (iec101ctl, iec102ctl, iec103ctl)

IEC 60870-5-101, -102 and -103 have no authentication and no encryption either, and the
tools add none (go-iec60870-5 has no IEC 62351-5 for them): anyone on the line, or who can
reach a terminal server's TCP port, can read and command the station.

- **A serial line has one controlling station.** Connecting to a line in service, through a
  terminal server or a splitter, competes with the control system: frame count bits and
  polls get confused, and the station may answer you instead of it. In balanced mode both
  ends send as primaries
- **`server start`** serves one connection at a time on all interfaces (`--listen` to bind
  one); on a serial port it answers whatever is on the line
- **iec102ctl access keys** (REE profile) are passwords of the meter. Give them with
  `--access-key-file` or the environment rather than `--access-key` on a shared machine:
  a command line is visible to other users. They are never printed: the `--debug` frame
  trace shows the login frame with the key hidden. On the line itself the key travels in
  clear
- **`iec103ctl client generic write`** changes settings of protection equipment, and
  **`client command`** switches its functions (auto-recloser, teleprotection) on and off
- A serial device (`serial:///dev/ttyUSB0`) needs the user to be allowed to open it (on
  Linux the `dialout` group); no tool needs root

### Commands That Change a Station

- **`client command`** operates switchgear and changes set points. On real equipment this opens and closes breakers, moves tap changers and changes what a plant does. Know what an address controls before sending to it. Everything is validated before anything is sent, and a command is sent exactly once (never retried)
- **`client clock`** changes the station's clock, and with it the time tags of its events
- **`client reset`** restarts the station's process or clears its event buffer; `--qualifier` has no default
- **`client counters`** with `--freeze freeze`, `freeze-reset` or `reset` freezes or clears counters. Energy counters that are reset are gone
- **`client file put`** delivers a file the station may act on (a configuration, firmware)

### Commands That Are Read-Only but Not Invisible

- **Every client command connects and starts data transfer.** A station sends its events on one connection: yours may take them from the control centre for as long as you are connected, and a station that accepts few connections may turn the control centre away meanwhile
- **`client interrogate`** and **`client scan`** make a station send its whole process image
- **`discover`** connects to every address of a subnet. It sends a single test frame and does not start data transfer, but the connection may be logged, or counted against a station's connection limit. Scan production networks only with the agreement of whoever runs them

### Running a Server

- `server start` listens on **all network interfaces** and accepts **any client** by default. Use `--listen` to bind to one address and `--allow` to restrict the clients, or a host firewall
- The simulated station executes every command it is sent on its simulated points and takes delivered files when the model accepts uploads. With `uploads.dir` those files are written to disk under names the server chooses (`ioa-<n>-name-<n>.bin`); the client does not control the path
- A model may reference files on disk (`files[].path`): the server serves whatever those paths name to any client it accepts

### Output

- Command output, event logs and fetched files contain process values, addresses and network details, which may be sensitive operational data. Be careful when storing or sharing them
- `--debug` prints every frame sent and received to stderr

### Privileges

`iec104ctl` needs no elevated privileges. `server start` on port 2404 works as a normal user on most systems; binding a port below 1024 may need a capability (for example `CAP_NET_BIND_SERVICE` on Linux) rather than root.

### Verifying Downloads

Release archives are published with SHA-256 checksums. Verify them as shown in the [installation instructions](README.md#install) before installing.
