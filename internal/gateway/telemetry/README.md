# internal/gateway/telemetry

## Responsibilities

`internal/gateway/telemetry` owns gateway-local observability primitives:

- Prometheus collectors and metric recording helpers
- metric label shaping and response-header projection
- dashboard rolling time-series store
- in-memory output-rate tracker used by dashboard snapshots

The package is intentionally runtime-agnostic. It does not select providers, run hooks, or own HTTP routing.
