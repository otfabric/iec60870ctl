# iec103ctl limitations

What iec103ctl does not do, and why. Most of it follows from go-iec60870-5 v0.5.1 (its LIMITATIONS.md and docs/iec103/INTEROPERABILITY.md).

## Sources and evidence

- **The text.** go-iec60870-5 reads IEC 60870-5-103:1997 in its identical Russian adoption GOST R IEC 60870-5-103-2005; where the text is ambiguous it records an interpretation (docs/iec103/INTEROPERABILITY.md of the library). Nothing here is a conformance claim.
- **The server has four independent clients.** `iec103ctl server start`, serving each reference's own baseline fixture, was run against the clients of vitusdong/iec103-dotnet, masarray/ARIEC103, XVincentX/Open103 and riclolsen/go-iecp5 (the IEC 103 images of otfabric/iec60870-5-interop v0.4.0, pinned by digest; `make interop`, `interop/iec103ctl`): every harness case and values check that a client declares passes, except the known upstream defects listed below, which are skipped with their bug id. The generic services and the disturbance list are declared by no reference client but vitusdong's: its generic commands carry INF 0 (known bug `vitusdong-generic-command-inf`), so the generic services are not evidenced against an independent client; its disturbance list request is answered (the harness case passes) but it decodes the status of fault wrongly (`vitusdong-sof-bits`), and its transfer deviates from figure 10 (`vitusdong-disturbance-procedure`). The disturbance transfer is: Open103 transfers the record with the procedure of clause 7.4.7 (its description passes; its values hit `open103-asdu30-nfe`).
- **The client has one independent server, which deviates.** `iec103ctl client` was run against the one reference server (vitusdong). The link start-up, the clock synchronization and the termination of the general interrogation pass; the identification, the answers of the general interrogation, general commands, generic read and write and the disturbance list meet the server's recorded deviations (`vitusdong-identification`, `vitusdong-gi-cause-and-asdu1`, `vitusdong-command-confirmation`, `vitusdong-generic-answer-inf`, `vitusdong-disturbance-list-asdu23`), which the suite checks on the wire and skips. The disturbance transfer is not declared by that server. Beyond that, the client is evidenced only by the e2e suite against go-iec60870-5's own protection equipment (`iec103/server`), which shares its codec with the client. A real relay may differ.
- **Broadcast time synchronization** is exercised by no reference.

## Not supported

- **One device per line.** go-iec60870-5's controlling system polls one item of protection equipment per transport: no multi-drop polling of several link addresses at once. `client scan` tries the addresses one after the other on one line.
- **No `discover`.** A line has one controlling system, and a terminal server's TCP port tells nothing about the devices behind it.
- **No abortion of a disturbance transfer** by the controlling system (the library cannot send it); a transfer that runs out of `--transfer-timeout` is left as it is.
- **Disturbance data as transmitted.** `client disturbance get` writes the channel values as the signed integers (SDV) of ASDU 30 with RPV, RSV and RFA as sent; it does not convert them to primary values and writes no COMTRADE or other recorder format.
- **Generic writes of one data set.** `client generic write` writes one entry per command (with execution, or staged and aborted); writing several entries at once, write with confirmation alone and the GDD continuation bit (one element over several ASDUs) are not offered. Structured values (data type 23) and the other data types are shown as read, not written.
- **No private ASDUs.** Private type identifications and causes are shown as hex by `client monitor`; nothing sends them.
- **Test mode and monitor-direction blocking** are not simulated by the server; the 15 s limit on DFC (clause 6.3) is not enforced (library).
- **Link timing.** The time correction of a synchronization covers the frame time only (library); over TCP the simulated device makes no correction.
- **The client never runs the time synchronization and general interrogation after the link start-up** of clause 7.4.1 on its own: a station controller does, a diagnostic tool should not change the device unasked.

## Known upstream defects

The interop suite applies the harness's `known-bugs.tsv` and `known-bugs-consumer.tsv` of otfabric/iec60870-5-interop v0.4.0 (copied to `interop/iec103ctl/testdata`). They are defects of the references, not of iec103ctl; each is reported as a skipped known difference, never as a pass.
