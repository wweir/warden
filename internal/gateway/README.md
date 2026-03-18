# internal/gateway

## Responsibilities

`internal/gateway` contains the core HTTP gateway and admin API implementation:

- Registers route handlers for proxy, chat completions, responses, admin API, and Prometheus metrics.
- Validates client API keys for route traffic when `config.api_keys` is non-empty, then strips client auth headers before upstream forwarding.
- Selects route-model upstream targets, records outcomes, and runs route-scoped tool-call hooks on returned tool calls.
- Exposes admin SSE streams for live status, request logs, and dashboard telemetry.
- Converts Prometheus cumulative counters into rolling dashboard time series for the admin UI.
- Bridges OpenAI `chat/completions` ↔ `responses` when a provider enables protocol-conversion flags.
- Logs inspectable upstream response bodies; transparent proxy logs decompress `gzip`/`br`/`zstd` bodies before persistence when possible.
- Keeps failover trail on request logs, so a single successful client request still shows intermediate upstream switches.

## Route-Centric Runtime

- `route.protocol` defines the primary external protocol surface of a route: `chat`, `responses`, or `anthropic`.
- `route.exact_models` and `route.wildcard_models` define the public model surface explicitly.
- Exact model entries use ordered `upstreams`, while wildcard entries use ordered `providers`.
- Retryable failures only fail over within the matched route-model candidate list, so HA can be configured for a single public model without affecting unrelated models on the same route.
- Exact model entries rewrite the request model to the configured upstream model automatically when names differ.
- Wildcard model entries preserve the request model name and only choose which provider serves it.
- Route hooks are carried through request context, and tool execution only reads hooks from the matched route.
- Anthropic routes still expose only `/messages`; OpenAI-compatible routes may expose both `/chat/completions` and `/responses` when the route has provider support for both, so provider-level protocol conversion flags are reachable.

## Key Interfaces

- `Gateway`: owns runtime dependencies, route registration, graceful shutdown, and admin handlers.
- `PromMiddleware`: records request-level Prometheus metrics for business endpoints.
- Client API key usage is tracked separately from provider usage, so gateway auth and upstream auth remain decoupled.
- `dashboardMetricsStore`: maintains in-memory rolling dashboard points sampled from Prometheus collectors.

## Admin Telemetry Flow

1. Business requests update Prometheus collectors in `metrics.go`.
2. `dashboardMetricsStore` samples cumulative counters every 2 seconds, while output-rate samples come from an in-memory freshness tracker updated by `RecordTokenMetrics`.
3. The store converts counter deltas into usage/error points, stores total output TPS plus per-provider output TPS samples, and also keeps per-route request-rate, error-rate, and output-rate time series for the routes page. Stale output-rate entries expire after one sample interval, so idle charts fall back to `0` instead of showing the last request forever.
4. `GET /_admin/api/metrics/stream` returns current aggregated metrics plus the rolling time series for the dashboard and routes charts.
