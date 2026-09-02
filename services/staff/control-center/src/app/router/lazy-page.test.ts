import { describe, expect, it, vi } from "vitest";

import { lazyPage } from "./lazy-page";

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
});
