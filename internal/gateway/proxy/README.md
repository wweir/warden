# internal/gateway/proxy

## Responsibilities

`internal/gateway/proxy` owns the transparent proxy surface:

- Routes unmatched subpaths to the selected upstream provider.
- Applies proxy-specific provider selection, auth retry, and optional failover for inference-like endpoints.
- Rewrites inspectable proxy responses into loggable final objects when the upstream returned SSE.
- Exposes shared helpers for Responses protocol classification and raw SSE fallback logging so root handlers do not keep duplicate proxy-only helpers.
- Emits request logs and token metrics for proxy traffic through injected callbacks.

The package does not own router registration, middleware, or admin wiring.
