# Protocol Coverage & SMPP v3.3, v3.4, v5.0 Specification Alignment

`smsc-sim` implements the SMSC / Message Centre (MC) side of **SMPP v3.3, SMPP v3.4 Issue 1.2, and SMPP v5.0** (as specified on [smpp.org](https://smpp.org/)), providing comprehensive capabilities for functional integration testing, load resilience, and fault generation.

## Supported PDUs

| PDU | Direction | Support & Features |
|---|---|---|
| `bind_transmitter` / `_receiver` / `_transceiver` | ESME → SMSC | Full. Credentials validated per account; `sc_interface_version` TLV returned on 3.4 (`0x34`) and 5.0 (`0x50`). |
| `bind_*_resp` | SMSC → ESME | Full. `system_id` = `smsc-sim`, optional `sc_interface_version` TLV. |
| `unbind` / `unbind_resp` | both | Full unbind lifecycle with metrics & events. |
| `enquire_link` / `_resp` | both | Full. Bidirectional keepalive / heartbeat with configurable interval. |
| `submit_sm` | ESME → SMSC | Mandatory fields + optional TLVs parsed. Latency sampling, token-bucket throttling, window enforcement, dynamic keyword simulation, configured fault injection, and asynchronous receipt scheduling. |
| `submit_sm_resp` | SMSC → ESME | Full, with `message_id`; full catalogue of 51 standard error status codes, plus SMPP v5.0 `congestion_state` TLV (`0x0428`). |
| `data_sm` | ESME → SMSC | Full alternative submit mechanism using `message_payload` TLVs. |
| `data_sm_resp` | SMSC → ESME | Full with `message_id` and optional TLVs. |
| `submit_multi` / `_resp` | both | Full. Supports multi-destination submissions with `submit_multi_resp`. |
| `query_sm` / `_resp` | both | Full. Returns message state, final timestamp, and delivery outcome. |
| `cancel_sm` / `_resp` | both | Full. Accepts cancel requests and responds with `ESME_ROK`. |
| `replace_sm` / `_resp` | both | Full. Accepts replace requests and responds with `ESME_ROK`. |
| `broadcast_sm` / `_resp` | both | Full SMPP v5.0 Cell Broadcast Center (CBC) messaging. |
| `query_broadcast_sm` / `_resp` | both | Full SMPP v5.0 broadcast status queries. |
| `cancel_broadcast_sm` / `_resp` | both | Full SMPP v5.0 broadcast cancellation. |
| `deliver_sm` | SMSC → ESME | Delivery receipts (DLR) and mobile-originated (MO) message delivery. Supports SMPP v5.0 registered delivery modes (success-only, failure-only, both). |
| `deliver_sm_resp` | ESME → SMSC | Accepted and tracked in session metrics. |
| `generic_nack` | both | Emitted on invalid command ID or simulated as a network fault. |

## SMPP v5.0 Enhancements

- **Interface Version**: Advertises `0x50` in `bind_*_resp`.
- **Congestion Control**: Implements `ESME_RCONGESTION` (`0x00000059`) with `congestion_state` TLV (`0x0428`, 0-100% load level).
- **Cell Broadcast**: Implements `broadcast_sm`, `query_broadcast_sm`, `cancel_broadcast_sm`.
- **Registered Delivery Modes**:
  - `0x01`: Success and failure receipts (v3.4 / v5.0)
  - `0x02`: Failure receipts only (v3.4 / v5.0)
  - `0x03`: Successful delivery only (SMPP v5.0 specific)
- **TLVs Recognized**:
  - `congestion_state` (`0x0428`)
  - `ussd_service_op` (`0x0501`)
  - `billing_identification` (`0x060B`)
  - `source_network_id` (`0x060D`) / `source_node_id` (`0x060E`)
  - `dest_network_id` (`0x060F`) / `dest_node_id` (`0x0610`)
  - `dest_addr_np_country` (`0x0100`) / `dest_addr_np_information` (`0x0102`) / `dest_addr_np_resolution` (`0x0103`)
  - `alert_on_message_delivery` (`0x0422`)
  - `network_error_code` (`0x0423`)
  - `message_state` (`0x0427`)
  - `receipted_message_id` (`0x001E`)
  - `message_payload` (`0x0424`)
- **Status Codes**: Full 51-code catalogue including `ESME_RSERTYPUNAUTH`, `ESME_RPROHIBITED`, `ESME_RSERTYPUNAVAIL`, `ESME_RSERTYPDENIED`.
