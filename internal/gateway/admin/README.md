# internal/gateway/admin

## Responsibilities

`internal/gateway/admin` owns the admin HTTP surface:

- Registers `/_admin/*` routes and serves the embedded admin SPA.
- Applies admin Basic Auth before dispatching API and asset requests.
- Handles config source/get/put, provider operations, route detail, metrics SSE, logs SSE, API key management, provider protocol probing, and tool-hook suggestion aggregation.
- Keeps the HTTP surface split by API domain (`router`, `config`, `status`, `providers`, `routes`, `apikeys`) so admin-only changes do not collapse back into one large handler file.
- Depends on injected selector, broadcaster, and a small set of runtime-state callbacks instead of importing the parent `gateway` package.

## Boundary

The parent `gateway` package stays the composition root:

- `gateway` keeps inference handlers, proxying, metrics collectors, and dashboard snapshots.
- `admin` owns pure admin logic locally and receives only selector access plus root-owned runtime snapshots via callbacks.

This split keeps the admin surface isolated without introducing package cycles.
