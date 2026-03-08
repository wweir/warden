# web/admin

## Responsibilities

`web/admin` is the embedded Vue 3 admin console for Warden.

- Renders runtime status, provider details, route details, config editor, MCP tools, and live logs.
- Connects to admin SSE endpoints for status, logs, and dashboard telemetry.
- Builds static assets into `dist/`, which are embedded by `web/embed.go`.

## Dashboard Data Flow

The dashboard consumes `GET /_admin/api/metrics/stream`:

- The backend sends aggregated Prometheus snapshots for summary cards and rankings.
- The backend also sends rolling time series points for the live usage, output-rate, and error charts; output-rate points include both total TPS and per-provider TPS for the multi-line chart.
- The same stream also includes per-route rolling request-rate, error-rate, and output-rate points for the routes page multi-line charts.
- `RealtimeLineChart.vue` renders those points with uPlot, and the charts share one synchronized time window and hover axis.

## Build

- Development: `npm run dev`
- Production bundle: `npm run build`
