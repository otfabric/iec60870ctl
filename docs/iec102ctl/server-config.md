# iec102ctl meter model

`iec102ctl server start --config <file>` serves the integrated total station a model file describes. The file is YAML or JSON (JSON is YAML); an unknown field is an error, so a misspelt name does not go unnoticed. `iec102ctl server generate-config` writes the commented model of the built-in demo meter to start from.

Times in the model are the meter's local time without a zone, read in the zone of `--timezone` (UTC by default), which also sets the SU bit: time a `2026-01-01T01:00` (or a date, `2026-01-01`, for midnight), time b `2026-01-15T10:20:30.500`. IEC 102 carries the years 2000-2099 only.

## `station`

| Field | Meaning |
|-------|---------|
| `name` | shown in the event log |
| `link_address` | the meter's link address (default 1); `--link-address` replaces it |
| `common_address` | the measuring point address, the common address of ASDU (default 1); `--common-address` replaces it. A request to another is refused with cause 16 |
| `clock` | `running` (the default: this computer's clock) or a time b at which the clock **stands still** until a controlling station sets it (C_CS_TA_2), as the interop fixtures do so that reads are deterministic. A time set lives as long as the process: a running clock keeps the offset, a still one stands at the new time |
| `product` | the answer to C_RD_NA_2 (P_MP_NA_2): `standard` (the first octet), `manufacturer` (an octet), `product` (four octets) |

## `totals`

A list of integrated totals of one record address and one family each.

| Field | Meaning |
|-------|---------|
| `record` | the record address (11: period 1, 12: period 2, 21: daily values, 1: accounting period, …) |
| `kind` | `operational` (or `absolute`, served as M_IT_TG_2), `reset-operational` (or `incremental`, M_IT_TK_2), `accounting` (M_IT_TA_2), `reset-accounting` (M_IT_TD_2) |
| `type` | instead of `kind`: the type the periods are served as, M_IT_TA_2 .. M_IT_TM_2 (2, 3 or 4 octet counters) |
| `signed` | add the signature octet to every total (its value from `signature`) |
| `periods` | the integration periods, **oldest first**: `end` (time a, the end of the period) and `totals` (`address`, `value`, `quality` (the octet as sent), `signature`) |
| `generate` | more periods after the listed ones: `start` (the end of the first), `interval` (`1h`, `15m`; at least `1m`), `count`, `totals` (`address`, `start`, `step`). An absolute total starts at `start` and grows by `step` each period; a periodical reset total is `step` in every period |

A record may hold one list per family (absolute and incremental totals of record 11, as REE meters have). Period ends must rise strictly within a list, an address may appear once per period, and a value must fit the counter width of the type.

The meter serves every read command C_CI_NA_2 .. C_CI_NU_2 from these lists: a read selects the family of its type, then the oldest period, the period that ends at the requested time, or the periods that end within the requested range (inclusive, to the minute, ignoring the flags), and of each the totals within the address range. It confirms (cause 7), sends one ASDU per period (cause 5; a period longer than a frame is split), and terminates (cause 10). Refusals: cause 16 for a record without totals (or another measuring point), 14 for a record without totals of the family, 18 when no period matches, 17 when no total is in the address range. These are go-iec60870-5's `iec102/server` and its interpretation of the causes.

## `events`

| Field | Meaning |
|-------|---------|
| `record` | the list address, normally 50-55 |
| `points` | single-point information with time tag: `address`, `on`, `qualifier` (0-127), `time` (time b, required) |

C_SP_NA_2 reads a whole list, C_SP_NB_2 the points whose time lies in the range; a list address the model does not have is refused with cause 16.

## `ree`

The Spanish REE profile; without this section the meter is plain IEC 102 and refuses every REE type with cause 14.

| Field | Meaning |
|-------|---------|
| `access_key` | the key C_AC_NA_2 must carry (0-4294967295). A wrong key is answered with the P/N bit; C_FS_NA_2 is confirmed; C_CS_TA_2 sets the clock and is confirmed. The reads do not depend on an open session |
| `contracted_powers` | the answer to C_PC_NA_2 (M_PC_NA_2, with the meter's clock as time a): `address` (tariff period), `power_kw`; at most 127 |
| `special_days` | the answer to C_DF_NA_2 (M_DF_NA_2): times a or dates; at most 127 |
| `tariffs` | per contract `register` (134-139): `current` records answer C_TA_VC_2, `stored` records C_TA_VM_2 (those whose `period_end` lies in the requested range); one M_TA_VC_2 / M_TA_VM_2 per record. Record fields: `address`, `active_abs`, `active_inc`, `active_quality`, `reactive_ind_abs`, `reactive_ind_inc`, `reactive_ind_quality`, `reactive_cap_abs`, `reactive_cap_inc`, `reactive_cap_quality`, `max_power`, `max_power_time` (time a), `max_power_quality`, `excess_power`, `excess_power_quality`, `period_start`, `period_end` (times a) |

The REE reads are confirmed (cause 7), answered and terminated (cause 10); an unknown register is refused with cause 16, no matching record with 18, no powers or days with 17. This procedure is iec102ctl's: the one of C_TA_VC_2 and C_TA_VM_2 is that of the interop contract, which gisce/iec870ree follows; for C_PC_NA_2 and C_DF_NA_2 no source we hold shows one.

## Limits

A model file is at most 16 MiB; at most 10000 totals lists, 100000 periods and 100000 events and tariff records in all.

## Example

```yaml
station:
  name: Feeder 7 meter
  link_address: 1
  common_address: 1
  clock: running
  product: {standard: 42, manufacturer: 33, product: 20261115}
totals:
  - record: 11
    kind: absolute
    periods:
      - end: 2026-01-01T01:00
        totals: [{address: 1, value: 1000}, {address: 3, value: 200, quality: 16}]
    generate: {start: 2026-01-01T02:00, interval: 1h, count: 23, totals: [{address: 1, start: 1450, step: 450}, {address: 3, start: 240, step: 40}]}
events:
  - record: 52
    points: [{address: 1, on: false, time: "2026-01-01T03:12:44.120"}]
ree:
  access_key: 7
  contracted_powers: [{address: 1, power_kw: 15}]
```
