# internal/gateway/upstream

## Responsibilities

`internal/gateway/upstream` owns protocol and transport adaptation helpers:

- Maps provider protocols to upstream endpoints.
- Executes upstream HTTP requests for JSON and SSE flows, including first-token timeout handling.
- Marshals and unmarshals OpenAI / Anthropic request and response bodies.
- Negotiates `Accept-Encoding`, normalizes `Content-Encoding`, and decodes compressed bodies.
- Sanitizes forwarded proxy headers before requests leave the gateway.
- Normalizes upstream HTTP errors before handlers decide whether to retry or fail over.

The package is intentionally stateless. `gateway` and `internal/gateway/inference` keep request lifecycle decisions, selector interaction, logging, and metrics ownership.
