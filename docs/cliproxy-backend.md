# CLIProxy Backend

Warden can route to a `github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy` deployment as an OpenAI-compatible upstream.

This integration deliberately does not add `codex`, `gemini`, or `claude-cli` as Warden provider families. In Warden, `provider.*.family` describes the HTTP adapter Warden uses to talk to the upstream. The `cliproxy` process owns CLI provider authentication, token refresh, and provider-specific execution.

## Sidecar Boundary

Use `family: openai` for the Warden provider and point `url` at the local cliproxy `/v1` endpoint:

```yaml
provider:
  codex:
    family: "openai"
    backend: "cliproxy"
    backend_provider: "codex"
    url: "http://127.0.0.1:18741/v1"
    service_protocols: ["chat"]
```

`backend` and `backend_provider` are metadata fields. They document that the OpenAI-compatible upstream is backed by cliproxy and which cliproxy provider is expected to serve it. They do not change Warden's request path by themselves.

## Embedded Service

Warden can also start cliproxy in-process:

```yaml
cliproxy:
  enabled: true
  auth_dir: "~/.cli-proxy-api"

provider:
  codex:
    family: "openai"
    backend: "cliproxy"
    backend_provider: "codex"
    url: "http://127.0.0.1:18741/v1"
    service_protocols: ["chat"]
```

When `cliproxy.enabled` is true, Warden builds a minimal SDK config for `github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy`, starts it before the main gateway listener, waits for `/healthz`, and shuts it down before process restart or exit.

The embedded service endpoint is derived from the first `backend: cliproxy` provider URL. All cliproxy-backed providers must point to the same `http://loopback:port/v1` endpoint because Warden starts one embedded cliproxy service per process.

Warden generates a runtime cliproxy config file for the SDK watcher. This avoids passing the Warden YAML file into cliproxy's watcher, because the two projects use different config schemas.

## Validation Rules

- `backend` is optional.
- The only currently accepted backend value is `cliproxy`.
- `backend: cliproxy` requires `family: openai`.
- `backend: cliproxy` requires `backend_provider`, such as `codex`, `gemini`, `claude`, `kimi`, or another provider configured inside cliproxy.
- `backend: cliproxy` requires explicit `service_protocols`; use `["chat"]` unless the concrete cliproxy endpoint has been verified to support additional OpenAI-compatible surfaces.
- `cliproxy.enabled` requires at least one `backend: cliproxy` provider.
- Embedded cliproxy provider URLs must use `http`, an IPv4 loopback host or `localhost`, an explicit port, and the `/v1` path.
- Embedded cliproxy providers must share the same endpoint.

## Why This Shape

The cliproxy SDK exposes service and auth-manager primitives, but its built-in Codex, Gemini, Claude, Kimi, and Antigravity executors are implemented under the upstream module's `internal` packages. Warden cannot import those executors directly.

Treating cliproxy as a local OpenAI-compatible upstream keeps responsibilities separate:

- cliproxy owns CLI credentials, refresh, and provider-specific execution.
- Warden owns route prefixes, public model mapping, failover, API keys, logs, hooks, and metrics.

The embedded implementation imports the public cliproxy SDK. That dependency currently requires Go 1.26.

Native Warden provider families for CLI-backed providers should only be added if CLIProxyAPI exposes the needed executor or credential-injection APIs as public SDK surface.
