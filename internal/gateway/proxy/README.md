# internal/gateway/proxy

## Responsibilities

`internal/gateway/proxy` owns the transparent proxy surface:

- Routes unmatched subpaths to the selected upstream provider.
- Applies proxy-specific provider selection, auth retry, and optional failover for inference-like endpoints.
- Rewrites inspectable proxy responses through `internal/gateway/observe` when the upstream returned SSE.
- Uses `internal/gateway/inference` for request-path classification and shared unsupported-protocol messages.
- Emits request logs and token metrics for proxy traffic through injected callbacks.

The package does not own router registration, middleware, or admin wiring.
