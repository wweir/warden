import { serve } from "bun";

const distDir = "./dist";

const baseTime = Date.now();

const mockLogs = [
  {
    request_id: "req-001",
    timestamp: new Date(baseTime - 120000).toISOString(),
    route: "chat/completions",
    model: "gpt-4o",
    provider: "openai",
    duration_ms: 1240,
    fingerprint: "fp-abc123",
    status: "ok",
    request: { messages: [{ role: "user", content: "Explain quantum computing in simple terms" }] },
    response: { choices: [{ message: { content: "Quantum computing uses qubits instead of classical bits. This allows it to perform certain calculations much faster than traditional computers." } }] },
  },
  {
    request_id: "req-002",
    timestamp: new Date(baseTime - 90000).toISOString(),
    route: "chat/completions",
    model: "claude-3-sonnet",
    provider: "anthropic",
    duration_ms: 890,
    fingerprint: "fp-def456",
    status: "ok",
    failovers: [{ from: "openai", to: "anthropic" }],
    request: { messages: [{ role: "user", content: "Write a Python function to sort a list" }] },
    response: { choices: [{ message: { content: "def sort_list(arr): return sorted(arr)" } }] },
  },
  {
    request_id: "req-003",
    timestamp: new Date(baseTime - 60000).toISOString(),
    route: "embeddings",
    model: "text-embedding-3",
    provider: "openai",
    duration_ms: 320,
    fingerprint: "fp-ghi789",
    status: "ok",
    request: { input: "Hello world" },
    response: { data: [{ embedding: [0.1, 0.2, 0.3] }] },
  },
  {
    request_id: "req-004",
    timestamp: new Date(baseTime - 30000).toISOString(),
    route: "chat/completions",
    model: "gpt-4o",
    provider: "openai",
    duration_ms: 2100,
    fingerprint: "fp-jkl012",
    pending: true,
    request: { messages: [{ role: "user", content: "Generate a long story about space exploration" }] },
    response: null,
  },
  {
    request_id: "req-005",
    timestamp: new Date(baseTime - 10000).toISOString(),
    route: "chat/completions",
    model: "gpt-4o-mini",
    provider: "openai",
    duration_ms: 450,
    fingerprint: "fp-mno345",
    error: "rate limit exceeded",
    request: { messages: [{ role: "user", content: "Quick summary of today's news" }] },
  },
];

const streamStoryParts = [
  "In the year 2147, humanity's most ambitious project reached its zenith. ",
  "The starship Aetheria, carrying twelve thousand colonists, ",
  "approached the Kepler-442b system after nearly a century of cryogenic sleep. ",
  "Captain Elena Vasquez was the first to wake, her eyes adjusting to the harsh light of an alien sun. ",
  "The ship's AI, named Prometheus, had already begun surface scans. ",
  "What it found would change everything they knew about life in the universe. ",
  "Beneath the purple methane clouds, vast crystalline structures pulsed with bioluminescent energy. ",
  "They were not natural formations. They were machines, ancient beyond measure, ",
  "waiting for visitors who might understand their purpose. ",
  "The colonists had come seeking a new home. They found something far more profound: ",
  "a message, encoded in the very architecture of the planet, ",
  "left by a civilization that had transcended physical form millions of years ago. ",
  "The story of humanity was about to merge with a much older narrative, ",
  "one written in starlight and stardust across the vast expanse of cosmic time.",
];

let streamingIdx = 0;
let streamLog = { ...mockLogs[3] };

function buildStreamLog() {
  const text = streamStoryParts.slice(0, streamingIdx).join("");
  return {
    ...streamLog,
    pending: streamingIdx < streamStoryParts.length,
    response: streamingIdx >= streamStoryParts.length
      ? { choices: [{ message: { content: text } }] }
      : { choices: [{ message: { content: text, role: "assistant" } }] },
  };
}

function handleSSE(req) {
  const stream = new ReadableStream({
    start(controller) {
      let i = 0;
      const send = () => {
        // Send static logs first
        if (i < mockLogs.length) {
          controller.enqueue(`data: ${JSON.stringify(mockLogs[i])}\n\n`);
          i++;
          setTimeout(send, 400);
          return;
        }

        // Then stream the story progressively
        if (streamingIdx <= streamStoryParts.length) {
          const log = buildStreamLog();
          controller.enqueue(`data: ${JSON.stringify(log)}\n\n`);
          streamingIdx++;
          if (streamingIdx <= streamStoryParts.length) {
            setTimeout(send, 600);
            return;
          }
        }

        // Keep alive
        const interval = setInterval(() => {
          controller.enqueue(`:ping\n\n`);
        }, 15000);
        req.signal.addEventListener("abort", () => clearInterval(interval));
      };
      send();
    },
  });
  return new Response(stream, {
    headers: {
      "Content-Type": "text/event-stream",
      "Cache-Control": "no-cache",
      "Connection": "keep-alive",
    },
  });
}

async function serveStatic(pathname) {
  const filePath = pathname === "/" ? "/index.html" : pathname;
  const file = Bun.file(`${distDir}${filePath}`);
  const exists = await file.exists();
  if (!exists) {
    return new Response("Not found", { status: 404 });
  }
  return new Response(file);
}

async function serveAdmin(req) {
  const url = new URL(req.url);
  const pathname = url.pathname;

  if (pathname === "/_admin/api/logs" || pathname === "/_admin/api/logs/stream") {
    return handleSSE(req);
  }

  const assetPath = pathname.slice("/_admin".length);
  if (assetPath && assetPath !== "/") {
    const file = Bun.file(`${distDir}${assetPath}`);
    if (await file.exists()) {
      return new Response(file);
    }
  }

  return new Response(Bun.file(`${distDir}/index.html`));
}

const server = serve({
  port: 3456,
  async fetch(req) {
    const url = new URL(req.url);
    const pathname = url.pathname;

    if (pathname === "/api/logs") {
      return handleSSE(req);
    }

    if (pathname.startsWith("/_admin/")) {
      return serveAdmin(req);
    }

    return serveStatic(pathname);
  },
});

console.log(`Server running at http://localhost:${server.port}`);
