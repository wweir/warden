# web/admin

## Responsibilities

`web/admin` is the embedded Vue 3 admin console for Warden.

- Renders runtime status, provider details, route details, config editor, MCP tools, live logs, and tool-hook suggestions derived from recent logs.
- The config editor exposes `route.<prefix>.hooks` inside each route card, while the dedicated Tool Hooks page provides a route-scoped editor for `exec` / `ai` / `http` rules.
- Hook suggestions support route-aware one-click AI rule filling plus Exec/HTTP rule skeletons, and all suggestion buttons share the same add-vs-fill detection logic.
- The dedicated Tool Hooks page adds collapsible quick-start guidance and collapsible log suggestions, plus MCP/tool breakdown chips and a stricter default AI safety prompt focused on command execution and privacy protection.
- On the Tool Hooks page, AI hook `route`/`model` fields are rendered as config-derived dropdowns, and model options stay scoped to exact route models, upstream mappings, and wildcard-provider model lists.
- Connects to admin SSE endpoints for status, logs, and dashboard telemetry.
- Formats long log durations with dynamic units (`ms` / `s` / `m` / `h`) instead of forcing milliseconds.
- Builds static assets into `dist/`, which are embedded by `web/embed.go`.

## Dashboard Data Flow

The dashboard consumes `GET /_admin/api/metrics/stream`:

- The backend sends aggregated Prometheus snapshots for summary cards and rankings.
- The backend also sends rolling time series points for the live usage, output-rate, and error charts; output-rate points include both total TPS and per-provider TPS for the multi-line chart, and stale output rates are zeroed automatically after one sampling window with no matching requests.
- The same stream also includes per-route rolling request-rate, error-rate, and output-rate points for the routes page multi-line charts.
- `RealtimeLineChart.vue` renders those points with uPlot, and the charts share one synchronized time window and hover axis.

## Build

- Development: `npm run dev`
- Production bundle: `npm run build`
