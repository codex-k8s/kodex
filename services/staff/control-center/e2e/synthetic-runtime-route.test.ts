import type { Route } from "@playwright/test";
import { describe, expect, test, vi } from "vitest";

import {
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
