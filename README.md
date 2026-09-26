# smsc-sim

A modern, single-binary **SMPP SMSC simulator** for load and integration testing
of SMPP clients and SMS gateways.

Point your ESME / SMS router at a realistic **fleet of operator endpoints** —
each with its own TCP port, credentials, TPS ceiling, response-latency curve and
delivery-receipt behaviour — all from one YAML file.

```
                    ┌──────────────── smsc-sim ────────────────┐
   your SMPP    ──▶ │ :2775  Beeline   80 tps   DLR 92/5/2/1    │
   client /         │ :2776  Ucell     50 tps   UNDELIV err 1282│
   gateway     ──▶ │ :2777  MobiUz    300/60s  shared concat id│
                    │ :2778  Uzmobile  50/1s                    │
                    └── /metrics (Prometheus)  /admin (JSON) ───┘
```

## Why

| Tool | Gap this project closes |
|---|---|
| melroselabs OSS SMSC | no auth, no config, no throttling, unmaintained |
| SMPPSim (Java)       | one port for all accounts, no submit-side TPS limit, no response-latency model |
| roll-your-own mock   | re-implemented on every project, never quite realistic |

`smsc-sim` models the two operator behaviours that actually matter under load —
a **per-operator TPS ceiling that returns `ESME_RTHROTTLED`** and **carrier
response latency** — plus configurable, delayed, partly-failing delivery
receipts.

## Install

```bash
go install github.com/dilshodgayibnazarov/smsc-sim/cmd/smsc-sim@latest
# or
docker run --rm -p 2775:2775 -p 9090:9090 \
  -v "$PWD/examples/single-operator.yaml:/etc/smsc-sim.yaml" \
  ghcr.io/dilshodgayibnazarov/smsc-sim:latest -config /etc/smsc-sim.yaml
```

Prebuilt binaries for linux/macOS (amd64/arm64) are attached to each
[release](https://github.com/dilshodgayibnazarov/smsc-sim/releases).

## Run

```bash
smsc-sim -config examples/uz-fleet.yaml
```

```yaml
# examples/single-operator.yaml
seed: 0                       # non-zero = reproducible run
metrics: { listen: ":9090" }
admin:   { listen: ":8080" }

operators:
  - name: local
    listen: "127.0.0.1:2775"
    accounts:
      - { system_id: esme, password: s3cret }
    window_size: 10
    submit_resp_latency: { dist: fixed, mean: 30ms, jitter: 10ms }
    throttle: { tps: 100 }
    dlr:
      delay: { min: 2s, max: 10s }
      outcomes: { DELIVRD: 95, UNDELIV: 4, EXPIRED: 1 }
```

Every operator inherits the top-level `defaults` block and overrides only what
differs — see [`examples/uz-fleet.yaml`](examples/uz-fleet.yaml).

## What it simulates

- **SMPP v3.3 / v3.4 / v5.0**: Complete protocol support including `0x50` interface version
  negotiation, `congestion_state` TLV (`0x0428`), `bind_transmitter` / `bind_receiver` / `bind_transceiver`,
  `unbind`, `enquire_link` (both directions), `submit_sm`, `deliver_sm`
  (delivery receipts **and** mobile-originated), and `generic_nack`.
- **Universal Error Simulation**:
  - **Dynamic Message Keywords**: Inject errors on-the-fly per message without changing configs:
    - `[ERR_THROTTLED]` &rarr; Returns `0x58` (`ESME_RTHROTTLED`)
    - `[ERR_CONGESTION]` &rarr; Returns `0x59` (`ESME_RCONGESTION`) with `congestion_state` TLV
    - `[ERR_MSGQFUL]` &rarr; Returns `0x14` (`ESME_RMSGQFUL`)
    - `[ERR_INVDEST]` / `[ERR_INVSRC]` &rarr; Returns `0x0B` / `0x0A`
    - `[STATUS:0x..]` or `[STATUS:name]` &rarr; Returns any of the 50 standard SMPP status codes
    - `[ERR_DROP]` &rarr; Silently drops the PDU (simulating socket timeout/hang)
    - `[ERR_NACK]` &rarr; Sends `generic_nack`
    - `[DLR:UNDELIV:1282]` &rarr; Forces asynchronous delivery receipt with failure & network error code
    - `[DLR:DROP]` &rarr; Simulates dropped/lost delivery receipt
    - `[DLR_DELAY:5s]` &rarr; Overrides delivery receipt arrival delay
  - **Configured Fault Injection**: Set failure rates (%) and target error status codes per operator via the Web UI or YAML.
- **Real bind auth** — `system_id` / `password` (and optional `system_type`) are
  validated per account; wrong credentials get `ESME_RINVPASWD` / `ESME_RINVSYSID`.
- **Per-operator throttling** — token bucket (`tps`+`burst`, or `count`+`window`);
  over-rate `submit_sm` gets **`ESME_RTHROTTLED`**.
- **Window enforcement** — more than `window_size` un-acked `submit_sm` gets
  `ESME_RMSGQFUL`.
- **Response latency** — `fixed` (± jitter), `uniform` or `exponential`
  distribution before each `submit_sm_resp`.
- **Delivery receipts** — configurable delay range, weighted outcome mix
  (`DELIVRD` / `UNDELIV` / `EXPIRED` / `DELETED` / `ACCEPTD` / `UNKNOWN` /
  `REJECTD`), per-outcome `err` code, templated receipt text, optional
  `message_state` / `receipted_message_id` / `network_error_code` TLVs.
- **Concatenated SMS** — reassembly-aware; optional shared message-id across parts.
- **Mobile-originated** — inject via `POST /admin/operators/{name}/mo`.
- **Fault injection** — `reject_bind_pct`, `generic_nack_pct`,
  `submit_reject_pct`, `drop_after`.
- **TLS listeners** — native, per operator.
- **Observability** — Prometheus `/metrics`, JSON `/admin`, structured logs,
  deterministic RNG via `seed`.

Full field-by-field reference: [`docs/config-reference.md`](docs/config-reference.md).
Protocol coverage and known limits: [`docs/protocol-coverage.md`](docs/protocol-coverage.md).
Load-testing recipe: [`docs/load-testing-guide.md`](docs/load-testing-guide.md).

## Web Admin Dashboard & HTTP API

`smsc-sim` ships with an **embedded single-page Web Admin UI** served directly from the binary on the admin HTTP port (default `http://localhost:8080/`). No Node.js or web server required.

### Features
- **Fleet Overview**: Live cards for each operator with bind counts, active ports, TPS limits, window sizes, and DLR statuses.
- **Active Sessions Inspector**: Real-time table of connected client sockets (TX, RX, TRX) with remote IPs, system IDs, in-flight queues, and one-click session disconnect for fault drills.
- **MO Studio**: Interactive form to inject Mobile-Originated (`deliver_sm`) messages with preset templates (OTP, delivery alerts, bank notifications) and a live GSM-7 / UCS-2 character counter and segment calculator.
- **Live Activity Stream**: Real-time circular log of recent simulator events (binds, unbinds, throttled submissions, DLRs, errors).
- **Config Viewer**: Formatted JSON/YAML runtime configuration inspector with one-click clipboard copy.

### Admin HTTP API Endpoints

| Method & path | Purpose |
|---|---|
| `GET /` | Web Admin UI dashboard |
| `GET /ui/*` | Embedded static dashboard assets (CSS, JS) |
| `GET /healthz` | Simulator liveness check (`200 OK`) |
| `GET /admin/overview` | High-level fleet telemetry (active binds, total messages, uptime) |
| `GET /admin/operators` | Per-operator snapshot (accounts, bind types, active binds, messages seen) |
| `GET /admin/sessions` | Active connected ESME client sockets |
| `POST /admin/sessions/{id}/disconnect` | Forcibly close a client socket to simulate link drops |
| `GET /admin/events` | Recent simulator events ring buffer (`?limit=100`) |
| `GET /admin/config` | Active parsed runtime configuration |
| `POST /admin/operators/{name}/mo` | Inject a mobile-originated SMS: `{"source","dest","text","data_coding"}` |

## Metrics

`smscsim_binds_total`, `smscsim_active_sessions`, `smscsim_submit_sm_total`
(`result=accepted|throttled|queue_full|rejected`),
`smscsim_submit_resp_latency_seconds`, `smscsim_dlr_total` (`stat=…`),
`smscsim_mo_total`, `smscsim_pdu_rx_total`, `smscsim_pdu_tx_total` — all labelled
by `operator`.

## Development

```bash
go test ./...
go test -race ./internal/smsc/...
golangci-lint run
```

The SMPP wire codec lives in [`internal/smpp`](internal/smpp) and is small enough
to read in one sitting; it also powers the integration tests' throwaway client.

## License

[Apache-2.0](LICENSE).
