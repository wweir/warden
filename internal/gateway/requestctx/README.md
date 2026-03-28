# internal/gateway/requestctx

## Responsibilities

`internal/gateway/requestctx` stores request-scoped metadata shared across gateway handlers:

- Original client request handle for upstream passthrough helpers.
- Matched route hooks for tool-hook execution.
- Authenticated client API key name for metrics and logging labels.

## Boundary

- The package only defines context helpers.
- It does not import gateway runtime packages or perform HTTP work itself.
