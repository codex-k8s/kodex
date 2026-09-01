import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const validConfig = {
  revision: "a".repeat(64),
  environment: "local",
  apiBaseUrl: "/api/v1",
  realtimeUrl: "/api/v1/realtime",
  requestTimeoutMs: 10_000,
  oidc: {
    authority: "https://sso.kodex.test/realms/kodex",
    clientId: "control-center",
    redirectUri: "/auth/callback",
    postLogoutRedirectUri: "/",
    scope: "openid profile email",
  },
};

describe("runtime config", () => {
  beforeEach(() => {
    vi.resetModules();
    vi.useFakeTimers();
    vi.stubGlobal("location", { origin: "https://control.kodex.test" });
  });

  afterEach(() => {
    vi.useRealTimers();
    vi.unstubAllGlobals();
  });

  it("повторяет краткий network failure и загружает конфигурацию", async () => {
    const fetchMock = vi
      .fn<typeof fetch>()
      .mockRejectedValueOnce(new TypeError("network changed"))
      .mockResolvedValueOnce(jsonResponse(validConfig));
    vi.stubGlobal("fetch", fetchMock);
    const { loadRuntimeConfig } = await import("./runtime");

    const loading = loadRuntimeConfig();
    await vi.advanceTimersByTimeAsync(150);

    await expect(loading).resolves.toMatchObject({ environment: "local" });
    expect(fetchMock).toHaveBeenCalledTimes(2);
  });

  it("повторяет временный gateway failure", async () => {
    const fetchMock = vi
      .fn<typeof fetch>()
      .mockResolvedValueOnce(new Response(null, { status: 503 }))
      .mockResolvedValueOnce(jsonResponse(validConfig));
    vi.stubGlobal("fetch", fetchMock);
    const { loadRuntimeConfig } = await import("./runtime");

    const loading = loadRuntimeConfig();
    await vi.advanceTimersByTimeAsync(150);

    await expect(loading).resolves.toMatchObject({ environment: "local" });
    expect(fetchMock).toHaveBeenCalledTimes(2);
  });

  it("не повторяет невалидную конфигурацию", async () => {
    const fetchMock = vi.fn<typeof fetch>().mockResolvedValue(
      jsonResponse({
        ...validConfig,
        revision: "invalid",
      }),
    );
    vi.stubGlobal("fetch", fetchMock);
    const { loadRuntimeConfig } = await import("./runtime");

    await expect(loadRuntimeConfig()).rejects.toThrow(
      "Runtime config revision is invalid",
    );
    expect(fetchMock).toHaveBeenCalledTimes(1);
  });
});

function jsonResponse(value: unknown): Response {
  return new Response(JSON.stringify(value), {
    status: 200,
    headers: { "Content-Type": "application/json" },
  });
}
