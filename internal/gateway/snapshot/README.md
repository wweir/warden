# internal/gateway/snapshot

## Responsibilities

`internal/gateway/snapshot` owns admin-facing runtime snapshot assembly:

- Build dashboard metrics payloads from Prometheus collectors and rolling output-rate state.
- Build dashboard counter samples for the in-memory telemetry store.
- Build API key usage payloads for the admin API.

## Boundary

- The package reads telemetry collectors and lightweight runtime inputs.
- It does not own HTTP routing or gateway lifecycle.
