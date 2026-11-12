# iec101ctl: limitations

What iec101ctl does not do, and why. The protocol behaviour is go-iec60870-5's ([LIMITATIONS.md](https://github.com/otfabric/go-iec60870-5/blob/main/LIMITATIONS.md) of the library); this page lists what that means for the tool, and what the tool adds or leaves out.

## Evidence

- **Interoperability** is evidence from runs against the IEC 101 images of otfabric/iec60870-5-interop v0.4.0 (contract 2.3, pinned by digest in `interop/iec101ctl`): lib60870-C, cB-Sigma/iec60870 and digitalpetri/iec60870, both directions, both link modes, with the default field sizes and the sets 2/2/2/3, 1/2/1/3, 2/1/2/2 and (balanced) 0/1/1/2. It is interop with these stacks over a TCP stream, not with field devices, and not on a serial line: the serial port path is tested with an in-memory line only.
- **Balanced against a digitalpetri station** nothing beyond the link start-up interworks: the station does not initialize its own direction of the link, an upstream defect listed in the harness's known bugs. The interop suite skips those runs after it has seen the defect reproduce.
- **Standards.** IEC 60870-5-101 and -2 are applied as go-iec60870-5 implements them. The delay acquisition formula comes from IEC 60870-5-5, which go-iec60870-5 does not hold: `client delay` reports an interpretation. Nothing is certified; no conformance is claimed.

## Not supported

- **No IEC 60870-5-104 features.** No TLS, no application-layer security (IEC 62351-5 / IEC TS 60870-5-7 is not wired into go-iec60870-5's IEC 101), no redundancy groups, no time-tagged commands (types 58 to 64 do not exist in IEC 101) and no C_TS_TA_1.
- **No file transfer.** go-iec60870-5's IEC 101 has none, although its codec knows the file transfer types.
- **No originator address on requests.** The controlling station of go-iec60870-5 sends originator 0 with every request; `--cot-size 2` carries the field, and received originators are shown.
- **One controlled station per client command.** The client of go-iec60870-5 serves one link address. `client scan --method link` polls the link addresses of a multidrop line with request status of link only; it does not bring up their links.
- **One controlling station per line.** `server start` on TCP serves one connection at a time, as a terminal server carries one line, and refuses a connection that arrives meanwhile (`busy`) once it has waited half a second for the previous one to end.
- **Character timing.** On a TCP stream there is no line speed, parity or character timing; on a serial port the operating system's driver does the framing. go-iec60870-5 discards a frame whose next octet comes later than the frame gap (default: the link time-out) instead of measuring the line idle of 33 bits (IEC 60870-5-101 6.2.4.2).
- **Class assignment** in the simulated station is fixed: events and the answers to requests are class 1, periodic values class 2.
- **No automatic interrogation after end of initialization**, except `client monitor --interrogate`, which interrogates each time the link comes up.
- **`discover`** asks one link address per port with one link mode and address size; a station configured otherwise is listed as `open`, not found.
