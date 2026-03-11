# internal/gateway

## Responsibilities

`internal/gateway` contains the core HTTP gateway and admin API implementation:

- Registers route handlers for proxy, chat completions, responses, admin API, and Prometheus metrics.
- Selects providers, records outcomes, and coordinates MCP tool injection/execution.
- Exposes admin SSE streams for live status, request logs, and dashboard telemetry.
- Converts Prometheus cumulative counters into rolling dashboard time series for the admin UI.
- Bridges OpenAI `chat/completions` ↔ `responses` when a provider enables protocol-conversion flags.

## Key Interfaces

- `Gateway`: owns runtime dependencies, route registration, graceful shutdown, and admin handlers.
- `PromMiddleware`: records request-level Prometheus metrics for business endpoints.
- `dashboardMetricsStore`: maintains in-memory rolling dashboard points sampled from Prometheus collectors.

## Admin Telemetry Flow

1. Business requests update Prometheus collectors in `metrics.go`.
2. `dashboardMetricsStore` samples cumulative counters every 2 seconds, while output-rate samples come from an in-memory freshness tracker updated by `RecordTokenMetrics`.
3. The store converts counter deltas into usage/error points, stores total output TPS plus per-provider output TPS samples, and also keeps per-route request-rate, error-rate, and output-rate time series for the routes page. Stale output-rate entries expire after one sample interval, so idle charts fall back to `0` instead of showing the last request forever.
4. `GET /_admin/api/metrics/stream` returns current aggregated metrics plus the rolling time series for the dashboard and routes charts.
