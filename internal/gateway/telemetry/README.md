# internal/gateway/telemetry

## Responsibilities

`internal/gateway/telemetry` owns gateway-local observability primitives:

- Prometheus collectors and metric recording helpers
- metric label shaping and response-header projection
- dashboard rolling time-series store
- in-memory last-request output-rate tracker used by admin snapshots
- token observation coverage counters and exact-token throughput accounting

The package is intentionally runtime-agnostic. It does not select providers, run hooks, or own HTTP routing.
Protocol-specific usage parsing lives in `internal/gateway/tokenusage`.
