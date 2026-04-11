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

function normalizeLogJSON(value) {
	if (Array.isArray(value)) {
		return value.map((item) => normalizeLogJSON(item));
	}
	if (!value || typeof value !== "object") {
		return maybeParseJSONObjectString(value);
	}

	const normalized = {};
	for (const [key, raw] of Object.entries(value)) {
		const parsed = maybeParseJSONObjectString(raw);
		normalized[key] = parsed && typeof parsed === "object"
			? normalizeLogJSON(parsed)
			: parsed;
	}
	return normalized;
}

function renderEscapes(s) {
	if (typeof s !== "string") return String(s);
	return s.replace(/\\n/g, "\n").replace(/\\t/g, "\t").replace(/\\r/g, "\r");
}

function extractPreview(msg) {
	const c = msg.content;
	if (!c) return "";
	if (typeof c === "string") return truncate(c, 120);
	if (Array.isArray(c)) {
		const text = c
			.filter((part) => ["text", "input_text", "output_text"].includes(part?.type) && typeof part.text === "string")
			.map((part) => part.text)
			.join(" ");
		if (text) return truncate(text, 120);
		const types = [...new Set(c.map((p) => p.type))];
		return "[" + types.join(", ") + "]";
	}
	return "";
}

function normalizeMsg(msg) {
	const preview = msg.role === "system"
		? truncate((typeof msg.content === "string" ? msg.content : "").replace(/\s+/g, " "), 60)
		: extractPreview(msg);
	return {
		role: msg.role,
		raw: msg,
		toolCalls: msg.tool_calls || null,
		toolCallId: msg.tool_call_id || "",
		preview,
	};
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
			const syntheticToolCalls = toolUseBlocks.map((b) => ({
				id: b.id,
				type: "function",
				function: {
					name: b.name,
					arguments: typeof b.input === "string" ? b.input : JSON.stringify(b.input),
				},
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
		if (item.type === "function_call") {
			nodes.push({
				role: "assistant",
				raw: item,
				toolCalls: [{
					id: item.call_id,
					type: "function",
					function: { name: item.name, arguments: typeof item.arguments === "string" ? item.arguments : JSON.stringify(item.arguments) },
				}],
				toolCallId: "",
				preview: "",
			});
		} else if (item.type === "function_call_output") {
			nodes.push({
				role: "tool",
				raw: item,
				toolCalls: null,
				toolCallId: item.call_id || "",
				preview: truncate(typeof item.output === "string" ? item.output : JSON.stringify(item.output), 120),
			});
		} else if (item.type === "message" && Array.isArray(item.content)) {
			const textPreview = item.content.filter((c) => c.type === "text" || c.type === "output_text").map((c) => c.text).join(" ");
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
		} catch {
			// ignore parse errors
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

	const messageChain = computed(() => {
		if (!selected.value) return [];
		let req = selected.value.request;
		if (!req) return [];
		if (typeof req === "string") {
			try {
				req = JSON.parse(req);
			} catch {
				return [];
			}
		}

		const fmt = detectRequestFormat(req);
		if (fmt === "anthropic") return parseAnthropicMessages(req);
		if (fmt === "responses") return parseResponsesMessages(req);

		const msgs = req.messages;
		if (!Array.isArray(msgs)) return [];
		return msgs.map(normalizeMsg);
	});

	const timelineNodes = computed(() => {
		if (!selected.value) return [];
		const chain = messageChain.value;
		if (!chain.length) return [];

		const toolResultMap = new Map();
		for (const msg of chain) {
			if (msg.role === "tool" && msg.toolCallId) {
				toolResultMap.set(msg.toolCallId, msg);
			}
		}

		const nodes = [];
		const pairedToolIds = new Set();

		let lastUserIdx = -1;
		for (let i = chain.length - 1; i >= 0; i--) {
			if (chain[i].role === "user") { lastUserIdx = i; break; }
		}

		for (let i = 0; i < chain.length; i++) {
			const msg = chain[i];
			if (msg.role === "tool" && pairedToolIds.has(msg.toolCallId)) continue;
			const isLastSection = i === lastUserIdx;

			if (msg.role === "assistant" && msg.toolCalls?.length) {
				nodes.push({
					type: "message",
					dotType: "assistant",
					label: t("logs.assistant"),
					preview: msg.preview,
					raw: msg.raw,
					defaultOpen: isLastSection,
				});
				for (const tc of msg.toolCalls) {
					const callId = tc.id || tc.tool_call_id;
					const result = callId ? toolResultMap.get(callId) : null;
					if (result) pairedToolIds.add(callId);
					nodes.push({
						type: "tool-pair",
						dotType: "tool",
						label: tc.function?.name || tc.name || t("logs.tool"),
						toolName: tc.function?.name || tc.name,
						toolArgs: tc.function?.arguments || tc.arguments,
						toolResult: result ? (result.raw?.content ?? result.preview ?? "") : undefined,
						toolError: result?.raw?.is_error || false,
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
		if (resp.choices?.[0]?.message?.tool_calls?.length) {
			return resp.choices[0].message.tool_calls.map((tc) => ({
				name: tc.function?.name || tc.name,
				input: tc.function?.arguments || tc.arguments,
			}));
		}
		if (Array.isArray(resp.output)) {
			const calls = resp.output.filter((item) => item.type === "function_call");
			if (calls.length) return calls.map((fc) => ({ name: fc.name, input: fc.arguments }));
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
