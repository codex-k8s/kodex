import type { Route } from "@playwright/test";
import type { RuntimeConfig } from "../src/shared/config/runtime";

const syntheticOrigin = "https://kodex.test";
const previewOrigin = "http://127.0.0.1:43122";

// Каждый ответ получает собственный объект, чтобы один сценарий не менял соседний.
export function syntheticRuntimeConfig(): RuntimeConfig {
  return {
    revision: "0".repeat(64),
    environment: "synthetic",
    apiBaseUrl: "/",
    realtimeUrl: "/api/v1",
    requestTimeoutMs: 10000,
    oidc: {
      authority: "https://identity.invalid",
      clientId: "synthetic",
      redirectUri: "/auth/callback",
      postLogoutRedirectUri: "/",
      scope: "openid",
    },
  };
}

// Общая граница только синтетических маршрутов; предметные API обрабатывает сценарий.
export async function handleSyntheticRuntimeRequest(
  route: Pick<Route, "abort" | "fulfill">,
  url: URL,
  failures: string[],
): Promise<boolean> {
  if (url.origin !== syntheticOrigin) {
    failures.push("Unexpected origin");
    await route.abort();
    return true;
  }
  if (url.pathname === "/config/runtime-config.json") {
    await route.fulfill({ json: syntheticRuntimeConfig() });
    return true;
  }
  return false;
}

// Preview отдаёт только статические GET из synthetic origin. API остаётся у оснастки сценария.
export async function forwardSyntheticStaticRequest(
  route: Pick<Route, "request" | "fetch" | "fulfill">,
): Promise<void> {
  const request = route.request();
  const original = new URL(request.url());
  if (
    original.origin !== syntheticOrigin ||
    original.username ||
    original.password ||
    request.method() !== "GET" ||
    original.pathname === "/api" ||
    original.pathname.startsWith("/api/") ||
    original.pathname === "/config/runtime-config.json"
  )
    throw new Error("Unexpected static synthetic request");

  const preview = new URL(previewOrigin);
  preview.pathname = original.pathname;
  preview.search = original.search;
  const response = await route.fetch({
    url: preview.href,
    maxRetries: 2,
    maxRedirects: 0,
    timeout: 15_000,
  });
  await route.fulfill({ response });
}
