# Product

## Register

product

## Users

AI gateway operators and developers who manage upstream LLM providers, routes, and monitor traffic. They use this admin UI on desktop browsers (primarily 1440px+ monitors) during debugging sessions and routine operational checks.

## Product Purpose

Warden is a route-centric AI gateway admin console. The Logs page specifically provides real-time observability into request traffic: streaming SSE logs, session grouping, request/response inspection, and status monitoring. Success means an operator can spot failures, trace request paths, and inspect conversation content without leaving the browser.

## Brand Personality

Technical, precise, unobtrusive. The UI should feel like a professional tool (think Linear, Datadog, or Stripe Dashboard) rather than a consumer app. Information density is valued over whitespace. Motion should be purposeful, not decorative.

## Anti-references

- SaaS landing-page aesthetics (big hero metrics, gradient cards, glassmorphism)
- Consumer chat UIs (bubble layouts, playful avatars)
- Generic Bootstrap/AdminLTE templates
- Dark-mode-for-dark-mode's-sake (this is an observability tool; light default is fine)

## Design Principles

- Show the data, hide the chrome: every pixel should serve observability
- Progressive disclosure: raw JSON and internals are available but not in your face
- Consistency across routes: same patterns for chat, responses, and anthropic logs
- Respect operator attention: streaming updates should be noticeable but not distracting

## Accessibility & Inclusion

- Target WCAG 2.1 AA
- Keyboard-navigable tables and filters
- Respect `prefers-reduced-motion` for streaming indicators
