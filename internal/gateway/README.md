# internal/gateway

## Responsibilities

`internal/gateway` contains the core HTTP gateway and admin API implementation:

- Registers route handlers for proxy, chat completions, responses, admin API, and Prometheus metrics.
- Validates client API keys for route traffic when `config.api_keys` is non-empty, then strips client auth headers before upstream forwarding.
- Selects route-model upstream targets, records outcomes, and runs route-scoped tool-call hooks on returned tool calls.
- Exposes admin SSE streams for live status, request logs, and dashboard telemetry.
- Converts Prometheus cumulative counters into rolling dashboard time series for the admin UI.
- Bridges stateless OpenAI `responses` requests to upstream `chat/completions` when a provider enables `responses_to_chat`.
- Logs inspectable upstream response bodies; transparent proxy logs decompress `gzip`/`br`/`zstd` bodies before persistence when possible.
- Keeps failover trail on request logs, so a single successful client request still shows intermediate upstream switches.

## Route-Centric Runtime

- `route.exact_models` and `route.wildcard_models` define the public model surface explicitly.
- `route.protocol` locks each route to exactly one configured protocol.
- Exact model entries use ordered `upstreams`, while wildcard model entries use ordered `providers`.
- Retryable failures only fail over within the matched route-model candidate list, so HA can be configured for a single public model without affecting unrelated models on the same route.
- Exact model entries rewrite the request model to the configured upstream model automatically when names differ.
- Wildcard model entries preserve the request model name and only choose which provider serves it.
- Route hooks are carried through request context, and tool execution only reads hooks from the matched route.
- The gateway derives endpoint exposure from `route.protocol`; it does not depend on provider-card display protocols.
- `chat` routes expose only `/chat/completions`.
- `responses_stateless` routes expose only stateless `/responses`.
- `responses_stateless` routes reject `previous_response_id`.
- `responses_stateful` routes accept both stateless and stateful `/responses`; stateful requests bypass `responses_to_chat` conversion and disable failover.
- Providers with `responses_to_chat` enabled cannot back `responses_stateful` route models, because that bridge does not implement `previous_response_id`.
- Anthropic routes still expose only `/messages`.
- Non-inference subpaths that fall through to transparent proxying keep raw passthrough behavior; route protocol checks only gate recognized inference endpoints.
- Provider family compatibility is derived centrally from provider config: `openai => chat + responses_*`, `anthropic => chat + anthropic`, `qwen/copilot/ollama => chat`.

## Key Interfaces

- `Gateway`: owns runtime dependencies, route registration, graceful shutdown, and admin handlers.
- `PromMiddleware`: records request-level Prometheus metrics for business endpoints.
- Client API key usage is tracked separately from provider usage, so gateway auth and upstream auth remain decoupled.
- `dashboardMetricsStore`: maintains in-memory rolling dashboard points sampled from Prometheus collectors.
- Inference request logging is assembled through shared helpers, so chat/responses/proxy paths keep the same `reqlog.Record` shape and stream-to-object logging behavior.
- Streaming inference requests publish a pending admin-log event early and overwrite it with the final record on completion, so the logs SSE feed does not wait for long streams to finish before surfacing the request.
- Admin SSE handlers explicitly disable proxy buffering and the logs stream sends an immediate comment frame plus keepalive heartbeats, so the admin UI is less likely to see delayed SSE delivery behind reverse proxies.

## Admin Telemetry Flow

1. Business requests update Prometheus collectors in `metrics.go`.
2. `dashboardMetricsStore` samples cumulative counters every 2 seconds, while output-rate samples come from an in-memory freshness tracker updated by `RecordTokenMetrics`.
3. The store converts counter deltas into usage/error points, stores total output TPS plus per-provider output TPS samples, and also keeps per-route request-rate, error-rate, and output-rate time series for the routes page. Stale output-rate entries expire after one sample interval, so idle charts fall back to `0` instead of showing the last request forever.
4. `GET /_admin/api/metrics/stream` returns current aggregated metrics plus the rolling time series for the dashboard and routes charts.
