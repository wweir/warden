# internal/gateway/inference

## Responsibilities

`internal/gateway/inference` owns the shared inference request lifecycle state:

- Matches `route` public models to concrete upstream targets.
- Keeps per-request auth-retry and failover state.
- Records failover trail entries so the final request log preserves intermediate provider switches.
- Exposes the current resolved provider/target so protocol-specific handlers can stay thin.

The package does not own HTTP parsing, protocol conversion, logging sinks, or metrics emission. Those remain in `gateway`, `bridge`, and `upstream`.
