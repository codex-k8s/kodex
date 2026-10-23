import type { Route } from "@playwright/test";
import type { RuntimeConfig } from "../src/shared/config/runtime";

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
  if (url.origin !== "https://kodex.test") {
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
