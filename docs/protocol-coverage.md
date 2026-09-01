# Protocol coverage & limitations

`smsc-sim` implements the SMSC side of enough SMPP v3.4 (and v3.3 binds) to
exercise a real ESME under load. It is **not** a conformance test suite.

## PDUs

| PDU | Direction | Support |
|---|---|---|
| `bind_transmitter` / `_receiver` / `_transceiver` | ESME → SIM | Full. Credentials validated; `sc_interface_version` TLV returned on 3.4. |
| `bind_*_resp` | SIM → ESME | Full. `system_id` = `smsc-sim`. |
| `unbind` / `unbind_resp` | both | Full. |
| `enquire_link` / `_resp` | both | Full. Server-initiated when `enquire_link_interval` > 0. |
| `submit_sm` | ESME → SIM | Mandatory fields + TLVs parsed. Latency, throttle, window, faults, receipt scheduling. |
| `submit_sm_resp` | SIM → ESME | Full, with `message_id`; error statuses on throttle/window/fault/bind-state. |
| `deliver_sm` | SIM → ESME | Delivery receipts and mobile-originated messages. |
| `deliver_sm_resp` | ESME → SIM | Accepted and counted. |
| `generic_nack` | both | Sent for unknown command id and as a fault; inbound ignored. |
| `data_sm`, `query_sm`, `replace_sm`, `cancel_sm`, `submit_multi` | — | **Not implemented** — answered with `generic_nack (ESME_RINVCMDID)`. |

## Encoding

- `data_coding` is stored and echoed, not transcoded. Message bytes are treated
  as opaque.
- Concatenated SMS: UDH (`0x00` / `0x08` IEI) and `sar_msg_ref_num` are
  recognised only to derive a stable key for `concat.shared_message_id`. Parts
  are **not** reassembled into one logical message.
- `message_payload` TLV is accepted as the message body when `short_message` is
  empty.

## Deliberate non-goals

- No message persistence, `query_sm` lifecycle, or `replace_sm` / `cancel_sm`.
- No SMPP v5.0.
- No MO from a CSV/script yet — inject via `POST /admin/operators/{name}/mo`.
- No billing, routing, or number-portability logic — outcomes are driven purely
  by the configured probability weights.

## Hard limits

| Limit | Value | Rationale |
|---|---|---|
| Max inbound PDU | 1 MiB | guard against a hostile `command_length` |
| `short_message` on encode | 254 octets | SMPP field is one octet of length |
| Read buffer | 4 KiB per session | grows as needed by `bufio` |
