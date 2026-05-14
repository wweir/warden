import { computed } from "vue";
import { useI18n } from "vue-i18n";

function truncate(s, n) {
	return s.length > n ? s.slice(0, n) + "..." : s;
}

function formatJSON(data) {
	if (!data) return "";
	try {
		const obj = typeof data === "string" ? JSON.parse(data) : data;
		return JSON.stringify(obj, null, 2);
	} catch {
		return String(data);
	}
}

function maybeParseJSONObjectString(value) {
	if (typeof value !== "string") return value;
	const trimmed = value.trim();
	if (!trimmed) return value;
	if (!(trimmed.startsWith("{") || trimmed.startsWith("["))) return value;
	try {
		return JSON.parse(trimmed);
	} catch {
		return value;
	}
}

const MAX_ARRAY_LEN = 128;

function normalizeLogJSON(value, depth = 0) {
	if (depth > 10) return "[Depth limit exceeded]";
	if (Array.isArray(value)) {
		if (value.length > MAX_ARRAY_LEN) {
			const head = value.slice(0, MAX_ARRAY_LEN).map((item) => normalizeLogJSON(item, depth + 1));
			head.push(`... (${value.length - MAX_ARRAY_LEN} more items)`);
			return head;
		}
		return value.map((item) => normalizeLogJSON(item, depth + 1));
	}
	if (!value || typeof value !== "object") {
		return maybeParseJSONObjectString(value);
	}

	const normalized = {};
	for (const [key, raw] of Object.entries(value)) {
		const parsed = maybeParseJSONObjectString(raw);
		normalized[key] = parsed && typeof parsed === "object"
			? normalizeLogJSON(parsed, depth + 1)
			: parsed;
	}
	return normalized;
}

function renderEscapes(s) {
	if (typeof s !== "string") return String(s);
	return s.replace(/\\n/g, "\n").replace(/\\t/g, "\t").replace(/\\r/g, "\r");
}

function extractTextParts(value) {
	if (value == null) return "";
	if (typeof value === "string") return value;
	if (Array.isArray(value)) {
		return value
			.map((part) => {
				if (typeof part === "string") return part;
				if (!part || typeof part !== "object") return "";
				if (["text", "input_text", "output_text"].includes(part.type) && typeof part.text === "string") return part.text;
				if (typeof part.input_text === "string") return part.input_text;
				if (typeof part.output_text === "string") return part.output_text;
				return "";
			})
			.filter(Boolean)
			.join(" ");
	}
	if (typeof value === "object") {
		if (typeof value.text === "string") return value.text;
		if (typeof value.input_text === "string") return value.input_text;
		if (typeof value.output_text === "string") return value.output_text;
	}
	return "";
}

function extractMessageText(msg) {
	if (msg == null) return "";
	if (typeof msg === "string") return msg;
	const contentText = extractTextParts(msg.content);
	if (contentText) return contentText;
	const inputText = extractTextParts(msg.input_text);
	if (inputText) return inputText;
	const outputText = extractTextParts(msg.output_text);
	if (outputText) return outputText;
	return extractTextParts(msg.text);
}

function extractPreview(msg) {
	const text = extractMessageText(msg);
	if (text) return truncate(text.replace(/\s+/g, " "), 120);
	if (Array.isArray(msg?.content)) {
		const types = [...new Set(msg.content.map((p) => p?.type).filter(Boolean))];
		if (types.length) return "[" + types.join(", ") + "]";
	}
	return "";
}

function normalizeMsg(msg) {
	const preview = msg.role === "system"
		? truncate(extractMessageText(msg).replace(/\s+/g, " "), 60)
		: extractPreview(msg);
	return {
		role: msg.role,
		raw: msg,
		toolCalls: msg.tool_calls || null,
		toolCallId: msg.tool_call_id || "",
		preview,
	};
}

function stringOrJSON(value) {
	if (value == null) return "";
	return typeof value === "string" ? value : JSON.stringify(value);
}

function isResponsesToolCall(call) {
	return call?.type === "function_call" || call?.type === "custom_tool_call";
}

function toolCallID(call) {
	if (isResponsesToolCall(call)) return call?.call_id || call?.id || call?.tool_call_id || "";
	return call?.id || call?.tool_call_id || call?.call_id || "";
}

function normalizedToolCall(call) {
	if (!call || typeof call !== "object") return null;
	const fn = call.function && typeof call.function === "object" ? call.function : null;
	const args = fn ? fn.arguments : (call.arguments ?? call.input ?? call.content ?? call.payload ?? "");
	return {
		id: toolCallID(call),
		type: call.type || "function",
		function: {
			name: fn?.name || call.name || call.tool_name || call.type || "",
			arguments: stringOrJSON(args),
		},
		raw: call,
	};
}

function toolResultContent(result) {
	if (!result || typeof result !== "object") return result ?? "";
	return result.raw?.content ??
		result.raw?.output ??
		result.raw?.result ??
		result.raw?.text ??
		result.content ??
		result.output ??
		result.result ??
		result.preview ??
		"";
}

function toolResultError(result) {
	if (!result || typeof result !== "object") return false;
	return Boolean(result.raw?.is_error || result.raw?.error || result.is_error || result.error);
}

function parseAnthropicMessages(req) {
	const nodes = [];
	if (req.system) {
		const sysText = Array.isArray(req.system)
			? req.system.filter((b) => b.type === "text").map((b) => b.text).join(" ")
			: req.system;
		nodes.push({
			role: "system",
			raw: { role: "system", content: req.system },
			toolCalls: null,
			toolCallId: "",
			preview: truncate(sysText.replace(/\s+/g, " "), 60),
		});
	}
	if (!Array.isArray(req.messages)) return nodes;

	for (const msg of req.messages) {
		const content = msg.content;
		if (typeof content === "string" || !Array.isArray(content)) {
			nodes.push(normalizeMsg(msg));
			continue;
		}
		const toolUseBlocks = content.filter((b) => b.type === "tool_use");
		const toolResultBlocks = content.filter((b) => b.type === "tool_result");
		const textBlocks = content.filter((b) => b.type === "text");

		if (toolUseBlocks.length > 0) {
			const textPreview = textBlocks.map((b) => b.text).join(" ");
			const syntheticToolCalls = toolUseBlocks.map((b) => normalizedToolCall({
				id: b.id,
				type: "function",
				name: b.name,
				input: b.input,
			}));
			nodes.push({
				role: "assistant",
				raw: msg,
				toolCalls: syntheticToolCalls,
				toolCallId: "",
				preview: textPreview ? truncate(textPreview, 120) : "",
			});
		} else if (toolResultBlocks.length > 0) {
			for (const b of toolResultBlocks) {
				const resultContent = Array.isArray(b.content)
					? b.content.filter((c) => c.type === "text").map((c) => c.text).join("")
					: (b.content || "");
				nodes.push({
					role: "tool",
					raw: { role: "tool", tool_call_id: b.tool_use_id, content: resultContent },
					toolCalls: null,
					toolCallId: b.tool_use_id || "",
					preview: truncate(resultContent, 120),
					isError: Boolean(b.is_error),
				});
			}
		} else {
			nodes.push(normalizeMsg(msg));
		}
	}
	return nodes;
}

function parseResponsesMessages(req) {
	const nodes = [];
	const input = req.input;
	if (!input) return nodes;
	if (typeof input === "string") {
		nodes.push({
			role: "user",
			raw: { role: "user", content: input },
			toolCalls: null,
			toolCallId: "",
			preview: truncate(input, 120),
		});
		return nodes;
	}
	if (!Array.isArray(input)) return nodes;

	for (const item of input) {
		if (typeof item === "string") {
			nodes.push({
				role: "user",
				raw: { role: "user", content: item },
				toolCalls: null,
				toolCallId: "",
				preview: truncate(item, 120),
			});
			continue;
		}
		const role = item.role || item.type || "user";
		if (item.type === "function_call" || item.type === "custom_tool_call") {
			const preview = extractMessageText(item);
			const toolCall = normalizedToolCall(item);
			nodes.push({
				role: "assistant",
				raw: item,
				toolCalls: toolCall ? [toolCall] : [],
				toolCallId: "",
				preview: preview ? truncate(preview.replace(/\s+/g, " "), 120) : "",
			});
		} else if (item.type === "function_call_output" || item.type === "custom_tool_call_output") {
			const output = item.output ?? item.content ?? item.result ?? "";
			nodes.push({
				role: "tool",
				raw: item,
				toolCalls: null,
				toolCallId: item.call_id || item.id || "",
				preview: truncate(typeof output === "string" ? output : JSON.stringify(output), 120),
				isError: Boolean(item.is_error || item.error),
			});
		} else if (item.type === "message") {
			const textPreview = extractMessageText(item);
			nodes.push({
				role: item.role || "user",
				raw: item,
				toolCalls: null,
				toolCallId: "",
				preview: truncate(textPreview, 120),
			});
		} else {
			nodes.push({
				role,
				raw: item,
				toolCalls: null,
				toolCallId: "",
				preview: extractPreview(item),
			});
		}
	}
	return nodes;
}

function detectRequestFormat(req) {
	if (!req) return "unknown";
	if (typeof req.system === "string" || Array.isArray(req.system)) return "anthropic";
	if (Array.isArray(req.messages)) {
		for (const m of req.messages) {
			if (Array.isArray(m.content) && m.content.some((b) => b.type === "tool_use" || b.type === "tool_result")) {
				return "anthropic";
			}
		}
		return "openai-chat";
	}
	if (req.input !== undefined) return "responses";
	return "unknown";
}

function extractAssembledText(log) {
	if (!log) return "";
	let resp = log.response;
	if (!resp) return "";
	if (typeof resp === "string") {
		try {
			resp = JSON.parse(resp);
		} catch {
			return extractTextFromSSE(resp);
		}
	}
	// Embedding response
	if (resp.data && Array.isArray(resp.data) && resp.data[0]?.embedding) {
		const count = resp.data.length;
		const dims = Array.isArray(resp.data[0].embedding) ? resp.data[0].embedding.length : "?";
		return `[Embedding: ${count} vector(s), ${dims} dimensions]`;
	}
	if (resp.choices && Array.isArray(resp.choices) && resp.choices.length > 0) {
		const msg = resp.choices[0].message || resp.choices[0].delta;
		if (msg) {
			const content = msg.content;
			if (typeof content === "string") return content;
			if (Array.isArray(content)) {
				return content
					.filter((p) => p.type === "text")
					.map((p) => p.text)
					.join("");
			}
		}
	}
	if (resp.content && Array.isArray(resp.content)) {
		const textParts = resp.content.filter((b) => b.type === "text").map((b) => b.text);
		if (textParts.length) return textParts.join("");
	}
	if (resp.output && Array.isArray(resp.output)) {
		const parts = [];
		for (const item of resp.output) {
			if (typeof item === "string") {
				parts.push(item);
				continue;
			}
			if (item.type === "message" && Array.isArray(item.content)) {
				for (const c of item.content) {
					if (c.type === "output_text" && c.text) parts.push(c.text);
					else if (c.type === "text" && c.text) parts.push(c.text);
				}
			}
		}
		if (parts.length) return parts.join("\n");
	}
	return formatJSON(resp);
}

function parseResponseBody(response) {
	if (!response) return null;
	if (typeof response !== "string") return response;
	try {
		return JSON.parse(response);
	} catch {
		return null;
	}
}

function responseToolPairNodes(log, seenToolIds = new Set()) {
	const resp = parseResponseBody(log?.response);
	if (!resp) {
		return typeof log?.response === "string" ? parseSSEToolPairNodes(log.response, seenToolIds) : [];
	}
	const nodes = [];

	if (Array.isArray(resp.choices)) {
		for (const choice of resp.choices) {
			const msg = choice?.message || choice?.delta;
			const toolCalls = Array.isArray(msg?.tool_calls) ? msg.tool_calls : [];
			for (const rawCall of toolCalls) {
				const call = normalizedToolCall(rawCall);
				if (!call || seenToolIds.has(call.id)) continue;
				seenToolIds.add(call.id);
				nodes.push(toolPairNodeFromCall(call, null, msg?.content || "", rawCall));
			}
		}
	}

	if (Array.isArray(resp.content)) {
		for (const block of resp.content) {
			if (block?.type !== "tool_use") continue;
			const call = normalizedToolCall({ id: block.id, name: block.name, input: block.input, raw: block });
			if (!call || seenToolIds.has(call.id)) continue;
			seenToolIds.add(call.id);
			nodes.push(toolPairNodeFromCall(call, null, "", block));
		}
	}

	if (Array.isArray(resp.output)) {
		const outputsByCallID = new Map();
		for (const item of resp.output) {
			if (item?.type !== "function_call_output" && item?.type !== "custom_tool_call_output") continue;
			const id = item.call_id || item.id || "";
			if (id) outputsByCallID.set(id, item);
		}
		for (const item of resp.output) {
			if (item?.type !== "function_call" && item?.type !== "custom_tool_call") continue;
			const call = normalizedToolCall(item);
			if (!call || seenToolIds.has(call.id)) continue;
			seenToolIds.add(call.id);
			nodes.push(toolPairNodeFromCall(call, outputsByCallID.get(call.id) || null, extractMessageText(item), item));
		}
	}

	return nodes;
}

function parseSSEToolPairNodes(text, seenToolIds = new Set()) {
	const chatCalls = new Map();
	const responseToolState = { calls: new Map(), aliases: new Map() };
	const anthropicBlocks = new Map();

	for (const line of text.split("\n")) {
		if (!line.startsWith("data: ")) continue;
		const data = line.slice(6);
		if (data === "[DONE]") continue;
		let chunk;
		try {
			chunk = JSON.parse(data);
		} catch {
			continue;
		}
		collectChatSSETools(chunk, chatCalls);
		collectResponsesSSETools(chunk, responseToolState);
		collectAnthropicSSETools(chunk, anthropicBlocks);
	}

	const nodes = [];
	for (const call of chatCalls.values()) {
		const normalized = normalizedToolCall(call);
		if (!normalized || seenToolIds.has(normalized.id)) continue;
		seenToolIds.add(normalized.id);
		nodes.push(toolPairNodeFromCall(normalized, null, "", call.raw || call));
	}
	for (const call of responseToolState.calls.values()) {
		const normalized = normalizedToolCall(call);
		if (!normalized || seenToolIds.has(normalized.id)) continue;
		seenToolIds.add(normalized.id);
		nodes.push(toolPairNodeFromCall(normalized, null, "", call.raw || call));
	}
	for (const block of anthropicBlocks.values()) {
		const normalized = normalizedToolCall({ id: block.id, name: block.name, input: block.input, raw: block.raw || block });
		if (!normalized || seenToolIds.has(normalized.id)) continue;
		seenToolIds.add(normalized.id);
		nodes.push(toolPairNodeFromCall(normalized, null, "", block.raw || block));
	}
	return nodes;
}

function collectChatSSETools(chunk, calls) {
	const deltas = Array.isArray(chunk?.choices) ? chunk.choices.map((c) => c?.delta).filter(Boolean) : [];
	for (const delta of deltas) {
		for (const tc of delta.tool_calls || []) {
			const key = tc.id || String(tc.index ?? calls.size);
			if (!calls.has(key)) calls.set(key, { id: tc.id || key, type: tc.type || "function", function: { name: "", arguments: "" }, raw: tc });
			const call = calls.get(key);
			if (tc.id) call.id = tc.id;
			if (tc.type) call.type = tc.type;
			if (tc.function?.name) call.function.name = tc.function.name;
			if (tc.function?.arguments) call.function.arguments += tc.function.arguments;
			call.raw = tc;
		}
	}
}

function collectResponsesSSETools(chunk, state) {
	const item = chunk.item || chunk.output_item || null;
	if (item?.type === "function_call" || item?.type === "custom_tool_call") {
		const itemKey = responseToolItemKey(item, chunk);
		const key = responseToolMergedKey(state, itemKey, item.call_id || "", String(item.output_index ?? state.calls.size));
		mergeResponseToolCall(state, key, item, itemKey);
	}
	if (chunk.type === "response.function_call_arguments.delta" && typeof chunk.delta === "string") {
		const itemKey = responseToolEventItemKey(chunk);
		const key = responseToolMergedKey(state, itemKey, chunk.call_id || "", String(chunk.output_index ?? ""));
		if (!key) return;
		const call = ensureResponseToolCall(state, key, itemKey);
		if (chunk.call_id) call.call_id = chunk.call_id;
		call.arguments = (call.arguments || "") + chunk.delta;
	}
	if (chunk.type === "response.function_call_arguments.done") {
		const itemKey = responseToolEventItemKey(chunk);
		const key = responseToolMergedKey(state, itemKey, chunk.call_id || "", String(chunk.output_index ?? ""));
		if (!key) return;
		const call = ensureResponseToolCall(state, key, itemKey);
		if (chunk.call_id) call.call_id = chunk.call_id;
		if (typeof chunk.arguments === "string") call.arguments = chunk.arguments;
	}
	if (chunk.response?.output && Array.isArray(chunk.response.output)) {
		for (const out of chunk.response.output) {
			if (out?.type !== "function_call" && out?.type !== "custom_tool_call") continue;
			const itemKey = responseToolItemKey(out, chunk);
			const key = responseToolMergedKey(state, itemKey, out.call_id || "", String(out.output_index ?? state.calls.size));
			mergeResponseToolCall(state, key, out, itemKey);
		}
	}
}

function responseToolItemKey(item, chunk) {
	return item?.id || responseToolEventItemKey(chunk) || (item?.output_index != null ? String(item.output_index) : "");
}

function responseToolEventItemKey(chunk) {
	return chunk?.item_id || (chunk?.output_index != null ? String(chunk.output_index) : "");
}

function responseToolCanonicalKey(state, key) {
	return state.aliases.get(key) || key;
}

function responseToolMergedKey(state, itemKey, callID, fallback = "") {
	const existingKey = itemKey ? responseToolCanonicalKey(state, itemKey) : "";
	const targetKey = callID || existingKey || fallback;
	if (!targetKey) return "";
	if (itemKey) state.aliases.set(itemKey, targetKey);
	if (callID) state.aliases.set(callID, targetKey);
	if (existingKey && existingKey !== targetKey) {
		const existing = state.calls.get(existingKey);
		const target = state.calls.get(targetKey) || { type: existing?.type || "function_call", call_id: callID || targetKey, name: "", arguments: "" };
		if (existing) {
			Object.assign(target, existing, target);
			state.calls.delete(existingKey);
		}
		state.calls.set(targetKey, target);
		state.aliases.set(existingKey, targetKey);
	}
	return targetKey;
}

function ensureResponseToolCall(state, key, itemKey = "") {
	const canonical = responseToolCanonicalKey(state, key);
	if (!state.calls.has(canonical)) {
		state.calls.set(canonical, { type: "function_call", call_id: canonical, name: "", arguments: "" });
	}
	if (itemKey) state.aliases.set(itemKey, canonical);
	return state.calls.get(canonical);
}

function mergeResponseToolCall(state, key, item, itemKey = "") {
	let canonical = responseToolCanonicalKey(state, key);
	if (item.call_id && itemKey && canonical !== item.call_id) {
		const existing = state.calls.get(canonical);
		const target = state.calls.get(item.call_id) || { type: item.type || "function_call", call_id: item.call_id, name: "", arguments: "" };
		if (existing) {
			Object.assign(target, existing, target);
			state.calls.delete(canonical);
		}
		canonical = item.call_id;
		state.aliases.set(itemKey, canonical);
		state.aliases.set(key, canonical);
	}
	const call = ensureResponseToolCall(state, canonical, itemKey);
	Object.assign(call, item, {
		call_id: item.call_id || call.call_id || canonical,
		raw: item,
	});
}

function collectAnthropicSSETools(chunk, blocks) {
	if (chunk.type === "content_block_start" && chunk.content_block?.type === "tool_use") {
		const idx = String(chunk.index ?? blocks.size);
		blocks.set(idx, {
			id: chunk.content_block.id || idx,
			name: chunk.content_block.name || "",
			input: "",
			raw: chunk.content_block,
		});
	}
	if (chunk.type === "content_block_delta" && chunk.delta?.type === "input_json_delta") {
		const idx = String(chunk.index ?? "");
		if (!idx || !blocks.has(idx)) return;
		blocks.get(idx).input += chunk.delta.partial_json || "";
	}
}

function toolPairNodeFromCall(call, result, preview, raw) {
	return {
		type: "tool-pair",
		dotType: "tool",
		label: call.function?.name || call.name || "tool",
		assistantPreview: preview || "",
		raw: raw || call.raw || call,
		toolName: call.function?.name || call.name,
		toolArgs: call.function?.arguments || call.arguments || "",
		toolResult: result ? toolResultContent({ raw: result }) : undefined,
		toolError: result ? toolResultError({ raw: result }) : false,
	};
}

function extractTextFromSSE(text) {
	const lines = text.split("\n");
	const deltaParts = [];
	let completedText = "";
	for (const line of lines) {
		if (!line.startsWith("data: ")) continue;
		const data = line.slice(6);
		if (data === "[DONE]") continue;
		try {
			const chunk = JSON.parse(data);
			if (chunk.choices?.[0]?.delta?.content) {
				deltaParts.push(chunk.choices[0].delta.content);
			}
			if (chunk.type === "response.output_text.delta" && typeof chunk.delta === "string") {
				deltaParts.push(chunk.delta);
			}
			if (chunk.response?.output && !completedText) {
				const cparts = [];
				for (const item of chunk.response.output) {
					if (item.type === "message" && Array.isArray(item.content)) {
						for (const c of item.content) {
							if ((c.type === "output_text" || c.type === "text") && c.text) {
								cparts.push(c.text);
							}
						}
					}
				}
				if (cparts.length) completedText = cparts.join("\n");
			}
			if (chunk.type === "content_block_delta" && chunk.delta?.type === "text_delta" && chunk.delta?.text) {
				deltaParts.push(chunk.delta.text);
			}
		} catch (err) {
			console.warn("[useTimeline] SSE chunk parse failed:", data.slice(0, 200), err);
		}
	}
	return deltaParts.length ? deltaParts.join("") : completedText;
}

export function useTimeline(selected) {
	const { t } = useI18n();

	function roleLabel(role) {
		switch (role) {
			case "system": return t("logs.system");
			case "user": return t("logs.user");
			case "assistant": return t("logs.assistant");
			case "tool": return t("logs.tool");
			default: return role || t("logs.unknown");
		}
	}

	let lastReqId = null;
	let lastChain = [];

	const messageChain = computed(() => {
		if (!selected.value) return [];
		const reqId = selected.value.request_id;
		let req = selected.value.request;
		if (!req) return [];
		if (typeof req === "string") {
			try {
				req = JSON.parse(req);
			} catch {
				return [];
			}
		}

		if (reqId === lastReqId) return lastChain;

		const fmt = detectRequestFormat(req);
		if (fmt === "anthropic") lastChain = parseAnthropicMessages(req);
		else if (fmt === "responses") lastChain = parseResponsesMessages(req);
		else {
			const msgs = req.messages;
			lastChain = Array.isArray(msgs) ? msgs.map(normalizeMsg) : [];
		}
		lastReqId = reqId;
		return lastChain;
	});

	const timelineNodes = computed(() => {
		if (!selected.value) return [];
		const chain = messageChain.value;
		if (!chain.length) return responseToolPairNodes(selected.value);

		const toolResultMap = new Map();
		for (const msg of chain) {
			if (msg.role === "tool" && msg.toolCallId) {
				toolResultMap.set(msg.toolCallId, msg);
			}
		}

		const nodes = [];
		const pairedToolIds = new Set();
		const seenToolIds = new Set();

		let lastUserIdx = -1;
		for (let i = chain.length - 1; i >= 0; i--) {
			if (chain[i].role === "user") { lastUserIdx = i; break; }
		}

		for (let i = 0; i < chain.length; i++) {
			const msg = chain[i];
			if (msg.role === "tool" && pairedToolIds.has(msg.toolCallId)) continue;
			const isLastSection = i === lastUserIdx;

			if (msg.role === "assistant" && msg.toolCalls?.length) {
				for (let j = 0; j < msg.toolCalls.length; j++) {
					const tc = msg.toolCalls[j];
					const callId = tc.id || tc.tool_call_id;
					const result = callId ? toolResultMap.get(callId) : null;
					if (callId) seenToolIds.add(callId);
					if (result) pairedToolIds.add(callId);
					nodes.push({
						type: "tool-pair",
						dotType: "tool",
						label: tc.function?.name || tc.name || t("logs.tool"),
						assistantPreview: msg.preview,
						raw: j === 0 ? msg.raw : null,
						toolName: tc.function?.name || tc.name,
						toolArgs: tc.function?.arguments || tc.arguments,
						toolResult: result ? toolResultContent(result) : undefined,
						toolError: toolResultError(result),
						defaultOpen: isLastSection,
					});
				}
				continue;
			}

			nodes.push({
				type: "message", dotType: msg.role, label: roleLabel(msg.role),
				preview: msg.preview, raw: msg.raw, defaultOpen: isLastSection,
			});
		}

		for (const step of selected.value.steps || []) {
			const stepNode = {
				type: "step",
				dotType: "step",
				label: t("logs.gatewayStep", { n: step.iteration }),
			};
			nodes.push(stepNode);
			if (step.tool_calls?.length) {
				for (const tc of step.tool_calls) {
					const tr = step.tool_results?.find((r) => r.tool_call_id === tc.id);
					if (tc.id) seenToolIds.add(tc.id);
					nodes.push({
						type: "tool-pair",
						dotType: "tool",
						label: tc.name || t("logs.tool"),
						toolName: tc.name,
						toolArgs: tc.arguments,
						toolResult: tr ? tr.output : undefined,
						toolError: tr?.is_error || false,
					});
				}
			}
		}

		nodes.push(...responseToolPairNodes(selected.value, seenToolIds));

		return nodes;
	});

	const assembledText = computed(() => {
		if (!selected.value) return "";
		return extractAssembledText(selected.value);
	});

	const responseHasText = computed(() => {
		if (!selected.value?.response) return false;
		return assembledText.value !== formatJSON(selected.value.response);
	});

	const responseToolCalls = computed(() => {
		if (!selected.value?.response) return [];
		let resp = selected.value.response;
		if (typeof resp === "string") {
			try { resp = JSON.parse(resp); } catch { return []; }
		}
		if (Array.isArray(resp.content)) {
			const tools = resp.content.filter((b) => b.type === "tool_use");
			if (tools.length) return tools.map((b) => ({ name: b.name, input: b.input }));
		}
		if (Array.isArray(resp.choices)) {
			const calls = [];
			for (const choice of resp.choices) {
				for (const tc of choice?.message?.tool_calls || choice?.delta?.tool_calls || []) {
					calls.push({
						name: tc.function?.name || tc.name,
						input: tc.function?.arguments || tc.arguments,
					});
				}
			}
			if (calls.length) return calls;
		}
		if (Array.isArray(resp.output)) {
			const calls = resp.output.filter((item) => item.type === "function_call" || item.type === "custom_tool_call");
			if (calls.length) return calls.map((fc) => ({ name: fc.name || fc.tool_name, input: fc.arguments ?? fc.input ?? fc.content ?? fc.payload }));
		}
		return [];
	});

	const selectedJSON = computed(() => {
		if (!selected.value) return "";
		return JSON.stringify(normalizeLogJSON(selected.value), null, 2);
	});

	function latestContentPreview(log) {
		if (!log || typeof log !== "object") return "";

		const responseText = extractAssembledText(log).trim();
		if (responseText && responseText !== formatJSON(log.response)) {
			return truncate(responseText.replace(/\s+/g, " "), 120);
		}

		const steps = Array.isArray(log.steps) ? log.steps : [];
		for (let i = steps.length - 1; i >= 0; i--) {
			const step = steps[i];
			const stepResponse = step?.llm_response;
			if (!stepResponse) continue;
			const stepText = extractAssembledText({ response: stepResponse }).trim();
			if (stepText && stepText !== formatJSON(stepResponse)) {
				return truncate(stepText.replace(/\s+/g, " "), 120);
			}
		}

		// fall back to lastUserPreview from session chaining
		return "";
	}

	return {
		messageChain,
		timelineNodes,
		responseToolCalls,
		responseHasText,
		assembledText,
		selectedJSON,
		formatJSON,
		renderEscapes,
		detectRequestFormat,
		latestContentPreview,
	};
}
