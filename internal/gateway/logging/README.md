# internal/gateway/logging

## Responsibilities

`internal/gateway/logging` owns request-log backend construction and request-attempt logging helpers:

- Build configured file / HTTP log sinks.
- Fan out records to multiple sinks.
- Emit lightweight request-attempt logs for debugging.

## Boundary

- The package does not know gateway routing or inference state.
- It only depends on config and `reqlog` backends.
