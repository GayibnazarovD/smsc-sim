# SMPP Protocol Implementation Conformance Statement (PICS)

This document follows the standardized **PICS proforma** referenced on [smpp.org/smpp-pics.html](https://smpp.org/smpp-pics.html) to detail protocol support for the **`smsc-sim`** open-source simulator.

---

## 1. Implementation Identification

- **Implementation Name**: `smsc-sim` (SMSC Simulator & Gateway Mock)
- **Vendor / Author**: Dilshod Gayibnazarov ([GitHub: GayibnazarovD/smsc-sim](https://github.com/GayibnazarovD/smsc-sim))
- **Supported Protocol Versions**: SMPP v3.3, SMPP v3.4 Issue 1.2, SMPP v5.0
- **Supported Roles**: SMSC / Message Centre (MC)
- **Transport**: TCP/IP, TLS 1.2 / 1.3

---

## 2. PICS Questionnaire: Operations & Commands

| Command | Reference | Supported | Notes |
|---|---|---|---|
| `bind_receiver` / `_resp` | SMPP v3.4 §4.1.1 | **YES** | Authentication via system_id & password; enforces bind type rules. |
| `bind_transmitter` / `_resp` | SMPP v3.4 §4.1.2 | **YES** | Authentication via system_id & password; enforces bind type rules. |
| `bind_transceiver` / `_resp` | SMPP v3.4 §4.1.5 | **YES** | Full duplex bind; returns `sc_interface_version` TLV (0x34 or 0x50). |
| `outbind` | SMPP v3.4 §4.1.6 | **NO** | Server-to-client outbind not initiated. |
| `unbind` / `_resp` | SMPP v3.4 §4.1.7 | **YES** | Graceful session termination. |
| `generic_nack` | SMPP v3.4 §4.1.8 | **YES** | Sent on invalid command_id or injected as a fault. |
| `submit_sm` / `_resp` | SMPP v3.4 §4.4.1 | **YES** | Supports short_message, message_payload TLV, TPS throttling, latency simulation, and fault injection. |
| `data_sm` / `_resp` | SMPP v3.4 §4.4.2 | **YES** | Supported for packet data / streaming applications. |
| `submit_multi` / `_resp` | SMPP v3.4 §4.5.1 | **YES** | Multi-destination submission. |
| `deliver_sm` / `_resp` | SMPP v3.4 §4.6.1 | **YES** | Generates delivery receipts (DLR) and mobile-originated (MO) SMS. |
| `query_sm` / `_resp` | SMPP v3.4 §4.8.1 | **YES** | Returns message status, delivery timestamp, and error code. |
| `cancel_sm` / `_resp` | SMPP v3.4 §4.9.1 | **YES** | Cancels queued/enroute messages. |
| `replace_sm` / `_resp` | SMPP v3.4 §4.10.1 | **YES** | Replaces short message text. |
| `enquire_link` / `_resp` | SMPP v3.4 §4.11.1 | **YES** | Both client-originated and server-originated heartbeats. |
| `alert_notification` | SMPP v3.4 §4.12.1 | **NO** | Optional alert notification. |
| `broadcast_sm` / `_resp` | SMPP v5.0 §4.4.3 | **YES** | SMPP v5.0 Cell Broadcast operation. |
| `query_broadcast_sm` / `_resp` | SMPP v5.0 §4.8.2 | **YES** | SMPP v5.0 Cell Broadcast query. |
| `cancel_broadcast_sm` / `_resp` | SMPP v5.0 §4.9.2 | **YES** | SMPP v5.0 Cell Broadcast cancellation. |

---

## 3. Registered Delivery Modes (SMPP v3.4 & v5.0)

| Mode | Bit Value | Description | Supported |
|---|---|---|---|
| No Receipt | `0x00` | No SMSC delivery receipt requested | **YES** |
| Success & Failure | `0x01` | Receipt requested for both delivered and failed states | **YES** |
| Failure Only | `0x02` | Receipt requested only when delivery fails | **YES** |
| Success Only | `0x03` | Receipt requested only when delivery succeeds (SMPP v5.0) | **YES** |

---

## 4. Optional Parameters (TLVs)

| TLV Tag | Hex | Name | Supported |
|---|---|---|---|
| `0x0005` | `dest_addr_subunit` | Destination Address Subunit | **YES** |
| `0x000D` | `source_addr_subunit` | Source Address Subunit | **YES** |
| `0x001E` | `receipted_message_id` | Delivery Receipt Message ID | **YES** |
| `0x0100` | `dest_addr_np_country` | SMPP v5 Number Portability Country Code | **YES** |
| `0x0102` | `dest_addr_np_information` | SMPP v5 Number Portability Information | **YES** |
| `0x0103` | `dest_addr_np_resolution` | SMPP v5 Number Portability Resolution | **YES** |
| `0x0204` | `user_message_reference` | Client Message Reference | **YES** |
| `0x020C` | `sar_msg_ref_num` | Segmentation and Reassembly Reference | **YES** |
| `0x020E` | `sar_total_segments` | Total Segments in Multipart SMS | **YES** |
| `0x020F` | `sar_segment_seqnum` | Sequence Number of Segment | **YES** |
| `0x0210` | `sc_interface_version` | SMSC Supported Interface Version | **YES** |
| `0x0422` | `alert_on_message_delivery`| Alert On Delivery (CDMA / v5) | **YES** |
| `0x0423` | `network_error_code` | GSM/CDMA Network Specific Error Code | **YES** |
| `0x0424` | `message_payload` | Extended Message Body | **YES** |
| `0x0426` | `more_messages_to_send` | Streaming / Batch Indicator | **YES** |
| `0x0427` | `message_state` | Delivery Receipt Message State | **YES** |
| `0x0428` | `congestion_state` | SMPP v5 Congestion Load Level (0-100) | **YES** |
| `0x0501` | `ussd_service_op` | SMPP v5 USSD Service Operation | **YES** |
| `0x060B` | `billing_identification` | SMPP v5 Billing Identification Pass-Through | **YES** |
| `0x060D` - `0x0610` | `source/dest_network/node_id` | SMPP v5 Carrier Routing Identifiers | **YES** |

---

## 5. Command Status (Error Codes) Conformance

All 51 standard SMPP error codes defined on [smpp.org/smpp-error-codes.html](https://smpp.org/smpp-error-codes.html) are implemented and can be simulated either dynamically via message keywords (`[STATUS:0x...]`) or via configured failure rates in the Web Admin UI:

- `ESME_ROK` (`0x00000000`)
- `ESME_RINVMSGLEN` (`0x00000001`) through `ESME_RUNKNOWNERR` (`0x000000FF`)
- SMPP v5.0 Codes: `ESME_RSERTYPUNAUTH` (`0x00000100`), `ESME_RPROHIBITED` (`0x00000101`), `ESME_RSERTYPUNAVAIL` (`0x00000102`), `ESME_RSERTYPDENIED` (`0x00000103`), `ESME_RCONGESTION` (`0x00000059`)
