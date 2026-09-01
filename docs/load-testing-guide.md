# Load-testing guide

The goal: put a realistic **operator ceiling** in front of your SMPP client so a
load test measures how *your* code behaves when carriers throttle, add latency,
and return failed receipts — not just how fast a mock can echo.

## 1. Model your fleet

One operator block per real carrier endpoint. Match the things that shape
behaviour under load:

- **`listen` port** — one per operator, matching your client's per-operator
  config/topology.
- **`throttle`** — the carrier's real TPS (`count`/`window` if that's how the
  contract reads).
- **`submit_resp_latency`** — a carrier round-trip. `exponential, mean: 35ms,
  max: 300ms` is a reasonable starting point; raise `mean` to model a slow
  carrier.
- **`window_size`** — the SMPP window the carrier grants (often 10).
- **`dlr`** — delay range and a realistic `outcomes` mix so your DLR pipeline is
  under load too.

See [`examples/uz-fleet.yaml`](../examples/uz-fleet.yaml).

## 2. Run it

```bash
smsc-sim -config examples/uz-fleet.yaml
# or the whole fleet in Docker:
docker compose -f deploy/docker-compose.yml up
```

Pin `seed` to a fixed value so two runs are comparable.

## 3. Drive load

Point your client/gateway at the operator ports and generate traffic with your
normal producer (Kafka, a script, a bench tool). Push **past** the configured TPS
on at least one operator so you exercise the `ESME_RTHROTTLED` path.

## 4. Read the result

Scrape `:9090/metrics` (or wire it into Prometheus/Grafana):

| Metric | What it tells you |
|---|---|
| `smscsim_submit_sm_total{result="accepted"}` vs `{result="throttled"}` | how much of your offered load the carrier absorbed |
| `smscsim_submit_resp_latency_seconds` | the latency your client actually saw |
| `smscsim_dlr_total{stat=…}` | receipt volume and outcome mix delivered back |
| `smscsim_active_sessions` | did your client hold its binds, or churn them |
| `smscsim_pdu_rx_total` / `smscsim_pdu_tx_total` | PDU throughput per operator |

Compare against your client's own metrics: if `smscsim_submit_sm_total` accepted
≈ your send rate but your end-to-end throughput lags, the bottleneck is in your
code, not the "carrier".

## 5. Failure-mode drills

- **Throttle storm** — set one operator's `throttle.tps` well below your load;
  confirm your client backs off / queues rather than hot-looping or dropping.
- **Slow carrier** — `submit_resp_latency.mean: 250ms`; confirm your window
  accounting and connection pool cope.
- **Failed deliveries** — `dlr.outcomes: {DELIVRD: 60, UNDELIV: 40}` with a
  carrier-specific `err_codes`; confirm your DLR classifier and retry logic fire.
- **Bind flaps** — `faults.drop_after: 45s`; confirm reconnect/backoff behaves.
- **Bind rejection** — `faults.reject_bind_pct: 20`; confirm your client retries
  instead of wedging.

## CI

Use [`examples/ci.yaml`](../examples/ci.yaml): zero latency, immediate receipts,
fixed seed. Start `smsc-sim` as a service step, run your integration tests
against `127.0.0.1:2775`, assert on both the happy path and (via the `sink-fail`
operator on `:2776`) the `UNDELIV` path.
