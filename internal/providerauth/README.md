# internal/providerauth

## Responsibilities

`internal/providerauth` owns provider outbound authentication header injection:

- Applies protocol-specific API key headers.
- Applies configured static provider headers.
- Keeps provider HTTP credential handling out of selector state and gateway handlers.

## Boundary

- The package does not select providers, send HTTP requests, or mutate runtime health state.
- Callers provide the request context so provider credentials can be resolved consistently.
