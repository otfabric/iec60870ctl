# Process exit codes

`iec104ctl` uses stable exit codes for scripting. A fatal error is rendered once: as one `error:` line on stderr, or as a single JSON `error` object on stdout when `--format json` is active.

| Code | Meaning |
|------|---------|
| 0 | Success (`ExitOK`) |
| 2 | Usage (`KindUsage`): unknown command or flag, a missing required flag, a value a flag cannot hold |
| 3 | Invalid input (`KindInvalidInput`): a flag value, combination of flags, environment variable or station model that does not make sense. Nothing was sent |
| 4 | Transport (`KindTransport`): the connection cannot be made (refused, unreachable, TLS failure), the secure session of IEC 62351-5 cannot be established, the connection is lost, or the server cannot listen |
| 5 | Timeout (`KindTimeout`): the station did not answer a request in time, or did not acknowledge within t1 |
| 6 | Protocol (`KindProtocol`, `KindNegative`): the station refused the request (negative confirmation, or "unknown type / cause / common address / information object address"), or violated the protocol |
| 7 | Partial success (`ExitPartial`): a **valid** result was written to stdout, and it holds failures: an address of `client read` that was refused or not answered, or a failed test of `client ping` |
| 10 | Output / internal (`KindOutput`, `KindInternal`): the result could not be written, or something unexpected happened |
| 130 | Interrupted (`KindInterrupted`): Ctrl-C or SIGTERM before a one-shot command had its result |

**Kind vs code:** `kind` drives the exit code; `code` in the JSON error object is stable for machines. Do not parse the human `message`.

| `kind` | `code` values |
|--------|---------------|
| `usage`, `invalid_input` | `invalid_input`, `invalid_format`, `invalid_url`, `invalid_flag_combination`, `invalid_config`, `input_file_open_failed`, `output_file_create_failed` |
| `transport` | `transport_connect_failed`, `transport_tls_failed`, `security_failed`, `connection_lost`, `listen_failed` |
| `timeout` | `request_timeout` (no answer to a request), `link_timeout` (t1: no acknowledgement) |
| `negative_confirmation` | `negative_confirmation`, `unknown_type`, `unknown_cause`, `unknown_common_address`, `unknown_object_address` |
| `protocol` | `protocol_violation`, `data_transfer_not_started`, `request_busy` |
| `output` | `json_encode_failed`, `table_render_failed`, `output_write_failed`, `output_file_write_failed`, `output_file_create_failed` |
| `internal` | `internal_unexpected` |
| `interrupted` | `context_canceled` |

**Partial exit:** only after a success payload was written. A failure to connect, to validate or to write is fatal and uses the codes above, never `7`.

**Streaming commands:** `client monitor`, `client ping` and `server start` write one record at a time. Stopping them with Ctrl-C (SIGINT) or SIGTERM ends the stream normally and does not change the exit code. `client ping` keeps testing after a failed test, which it reports as a record carrying an `error`, and exits `7` when at least one test failed; a test that fails because the connection is gone ends it with the fatal code of that error. `client monitor` exits `0` when it ends by `--count`, `--duration` or a signal, and `4` when the connection is lost and `--no-reconnect` is set.

**Security (`--secure`):** a security flag without `--secure`, `--secure` without `--security-cert`, `--security-key` and `--security-trust`, a file that cannot be read or used, or an unknown algorithm or role exits `3` (`invalid_flag_combination` or `invalid_input`) before anything is sent or the server listens. `security_failed` (exit `4`) means that the association of IEC 62351-5 was not established after STARTDT: a certificate one side does not trust, an algorithm the station does not accept, a MAC that did not verify, or no answer to the key management within `--timeout`; nothing but the key management was sent. A request the station's roles do not permit is a `negative_confirmation` with cause `not-authorized` (17), exit `6`. A client without `--secure` gets no answer from a secure station: `request_timeout`, exit `5`.

**Commands that change a station:** `client command`, `client clock`, `client reset`, `client counters` with a freeze or reset, and `client file put` validate everything before sending; a rejected invocation exits `3` and has sent nothing. A refusal by the station exits `6`, a missing answer `5`. Nothing is ever sent a second time.

**`--debug`:** adds the frame trace and, after a fatal error, the chain of causes (`cause: …` lines) on stderr. It does not change stdout or the exit code.

**Organisation table:** these codes follow the OT Fabric CLI exit-code table. iec104ctl does not use `1`, `8` and `9`: they keep their organisation meaning and are not reused for anything else.

**Format variable:** `IEC104CTL_FORMAT` is the name to use; `IEC104CTL_OUTPUT_FORMAT` stays accepted. When both are set, `IEC104CTL_FORMAT` wins.
