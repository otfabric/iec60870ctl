# iec102ctl limitations

What iec102ctl does not do, and why. Most of it follows from go-iec60870-5 v0.5.2 (its LIMITATIONS.md and docs/iec102/INTEROPERABILITY.md) and from the sources that exist for IEC 102.

## Sources and evidence

- **The standard's text is not held.** Neither iec102ctl nor go-iec60870-5 holds IEC 60870-5-102 or any document of the Spanish REE profile. Every layout and procedure is an interpretation of the Beckhoff TS650x manual (vendor documentation of a commercial IEC 102 master), recorded traffic of one meter (Actaris SL762B) and the implementations gisce/iec870ree and daiiniel/IEC60870-5-102. Nothing here is a conformance claim.
- **The server has one independent peer.** `iec102ctl server start` was run against the controlling station of gisce/iec870ree v0.14.1 (the `iec102-gisce` image of otfabric/iec60870-5-interop v0.5.0, pinned by digest): 18 of 18 operations pass (`make interop`, `interop/iec102ctl`): link start-up and a link address nobody answers, the REE session and a refused key, product, clock, C_CI_NT_2 and C_CI_NU_2 with address ranges and the refusals 16, 17, 18, C_TA_VC_2 and C_TA_VM_2, C_CS_TA_2 and the clock read back. Only two octets of link and common address and the REE profile as gisce uses it are exercised; the other read commands (C_CI_NA_2 .. C_CI_NS_2), events, contracted powers and special days are not.
- **The client has no independent peer.** There is no reference controlled station (meter) in the interop harness: every open IEC 102 stack we found is a controlling station. `iec102ctl client` is evidenced only by the e2e suite against go-iec60870-5's own `iec102/server` (and against `iec102ctl server start`), which shares its codec with the client. A real meter may differ.
- **REE requests without a documented procedure.** C_PC_NA_2 (contracted powers) and C_DF_NA_2 (special days) are sent with cause 6 and completed by the activation termination, as C_TA_VC_2 is; no source we hold shows how a meter answers them. A meter that answers without a termination makes these commands time out (`request_timeout`); `client request --type C_PC_NA_2 --until reply` reads such a meter.
- **The REE quality bits** (VH, MP, IN, AL) are named after daiiniel/IEC60870-5-102 and the labels of the recorded traffic; their meaning beyond the name is not documented. Without the REE profile bits 0-4 are shown as the sequence number of the Beckhoff manual.
- **The product specification** (P_MP_NA_2) is shown as its three raw fields; the reading of the first octet as an edition date (gisce) has low confidence and is not applied.

## Not supported

- **No `client monitor`.** IEC 102 is request driven and go-iec60870-5 has no class 1 or spontaneous data for it: a meter sends only what is asked. Read periodically instead (`client totals` in a loop).
- **No `scan` or `discover`.** A line (serial port or terminal server connection) carries one controlling station and, in go-iec60870-5, one station per client; there is nothing on a TCP network to discover beyond the terminal server's port. A meter whose link address is unknown is found by trying `--link-address` values with a short `--link-timeout`.
- **No writing of contracted powers or configuration.** No source gives the layout of the REE requests that change them (C_MC_NA_2, C_MR_NA_2, C_MF_NA_2); `client request` can send one if you know its octets, at your own risk.
- **Raw types.** M_EI_NA_2, the REE configuration (M_RM_NA_2), the blocks (M_IB_TG_2, M_IB_TK_2) and the extension types 150..163 are shown as hex by `client request`.
- **Signatures** are shown, not checked: their computation is not documented.
- **Session enforcement.** The simulated meter answers its reads without an open session, as the contract's stations do; a real REE meter may refuse them.
- **One link address per line.** Neither the client nor the server polls or serves several stations on a shared (multi-drop) line.
- **The frame count bit is one bit** (IEC 60870-5-2): over a transport that could reorder frames a repetition may be taken as a new request. A serial line and one TCP connection deliver in order.
