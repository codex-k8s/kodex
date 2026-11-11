import { createHmac, randomBytes } from "node:crypto";

// Image-owned hook: model arguments do not assign the native execution identity.
// Штатный hook из image: аргументы модели не назначают native execution identity.
export default async () => ({
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
      return send(input, { ...init, body: JSON.stringify(body), redirect: "error" });
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
