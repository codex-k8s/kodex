import type { Request, Route } from "@playwright/test";
import { describe, expect, test, vi } from "vitest";

import {
  forwardSyntheticStaticRequest,
  handleSyntheticRuntimeRequest,
  syntheticRuntimeConfig,
} from "./synthetic-runtime-route";

function routeStub() {
  return {
    abort: vi.fn<Route["abort"]>().mockResolvedValue(undefined),
    fulfill: vi.fn<Route["fulfill"]>().mockResolvedValue(undefined),
  };
}

describe("маршрут синтетического runtime-config", () => {
  test("отклоняет чужой origin до выдачи конфигурации", async () => {
    const route = routeStub();
    const failures: string[] = [];
    expect(
      await handleSyntheticRuntimeRequest(
        route,
        new URL("https://other.invalid/config/runtime-config.json"),
        failures,
      ),
    ).toBe(true);
    expect(failures).toEqual(["Unexpected origin"]);
    expect(route.abort).toHaveBeenCalledOnce();
    expect(route.fulfill).not.toHaveBeenCalled();
  });

  test("возвращает конфигурацию только по штатному пути", async () => {
    const route = routeStub();
    const failures: string[] = [];
    expect(
      await handleSyntheticRuntimeRequest(
        route,
        new URL("https://kodex.test/config/runtime-config.json?revision=1"),
        failures,
      ),
    ).toBe(true);
    expect(route.fulfill).toHaveBeenCalledExactlyOnceWith({
      json: syntheticRuntimeConfig(),
    });
    expect(route.abort).not.toHaveBeenCalled();
    expect(failures).toEqual([]);
  });

  test("оставляет предметный API обработчику сценария", async () => {
    const route = routeStub();
    const failures: string[] = [];
    expect(
      await handleSyntheticRuntimeRequest(
        route,
        new URL("https://kodex.test/api/v1/projects"),
        failures,
      ),
    ).toBe(false);
    expect(route.fulfill).not.toHaveBeenCalled();
    expect(route.abort).not.toHaveBeenCalled();
    expect(failures).toEqual([]);
  });

  test("не разделяет изменяемую конфигурацию между сценариями", () => {
    const changed = syntheticRuntimeConfig();
    changed.revision = "a".repeat(64);
    changed.oidc.clientId = "changed";
    const fresh = syntheticRuntimeConfig();
    expect(fresh.revision).toBe("0".repeat(64));
    expect(fresh.oidc.clientId).toBe("synthetic");
    expect(fresh.oidc).not.toBe(changed.oidc);
  });
});

function staticRoute(url: string, method = "GET") {
  const request = { url: () => url, method: () => method } as Request;
  const response = { status: () => 302 } as Awaited<ReturnType<Route["fetch"]>>;
  return {
    ...routeStub(),
    request: () => request,
    fetch: vi.fn<Route["fetch"]>().mockResolvedValue(response),
    response,
  };
}

describe("статический preview браузерных сценариев", () => {
  test("сохраняет путь и запрос, ограничивает повторы и передаёт фактический ответ", async () => {
    const route = staticRoute("https://kodex.test/assets/menu.js?revision=1");
    await forwardSyntheticStaticRequest(route);
    expect(route.fetch).toHaveBeenCalledExactlyOnceWith({
      url: "http://127.0.0.1:43122/assets/menu.js?revision=1",
      maxRetries: 2,
      maxRedirects: 0,
      timeout: 15_000,
    });
    expect(route.fulfill).toHaveBeenCalledExactlyOnceWith({
      response: route.response,
    });
  });

  test.each([
    ["https://different.invalid/assets/menu.js", "GET"],
    ["https://fixture@kodex.test/assets/menu.js", "GET"],
    ["https://kodex.test/api", "GET"],
    ["https://kodex.test/api/v1/runs", "GET"],
    ["https://kodex.test/config/runtime-config.json", "GET"],
    ["https://kodex.test/assets/menu.js", "POST"],
  ])("закрывает запрос %s %s до обращения к preview", async (url, method) => {
    const route = staticRoute(url, method);
    await expect(forwardSyntheticStaticRequest(route)).rejects.toThrow(
      "Unexpected static synthetic request",
    );
    expect(route.fetch).not.toHaveBeenCalled();
    expect(route.fulfill).not.toHaveBeenCalled();
  });
});
