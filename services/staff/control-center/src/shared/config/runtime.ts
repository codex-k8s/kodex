export interface OidcRuntimeConfig {
  authority: string;
  clientId: string;
  redirectUri: string;
  postLogoutRedirectUri: string;
  scope: string;
}

export interface RuntimeConfig {
  environment: string;
  apiBaseUrl: string;
  realtimeUrl: string;
  requestTimeoutMs: number;
  oidc: OidcRuntimeConfig;
}

let loadedConfig: Readonly<RuntimeConfig> | undefined;

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

function requiredString(source: Record<string, unknown>, key: string): string {
  const value = source[key];
  if (typeof value !== "string" || value.trim().length === 0) {
    throw new Error(`Runtime config field ${key} is invalid`);
  }
  return value;
}

function exactKeys(
  source: Record<string, unknown>,
  allowed: readonly string[],
): void {
  if (Object.keys(source).some((key) => !allowed.includes(key)))
    throw new Error("Runtime config contains unknown fields");
}

function exactUrl(value: string, protocols: readonly string[]): string {
  const parsed = new URL(value);
  if (
    !protocols.includes(parsed.protocol) ||
    parsed.username ||
    parsed.password ||
    parsed.search ||
    parsed.hash
  ) {
    throw new Error("Runtime config URL is invalid");
  }
  return parsed.toString().replace(/\/$/, "");
}

function parseConfig(value: unknown): RuntimeConfig {
  if (!isRecord(value) || !isRecord(value.oidc)) {
    throw new Error("Runtime config shape is invalid");
  }
  exactKeys(value, [
    "environment",
    "apiBaseUrl",
    "realtimeUrl",
    "requestTimeoutMs",
    "oidc",
  ]);
  exactKeys(value.oidc, [
    "authority",
    "clientId",
    "redirectUri",
    "postLogoutRedirectUri",
    "scope",
  ]);
  const timeout = value.requestTimeoutMs;
  if (
    typeof timeout !== "number" ||
    !Number.isInteger(timeout) ||
    timeout < 1_000 ||
    timeout > 60_000
  ) {
    throw new Error("Runtime config request timeout is invalid");
  }
  const apiBaseUrl = exactUrl(requiredString(value, "apiBaseUrl"), ["https:"]);
  const realtimeUrl = exactUrl(requiredString(value, "realtimeUrl"), ["wss:"]);
  const redirectUri = exactUrl(requiredString(value.oidc, "redirectUri"), [
    "https:",
  ]);
  if (
    new URL(apiBaseUrl).origin !== new URL(redirectUri).origin ||
    new URL(realtimeUrl).host !== new URL(apiBaseUrl).host
  )
    throw new Error("Runtime API origin is invalid");
  return {
    environment: requiredString(value, "environment"),
    apiBaseUrl,
    realtimeUrl,
    requestTimeoutMs: timeout,
    oidc: {
      authority: exactUrl(requiredString(value.oidc, "authority"), ["https:"]),
      clientId: requiredString(value.oidc, "clientId"),
      redirectUri,
      postLogoutRedirectUri: exactUrl(
        requiredString(value.oidc, "postLogoutRedirectUri"),
        ["https:"],
      ),
      scope: requiredString(value.oidc, "scope"),
    },
  };
}

export async function loadRuntimeConfig(): Promise<Readonly<RuntimeConfig>> {
  if (loadedConfig) return loadedConfig;
  const response = await fetch("/config/runtime-config.json", {
    cache: "no-store",
    credentials: "same-origin",
    headers: { Accept: "application/json" },
  });
  if (!response.ok) throw new Error("Runtime config request failed");
  loadedConfig = Object.freeze(parseConfig(await response.json()));
  return loadedConfig;
}

export function runtimeConfig(): Readonly<RuntimeConfig> {
  if (!loadedConfig) throw new Error("Runtime config is not loaded");
  return loadedConfig;
}
