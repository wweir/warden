# internal/selector

## Responsibilities

`internal/selector` owns upstream selection and provider runtime state:

- Selects the current provider/route target for one public route model.
- Tracks suppression windows, failover counters, stream-phase error counters, and provider status snapshots.
- Releases automatic suppression for remaining route candidates when manual suppression would otherwise leave the route with no selectable provider.
- Loads provider models from static config and upstream `/models`, with discovery requests bound to caller/gateway context so shutdown or canceled admin checks do not linger.
- Records wildcard route-model hits as soon as a concrete target is selected, so route `/models` can expose matched models even when the upstream request later fails.
- Guards model discovery pagination against empty or repeated cursors so a broken upstream cannot trap startup/background refresh in an endless `/models` loop.
- Stores admin-facing protocol probe state.
- Exposes shared provider auth-header injection for gateway upstream calls.
- Classifies wrapped downstream cancellation/deadline errors as non-retryable so request termination does not masquerade as a network failover signal.

## File Layout

- `types.go`: core state, public snapshot types, constructor.
- `select.go`: route-target selection and candidate building.
- `state.go`: runtime health mutation and status-query methods.
- `models.go`: model discovery, route model listing, and auth-header helpers.
- `errors.go`: upstream error classification and retryability rules.

## Boundary

`selector` decides provider availability and target choice. It does not own HTTP routing, protocol conversion, request logging, or admin response assembly.
