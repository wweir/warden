# internal/selector

## Responsibilities

`internal/selector` owns upstream selection and provider runtime state:

- Selects the current provider/route target for one public route model.
- Tracks suppression windows, failover counters, stream-phase error counters, and provider status snapshots.
- Loads provider models from static config and upstream `/models`.
- Stores admin-facing protocol probe state.
- Exposes shared provider auth-header injection for gateway upstream calls.

## File Layout

- `types.go`: core state, public snapshot types, constructor.
- `select.go`: route-target selection and candidate building.
- `state.go`: runtime health mutation and status-query methods.
- `models.go`: model discovery, route model listing, and auth-header helpers.
- `errors.go`: upstream error classification and retryability rules.

## Boundary

`selector` decides provider availability and target choice. It does not own HTTP routing, protocol conversion, request logging, or admin response assembly.
