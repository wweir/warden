# internal/providerauth

## Responsibilities

`internal/providerauth` owns provider outbound authentication header injection:

- Resolves provider credentials from static `api_key`, dynamic `api_key_command`, or provider token sources such as Copilot OAuth.
- Applies protocol-specific API key headers.
- Applies configured static provider headers.
- Returns credential resolution errors to callers instead of hiding them, so gateway paths can use the existing auth retry/failover boundary.
- Keeps provider HTTP credential handling out of selector state and gateway handlers.

## Boundary

- The package does not select providers, send HTTP requests, or mutate runtime health state.
- Callers provide the request context so provider credentials can be resolved consistently and command execution can be cancelled.
- `api_key_command` is treated as trusted operator configuration; this package never logs command stdout/stderr as credential material.
