# internal/gateway/admin

## Responsibilities

`internal/gateway/admin` owns the admin HTTP surface:

- Registers `/_admin/*` routes and serves the embedded admin SPA.
- Applies admin Basic Auth before dispatching API and asset requests.
- Handles config source/get/put, provider operations, route detail, metrics SSE, logs SSE, API key management, provider protocol probing, and tool-hook suggestion aggregation.
- Route detail includes compiled exact/wildcard model rows and wildcard matched model IDs derived from the selector route model list, so the admin UI can show detected wildcard support without reimplementing selector matching.
- Exposes provider form metadata for the admin create flow, including provider presets, capability templates, and cliproxy default endpoint hints.
- Manages cliproxy auth files under `cliproxy.auth_dir`, including import, delete, online validation, and sanitized per-file usage-state reads with a short in-memory cache. Usage summaries prioritize plan, auth status, 5-hour quota, weekly quota, reset time, quota cooldown, and remaining credit fields, and can merge matching cliproxy runtime error bodies from selector suppression state when the auth file has no usage metadata.
- Keeps cliproxy auth internals split by responsibility: HTTP handlers, file store/cache, auth payload normalization, usage summarization, runtime suppression-state merging, and online probe execution live in separate files inside this package.
- Binds provider health/probe network calls to the admin request context, so abandoned UI actions stop their upstream checks instead of running to timeout in the background.
- Keeps the HTTP surface split by API domain (`router`, `config`, `status`, `providers`, `routes`, `apikeys`) so admin-only changes do not collapse back into one large handler file.
- Depends on injected selector, broadcaster, and a small set of runtime-state callbacks instead of importing the parent `gateway` package.

## Boundary

The parent `gateway` package stays the composition root:

- `gateway` keeps inference handlers, proxying, metrics collectors, and dashboard snapshots.
- `admin` owns pure admin logic locally and receives only selector access plus root-owned runtime snapshots via callbacks.

This split keeps the admin surface isolated without introducing package cycles.
