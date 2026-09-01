# Configuration reference

One YAML file. Top-level keys: `seed`, `log`, `metrics`, `admin`, `defaults`,
`operators`.

Every field under an operator may also be set in `defaults`; an operator
inherits the default and overrides only the keys it names. Maps (`dlr.outcomes`,
`dlr.err_codes`) are merged key-by-key; scalars and lists are replaced.

## Top level

| Key | Type | Default | Meaning |
|---|---|---|---|
| `seed` | int | `0` | RNG seed for the whole process. `0` = derive from wall clock (non-reproducible). Any non-zero value makes latency, DLR outcomes and fault rolls repeatable. |
| `log.level` | string | `info` | `debug` \| `info` \| `warn` \| `error` |
| `log.format` | string | `text` | `text` \| `json` |
| `metrics.listen` | string | *(off)* | Address for the Prometheus `/metrics` endpoint, e.g. `":9090"`. Empty = disabled. |
| `admin.listen` | string | *(off)* | Address for the JSON admin API. Empty = disabled. |

## Operator

| Key | Type | Default | Meaning |
|---|---|---|---|
| `name` | string | — | Unique operator name (metric label, admin path). Required. |
| `listen` | string | — | `host:port` for this operator's SMPP listener. Must be unique. Use `:0` to let the OS pick (tests). Required. |
| `accounts` | list | — | ≥1 credential set. Required. |
| `accounts[].system_id` | string | — | Bind `system_id`. Required. |
| `accounts[].password` | string | `""` | Expected password. |
| `accounts[].system_type` | string | `""` | If set, the bind's `system_type` must match, else `ESME_RINVSYSTYP`. |
| `smpp_version` | string | `3.4` | `3.3` or `3.4`. On `3.4` a `bind_transceiver` declaring an interface version below `0x34` is refused. |
| `bind_types` | list | all | Subset of `tx`, `rx`, `trx`. A disallowed bind gets `ESME_RBINDFAIL`. |
| `max_binds` | int | `0` | Max concurrent bound sessions; `0` = unlimited. Over-limit → `ESME_RBINDFAIL`. |
| `window_size` | int | `0` | Max un-acked `submit_sm` in flight per session; `0` = unlimited. Over-window → `ESME_RMSGQFUL`. |
| `enquire_link_interval` | duration | `0` | If > 0, the server sends `enquire_link` on this cadence. |
| `session_idle_timeout` | duration | `0` | If > 0, a session with no inbound PDU for this long is closed. |
| `submit_resp_latency` | object | see below | Delay before each `submit_sm_resp`. |
| `throttle` | object | *(none)* | Per-operator submit rate limit. |
| `dlr` | object | enabled, all `DELIVRD` | Delivery-receipt behaviour. |
| `mo` | object | disabled | Reserved for scheduled MO generation (inject via admin API today). |
| `faults` | object | none | Protocol misbehaviour injection. |
| `concat.shared_message_id` | bool | `false` | Reuse one message id across the parts of a concatenated message (matches some carriers' DLR correlation). |
| `tls.cert` / `tls.key` | path | — | PEM cert/key; enables a TLS listener for this operator. |

### `submit_resp_latency`

| Key | Type | Meaning |
|---|---|---|
| `dist` | string | `fixed` (default), `uniform`, `exponential`. |
| `mean` | duration | Center of the distribution. |
| `min` / `max` | duration | `uniform`: bounds (if `max` unset, `[0, 2·mean]`). `exponential`: floor / cap. |
| `jitter` | duration | `fixed` only: add a uniform value in `[-jitter, +jitter]`. |

### `throttle`

Give **either** `tps` (+ optional `burst`) **or** `count` + `window`.

| Key | Type | Meaning |
|---|---|---|
| `tps` | float | Sustained submits/second. |
| `burst` | float | Bucket capacity. Defaults to `tps` (or `count`). |
| `count` | int | Submits allowed per `window`. |
| `window` | duration | Window for `count`. `count: 300, window: 60s` ⇒ 5/s sustained, burst 300. |

Over-rate `submit_sm` → `submit_sm_resp` with `command_status = ESME_RTHROTTLED (0x58)`.

### `dlr`

| Key | Type | Default | Meaning |
|---|---|---|---|
| `enabled` | bool | `true` | Master switch. Receipts are only sent when the submit's `registered_delivery` requests one. |
| `delay.min` / `delay.max` | duration | `0` / `0` | Uniform delay before the receipt. `0/0` = immediate. |
| `outcomes` | map | `{DELIVRD: 100}` | Stat word → weight. Keys: `DELIVRD`, `UNDELIV`, `EXPIRED`, `DELETED`, `ACCEPTD`, `UNKNOWN`, `REJECTD`. |
| `err_codes` | map | per-stat default | Stat word → integer `err:` value in the receipt (and `network_error_code` TLV). |
| `tlv` | bool | `false` | Append `receipted_message_id`, `message_state`, `network_error_code` TLVs. |
| `receipt_template` | string | see below | Text of the receipt `short_message`. |

**Default template**

```
id:{msgid} sub:001 dlvrd:{dlvrd} submit date:{submit} done date:{done} stat:{stat} err:{err} text:{text}
```

**Placeholders**

| Token | Value |
|---|---|
| `{msgid}` | message id returned in `submit_sm_resp` |
| `{stat}` | outcome stat word |
| `{err}` | outcome err code, zero-padded to 3 digits |
| `{submit}` | submit time, `YYMMDDhhmm` |
| `{done}` | receipt time, `YYMMDDhhmm` |
| `{sub}` | `001` |
| `{dlvrd}` | `001` if delivered, else `000` |
| `{text}` | first 20 chars of the original message |
| `{source}` / `{dest}` | original source / destination address |

### `faults`

| Key | Type | Meaning |
|---|---|---|
| `reject_bind_pct` | float 0–100 | Chance a bind is answered `ESME_RBINDFAIL` despite valid credentials. |
| `generic_nack_pct` | float 0–100 | Chance a `submit_sm` is answered `generic_nack`. |
| `submit_reject_pct` | float 0–100 | Chance a `submit_sm` is answered `ESME_RSUBMITFAIL`. |
| `drop_after` | duration | Close a bound session this long after it binds (`0` = never). |
