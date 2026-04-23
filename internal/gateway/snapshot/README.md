# internal/gateway/snapshot

## Responsibilities

`internal/gateway/snapshot` owns admin-facing runtime snapshot assembly:

- Build dashboard metrics payloads from Prometheus collectors, rolling dashboard state, and last-request throughput freshness state.
- Build dashboard counter samples for the in-memory telemetry store.
- Build API key usage payloads for the admin API.
- Expose token observation coverage aggregates alongside exact token counters.

## Boundary

- The package reads telemetry collectors and lightweight runtime inputs.
- It does not own HTTP routing or gateway lifecycle.
