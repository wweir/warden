# CLIProxy Backend

Warden can route to a `github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy` deployment as an OpenAI-compatible upstream.

This integration deliberately does not add `codex`, `gemini`, or `claude-cli` as Warden provider families. In Warden, `provider.*.family` describes the HTTP adapter Warden uses to talk to the upstream. The `cliproxy` process owns CLI provider authentication, token refresh, and provider-specific execution.

## Sidecar Boundary

Use `family: openai` for the Warden provider and point `url` at the local cliproxy `/v1` endpoint. In embedded mode this URL is still required because it is the internal HTTP boundary Warden uses to start, probe, and call the in-process CLIProxyAPI service; it is not a second external model service.

```toml
[provider.codex]
family = "openai"
backend = "cliproxy"
backend_provider = "codex"
url = "http://127.0.0.1:18741/v1"
service_protocols = ["chat"]
```

`backend` and `backend_provider` are metadata fields. They document that the OpenAI-compatible upstream is backed by cliproxy and which cliproxy provider is expected to serve it. They do not change Warden's request path by themselves.

The built-in admin presets for `cliproxy-codex`, `cliproxy-claude`, and `cliproxy-gemini` all use this shape and default to `service_protocols: ["chat"]`. CLIProxyAPI exposes additional native surfaces, including Responses, Claude Messages, and Gemini model-path APIs, but Warden's current cliproxy backend path only treats the endpoint as an OpenAI-compatible upstream. Do not enable embeddings for cliproxy unless the concrete endpoint has a verified `/v1/embeddings` implementation.

## Embedded Service

Warden can also start cliproxy in-process:

```toml
[cliproxy]
enabled = true
# Default when omitted: /etc/warden
auth_dir = "/etc/warden"

[provider.codex]
family = "openai"
backend = "cliproxy"
backend_provider = "codex"
url = "http://127.0.0.1:18741/v1"
service_protocols = ["chat"]
```

When `cliproxy.enabled` is true, Warden defaults `cliproxy.auth_dir` to `/etc/warden` when omitted, then builds a minimal SDK config for `github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy`, starts it before the main gateway listener, waits for `/healthz`, and shuts it down before process restart or exit.

Embedded Warden runs CLIProxyAPI v7 in local `auth_dir` mode, not Home control-plane mode. In this mode CLIProxyAPI loads auth JSON files through its file token store, starts the core auth auto-refresh loop, and uses the same store to persist refreshed token metadata back to the backing file. A successful refresh is applied to the in-memory auth manager immediately before the file watcher observes the write. If the filesystem write fails, the current CLIProxyAPI SDK can still keep the refreshed in-memory state; treat that as a persistence failure, not durable credential rotation.

The provider admin page can import a complete CLIProxyAPI auth JSON file into this directory. The imported content is written as a plain `.json` file under `cliproxy.auth_dir`; Warden does not convert it into provider config fields.

Warden validates imported auth files offline. It rejects malformed JSON, non-object payloads, and files without a non-empty `type` field. It also reports a structural status for existing and newly imported files by checking common credential indicators, disabled state, and locally encoded expiration fields. This status does not prove that the upstream account is still accepted online, because Warden does not refresh tokens or call provider APIs during import.

The admin page also exposes a manual online validation action. The browser only calls Warden's admin API; Warden sends the actual validation request from the backend through the same provider request path used for normal OpenAI-compatible requests. The request targets the current saved cliproxy provider and a selected or first known model with a minimal Responses payload: `model`, `input: "ping"`, and `store: false`. This validates that the provider's CLIProxyAPI credential pool can serve a real request. It does not pin the request to a specific auth file because CLIProxyAPI's normal HTTP request surface does not expose a per-file auth selector.

For each listed auth file, the admin page asynchronously asks Warden for sanitized usage state. Warden reads the selected auth JSON and returns non-secret status data. If the auth file itself lacks usage metadata, Warden also merges the latest matching cliproxy provider runtime body recorded by selector suppression, such as Codex `usage_limit_reached` reset fields from a recent 429 or auth-unavailable errors from a recent 401/503. Newer auth errors override older quota data so the page does not present stale limit windows as current usage. The summary prioritizes the account plan, current auth state, 5-hour quota, weekly quota, reset time, quota cooldown, and remaining credit fields when those fields are present; the response also passes through whitelisted usage JSON for the detail tooltip. It does not return tokens, cookies, API keys, or raw auth metadata. Results are cached briefly by auth file path so page refreshes do not repeatedly parse unchanged files; the cache is invalidated immediately when file mtime or size changes, including after CLIProxyAPI writes refreshed token metadata.

The embedded service endpoint is derived from the first `backend: cliproxy` provider URL. All cliproxy-backed providers must point to the same `http://loopback:port/v1` endpoint because Warden starts one embedded cliproxy service per process.

For `backend: cliproxy`, `provider.*.proxy` does not proxy Warden's local HTTP call to this loopback endpoint. It is only used as the embedded service's outbound proxy to the real upstream when `cliproxy.proxy` is empty. Set `cliproxy.proxy` to override this derived value.

Warden main config is TOML-only. The embedded cliproxy bridge still generates a temporary YAML runtime file for CLIProxyAPI's SDK watcher because the SDK reads its own YAML schema. That file is internal process state, not a Warden user config, and must not be edited as `/etc/warden/warden.toml`.

Embedded cliproxy is started with Warden-owned feature-hiding defaults:

- Codex OAuth requests receive pinned cliproxy header defaults for `User-Agent` and websocket beta features instead of inheriting arbitrary client values.
- Claude requests receive pinned CLI header defaults and `stabilize-device-profile: true`, so CLIProxyAPI pins OS/arch and uses its device-profile stabilization path.
- Embedded CLIProxyAPI response header passthrough is kept disabled, so upstream/provider headers are not blindly copied through cliproxy.
- Before Warden forwards a request to a `backend: cliproxy` provider, it strips client-supplied CLI fingerprint headers, OpenAI/Anthropic SDK feature headers, Codex turn metadata, and Warden-generated forwarding headers. Provider-level static `headers` are still applied afterwards, so explicit operator overrides remain possible.

This is not full TLS fingerprint emulation. Warden still delegates upstream execution and outbound transport behavior to CLIProxyAPI.

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
