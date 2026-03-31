# internal/gateway

## Responsibilities

`internal/gateway` contains the core HTTP gateway runtime and composes the admin API package:

- Registers route handlers for proxy, chat completions, responses, admin API, and Prometheus metrics.
- Validates client API keys for route traffic when `config.api_keys` is non-empty, then strips client auth headers before upstream forwarding.
- Selects route-model upstream targets, records outcomes, and runs route-scoped tool-call hooks on returned tool calls.
- Exposes admin SSE streams for live status, request logs, and dashboard telemetry.
- Converts Prometheus cumulative counters into rolling dashboard time series for the admin UI.
- Bridges stateless OpenAI `responses` requests to upstream `chat/completions` when a provider enables `responses_to_chat`, with explicit subset validation instead of mock passthrough.
- Bridges Anthropic `messages` requests to upstream OpenAI `chat/completions` when a provider enables `anthropic_to_chat`, again using explicit subset validation instead of raw passthrough.
- Keeps stream bridges live: `responses_to_chat` and `anthropic_to_chat` relay upstream SSE incrementally instead of buffering the whole body before writing downstream.
- Logs inspectable upstream response bodies; transparent proxy logs decompress `gzip`/`br`/`zstd` bodies before persistence when possible.
- Keeps failover trail on request logs, so a single successful client request still shows intermediate upstream switches.
- Delegates the admin HTTP surface, embedded SPA wiring, provider protocol probes, and tool-hook suggestion aggregation to `internal/gateway/admin` instead of keeping admin-only logic in the root package.
- Delegates shared recovery/CORS/client-auth middleware to `internal/gateway/httpmw`.
- Delegates log sink construction and request-attempt logging helpers to `internal/gateway/logging`.
- Delegates inference log assembly, stream normalization, and observed tool-hook dispatch to `internal/gateway/observe`.
- Delegates transparent proxy request handling, proxy-specific provider selection, and proxy response log assembly to `internal/gateway/proxy`.
- Delegates request-scoped context metadata helpers to `internal/gateway/requestctx`.
- Delegates admin-facing runtime metrics/API-key snapshot assembly to `internal/gateway/snapshot`.
- Delegates live SSE relay and protocol stream conversion to `internal/gateway/bridge`.
- Delegates shared inference target selection, auth retry, and failover state to `internal/gateway/inference`.
- Delegates protocol/transport adaptation helpers and upstream HTTP execution to `internal/gateway/upstream`.
- Delegates Prometheus collector ownership, metric label shaping, and dashboard telemetry primitives to `internal/gateway/telemetry`, while the root package keeps middleware wiring and admin-facing snapshot assembly.
- Compiles route config into a deterministic longest-prefix binding table so transparent proxy fallback does not depend on Go map iteration when route prefixes overlap.

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
- `responses_to_chat` accepts only a constrained stateless subset; unsupported Responses-only fields, non-`function` tools, and unknown input items fail fast with `400`, while compatible `function` tools keep their `strict` flag, `max_output_tokens` is mapped to `max_completion_tokens`, Responses-style `tool_choice` is normalized before forwarding, and `function_call_output.output` may carry arbitrary JSON before being normalized into chat tool content.
- Chat -> Responses rewrite normalizes Chat `usage` into Responses `input_tokens` / `output_tokens`, maps Chat `finish_reason` into Responses `status` / `incomplete_details`, and includes final item snapshots in `response.output_item.done` events for better SDK state-machine compatibility.
- `responses_stateful` routes accept both stateless and stateful `/responses`; stateful requests bypass `responses_to_chat` conversion and disable failover.
- Providers with `responses_to_chat` enabled cannot back `responses_stateful` route models, because that bridge does not implement `previous_response_id`.
- If a `responses_to_chat` upstream rejects `developer` role with a pre-stream `400`, the chat bridge retries the same provider once after downgrading those messages to `system`.
- Anthropic routes still expose only `/messages`.
- `anthropic_to_chat` accepts only a constrained Messages subset; non-text content blocks, mixed user text + tool_result messages, and unknown Anthropic-only fields fail fast with `400`.
- The Responses stream bridge emits a richer event sequence (`response.created`, `response.in_progress`, `response.output_text.done`, `response.function_call_arguments.done`, `response.output_item.done`) and attaches stable `output_index` / `item_id` metadata so stricter SDK state machines can track items incrementally.
- Streaming provider accounting distinguishes `pre_stream` from `in_stream`: pre-stream failures may retry/fail over, in-stream upstream truncation only marks the current provider unhealthy, and downstream disconnects do not suppress the provider.
- Non-inference subpaths that fall through to transparent proxying keep raw passthrough behavior; route protocol checks only gate recognized inference endpoints.
- Provider family compatibility is derived centrally from provider config: `openai => chat + responses_*` plus optional `anthropic` when `anthropic_to_chat` is enabled, `anthropic => chat + anthropic`, `qwen/copilot/ollama => chat`.
- Provider model protocol probes follow the real request path: `anthropic_to_chat` providers probe Anthropic support by converting a Messages request into upstream Chat, instead of hard-rejecting non-native Anthropic providers.

## Key Interfaces

- `Gateway`: owns runtime dependencies, route registration, graceful shutdown, and admin handlers.
- `internal/gateway/proxy`: owns transparent proxy handling plus shared helpers for proxy protocol classification and SSE-to-object log assembly.
- `internal/gateway/httpmw`: owns shared HTTP middleware for panic recovery, CORS, and client API key auth.
- `internal/gateway/logging`: owns log sink construction and lightweight request-attempt logging helpers.
- `internal/gateway/observe`: owns inference-observation helpers for request logs, stream assembly, tool-call extraction, and route hook dispatch.
- `internal/gateway/bridge`: owns SSE relay and live protocol stream conversion between chat / responses / messages surfaces.
- `internal/gateway/inference`: owns route-model matching plus per-request auth retry / failover lifecycle state.
- `internal/gateway/admin`: owns the admin HTTP surface plus admin-only probe/suggestion helpers, while `gateway` injects selector/broadcaster plus the few runtime callbacks that still need root state.
- `internal/gateway/admin` now keeps router/auth wiring separate from config/status/provider/route/API-key handlers, so admin-only changes stay localized instead of growing one monolithic entry file.
- `internal/gateway/requestctx`: owns request-context helpers for original client requests, route hooks, and authenticated API key labels.
- `internal/gateway/snapshot`: owns admin-facing snapshot assembly for dashboard metrics and API key usage payloads.
- `internal/gateway/upstream`: owns protocol endpoint mapping, upstream request execution, body conversion, encoding negotiation, and forwarded-header sanitization.
- `internal/gateway/telemetry`: owns Prometheus collectors, metric helpers, dashboard rolling store, and output-rate freshness tracking.
- `PromMiddleware`: records request-level Prometheus metrics for business endpoints.
- Client API key usage is tracked separately from provider usage, so gateway auth and upstream auth remain decoupled.
- Shared inference helpers centralize JSON request parsing, metric-header wiring, and common response headers so chat / responses / messages handlers stay behaviorally aligned.
- Shared inference helpers also centralize route-context bootstrap, request-id/start-time allocation, manager creation, and route-model prompt injection, so protocol handlers keep only protocol-specific branching.
- Shared inference session helpers centralize current provider/target refresh, per-attempt log params, pending-log emission, and metric-label refresh after failover so chat / responses / messages handlers no longer duplicate the same lifecycle scaffolding.
- Request-scoped retry paths short-circuit when the client request context is already canceled or expired, so wrapped disconnect/deadline errors do not spin in hot retry loops after the downstream is gone.
- Chat-bridge helpers also centralize the shared `responses_to_chat` / `anthropic_to_chat` retry loop, pre-stream vs in-stream failure accounting, and final response logging so the two bridge paths stop carrying near-identical control flow.
- Buffered inference helpers now also centralize the common `chat` / native `responses` request loop: upstream request preparation, retry/failover handling, success-path tool hook dispatch, and final log writing no longer live in two separate copies.
- Buffered / relay helpers preserve protocol-path switching across failover: if a request starts on a native provider and retries onto a bridge-capable provider, control flow re-enters the correct `responses_to_chat` or `anthropic_to_chat` path instead of staying pinned to the old execution branch.
- Success-path `post` hooks keep route-scoped context values but detach from downstream request cancellation, so audit-only async hooks still run after the handler returns; each hook remains bounded by its own timeout.
- Dashboard and API key snapshots stay in the root package as admin callbacks, but Prometheus collector ownership and rolling time-series state now live in `internal/gateway/telemetry`.
- Gateway root no longer keeps one-line snapshot wrapper methods that only forwarded to `internal/gateway/snapshot`; callback wiring now points at the snapshot package directly.
- Inference request logging is assembled through shared helpers, so chat/responses/proxy paths keep the same `reqlog.Record` shape and stream-to-object logging behavior.
- Streaming inference requests publish a pending admin-log event early and overwrite it with the final record on completion, so the logs SSE feed does not wait for long streams to finish before surfacing the request.
- Stream logs persist partial SSE payloads plus an error string when a live bridge is truncated after headers, so admin logs can distinguish upstream mid-stream failure from clean completion; Responses stream tool hooks also recover function calls from incremental events when `response.completed` never arrives.
- Admin SSE handlers explicitly disable proxy buffering and the logs stream sends an immediate comment frame plus keepalive heartbeats, so the admin UI is less likely to see delayed SSE delivery behind reverse proxies.

## Admin Telemetry Flow

1. Business requests update Prometheus collectors owned by `internal/gateway/telemetry`.
2. `internal/gateway/telemetry` samples cumulative counters every 2 seconds, while output-rate samples come from an in-memory freshness tracker updated by `RecordTokenMetrics`.
3. The store converts counter deltas into usage/error points, stores total output TPS plus per-provider output TPS samples, and also keeps per-route request-rate, error-rate, and output-rate time series for the routes page. Stale output-rate entries expire after one sample interval, so idle charts fall back to `0` instead of showing the last request forever.
4. `GET /_admin/api/metrics/stream` returns current aggregated metrics plus the rolling time series for the dashboard and routes charts.
