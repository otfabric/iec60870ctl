# JSON output

Every command of the four tools that prints a result takes `--format json` (or the tool's
`<TOOL>_FORMAT=json`). The shapes are a stable contract for scripts: a breaking change to
a field name or shape is called out in [RELEASE.md](../RELEASE.md).

## Shared by the four tools

- Results go to stdout, one JSON document per command, or JSON Lines for streaming
  commands (`client monitor`, `client ping`, `server start`). Progress and diagnostics go
  to stderr.
- Field names are snake_case. Repeated rows are arrays of objects, empty rather than
  `null`.
- A row that failed carries an `error` object with `kind`, `code` and `message`; it is
  omitted when the row is fine.
- A fatal error is one object on stdout and nothing else:

  ```json
  {"error": {"kind": "timeout", "code": "request_timeout", "message": "..."}}
  ```

  `kind` decides the exit code, `code` is stable for machines, `message` is for people.
  The values are listed in [exitcodes.md](exitcodes.md).

## Per tool

| Tool | Reference |
|------|-----------|
| iec101ctl | [iec101ctl/json-output.md](iec101ctl/json-output.md) |
| iec102ctl | [iec102ctl/json-output.md](iec102ctl/json-output.md) |
| iec103ctl | [iec103ctl/json-output.md](iec103ctl/json-output.md) |
| iec104ctl | [iec104ctl/json-output.md](iec104ctl/json-output.md) |
