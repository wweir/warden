# internal/gateway/inference

## Responsibilities

`internal/gateway/inference` owns the shared inference request lifecycle state:

- Matches `route` public models to concrete upstream targets.
- Keeps per-request auth-retry and failover state.
- When a route model has only one candidate provider left after manual suppression, retries stay on that provider instead of excluding it for the rest of the request.
- Treats downstream request cancellation or deadline expiry as terminal for the current request lifecycle; wrapped `context.Canceled` / `context.DeadlineExceeded` errors must not re-enter provider retry/failover paths.
- Records failover trail entries so the final request log preserves intermediate provider switches.
- Exposes the current resolved provider/target so protocol-specific handlers can stay thin.

The package does not own HTTP parsing, protocol conversion, logging sinks, or metrics emission. Those remain in `gateway`, `bridge`, and `upstream`.
