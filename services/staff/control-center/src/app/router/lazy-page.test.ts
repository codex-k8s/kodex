import { describe, expect, it, vi } from "vitest";

import {
  clearLazyPageRecovery,
  lazyPage,
  recoverLazyPageNavigation,
} from "./lazy-page";

describe("lazyPage", () => {
  it("повторяет временно недоступный dynamic import", async () => {
    vi.useFakeTimers();
    const module = { default: "page" };
    const load = vi
      .fn<() => Promise<typeof module>>()
      .mockRejectedValueOnce(
        new TypeError("Failed to fetch dynamically imported module"),
      )
      .mockResolvedValue(module);

    const result = lazyPage(load)();
    await vi.runAllTimersAsync();

    await expect(result).resolves.toBe(module);
    expect(load).toHaveBeenCalledTimes(2);
    vi.useRealTimers();
  });

  it("переживает длительное обновление сетевого состояния без reload", async () => {
    vi.useFakeTimers();
    const module = { default: "page" };
    const load = vi
      .fn<() => Promise<typeof module>>()
      .mockRejectedValueOnce(
        new TypeError("Failed to fetch dynamically imported module"),
      )
      .mockRejectedValueOnce(
        new TypeError("Failed to fetch dynamically imported module"),
      )
      .mockRejectedValueOnce(
        new TypeError("Failed to fetch dynamically imported module"),
      )
      .mockResolvedValue(module);

    const result = lazyPage(load)();
    await vi.runAllTimersAsync();

    await expect(result).resolves.toBe(module);
    expect(load).toHaveBeenCalledTimes(4);
    vi.useRealTimers();
  });

  it("не скрывает ошибку выполнения модуля", async () => {
    const failure = new Error("module initialization failed");
    const load = vi.fn<() => Promise<never>>().mockRejectedValue(failure);

    await expect(lazyPage(load)()).rejects.toBe(failure);
    expect(load).toHaveBeenCalledOnce();
  });

  it("после исчерпания retry один раз открывает целевой route новым документом", () => {
    const values = new Map<string, string>();
    const storage = {
      getItem: (key: string) => values.get(key) ?? null,
      removeItem: (key: string) => values.delete(key),
      setItem: (key: string, value: string) => values.set(key, value),
    };
    const navigate = vi.fn();
    const error = new TypeError("Failed to fetch dynamically imported module");

    expect(
      recoverLazyPageNavigation(error, "/projects", storage, navigate),
    ).toBe(true);
    expect(navigate).toHaveBeenCalledWith("/projects");
    expect(
      recoverLazyPageNavigation(error, "/projects", storage, navigate),
    ).toBe(false);
    expect(navigate).toHaveBeenCalledOnce();

    clearLazyPageRecovery("/projects", storage);
    expect(
      recoverLazyPageNavigation(error, "/projects", storage, navigate),
    ).toBe(true);
  });

  it("не открывает внешний адрес и не перезагружает ошибку выполнения модуля", () => {
    const storage = {
      getItem: vi.fn(() => null),
      removeItem: vi.fn(),
      setItem: vi.fn(),
    };
    const navigate = vi.fn();

    expect(
      recoverLazyPageNavigation(
        new TypeError("Failed to fetch dynamically imported module"),
        "//attacker.example/collect",
        storage,
        navigate,
      ),
    ).toBe(false);
    expect(
      recoverLazyPageNavigation(
        new Error("module initialization failed"),
        "/",
        storage,
        navigate,
      ),
    ).toBe(false);
    expect(navigate).not.toHaveBeenCalled();
  });
});
