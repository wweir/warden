import { computed } from "vue";
import { useI18n } from "vue-i18n";

const parsedRequestCache = new WeakMap();
const parsedResponseCache = new WeakMap();
const previewCache = new WeakMap();
const fingerprintCache = new WeakMap();
const timestampCache = new WeakMap();
const previousResponseIDCache = new WeakMap();
const responseIDCache = new WeakMap();

function parseFingerprint(fp) {
	if (!fp || typeof fp !== "string" || fp.length < 6) return null;
	const sysHash = fp.slice(0, 6);
	const fsmStr = fp.slice(6);
	if (!fsmStr) return { sysHash, fsm: [] };

	const fsm = [];
	let pos = 0;
	let width = 6;
	while (pos < fsmStr.length) {
		if (fsm.length > 0) {
			width = Math.max(2, 6 - fsm.length);
		}
		const end = pos + width;
		if (end > fsmStr.length) break;
		fsm.push(fsmStr.slice(pos, end));
		pos = end;
	}
	return { sysHash, fsm };
}

function isFSMPrefix(fsm_a, fsm_b) {
	if (fsm_a.length === 0 || fsm_b.length <= fsm_a.length) return false;
	for (let i = 0; i < fsm_a.length; i++) {
		if (fsm_a[i] !== fsm_b[i]) return false;
	}
	return true;
}

function parseRequest(log) {
	if (!log || typeof log !== "object") return null;
	if (parsedRequestCache.has(log)) return parsedRequestCache.get(log);

	let req = log.request;
	if (!req) {
		parsedRequestCache.set(log, null);
		return null;
	}
	if (typeof req === "string") {
		try {
			req = JSON.parse(req);
		} catch {
			parsedRequestCache.set(log, null);
			return null;
		}
	}
	parsedRequestCache.set(log, req);
	return req;
}

function parseResponse(log) {
	if (!log || typeof log !== "object") return null;
	if (parsedResponseCache.has(log)) return parsedResponseCache.get(log);

	let resp = log.response;
	if (!resp) {
		parsedResponseCache.set(log, null);
		return null;
	}
	if (typeof resp === "string") {
		try {
			resp = JSON.parse(resp);
		} catch {
			parsedResponseCache.set(log, null);
			return null;
		}
	}
	parsedResponseCache.set(log, resp);
	return resp;
}

function getPreviousResponseID(log) {
	if (!log || typeof log !== "object") return "";
	if (previousResponseIDCache.has(log)) return previousResponseIDCache.get(log);
	const req = parseRequest(log);
	const id = req && typeof req.previous_response_id === "string" ? req.previous_response_id : "";
	previousResponseIDCache.set(log, id);
	return id;
}

function getResponseID(log) {
	if (!log || typeof log !== "object") return "";
	if (responseIDCache.has(log)) return responseIDCache.get(log);
	const resp = parseResponse(log);
	const id = resp && typeof resp.id === "string" ? resp.id : "";
	responseIDCache.set(log, id);
	return id;
}

function getTimestampMs(log) {
	if (!log || typeof log !== "object") return 0;
	if (timestampCache.has(log)) return timestampCache.get(log);
	const ts = new Date(log.timestamp).getTime();
	const normalized = Number.isFinite(ts) ? ts : 0;
	timestampCache.set(log, normalized);
	return normalized;
}

function getParsedFingerprint(log) {
	if (!log || typeof log !== "object") return null;
	if (fingerprintCache.has(log)) return fingerprintCache.get(log);
	const parsed = parseFingerprint(log.fingerprint);
	const normalized = parsed && parsed.fsm.length > 0 ? parsed : null;
	fingerprintCache.set(log, normalized);
	return normalized;
}

function truncate(s, n) {
	return s.length > n ? s.slice(0, n) + "..." : s;
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

function extractUserMessageText(message) {
	if (message == null) return "";
	if (typeof message === "string") return message;

	const content = message.content;
	if (typeof content === "string") return content;
	if (Array.isArray(content)) {
		const text = content
			.filter((part) => ["text", "input_text", "output_text"].includes(part?.type) && typeof part.text === "string")
			.map((part) => part.text)
			.join(" ");
		if (text) return text;
	}

	if (Array.isArray(message.input_text)) {
		const text = message.input_text
			.filter((part) => typeof part === "string")
			.join(" ");
		if (text) return text;
	}

	if (typeof message.input_text === "string") return message.input_text;
	if (typeof message.text === "string") return message.text;
	return "";
}

function lastUserPreview(log) {
	if (!log || typeof log !== "object") return "";
	if (previewCache.has(log)) return previewCache.get(log);

	const req = parseRequest(log);
	if (!req) {
		previewCache.set(log, "");
		return "";
	}

	let lastMsg = null;
	if (Array.isArray(req.messages)) {
		const users = req.messages.filter((m) => {
			if (m.role !== "user") return false;
			if (Array.isArray(m.content) && m.content.length > 0 &&
				m.content.every((b) => b.type === "tool_result")) return false;
			return true;
		});
		if (users.length) lastMsg = users[users.length - 1];
		if (!lastMsg && typeof req.system === "string") {
			const preview = truncate(req.system, 40);
			previewCache.set(log, preview);
			return preview;
		}
	} else if (req.input != null) {
		if (typeof req.input === "string") {
			const preview = truncate(req.input, 40);
			previewCache.set(log, preview);
			return preview;
		}
		if (Array.isArray(req.input)) {
			const users = req.input.filter((m) => m.role === "user" || typeof m === "string");
			if (users.length) lastMsg = users[users.length - 1];
		}
	}

	let preview = "";
	if (!lastMsg) {
		preview = "";
	} else if (typeof lastMsg === "string") {
		preview = truncate(lastMsg, 40);
	} else {
		preview = truncate(extractUserMessageText(lastMsg) || extractPreview(lastMsg), 40);
	}
	previewCache.set(log, preview);
	return preview;
}

function sessionTitlePreview(chain) {
	if (!chain?.logs?.length) return "";

	let lastUserText = "";
	for (const log of chain.logs) {
		const req = parseRequest(log);
		if (!req) continue;

		if (Array.isArray(req.messages)) {
			for (const message of req.messages) {
				if (message?.role === "assistant") {
					return truncate(lastUserText, 40);
				}
				if (message?.role !== "user") continue;
				if (Array.isArray(message.content) && message.content.length > 0 &&
					message.content.every((part) => part?.type === "tool_result")) {
					continue;
				}
				const text = extractUserMessageText(message).trim();
				if (text) lastUserText = text;
			}
			continue;
		}

		if (req.input == null) continue;
		if (typeof req.input === "string") {
			lastUserText = req.input.trim() || lastUserText;
			continue;
		}
		if (!Array.isArray(req.input)) continue;

		for (const item of req.input) {
			if (typeof item === "string") {
				const text = item.trim();
				if (text) lastUserText = text;
				continue;
			}
			if (item?.role === "assistant" || item?.type === "function_call" || item?.type === "message" && item?.role === "assistant") {
				return truncate(lastUserText, 40);
			}
			if (item?.role !== "user" && item?.type !== "message") continue;
			const text = extractUserMessageText(item).trim();
			if (text) lastUserText = text;
		}
	}

	if (lastUserText) return truncate(lastUserText, 40);
	for (const log of chain.logs) {
		const preview = lastUserPreview(log);
		if (preview) return preview;
	}
	return "";
}

const EMPTY_HASHES = Object.freeze([]);

export function useSessionChaining(logs) {
	const { t } = useI18n();

	const chainedLogs = computed(() => {
		const items = logs.value;
		if (!items.length) return [];

		const sorted = [...items].sort((a, b) => getTimestampMs(a) - getTimestampMs(b));

		const statefulChainsByResponseID = new Map();
		const fpChainsByKey = new Map();
		const chains = [];

		function insertChainIndex(indexMap, key, chainIdx) {
			if (!key) return;
			let arr = indexMap.get(key);
			if (!arr) {
				indexMap.set(key, [chainIdx]);
				return;
			}
			if (arr.includes(chainIdx)) return;
			if (arr.length === 0 || arr[arr.length - 1] < chainIdx) {
				arr.push(chainIdx);
				return;
			}
			for (let i = 0; i < arr.length; i++) {
				if (arr[i] > chainIdx) {
					arr.splice(i, 0, chainIdx);
					return;
				}
			}
			arr.push(chainIdx);
		}

		function routeKey(log) {
			return String(log?.route || "(unknown)");
		}

		function fpKey(route, sysHash) {
			return route + "\u0000" + sysHash;
		}

		function upgradeFingerprintIndex(chain, parsed, route) {
			if (!parsed || parsed.fsm.length === 0) return;
			if (!chain.fpKey) {
				chain.fpKey = fpKey(route, parsed.sysHash);
				insertChainIndex(fpChainsByKey, chain.fpKey, chain.idx);
			}
			chain.routeKey = route;
			chain.lastParsed = parsed;
		}

		function maybeUpgradeFingerprintIndex(chain, parsed, route) {
			if (!parsed || parsed.fsm.length === 0) return;
			if (!chain.lastParsed) {
				upgradeFingerprintIndex(chain, parsed, route);
				return;
			}
			if (chain.routeKey !== route) return;
			if (chain.fpKey && chain.fpKey !== fpKey(route, parsed.sysHash)) return;
			if (isFSMPrefix(chain.lastParsed.fsm, parsed.fsm)) {
				upgradeFingerprintIndex(chain, parsed, route);
			}
		}

		function appendToChain(chain, log, parsed) {
			chain.logs.push(log);
			const responseID = getResponseID(log);
			if (responseID) {
				statefulChainsByResponseID.set(responseID, chain.idx);
			}
			maybeUpgradeFingerprintIndex(chain, parsed, routeKey(log));
		}

		for (const log of sorted) {
			const parsed = getParsedFingerprint(log);
			const currentRouteKey = routeKey(log);
			const previousResponseID = getPreviousResponseID(log);
			let matched = false;

			if (previousResponseID) {
				const chainIdx = statefulChainsByResponseID.get(previousResponseID);
				const chain = chainIdx == null ? null : chains[chainIdx];
				if (chain && chain.routeKey === currentRouteKey) {
					appendToChain(chain, log, parsed);
					matched = true;
				}
			}

			if (!matched && parsed) {
				const candidates = fpChainsByKey.get(fpKey(currentRouteKey, parsed.sysHash)) || EMPTY_HASHES;
				for (let i = candidates.length - 1; i >= 0; i--) {
					const chain = chains[candidates[i]];
					const lastParsed = chain.lastParsed;
					if (!lastParsed) continue;
					if (chain.routeKey !== currentRouteKey) continue;
					if (isFSMPrefix(lastParsed.fsm, parsed.fsm)) {
						appendToChain(chain, log, parsed);
						matched = true;
						break;
					}
				}
			}

			if (!matched) {
				const chain = {
					idx: chains.length,
					id: (log.request_id || "") + "_" + chains.length,
					logs: [log],
					routeKey: currentRouteKey,
					lastParsed: null,
					fpKey: "",
				};
				chains.push(chain);
				maybeUpgradeFingerprintIndex(chain, parsed, currentRouteKey);
				const responseID = getResponseID(log);
				if (responseID) {
					statefulChainsByResponseID.set(responseID, chain.idx);
				}
			}
		}

		return chains;
	});

	function chainTotalDuration(chain) {
		return chain.logs.reduce((sum, l) => sum + (l.duration_ms || 0), 0);
	}

	function failoverCount(log) {
		return Array.isArray(log?.failovers) ? log.failovers.length : 0;
	}

	function isRecoveredByFailover(log) {
		return !log?.pending && !log?.error && failoverCount(log) > 0;
	}

	function chainStatus(chain) {
		const errors = chain.logs.filter((l) => l.error);
		const recoveredFailovers = chain.logs.reduce((sum, log) => sum + failoverCount(log), 0);
		if (errors.length === 0 && recoveredFailovers > 0) {
			return { isOk: false, text: t("logs.failoverRecovered", { n: recoveredFailovers }) };
		}
		if (errors.length === 0) return { isOk: true, text: t("common.ok") };
		if (errors.length === chain.logs.length) return { isOk: false, text: t("logs.failedAll") };
		return { isOk: false, text: t("logs.failed", { n: errors.length, total: chain.logs.length }) };
	}

	return {
		chainedLogs,
		lastUserPreview,
		sessionTitlePreview,
		chainTotalDuration,
		failoverCount,
		isRecoveredByFailover,
		chainStatus,
		getTimestampMs,
	};
}
