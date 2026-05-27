# Protocol Bridge Review

> Date: 2026-05-27
>
> Status: review record
>
> Scope: `responses_to_chat`, `anthropic_to_chat`, and `anthropic_to_responses` bridge paths.
>
> Authority: this file is a review and repair plan, not a protocol source of truth. Stable bridge rules must be written back to `ARCHITECTURE.md`, `docs/responses-protocol-support.md`, `docs/anthropic-messages-to-chat-plan.md`, or `docs/provider.md` after implementation.

## Context

Warden currently exposes three protocol bridge modes:

- `responses_to_chat`: public Responses API requests are converted to upstream OpenAI-compatible Chat Completions, then converted back to Responses.
- `anthropic_to_chat`: public Anthropic Messages requests are converted to upstream OpenAI-compatible Chat Completions, then converted back to Messages.
- `anthropic_to_responses`: public Responses API requests are converted through Chat IR and sent to an upstream Anthropic Messages provider, then converted back to Responses.

The bridge is intentionally not a full protocol emulator. The current implementation is a controlled compatibility subset. The problem is that some unsupported or lossy paths are not explicit enough, so clients and operators can believe a feature worked when it was ignored, downgraded, or converted to plain text.

## Evidence

- `pkg/protocol/openai/convert_responses_to_chat.go`
  - Top-level Responses extras are selectively mapped, ignored, or passed through.
  - Some unknown Responses fields are silently ignored.
  - Non-function tools can be silently dropped.
  - Built-in tool call items are converted to plain text markers.
- `pkg/protocol/openai/convert_chat_to_responses.go`
  - Chat responses are converted back to Responses output items, including message, function call, reasoning, DSML, usage, and finish-reason mapping.
- `pkg/protocol/anthropic/request_to_chat.go`
  - Anthropic Messages input is limited to text plus tool use/tool result.
  - Assistant history `thinking` blocks are currently dropped.
- `pkg/protocol/anthropic/response_from_chat.go`
  - Chat responses are converted to Anthropic text and tool_use blocks.
  - Chat reasoning content is not converted to Anthropic thinking blocks.
  - Chat SSE to Messages SSE assumes text appears before tool calls.
- `internal/gateway/bridge/relay.go`
  - `anthropic_to_responses` stream conversion buffers the whole Anthropic SSE body before emitting Responses SSE.

## Findings

### P0. Stateful Responses cannot be bridged to Chat

`responses_to_chat` only supports stateless requests. Requests with `previous_response_id` bypass the bridge and enter transparent forwarding. For chat-only upstreams, that means the request can hit an upstream `/responses` endpoint that does not exist or does not hold the requested state. Failover is also disabled on that transparent path.

This is a current design boundary, not a small bug. Supporting it requires Warden to own a conversation-state adapter, not just a field mapping.

Risk:

- Multi-turn clients may work against native Responses providers but fail against bridged providers.
- The same public route can have different state behavior depending on the selected upstream.

Repair options:

- Short term: make stateful requests to chat-bridge-only providers fail fast with a clear `400`, instead of transparently forwarding to a likely unsupported upstream.
- Medium term: expose provider/route capability metadata so the admin UI and clients can tell whether a route supports stateful Responses.
- Long term: implement a server-side state adapter that stores previous request/response items and reconstructs Chat history for `previous_response_id`. This needs storage, retention, redaction, and session ownership rules, so it should be a separate design.

### P1. Responses-only fields are sometimes ignored silently

`convertResponsesRequestExtras` explicitly ignores `include` and `truncation`. It also ignores unknown fields after checking the allowlist. The comment mentions fields such as `prompt_cache_key`, `text`, and `include`.

Risk:

- A client can send a valid Responses parameter and receive a successful response even though the parameter had no effect.
- Debugging is difficult because request logs show the client-side field but the upstream Chat request no longer carries equivalent semantics.

Repair plan:

- Replace silent ignore with a named compatibility policy:
  - `mapped`: converted to Chat equivalent.
  - `passthrough`: forwarded unchanged because Chat-compatible providers commonly accept it.
  - `ignored_with_warning`: removed and recorded in log metadata.
  - `unsupported`: fail fast with `400`.
- Default unknown Responses fields should become `unsupported` for bridge mode unless there is a documented compatibility reason.
- Add tests for `include`, `truncation`, `text`, `prompt_cache_key`, and an arbitrary unknown field.

Recommended first patch:

- Fail fast for `include`, `truncation`, `text`, `prompt_cache_key` in `responses_to_chat`, unless a specific mapping is implemented.
- Keep `metadata`, sampling, penalties, `store`, and `stream_options` as passthrough only where provider compatibility is already relied upon.

### P1. Non-function tools and built-in tool calls lose semantics

Responses tools with type `custom` or any non-`function` tool are currently skipped in tool conversion. Input items like `custom_tool_call`, `web_search_call`, `file_search_call`, `computer_call`, and `code_interpreter_call` are converted into plain text markers.

Risk:

- A built-in or custom tool transcript can be flattened into text and accidentally influence the model as prompt content.
- Tool hooks and tool-call extraction no longer see these as tool calls.
- `tool_choice` validation only sees converted function tools, so requests involving dropped tools can become misleading.

Repair plan:

- Do not silently drop tool definitions. Return `400` for non-function tools in `responses_to_chat` unless an explicit text-degradation mode is introduced.
- Do not convert built-in tool call items to plain text by default. Either reject them or require an explicit bridge option such as `allow_tool_transcript_text=true`.
- Preserve current plain-text conversion only behind a clearly named compatibility flag if some existing providers depend on it.
- Add request-conversion tests for custom tools, web search calls, file search calls, computer calls, and code interpreter calls.

### P1. `anthropic_to_responses` streaming is buffered, not live

`StreamAnthropicAsResponses` reads the whole upstream Anthropic SSE body into memory, then converts it into Responses SSE. It has a 64 MiB cap.

Risk:

- Client TTFT equals full upstream completion time.
- Long tool-use streams lose incremental behavior.
- Memory usage grows with response size.
- Admin TTFT and user-visible stream behavior diverge from native Responses and `responses_to_chat`.

Repair plan:

- Implement a stateful Anthropic SSE -> Responses SSE converter instead of routing through buffered Anthropic -> Chat SSE -> Responses SSE.
- Map Anthropic events directly:
  - `message_start` -> `response.created` / `response.in_progress`
  - text block start/delta/stop -> Responses message item and output_text delta/done
  - thinking block start/delta/stop -> Responses reasoning item and `response.reasoning.delta`
  - tool_use block start/input_json_delta/stop -> Responses function_call item and function_call_arguments delta/done
  - `message_delta` / `message_stop` -> final item done events and `response.completed`
- Preserve original Anthropic SSE for usage extraction until the direct converter has equivalent usage coverage.

### P2. Chat -> Anthropic does not emit thinking blocks

`ChatResponseToMessagesResponse` converts Chat content and tool calls into Anthropic content blocks. It does not convert `Message.ReasoningContent` into an Anthropic `thinking` block. The streaming converter's `chatStreamChunk` also has no reasoning fields.

Risk:

- OpenAI-compatible providers that return `reasoning_content`, `reasoning`, or `thinking` lose that content when exposed through Anthropic Messages.
- This is asymmetric with recent Responses bridge fixes, where reasoning content is preserved.

Repair plan:

- Extend non-stream Chat -> Messages conversion to prepend or append a `thinking` block when `Message.ReasoningContent` is present.
- Extend Chat SSE -> Messages SSE to recognize `delta.reasoning_content`, `delta.reasoningContent`, `delta.reasoning`, and `delta.thinking`.
- Emit Anthropic `content_block_start(type=thinking)`, `content_block_delta(type=thinking_delta)`, and `content_block_stop` where compatible.
- Add tests for non-stream and stream reasoning preservation.

Open question:

- Some Anthropic clients may not expect thinking blocks unless extended thinking was requested. If compatibility risk is high, expose it behind a provider option.

### P2. Anthropic assistant history thinking is dropped

In `MessagesRequestToChatRequest`, assistant history content blocks of type `thinking` are deliberately ignored.

Risk:

- Replay of a prior Anthropic conversation into a Chat upstream loses part of the assistant transcript.
- Some models use reasoning history as hidden context; dropping it can change behavior.

Repair plan:

- Convert assistant `thinking` blocks to `Message.ReasoningContent` in Chat IR instead of dropping them.
- If the target Chat upstream does not support reasoning fields, decide at the upstream adapter layer whether to pass, strip with warning, or reject.
- Add tests for assistant history with text + thinking + tool_use.

### P2. Multimodal and file-like content support is narrow

Responses input image blocks are mapped to Chat `image_url` when possible. Other multimodal or file-like content is not mapped as a first-class bridge feature.

Risk:

- `input_file`, audio, generated image references, computer-use screenshots, and provider-specific content blocks can be lost, rejected late, or passed through in shapes the upstream does not understand.
- Invalid `input_image` blocks are currently dropped instead of failing.

Repair plan:

- Make invalid `input_image` a conversion error.
- Add explicit decisions for `input_file`, audio, and computer-use image blocks:
  - map if Chat has a stable equivalent,
  - reject if no equivalent exists,
  - or require an explicit text-degradation option.
- Add tests for invalid image block, input file, and unsupported media content.

### P2. Anthropic text/tool ordering restriction is brittle

The Chat SSE -> Anthropic SSE converter fails if text content appears after tool call deltas. Some OpenAI-compatible providers can emit tool-call deltas and later text, or interleave fields in ways that are not strictly ordered.

Risk:

- A provider-specific stream order can abort an otherwise valid request mid-stream.
- The error happens after response headers may already be sent, so client handling is poor.

Repair plan:

- Decide the supported ordering contract and document it.
- Prefer a converter that can maintain multiple content blocks and assign stable Anthropic indexes even when text and tool deltas are interleaved.
- If interleaving cannot be supported, preflight cannot catch it, so log and surface a clear bridge error in the final stream record.

### P3. Usage and finish semantics are approximate

Usage mapping covers the main prompt/completion totals and some details, but bridge paths cannot guarantee full parity for provider-specific fields such as cache tokens, reasoning tokens, tool usage, image token details, service tier behavior, or Anthropic-specific stop metadata.

Risk:

- Admin metrics may be exact for common fields but incomplete for advanced provider accounting.
- Clients consuming Responses usage details may see less information on bridged providers.

Repair plan:

- Define a usage fidelity matrix per bridge mode.
- Preserve raw usage objects in `Extra` or log metadata where possible.
- Add tests that verify known fields are mapped and unknown fields are not silently misrepresented.

## Recommended Repair Phases

### Phase 1: Make lossy behavior explicit

Goal: stop silent semantic loss.

Tasks:

- Fail fast or log structured warnings for ignored Responses fields.
- Reject non-function tools in `responses_to_chat` unless explicitly configured otherwise.
- Reject built-in tool call items instead of converting them to plain text by default.
- Make invalid `input_image` a conversion error.
- Add tests for every rejected field and rejected item type.

Acceptance:

- Unsupported bridge requests return `400` with field-level context.
- Request logs expose bridge-compatibility warnings only for deliberate degradations.
- Existing supported function-tool and image cases keep passing.

### Phase 2: Preserve reasoning across all bridge directions

Goal: make reasoning/thinking visibility symmetric.

Tasks:

- Convert Chat `ReasoningContent` to Anthropic `thinking` blocks in non-stream responses.
- Convert Chat reasoning delta aliases to Anthropic thinking deltas in stream responses.
- Convert Anthropic assistant history `thinking` blocks into Chat IR reasoning content.
- Add log-detail tests to keep reasoning visible after bridge assembly.

Acceptance:

- `responses_to_chat`, `anthropic_to_chat`, and `anthropic_to_responses` preserve reasoning in both logs and client responses where the target protocol has a visible representation.

### Phase 3: Replace buffered `anthropic_to_responses` streaming

Goal: restore real streaming semantics for Anthropic-backed Responses routes.

Tasks:

- Implement a direct Anthropic SSE -> Responses SSE state machine.
- Keep raw Anthropic event capture for token usage and diagnostics.
- Cover text, thinking, tool_use, message_delta usage, clean stop, and truncated streams.

Acceptance:

- Client receives early `response.created` / `response.in_progress` before upstream completion.
- Text, thinking, and tool-use deltas stream incrementally.
- Truncated upstream streams still produce inspectable partial logs.

### Phase 4: Decide stateful bridge strategy

Goal: avoid misleading stateful Responses behavior on chat-only providers.

Tasks:

- Add capability detection for stateful Responses by provider/route.
- Fail fast for `previous_response_id` on chat-bridge-only upstreams.
- Draft a separate design if Warden should own response state reconstruction.

Acceptance:

- Clients can tell whether a route supports stateful Responses.
- No chat-only bridge path transparently forwards stateful Requests to an unsupported upstream endpoint.

## Test Plan

Targeted packages:

- `go test ./pkg/protocol/openai`
- `go test ./pkg/protocol/anthropic`
- `go test ./internal/gateway/bridge`
- `go test ./internal/gateway ./internal/gateway/integration`
- `go test ./internal/gateway/observe`

Required new tests:

- Responses extra fields:
  - `include`
  - `truncation`
  - `text`
  - `prompt_cache_key`
  - unknown field
- Responses tools:
  - custom tool definition rejected
  - non-function tool definition rejected
  - built-in tool call item rejected unless explicit degradation is enabled
  - function tool behavior remains unchanged
- Multimodal:
  - valid `input_image` still maps to Chat `image_url`
  - invalid `input_image` returns conversion error
  - unsupported media blocks return conversion error
- Reasoning:
  - Chat non-stream `reasoning_content` -> Anthropic thinking block
  - Chat stream reasoning aliases -> Anthropic thinking delta
  - Anthropic assistant history thinking -> Chat IR reasoning content
  - Responses log assembly still shows reasoning content
- Streaming:
  - direct Anthropic SSE -> Responses SSE text path
  - direct Anthropic SSE -> Responses SSE thinking path
  - direct Anthropic SSE -> Responses SSE tool_use path
  - upstream truncation produces partial log and bridge error

## Documentation Updates After Repair

After implementation, update authoritative docs:

- `ARCHITECTURE.md`: bridge-mode state machine and stateful Responses behavior.
- `docs/responses-protocol-support.md`: exact `responses_to_chat` and `anthropic_to_responses` compatibility matrix.
- `docs/anthropic-messages-to-chat-plan.md`: reasoning/thinking behavior and stream ordering contract.
- `docs/provider.md`: route/provider capability display and probe behavior.
- Package READMEs under `pkg/protocol`, `internal/gateway`, and `internal/gateway/bridge` if implementation boundaries move.

## Do Not Do

- Do not add broad fallback text conversion for unsupported tools by default. It hides semantic loss.
- Do not claim full Responses or Anthropic compatibility while the bridge remains a controlled subset.
- Do not implement server-side stateful Responses reconstruction without a separate persistence and retention design.
- Do not silently discard fields just to keep third-party clients from failing; bridge mode should be predictable before it is permissive.
