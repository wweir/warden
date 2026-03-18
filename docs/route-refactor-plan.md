# Route-Centric Routing Refactor

> Note (2026-03-18): OpenAI-compatible routes no longer hard-cut the alternate `/chat/completions` or `/responses` entry at registration time. `route.protocol` remains the primary protocol surface, but the gateway now also registers the alternate OpenAI endpoint when route providers can serve it directly or, for stateless Responses only, through `responses_to_chat`. Anthropic routes still expose only `/messages`.

## Goals

1. Route becomes the primary product surface.
2. Each route declares one primary external protocol: `chat`, `responses`, or `anthropic`.
3. Routing and failover are bound to `route + requested model`, not to the route-wide provider list.
4. Public model names may differ from upstream model names; renaming is enabled automatically for exact model mappings.
5. Wildcard route models forward the requested model name unchanged and only choose upstream providers.
6. Tool hooks are attached to routes instead of being primarily global.
7. Monitoring keeps both route-level and provider-level statistics.

## Config Shape

```yaml
route:
  /openai:
    protocol: chat
    hooks:
      - match: "filesystem__write_*"
        hook:
          type: http
          when: post
          webhook: audit-webhook
    exact_models:
      gpt-4o:
        system_prompt: "You are a helpful assistant."
        upstreams:
          - provider: openai
            model: gpt-4o
          - provider: anthropic-fallback
            model: claude-sonnet-4
    wildcard_models:
      "gpt-*":
        providers:
          - openai
          - openai-fast
```

## Matching Semantics

- `exact_models` keys do not contain `*` and must use `upstreams`.
- `wildcard_models` keys contain `*` and must use `providers`.
- Exact match wins over wildcard match.
- If multiple wildcard patterns match, the winner is the one with more literal characters and fewer `*`.
- If two wildcard patterns still have equal precedence and overlap, configuration validation fails.

## Execution Model

1. Match route from the request path.
2. Match the request model against route model definitions.
3. Build the ordered upstream candidate list from the matched route model.
4. Filter candidates by external protocol support and provider suppression state.
5. For exact model entries, rewrite the request model to the configured upstream model.
6. For wildcard entries, keep the request model unchanged.
7. Retryable failures fail over only within the matched route model candidate list.
8. A single configured public model can carry its own HA policy by listing multiple upstream targets/providers in that route model.

## Compatibility

- `route.protocol` is required.
- Only `exact_models`, `wildcard_models`, and `route.<prefix>.hooks` are accepted.
- Tool hooks are only loaded from `route.<prefix>.hooks`.

## Status

- Completed: `config` runtime compilation for route protocol/models/hooks.
- Completed: `selector` route-model candidate selection and route-facing `/models`.
- Completed: `gateway` route-model selection, protocol-aware route registration, exact-model rename, wildcard passthrough, and route-scoped hooks.
- Completed: admin UI editors for route models/hooks and route/provider split metrics.
- Completed: `Routes` page now edits route config directly with separate exact/wildcard model sections; `Tool Hooks` remains the dedicated hook editor.
- Completed: tool hooks now load only from `route.<prefix>.hooks`; no global `tool_hooks` compatibility remains.
- Completed: legacy `route.models`, `route.providers`, `route.system_prompts`, and empty `route.protocol` compatibility removed.

## Deferred Follow-Up

- Further UI polish for large route-model maps if configuration scale grows.
