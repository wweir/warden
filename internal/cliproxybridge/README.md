# internal/cliproxybridge

## Responsibilities

`internal/cliproxybridge` owns the embedded CLIProxyAPI/cliproxy lifecycle:

- Detects whether the current config contains cliproxy-backed providers.
- Builds a minimal runtime cliproxy SDK config and defaults omitted `cliproxy.auth_dir` to `/etc/warden`.
- Warden user config is TOML-only; the temporary runtime config written here is YAML only because the CLIProxyAPI SDK watcher reads its own YAML schema.
- Applies Warden's embedded feature-hiding defaults for Codex/Claude header profiles and disables CLIProxyAPI response header passthrough.
- Starts, health-checks, and shuts down the embedded service.
- Removes temporary runtime config on close.

## Boundary

- The package does not participate in provider selection or request routing.
- The gateway still treats cliproxy as an OpenAI-compatible provider reached over HTTP.
- Gateway-side header sanitization for `backend: cliproxy` lives in `internal/gateway/upstream`; this package only configures the embedded CLIProxyAPI service.
