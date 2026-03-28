# internal/gateway/bridge

## Responsibilities

`internal/gateway/bridge` owns live protocol bridge and SSE relay helpers:

- Relays native Anthropic SSE streams.
- Converts OpenAI chat SSE into Anthropic or Responses SSE on the fly.
- Tags relay failures by `upstream` or `downstream` source so request lifecycle code can attribute stream errors correctly.
- Exposes shared SSE frame parsing used by stream relay tests.

The package does not own route selection, auth retry, failover, logging, or metrics.
