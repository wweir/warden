# internal/gateway/httpmw

## Responsibilities

`internal/gateway/httpmw` owns shared HTTP middleware for the gateway surface:

- Panic recovery middleware.
- CORS middleware for browser clients.
- Client API key authentication middleware, including client credential stripping before upstream forwarding.

## Boundary

- The package does not know gateway runtime state beyond injected config.
- API key identity is attached to request context through `internal/gateway/requestctx`.
