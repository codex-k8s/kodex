import { createHash } from "node:crypto";
import { boundedResponseBody } from "./runtime-workspace-acceptance.mjs";
import {
  exactOrigin,
  readAuthenticatedState,
  requireSession,
  sessionCookieNames,
  selectedSessionCookies,
  isProxySessionCookie,
  maximumProxyCookies,
  maximumProxyAge,
  proxyCookieName,
  sessionHeaders,
  writeAuthenticatedState,
} from "./owner-session-storage.mjs";

const sessionPath = "/api/v1/session";
const metadataTimeoutMilliseconds = 30000;
const invalidMetadataTime = "metadata time is invalid";
const renewalFailed = "renewal failed";
const timeFields = [
  "serverTime",
  "expiresAt",
  "accessExpiresAt",
  "absoluteExpiresAt",
  "renewAfter",
];
const metadataFields = [
  "generation",
  "version",
  "sessionRevision",
  "renewalMode",
  ...timeFields,
];
const metadataSignal = (signal) =>
  signal
    ? AbortSignal.any([
        signal,
        AbortSignal.timeout(metadataTimeoutMilliseconds),
      ])
    : AbortSignal.timeout(metadataTimeoutMilliseconds);

function metadata(value) {
  requireSession(
    value &&
      Object.keys(value).length === metadataFields.length &&
      Object.keys(value).every((key) => metadataFields.includes(key)),
    "metadata fields are invalid",
  );
  requireSession(
    typeof value.generation === "string" &&
      /^[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12}$/i.test(
        value.generation,
      ) &&
      Number.isSafeInteger(value.version) &&
      value.version > 0 &&
      Number.isSafeInteger(value.sessionRevision) &&
      value.sessionRevision > 0 &&
      ["BACKEND_REFRESH", "REAUTHENTICATION"].includes(value.renewalMode),
    "metadata identity is invalid",
  );
  const times = {};
  for (const field of timeFields) {
    requireSession(
      typeof value[field] === "string" &&
        /^\d{4}-\d{2}-\d{2}T.*Z$/.test(value[field]),
      invalidMetadataTime,
    );
    times[field] = Date.parse(value[field]);
    requireSession(Number.isFinite(times[field]), invalidMetadataTime);
  }
  requireSession(
    times.expiresAt > times.serverTime &&
      times.expiresAt <= times.absoluteExpiresAt &&
      times.renewAfter <= times.expiresAt &&
      times.accessExpiresAt > times.serverTime,
    "metadata deadlines are invalid",
  );
  return { ...value, times };
}

// BFF принимает только полную пару, proxy — полное новое поколение chunks.
// Оба слоя проверяются до изменения state. Значения не попадают в ошибки.
function adoptCookies(response, state, origin, now, required = false) {
  const lines = response.headers.getSetCookie();
  if (!lines.length && !required) return false;
  requireSession(
    lines.length <= 2 + maximumProxyCookies * 2 &&
      lines.every((line) => line.length <= 4608),
    "cookie response is invalid",
  );
  const cookies = lines.map((line) => {
    const parts = line.split(";").map((part) => part.trim());
    const separator = parts[0].indexOf("=");
    const name = parts[0].slice(0, separator);
    const value = parts[0].slice(separator + 1);
    const proxy = isProxySessionCookie(name);
    requireSession(
      separator > 0 && (sessionCookieNames.includes(name) || proxy),
      "cookie name is invalid",
    );
    const attributes = new Map();
    for (const part of parts.slice(1)) {
      const position = part.indexOf("=");
      const key = (position < 0 ? part : part.slice(0, position)).toLowerCase();
      const attribute = position < 0 ? true : part.slice(position + 1);
      requireSession(
        !attributes.has(key) &&
          ["path", "secure", "httponly", "samesite", "max-age"].includes(key),
        "cookie attribute is invalid",
      );
      attributes.set(key, attribute);
    }
    const maxAge = attributes.get("max-age");
    requireSession(
      attributes.get("path") === "/" &&
        attributes.get("secure") === true &&
        attributes.get("samesite") === (proxy ? "Lax" : "Strict") &&
        attributes.has("httponly") ===
          (proxy || name === sessionCookieNames[0]) &&
        (!attributes.has("httponly") || attributes.get("httponly") === true) &&
        typeof maxAge === "string" &&
        (proxy ? /^(?:0|[1-9][0-9]{0,4})$/ : /^[1-9][0-9]{0,3}$/).test(
          maxAge,
        ) &&
        Number(maxAge) <= (proxy ? maximumProxyAge : 3600),
      "cookie attributes are invalid",
    );
    return {
      name,
      value,
      domain: new URL(origin).hostname,
      path: "/",
      secure: true,
      httpOnly: proxy || name === sessionCookieNames[0],
      sameSite: proxy ? "Lax" : "Strict",
      expires: now / 1000 + Number(maxAge),
    };
  });
  requireSession(
    new Set(cookies.map((cookie) => cookie.name)).size === cookies.length,
    "cookie response is ambiguous",
  );
  const bff = cookies.filter((cookie) =>
    sessionCookieNames.includes(cookie.name),
  );
  const proxy = cookies.filter((cookie) => isProxySessionCookie(cookie.name));
  requireSession(
    bff.length === 2 || (!required && bff.length === 0),
    "cookie pair is incomplete",
  );
  for (const cookie of proxy) {
    requireSession(
      cookie.name === proxyCookieName ||
        Array.from(
          { length: maximumProxyCookies },
          (_, index) => `${proxyCookieName}_${index}`,
        ).includes(cookie.name),
      "proxy cookie name is invalid",
    );
    if (cookie.expires * 1000 <= now)
      requireSession(cookie.value === "", "proxy cookie deletion is invalid");
  }
  const activeProxy = proxy.filter((cookie) => cookie.expires * 1000 > now);
  requireSession(
    !proxy.length || activeProxy.length > 0,
    "proxy session requires reauthentication",
  );
  const next = {
    ...state,
    cookies: [
      ...state.cookies.filter(
        (cookie) =>
          !(bff.length && sessionCookieNames.includes(cookie.name)) &&
          !(proxy.length && isProxySessionCookie(cookie.name)),
      ),
      ...bff,
      ...activeProxy,
    ],
  };
  sessionHeaders(next, origin, now);
  state.cookies = next.cookies;
  return true;
}

// Между фазами cookie-bound metadata служит только подсказкой: первый GET всегда
// подтверждает живую сессию. Доступ и refresh token остаются на стороне BFF.
export function createOwnerSessionClient({
  origin,
  storage,
  storagePath,
  fetchAPI = fetch,
  now = Date.now,
}) {
  origin = exactOrigin(origin);
  const state = structuredClone(storage ?? readAuthenticatedState(storagePath));
  sessionHeaders(state, origin, now());
  const cachePath = storagePath ? `${storagePath}.session.json` : undefined;
  const binding = () =>
    createHash("sha256")
      .update(sessionHeaders(state, origin, now()).Cookie)
      .digest("hex");
  let writtenBinding = binding();
  let observation;
  let ready = false;
  let failed = false;
  let queue = Promise.resolve();
  if (cachePath) {
    let cached;
    try {
      cached = readAuthenticatedState(cachePath);
    } catch (error) {
      if (error.code !== "ENOENT") throw error;
    }
    if (cached) {
      requireSession(
        cached.version === 1 &&
          cached.origin === origin &&
          Number.isSafeInteger(cached.observedAt) &&
          cached.observedAt <= now() &&
          /^[a-f0-9]{64}$/.test(cached.binding),
        "cached metadata is invalid",
      );
      const parsed = metadata(cached.metadata);
      if (cached.binding === binding())
        observation = { value: parsed, observedAt: cached.observedAt };
    }
  }

  function serverNow() {
    const elapsed = now() - observation.observedAt;
    requireSession(elapsed >= 0, "local clock moved backwards");
    return observation.value.times.serverTime + elapsed;
  }

  function renewable() {
    return (
      observation &&
      observation.value.renewalMode === "BACKEND_REFRESH" &&
      serverNow() < observation.value.times.expiresAt &&
      serverNow() < observation.value.times.absoluteExpiresAt
    );
  }

  function save() {
    if (!storagePath) return;
    const current = sessionHeaders(
      readAuthenticatedState(storagePath),
      origin,
      now(),
    );
    requireSession(
      createHash("sha256").update(current.Cookie).digest("hex") ===
        writtenBinding,
      "authenticated state changed outside this phase",
    );
    const { times: _times, ...value } = observation.value;
    // Crash между двумя rename даёт mismatch, который запрещает blind PUT.
    writeAuthenticatedState(storagePath, state);
    writtenBinding = binding();
    writeAuthenticatedState(cachePath, {
      version: 1,
      origin,
      binding: binding(),
      observedAt: observation.observedAt,
      metadata: value,
    });
  }

  async function readMetadata(method, signal) {
    const response = await fetchAPI(new URL(sessionPath, origin), {
      method,
      headers: sessionHeaders(state, origin, now()),
      redirect: "error",
      signal,
    });
    if (response.status !== 200) {
      await response.body?.cancel().catch(() => {});
      // ForwardAuth 401 не доказывает истечение access token внутри BFF.
      requireSession(
        response.status !== 401 ||
          !state.cookies.some((cookie) => isProxySessionCookie(cookie.name)) ||
          response.headers.get("content-type")?.split(";")[0].trim() ===
            "application/problem+json",
        "proxy authentication failed",
      );
      return response.status;
    }
    try {
      requireSession(
        response.headers.get("content-type")?.split(";")[0].trim() ===
          "application/json" &&
          response.headers
            .get("cache-control")
            ?.split(",")
            .some((part) => part.trim() === "no-store"),
        "metadata policy is invalid",
      );
      const value = metadata(
        JSON.parse(
          (await boundedResponseBody(response, 16384)).toString("utf8"),
        ),
      );
      requireSession(
        !observation ||
          observation.value.generation !== value.generation ||
          value.version >= observation.value.version,
        "metadata version regressed",
      );
      adoptCookies(response, state, origin, now(), method === "PUT");
      observation = { value, observedAt: now() };
      save();
      return 200;
    } finally {
      await response.body?.cancel().catch(() => {});
    }
  }

  async function prepare(signal) {
    try {
      requireSession(!failed, "client requires a new authenticated session");
      let renewed = false;
      if (!ready) {
        const status = await readMetadata("GET", signal);
        if (status === 401 && renewable()) {
          requireSession(
            (await readMetadata("PUT", signal)) === 200,
            renewalFailed,
          );
          renewed = true;
        } else requireSession(status === 200, "session read failed");
        ready = true;
      }
      const times = observation.value.times;
      requireSession(
        serverNow() < times.expiresAt && serverNow() < times.absoluteExpiresAt,
        "session expired",
      );
      if (
        !renewed &&
        (serverNow() >= times.renewAfter ||
          serverNow() >= times.accessExpiresAt)
      ) {
        requireSession(renewable(), "session requires reauthentication");
        requireSession(
          (await readMetadata("PUT", signal)) === 200,
          renewalFailed,
        );
      }
      requireSession(
        serverNow() < observation.value.times.accessExpiresAt,
        "access expired",
      );
    } catch {
      failed = true;
      throw new Error(
        "Owner session acceptance preflight failed; no automatic renewal retry was performed",
      );
    }
  }

  function request(path, options = {}, observation = false) {
    const task = queue.then(async () => {
      const url = new URL(path, origin);
      requireSession(
        url.origin === origin &&
          !url.username &&
          !url.password &&
          !url.hash &&
          (observation
            ? ["/", sessionPath].includes(url.pathname) && !url.search && (options.method ?? "GET") === "GET"
            : url.pathname.startsWith("/api/v1/") && url.pathname !== sessionPath),
        "request path is invalid",
      );
      const supplied = new Headers(options.headers);
      for (const key of ["authorization", "cookie", "origin", "x-csrf-token"])
        requireSession(
          !supplied.has(key),
          "caller supplied an authentication header",
        );
      const signal = metadataSignal(options.signal);
      await prepare(signal);
      const headers = Object.fromEntries(supplied);
      for (const [key, value] of Object.entries(
        sessionHeaders(state, origin, now()),
      )) {
        if (key === "Accept" && supplied.has("accept")) continue;
        headers[key] = value;
      }
      let response;
      try {
        response = await fetchAPI(url, {
          ...options,
          headers,
          signal: options.signal ?? signal,
          redirect: observation ? "manual" : "error",
        });
      } catch {
        throw new Error(
          "Owner session acceptance request failed; no automatic retry was performed",
        );
      }
      try {
        if (adoptCookies(response, state, origin, now())) {
          requireSession(
            (await readMetadata("GET", metadataSignal(options.signal))) ===
              200,
            "rotated session read failed",
          );
        }
        if (response.status === 401) failed = true;
        return response;
      } catch {
        failed = true;
        await response.body?.cancel().catch(() => {});
        throw new Error("Owner session acceptance cookie readback failed");
      }
    });
    queue = task.catch(() => {});
    return task;
  }

  return {
    // Только проверенные cookies обоих слоёв для продолжения browser acceptance после
    // Node-only запроса. Snapshot не содержит origins/localStorage и не даёт
    // вызывающей стороне менять внутреннее состояние клиента.
    authenticatedCookies() {
      requireSession(ready && !failed, "session snapshot is unavailable");
      sessionHeaders(state, origin, now());
      return structuredClone(selectedSessionCookies(state, origin, now()));
    },
    // Сериализация включает refresh и adoption; бизнес-запрос никогда не
    // повторяется здесь, даже после 401 либо неизвестного результата mutation.
    request,
    // Ограниченный GET monitor использует то же принятие cookies и очередь refresh.
    observe(path, options = {}) { return request(path, options, true); },
  };
}
