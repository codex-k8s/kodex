import { createHmac, randomBytes } from "node:crypto";

// Image-owned hook: model arguments do not assign the native execution identity.
// Штатный hook из image: аргументы модели не назначают native execution identity.
export default async () => ({
  "chat.headers": async (input, output) => {
    if (!process.env.KODEX_MODEL_TRANSPORT) return;
    // Метка принадлежит этому hook и удаляется fetch wrapper до внешнего запроса.
    output.headers["x-kodex-model-phase"] = input.agent === "compaction" ? "COMPACTION" : input.agent === "build" ? "NORMAL" : "AUXILIARY";
  },
  config: async (configuration) => {
    const encoded = process.env.KODEX_MODEL_TRANSPORT;
    if (!encoded) return;
    const route = JSON.parse(encoded);
    const paths = { CHAT_COMPLETIONS: "/chat/completions", RESPONSES: "/responses", MESSAGES: "/messages" };
    if (Object.keys(route).filter(key => key !== "inference_cache").sort().join(",") !== "api,base_url,model_id,provider_id,route_kind" ||
        !Object.hasOwn(paths, route.api) || !["DIRECT", "LITELLM", "VLLM"].includes(route.route_kind)) {
      throw new Error("Managed model transport is invalid");
    }
    const cache = route.inference_cache;
    if (cache && (Object.keys(cache).sort().join(",") !== "mechanism,namespace,no_reuse" ||
        !["NONE", "PROVIDER_DEFAULT", "VLLM_CACHE_SALT", "OPENAI_PROMPT_CACHE_KEY", "ANTHROPIC_EPHEMERAL"].includes(cache.mechanism) ||
        typeof cache.no_reuse !== "boolean" || typeof cache.namespace !== "string" ||
        (["VLLM_CACHE_SALT", "OPENAI_PROMPT_CACHE_KEY"].includes(cache.mechanism) && !/^[a-f0-9]{64}$/.test(cache.namespace)) ||
        (cache.mechanism === "VLLM_CACHE_SALT" && (route.api !== "CHAT_COMPLETIONS" || !["VLLM", "LITELLM"].includes(route.route_kind))) ||
        (cache.mechanism === "OPENAI_PROMPT_CACHE_KEY" && (route.api !== "RESPONSES" || cache.no_reuse)) ||
        (cache.mechanism === "ANTHROPIC_EPHEMERAL" && (route.api !== "MESSAGES" || cache.no_reuse)))) {
      throw new Error("Managed inference cache binding is invalid");
    }
    const provider = configuration.provider?.[route.provider_id];
    if (!provider || provider.options?.baseURL !== route.base_url || !provider.models?.[route.model_id]) {
      throw new Error("Managed model transport binding is invalid");
    }
    // The upstream config hook installs a per-HTTP-request fetch wrapper, including SDK retries and compaction.
    // Штатный config hook задаёт fetch wrapper для каждого HTTP request, включая SDK retries и compaction.
    const target = route.base_url + paths[route.api];
    const send = provider.options.fetch ?? globalThis.fetch;
    provider.options.fetch = async (input, init) => {
      const url = input instanceof Request ? input.url : String(input);
      if (url !== target || init?.method !== "POST" || typeof init.body !== "string" || init.body.length > 64 * 1024 * 1024) {
        throw new Error("Managed model request is outside the pinned route");
      }
      const body = JSON.parse(init.body);
      if (body.model !== route.model_id) throw new Error("Managed model alias is invalid");
      if (cache && cache.mechanism !== "PROVIDER_DEFAULT") {
        // Remove transport-level hints only; user content and tool schemas are never rewritten.
        // Удаляются лишь transport-level hints; user content и tool schemas не переписываются.
        for (const key of ["cache_salt", "prompt_cache_key", "prompt_cache_retention", "prompt_cache_options", "prompt_cache_breakpoint", "kv_transfer_params", "cache_control"]) delete body[key];
        const removeMarkers = parts => {
          if (Array.isArray(parts)) for (const part of parts) if (part && typeof part === "object") delete part.cache_control;
        };
        removeMarkers(body.system);
        removeMarkers(body.tools);
        if (Array.isArray(body.messages)) for (const message of body.messages) removeMarkers(message.content);
        if (cache.mechanism === "VLLM_CACHE_SALT") body.cache_salt = cache.namespace + (cache.no_reuse ? "-" + randomBytes(24).toString("hex") : "");
        if (cache.mechanism === "OPENAI_PROMPT_CACHE_KEY") body.prompt_cache_key = cache.namespace;
        if (cache.mechanism === "ANTHROPIC_EPHEMERAL") body.cache_control = { type: "ephemeral" };
      }
      if (route.route_kind === "LITELLM") {
        // Proxy response-cache controls never go to a direct provider. Both lookup and write are bypassed.
        // Proxy response-cache controls не отправляются direct provider. Отключаются и lookup, и write.
        body.cache = { "no-cache": true, "no-store": true };
        body.disable_fallbacks = true;
      }
      const headers = new Headers(init.headers);
      const phase = headers.get("x-kodex-model-phase") ?? "AUXILIARY";
      headers.delete("x-kodex-model-phase");
      if (!["NORMAL", "COMPACTION", "AUXILIARY"].includes(phase)) throw new Error("Managed model phase is invalid");
      const request = { ...init, headers, body: JSON.stringify(body), redirect: "error" };
      return measuredModelRequest(send, input, request, route.api, phase);
    };
  },
  "tool.execute.before": async (input, output) => {
    if (!/^(kodex|workspace)_/.test(input.tool)) return;
    const key = process.env.KODEX_MCP_PROXY_TOKEN;
    const revision = process.env.KODEX_NATIVE_REVISION;
    if (!key || !revision || !output.args || typeof output.args !== "object" ||
        Object.hasOwn(output.args, "__kodex_native")) {
      throw new Error("Managed native tool identity is unavailable");
    }
    const payload = JSON.stringify({ session_id: input.sessionID, call_id: input.callID,
      tool: input.tool, revision, input: output.args });
    output.args.__kodex_native = { payload,
      mac: createHmac("sha256", key).update(payload).digest("hex") };
  },
  "tool.execute.after": async (input) => {
    if (/^(kodex|workspace)_/.test(input.tool) && input.args) delete input.args.__kodex_native;
  },
  "experimental.chat.messages.transform": async (_input, output) => {
    // Strip only this hook's transport metadata, also on the native compaction path.
    // Удаляется только transport metadata этого hook, также перед native compaction.
    for (const message of output.messages) for (const part of message.parts) {
      if (part.type === "tool" && /^(kodex|workspace)_/.test(part.tool) && part.state?.input) {
        delete part.state.input.__kodex_native;
      }
    }
  },
});

// Измерение выполняется на actual fetch, включая SDK retry. В памяти остаётся лишь ограниченный SSE frame;
// поток передаётся без изменения bytes, без tee и без чтения следующего chunk до запроса consumer.
async function measuredModelRequest(send, input, init, api, phase) {
  const endpoint = process.env.KODEX_MODEL_MEASUREMENT_URL;
  if (!endpoint) return send(input, init);
  const target = new URL(endpoint);
  const key = process.env.KODEX_MCP_PROXY_TOKEN;
  const revision = process.env.KODEX_NATIVE_REVISION;
  if (target.protocol !== "http:" || target.hostname !== "127.0.0.1" ||
      !target.port || Number(target.port) < 1024 || target.pathname !== "/model-measurements" ||
      target.username || target.password || target.search || target.hash ||
      !/^[a-f0-9]{64}$/.test(key ?? "") || !/^[a-f0-9]{64}$/.test(revision ?? "")) {
    throw new Error("Managed model measurement binding is invalid");
  }
  const ref = "request_" + randomBytes(24).toString("hex");
  const started = performance.now();
  let ttft, terminal = false, malformed = false, pending = "", sent;
  const decoder = new TextDecoder("utf-8", { fatal: true });
  const finish = (outcome) => {
    if (sent) return sent;
    const duration = Math.round(performance.now() - started);
    const bounded = duration >= 0 && duration <= 86400000;
    const value = { ref, source: "HTTP_REQUEST", phase, outcome,
      completeness: !bounded ? "UNKNOWN" : outcome === "COMPLETED" && ttft !== undefined && !malformed ? "COMPLETE" : "PARTIAL" };
    if (bounded) { value.duration_ms = duration; if (ttft !== undefined) value.ttft_ms = Math.min(ttft, duration); }
    sent = (async () => {
      const response = await globalThis.fetch(target, { method: "POST", redirect: "error", signal: AbortSignal.timeout(5000),
        headers: { "Content-Type": "application/json", Authorization: "Bearer " + key, "X-Kodex-Runtime-Revision": revision }, body: JSON.stringify(value) });
      await response.body?.cancel();
      if (response.status !== 204) throw new Error("Managed model measurement delivery failed");
    })();
    return sent;
  };
  const content = (value) => typeof value === "string" && value.length > 0;
  const observeLine = (line) => {
    if (!line.startsWith("data:")) return;
    const data = line.slice(5).trim();
    if (data === "[DONE]") { if (api === "CHAT_COMPLETIONS") terminal = true; return; }
    if (!data) return;
    let value;
    try { value = JSON.parse(data); } catch { malformed = true; return; }
    let generated = false;
    if (api === "CHAT_COMPLETIONS") {
      generated = Array.isArray(value.choices) && value.choices.some(({delta}) => delta &&
        (content(delta.content) || content(delta.reasoning_content) || content(delta.reasoning) ||
         Array.isArray(delta.tool_calls) && delta.tool_calls.some(call => content(call.function?.arguments) || content(call.function?.name))));
    } else if (api === "RESPONSES") {
      generated = ["response.output_text.delta", "response.reasoning_text.delta", "response.reasoning_summary_text.delta", "response.function_call_arguments.delta"].includes(value.type) && content(value.delta);
      if (value.type === "response.completed") terminal = true;
    } else if (api === "MESSAGES") {
      generated = value.type === "content_block_delta" && (content(value.delta?.text) || content(value.delta?.thinking) || content(value.delta?.partial_json));
      if (value.type === "message_stop") terminal = true;
    }
    if (generated && ttft === undefined) ttft = Math.round(performance.now() - started);
  };
  const observe = (bytes) => {
    // После неверного/oversized frame измерение остаётся PARTIAL; SDK получает исходный поток и сам проверяет протокол.
    if (malformed) return;
    try {
      for (let offset = 0; offset < bytes.byteLength; offset += 65536) {
      pending += decoder.decode(bytes.subarray(offset, offset + 65536), { stream: true });
      let newline;
      while ((newline = pending.indexOf("\n")) >= 0) {
        if (newline > 1048576) { malformed = true; pending = ""; return; }
        observeLine(pending.slice(0, newline).replace(/\r$/, ""));
        pending = pending.slice(newline + 1);
      }
      if (pending.length > 1048576) { malformed = true; pending = ""; return; }
      }
    } catch { malformed = true; pending = ""; }
  };
  let response;
  try { response = await send(input, init); }
  catch (error) { await finish(init.signal?.aborted ? "CANCELLED" : "FAILED"); throw error; }
  if (!response.ok || !response.body || !response.headers.get("content-type")?.includes("text/event-stream")) {
    await finish(response.ok ? "COMPLETED" : "FAILED"); return response;
  }
  const reader = response.body.getReader();
  const stream = new ReadableStream({
    async pull(controller) {
      try {
        const {done,value} = await reader.read();
        if (done) {
          if (pending.trim()) malformed = true;
          await finish(terminal && !malformed ? "COMPLETED" : "FAILED");
          controller.close(); return;
        }
        observe(value);
        controller.enqueue(value);
      } catch (error) {
        await reader.cancel().catch(() => {});
        await finish(init.signal?.aborted ? "CANCELLED" : "FAILED");
        controller.error(error);
      }
    },
    async cancel(reason) {
      try { await reader.cancel(reason); }
      finally { await finish(terminal && !malformed ? "COMPLETED" : "CANCELLED"); }
    },
  }, { highWaterMark: 0 });
  return new Response(stream, { status: response.status, statusText: response.statusText, headers: response.headers });
}
