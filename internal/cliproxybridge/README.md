# internal/cliproxybridge

## Responsibilities

`internal/cliproxybridge` owns the embedded CLIProxyAPI/cliproxy lifecycle:

- Detects whether the current config contains cliproxy-backed providers.
- Builds a minimal runtime cliproxy SDK config.
- Starts, health-checks, and shuts down the embedded service.
- Removes temporary runtime config on close.

## Boundary

- The package does not participate in provider selection or request routing.
- The gateway still treats cliproxy as an OpenAI-compatible provider reached over HTTP.
