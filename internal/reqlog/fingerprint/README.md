# internal/reqlog/fingerprint

## Responsibilities

`internal/reqlog/fingerprint` owns request-body fingerprint construction:

- Extracts stable conversation inputs from Chat, Anthropic-style content blocks, and Responses payloads.
- Filters dynamic provider billing headers and hidden thinking blocks before hashing.
- Builds compact deterministic hashes used by request logs to group related conversations.

## Boundary

- The package does not write logs, broadcast records, or know any logging backend.
- Callers import this package directly; `internal/reqlog` only owns record types, output backends, and SSE broadcast.
