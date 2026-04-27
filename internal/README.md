# internal

## Package Boundaries

`internal` contains Warden runtime implementation packages:

- `gateway`: HTTP gateway runtime, admin surface wiring, protocol bridges, proxy fallback, metrics, and request observation.
- `providerauth`: provider outbound authentication/header injection shared by gateway, selector model discovery, and admin probes.
- `selector`: provider runtime state, route-model target selection, suppression/failover state, and model discovery state.
- `reqlog`: request log records, log backends, and SSE broadcaster.
- `reqlog/fingerprint`: request-body fingerprint extraction and compact hash construction.
- `cliproxybridge`: embedded CLIProxyAPI/cliproxy lifecycle bridge.
- `install`: managed service installation helpers for supported platforms.
- `setupbundle`: self-extracting setup bundle encoding/decoding.

Keep packages pointed in one direction: gateway composes runtime behavior; selector decides provider availability; providerauth owns credentials; reqlog owns log records and sinks. Shared pure helpers should live in focused packages instead of being attached to a package only because that package was the first caller.
