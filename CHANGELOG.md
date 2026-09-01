# Changelog

All notable changes to this project are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/); versions follow
[SemVer](https://semver.org/).

## Unreleased

### Added
- Multi-operator SMPP v3.3/v3.4 SMSC simulator: one TCP listener per operator,
  each with its own accounts, bind-type policy and `max_binds`.
- Per-account bind authentication (`system_id` / `password` / optional
  `system_type`) with spec error codes.
- Per-operator submit_sm throttling (token bucket, `tps`+`burst` or
  `count`+`window`) returning `ESME_RTHROTTLED`; window enforcement returning
  `ESME_RMSGQFUL`.
- Configurable `submit_sm_resp` latency: `fixed` (± jitter), `uniform`,
  `exponential`.
- Delivery-receipt engine: delay range, weighted outcome mix, per-outcome `err`
  codes, templated receipt text, optional receipt TLVs, shared message-id for
  concatenated SMS.
- Mobile-originated `deliver_sm` injection via the admin API.
- Fault injection: `reject_bind_pct`, `generic_nack_pct`, `submit_reject_pct`,
  `drop_after`.
- Native per-operator TLS listeners.
- Prometheus `/metrics`, JSON `/admin` API, structured logging, deterministic
  `seed`.
- Config inheritance: operators overlay a shared `defaults` block.
- Docker image, GoReleaser config, GitHub Actions CI.
