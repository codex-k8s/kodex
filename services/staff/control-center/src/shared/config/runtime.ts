export interface OidcRuntimeConfig {
  authority: string;
  clientId: string;
  redirectUri: string;
  postLogoutRedirectUri: string;
  scope: string;
}

export interface RuntimeConfig {
  revision: string;
  environment: string;
  apiBaseUrl: string;
  realtimeUrl: string;
  requestTimeoutMs: number;
  oidc: OidcRuntimeConfig;
}

let loadedConfig: Readonly<RuntimeConfig> | undefined;
const retryableRuntimeConfigStatuses = new Set([502, 503, 504]);
const runtimeConfigRetryDelaysMs = [150, 500] as const;

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

function browserOrigin(): URL {
  const parsed = new URL(globalThis.location.origin);
  if (parsed.protocol !== "https:") {
    throw new Error("Runtime browser origin is invalid");
  }
  return parsed;
}

function sameOriginUrl(
  value: string,
  origin: URL,
  websocket: boolean = false,
): string {
  const parsed = new URL(value, origin);
  if (parsed.origin !== origin.origin || parsed.username || parsed.password) {
    throw new Error("Runtime same-origin URL is invalid");
  }
  if (websocket) parsed.protocol = "wss:";
  if (parsed.protocol !== (websocket ? "wss:" : "https:")) {
    throw new Error("Runtime same-origin URL protocol is invalid");
  }
  return parsed.toString().replace(/\/$/, "");
}

function parseConfig(value: unknown): RuntimeConfig {
  if (!isRecord(value) || !isRecord(value.oidc)) {
    throw new Error("Runtime config shape is invalid");
  }
  exactKeys(value, [
    "revision",
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
  const revision = requiredString(value, "revision");
  if (!/^[0-9a-f]{64}$/.test(revision))
    throw new Error("Runtime config revision is invalid");
  if (
    typeof timeout !== "number" ||
    !Number.isInteger(timeout) ||
    timeout < 1_000 ||
    timeout > 60_000
  ) {
    throw new Error("Runtime config request timeout is invalid");
  }
  const origin = browserOrigin();
  const apiBaseUrl = sameOriginUrl(requiredString(value, "apiBaseUrl"), origin);
  const realtimeUrl = sameOriginUrl(
    requiredString(value, "realtimeUrl"),
    origin,
    true,
  );
  const redirectUri = sameOriginUrl(
    requiredString(value.oidc, "redirectUri"),
    origin,
  );
  const postLogoutRedirectUri = sameOriginUrl(
    requiredString(value.oidc, "postLogoutRedirectUri"),
    origin,
  );
  return {
    revision,
    environment: requiredString(value, "environment"),
    apiBaseUrl,
    realtimeUrl,
    requestTimeoutMs: timeout,
    oidc: {
      authority: exactUrl(requiredString(value.oidc, "authority"), ["https:"]),
      clientId: requiredString(value.oidc, "clientId"),
      redirectUri,
      postLogoutRedirectUri,
      scope: requiredString(value.oidc, "scope"),
    },
  };
}

export async function loadRuntimeConfig(): Promise<Readonly<RuntimeConfig>> {
  if (loadedConfig) return loadedConfig;
  for (let attempt = 0; ; attempt += 1) {
    let response: Response;
    try {
      response = await fetch("/config/runtime-config.json", {
        cache: "no-store",
        credentials: "same-origin",
        headers: { Accept: "application/json" },
      });
    } catch (error) {
      if (!(error instanceof TypeError) || !hasRuntimeConfigRetry(attempt))
        throw error;
      await waitBeforeRuntimeConfigRetry(attempt);
      continue;
    }
    if (!response.ok) {
      if (
        retryableRuntimeConfigStatuses.has(response.status) &&
        hasRuntimeConfigRetry(attempt)
      ) {
        await waitBeforeRuntimeConfigRetry(attempt);
        continue;
      }
      throw new Error("Runtime config request failed");
    }
    loadedConfig = Object.freeze(parseConfig(await response.json()));
    return loadedConfig;
  }
}

function hasRuntimeConfigRetry(attempt: number): boolean {
  return attempt < runtimeConfigRetryDelaysMs.length;
}

async function waitBeforeRuntimeConfigRetry(attempt: number): Promise<void> {
  await new Promise<void>((resolve) => {
    globalThis.setTimeout(resolve, runtimeConfigRetryDelaysMs[attempt]);
  });
}

export function runtimeConfig(): Readonly<RuntimeConfig> {
  if (!loadedConfig) throw new Error("Runtime config is not loaded");
  return loadedConfig;
}
